package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/api"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/demo"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/identity"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

// 模型目录导入的传输层（模型域 §4.6）。
//
// # 为什么是两步而不是一步
//
// 「预览 → 应用」不是界面洁癖，是**上游数据不能直接进库**这一条规矩的执行形式：
// 上游 8890 条里约 1300 条没有 canonical（厂商要人工确认）、904 条上游已弃用、
// 还有 `:free` 这类渠道标记混在展示名里。一步式导入等于把这些判断交给脚本，
// 而脚本没有「这条看着不对」的能力。
//
// 第二步只写 `fields` 里列出的字段——勾选清单是**调用方看过 diff 之后**的产物，
// 不是服务端猜的。这样「上游把 description 改坏了」不会顺手覆盖本地人工写的说明。

// modelImporter 是模型导入链路唯一的鉴权点。
//
// 权限键 `model.import` 与 `entity.edit` 分开：前者管「把上游数据搬进来」，
// 后者管「改一条已有实体」。合成一个键的后果是「只想让人补档案」办不到。
func (h *Handler) modelImporter(ctx context.Context, r *http.Request) (demo.Actor, error) {
	if h.identity == nil || h.queries == nil {
		return demo.Actor{}, errNotWired
	}
	account, err := h.identity.Me(ctx, readSessionCookie(r))
	if err != nil {
		return demo.Actor{}, err
	}
	ok, err := h.queries.HasPermission(ctx, store.HasPermissionParams{
		Sub:           account.Sub,
		PermissionKey: "model.import",
	})
	if err != nil {
		return demo.Actor{}, err
	}
	if !ok {
		return demo.Actor{}, errForbidden
	}
	return demo.Actor{
		AccountID: account.ID,
		UserID:    account.UserID,
		Role:      account.Role,
	}, nil
}

// writeModelError 把模型导入链路的错误映射成状态码。
func (h *Handler) writeModelError(w http.ResponseWriter, err error) {
	status, code, msg := modelErrorParts(err)
	writeError(w, status, code, msg)
}

// modelErrorParts 把导入链路的分支错误摊成「状态码 + 契约错误码 + 人话」。
//
// 单独抽出来是因为批量导入要把同一句话写进逐条结果的 `error` 字段，而不是再写一份
// 映射——两份映射迟早会漂成两种说法。
func modelErrorParts(err error) (int, api.ErrorCode, string) {
	switch {
	case errors.Is(err, identity.ErrUnauthorized):
		return http.StatusUnauthorized, api.Unauthorized, "请先登录"
	case errors.Is(err, errForbidden):
		return http.StatusForbidden, api.Forbidden, "没有模型导入权限"
	case errors.Is(err, errCatalogItemMissing):
		return http.StatusNotFound, api.NotFound, "目录里没有这条上游条目，先刷新目录"
	case errors.Is(err, errCatalogItemClaimed):
		return http.StatusConflict, api.Conflict, "这条上游条目已被另一个模型认领"
	case errors.Is(err, errNoFieldsSelected):
		return http.StatusBadRequest, api.InvalidParam, "一个字段都没勾"
	case errors.Is(err, errUnknownSpecField):
		return http.StatusBadRequest, api.InvalidParam, err.Error()
	default:
		slog.Error("模型导入链路未分类错误", "err", err)
		return http.StatusServiceUnavailable, api.Unavailable, "服务暂时不可用"
	}
}

// modelErrorText 是 modelErrorParts 的人话部分，给批量结果用。
func modelErrorText(err error) string {
	_, _, msg := modelErrorParts(err)
	return msg
}

// 传输层自己的哨兵：这几个判断只在这里用得上，不进领域包。
var (
	errCatalogItemMissing = errors.New("httpapi: 目录里没有这条上游条目")
	errCatalogItemClaimed = errors.New("httpapi: 上游条目已被认领")
	errNoFieldsSelected   = errors.New("httpapi: 一个字段都没勾")
	errUnknownSpecField   = errors.New("httpapi: 未知的档案字段")
)

// specFieldNames 是 `fields` 允许的取值。
//
// 与契约 `ModelImportField.field` 的 enum 逐字一致。写成白名单而不是「有就写」：
// `fields` 直接映射到 SQL 的列名，放任意字符串进来等于把列名交给调用方选。
var specFieldNames = map[string]struct{}{
	"name":            {},
	"description":     {},
	"contextWindow":   {},
	"maxOutput":       {},
	"costIn":          {},
	"costOut":         {},
	"releaseDate":     {},
	"knowledgeCutoff": {},
	"openWeights":     {},
	"modalityIn":      {},
	"modalityOut":     {},
	"license":         {},
}

// ListModelCatalog 查本地镜像里的上游条目。
func (h *Handler) ListModelCatalog(w http.ResponseWriter, r *http.Request, params api.ListModelCatalogParams) {
	if h.queries == nil {
		unavailable(w, errors.New("database not configured"))
		return
	}
	if _, err := h.modelImporter(r.Context(), r); err != nil {
		h.writeModelError(w, err)
		return
	}

	var q pgtype.Text
	if params.Q != nil && strings.TrimSpace(*params.Q) != "" {
		q = pgtype.Text{String: strings.TrimSpace(*params.Q), Valid: true}
	}
	includeDeprecated := false
	if params.IncludeDeprecated != nil {
		includeDeprecated = *params.IncludeDeprecated
	}
	limit := int32(50)
	if params.Limit != nil {
		limit = int32(*params.Limit)
	}

	ctx, cancel := context.WithTimeout(r.Context(), queryTimeout)
	defer cancel()

	rows, err := h.queries.ListCatalogItems(ctx, store.ListCatalogItemsParams{
		Q:                 q,
		IncludeDeprecated: pgtype.Bool{Bool: includeDeprecated, Valid: true},
		RowLimit:          limit,
	})
	if err != nil {
		unavailable(w, err)
		return
	}

	items := make([]api.ModelCatalogItem, 0, len(rows))
	for _, c := range rows {
		items = append(items, api.ModelCatalogItem{
			ProviderId:     c.ProviderID,
			ProviderName:   c.ProviderName,
			ExternalId:     c.ExternalID,
			Name:           c.Name,
			CanonicalId:    c.CanonicalID.String,
			VendorSlug:     c.VendorSlug.String,
			Family:         c.Family.String,
			ModelType:      c.ModelType.String,
			UpstreamStatus: c.UpstreamStatus.String,
			Imported:       c.Imported,
			ContextWindow:  intPtrOf(c.ContextWindow),
			MaxOutput:      intPtrOf(c.MaxOutput),
			CostIn:         floatPtrOf(c.CostIn),
			CostOut:        floatPtrOf(c.CostOut),
			ReleaseDate:    datePtrOf(c.ReleaseDate),
			ModalityIn:     strPtrOf(c.ModalitiesIn),
			ModalityOut:    strPtrOf(c.ModalitiesOut),
			Description:    strPtrOf(c.Description),
		})
	}
	writeJSON(w, http.StatusOK, api.ModelCatalogList{Items: items})
}

func strPtrOf(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func intPtrOf(v pgtype.Int4) *int {
	if !v.Valid {
		return nil
	}
	n := int(v.Int32)
	return &n
}

func floatPtrOf(v pgtype.Float8) *float64 {
	if !v.Valid {
		return nil
	}
	f := v.Float64
	return &f
}

// datePtrOf 把 DATE 摊成 `YYYY-MM-DD` 字符串。
//
// 不给 RFC3339 时间戳：发布日期在库里是日期，不是时刻；带上 00:00:00Z
// 会让前端在某些时区显示成前一天。契约里它的 format 就是 date。
func datePtrOf(v pgtype.Date) *openapi_types.Date {
	if !v.Valid {
		return nil
	}
	return &openapi_types.Date{Time: v.Time}
}

// catalogRef 是 preview 与 apply 共用的「上游条目身份」。
type catalogRef struct {
	Source      string
	ProviderID  string
	ExternalID  string
	CanonicalID string
}

func (c catalogRef) params() (store.GetCatalogItemParams, error) {
	if strings.TrimSpace(c.ProviderID) == "" || strings.TrimSpace(c.ExternalID) == "" {
		return store.GetCatalogItemParams{}, errors.New("providerId 与 externalId 都必填")
	}
	return store.GetCatalogItemParams{
		Source:     c.Source,
		ProviderID: c.ProviderID,
		ExternalID: c.ExternalID,
	}, nil
}

// PreviewModelImport 算出「本地当前值 / 上游值 / 来源 / 锁定 / 能不能勾」。
func (h *Handler) PreviewModelImport(w http.ResponseWriter, r *http.Request) {
	if h.queries == nil {
		unavailable(w, errors.New("database not configured"))
		return
	}
	if _, err := h.modelImporter(r.Context(), r); err != nil {
		h.writeModelError(w, err)
		return
	}

	var body api.ModelImportRef
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, api.InvalidParam, "请求体不是合法的 JSON")
		return
	}
	ref := catalogRef{Source: sourceOrDefault(body.Source), ProviderID: body.ProviderId, ExternalID: body.ExternalId}
	if body.CanonicalId != nil {
		ref.CanonicalID = *body.CanonicalId
	}

	ctx, cancel := context.WithTimeout(r.Context(), queryTimeout)
	defer cancel()

	out, err := h.previewImport(ctx, ref)
	if err != nil {
		h.writeModelError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) previewImport(ctx context.Context, ref catalogRef) (api.ModelImportPreview, error) {
	item, err := h.catalogItem(ctx, ref)
	if err != nil {
		return api.ModelImportPreview{}, err
	}

	target, err := h.diffTarget(ctx, ref)
	if err != nil {
		return api.ModelImportPreview{}, err
	}

	out := api.ModelImportPreview{Mode: "create"}
	if target != nil {
		// 已被认领：对方是 candidate / unverified 时提示可合并，但不是这条路能做的，
		// 所以这里只把「是谁占着」说清楚（模型域 §4.7.4）。
		out.Mode = "complete"
		m := modelOfDetailRow(*target, nil)
		out.Target = &m
	}
	out.Fields = h.diffFields(ctx, target, item)
	return out, nil
}

// catalogItem 取一条镜像行；不存在时报 errCatalogItemMissing（→ 404，提示先刷新目录）。
func (h *Handler) catalogItem(ctx context.Context, ref catalogRef) (store.GetCatalogItemRow, error) {
	params, err := ref.params()
	if err != nil {
		return store.GetCatalogItemRow{}, err
	}
	item, err := h.queries.GetCatalogItem(ctx, params)
	if errors.Is(err, pgx.ErrNoRows) {
		return store.GetCatalogItemRow{}, errCatalogItemMissing
	}
	if err != nil {
		return store.GetCatalogItemRow{}, err
	}
	return item, nil
}

// diffTarget 找这条上游条目认领到的本地模型（没有就是新建）。
func (h *Handler) diffTarget(ctx context.Context, ref catalogRef) (*store.GetModelByIDRow, error) {
	var canonical pgtype.Text
	if ref.CanonicalID != "" {
		canonical = pgtype.Text{String: ref.CanonicalID, Valid: true}
	}
	row, err := h.queries.GetModelDiffTarget(ctx, store.GetModelDiffTargetParams{
		Source:      ref.Source,
		ProviderID:  ref.ProviderID,
		ExternalID:  ref.ExternalID,
		CanonicalID: canonical,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	model, err := h.modelByID(ctx, row)
	if err != nil {
		return nil, err
	}
	if model.Status == "deprecated" {
		// 命中已退役的模型不自动复活（模型域 §4.7.4）：那是合并过的实体，
		// 往里补档案等于给一个「已经并到别人身上」的名字续命。
		return nil, errCatalogItemClaimed
	}
	return &model, nil
}

func (h *Handler) modelByID(ctx context.Context, id int64) (store.GetModelByIDRow, error) {
	return h.queries.GetModelByID(ctx, id)
}

// diffFields 逐字段算 diff。
//
// 三种情况不可勾（模型域 §4.6.5）：值相同（no-op）、`source = 'manual'`、`locked = true`。
// 后两者要显式解锁——接口不代劳，因为「我手工写的说明被同步冲掉」是不可接受的失败。
func (h *Handler) diffFields(ctx context.Context, target *store.GetModelByIDRow, item store.GetCatalogItemRow) []api.ModelImportField {
	locked := map[string]store.ListLockedSpecFieldsRow{}
	current := map[string]string{}
	source := map[string]string{}

	var spec store.ListModelSpecsForFieldsRow
	if target != nil {
		if row, err := h.queries.ListModelSpecsForFields(ctx, target.ID); err == nil {
			spec = row
		} else if !errors.Is(err, pgx.ErrNoRows) {
			slog.Error("读模型档案失败", "err", err, "model", target.ID)
		}
		rows, err := h.queries.ListLockedSpecFields(ctx, target.ID)
		if err != nil {
			slog.Error("读字段来源失败", "err", err, "model", target.ID)
		}
		for _, f := range rows {
			locked[f.Field] = f
		}
		current["name"] = target.Name
		current["description"] = target.Description
		current["contextWindow"] = intText(spec.ContextWindow)
		current["maxOutput"] = intText(spec.MaxOutput)
		current["costIn"] = floatText(spec.CostIn)
		current["costOut"] = floatText(spec.CostOut)
		current["releaseDate"] = dateText(spec.ReleaseDate)
		current["knowledgeCutoff"] = spec.KnowledgeCutoff.String
		current["openWeights"] = intText(spec.OpenWeights)
		current["modalityIn"] = spec.ModalitiesIn
		current["modalityOut"] = spec.ModalitiesOut
		current["license"] = spec.License.String
		for name, f := range locked {
			source[name] = f.Source
		}
	}

	upstream := map[string]string{
		"name":            item.Name,
		"description":     item.Description,
		"contextWindow":   intText(item.ContextWindow),
		"maxOutput":       intText(item.MaxOutput),
		"costIn":          floatText(item.CostIn),
		"costOut":         floatText(item.CostOut),
		"releaseDate":     dateText(item.ReleaseDate),
		"knowledgeCutoff": item.KnowledgeCutoff.String,
		"openWeights":     intText(item.OpenWeights),
		"modalityIn":      item.ModalitiesIn,
		"modalityOut":     item.ModalitiesOut,
		"license":         "",
	}

	// 顺序固定：diff 是给人看的，两次打开顺序不同会让人以为内容变了。
	order := []string{
		"name", "description", "contextWindow", "maxOutput", "costIn", "costOut",
		"releaseDate", "knowledgeCutoff", "openWeights", "modalityIn", "modalityOut", "license",
	}
	out := make([]api.ModelImportField, 0, len(order))
	for _, name := range order {
		cur := current[name]
		up := upstream[name]
		src := source[name]
		if src == "" {
			if cur == "" {
				src = "none"
			} else {
				src = "models.dev"
			}
		}
		isLocked := false
		if f, ok := locked[name]; ok {
			isLocked = f.Locked
		}
		selectable := up != "" && up != cur && src != "manual" && !isLocked
		out = append(out, api.ModelImportField{
			Field:      name,
			Current:    cur,
			Upstream:   up,
			Source:     src,
			Locked:     isLocked,
			Selectable: selectable,
		})
	}
	return out
}

func intText(v pgtype.Int4) string {
	if !v.Valid {
		return ""
	}
	return strconv.Itoa(int(v.Int32))
}

func floatText(v pgtype.Float8) string {
	if !v.Valid {
		return ""
	}
	return strconv.FormatFloat(v.Float64, 'f', -1, 64)
}

func dateText(v pgtype.Date) string {
	if !v.Valid {
		return ""
	}
	return v.Time.Format("2006-01-02")
}

func sourceOrDefault(s *string) string {
	if s == nil || strings.TrimSpace(*s) == "" {
		return "models.dev"
	}
	return strings.TrimSpace(*s)
}
