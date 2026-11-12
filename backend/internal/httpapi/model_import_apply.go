package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/api"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/demo"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

// ApplyModelImport 把勾选的字段落库。
//
// # 为什么只写 `fields` 里列出的
//
// 勾选清单是调用方**看过 diff 之后**的产物。整行覆盖等于说「上游说什么就是什么」，
// 而上游会改名（`:free` 渠道标记）、会丢字段、会把 description 写成营销话术。
// 只写勾了的，人工写的东西就不会被顺手冲掉。
//
// # 为什么新建是 active 而不是 candidate
//
// 作者手填建的是 `candidate`（没人给它背书）；目录导入是管理员对着一条上游条目点的，
// 那是**有人背过书**的。状态不同正是为了让「这条凭什么可信」有答案。
func (h *Handler) ApplyModelImport(w http.ResponseWriter, r *http.Request) {
	if h.queries == nil || h.demos == nil {
		writeError(w, http.StatusServiceUnavailable, api.Unavailable, "服务暂时不可用")
		return
	}
	actor, err := h.modelImporter(r.Context(), r)
	if err != nil {
		h.writeModelError(w, err)
		return
	}

	var body api.ModelImportApply
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, api.InvalidParam, "请求体不是合法的 JSON")
		return
	}

	fields, err := normalizeSpecFields(body.Fields)
	if err != nil {
		h.writeModelError(w, err)
		return
	}

	ref := catalogRef{
		Source:     sourceOrDefault(body.Source),
		ProviderID: body.ProviderId,
		ExternalID: body.ExternalId,
	}
	if body.CanonicalId != nil {
		ref.CanonicalID = *body.CanonicalId
	}
	if _, err := ref.params(); err != nil {
		writeError(w, http.StatusBadRequest, api.InvalidParam, err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), queryTimeout)
	defer cancel()

	model, _, err := h.applyImport(ctx, actor, ref, body, fields)
	if err != nil {
		h.writeModelError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, model)
}

// normalizeSpecFields 校验并去重字段名，同时拒掉「一个都没勾」。
//
// 「一个都没勾」按 400 处理：那多半是调用方漏传了勾选结果，静默成功会让人以为
// 导入已经生效。
func normalizeSpecFields(raw []string) ([]string, error) {
	if len(raw) == 0 {
		return nil, errNoFieldsSelected
	}
	seen := make(map[string]struct{}, len(raw))
	out := make([]string, 0, len(raw))
	for _, f := range raw {
		f = strings.TrimSpace(f)
		if _, ok := specFieldNames[f]; !ok {
			return nil, fmt.Errorf("%w: %s", errUnknownSpecField, f)
		}
		if _, dup := seen[f]; dup {
			continue
		}
		seen[f] = struct{}{}
		out = append(out, f)
	}
	if len(out) == 0 {
		return nil, errNoFieldsSelected
	}
	return out, nil
}

// applyImport 是单条导入的唯一实现：批量版只是循环调用它并收集结果。
//
// 第二个返回值是「这次是新建还是补全」——批量要把它写进逐条结果，单独再查一次
// 会引入竞态（查的时候还没有，写的时候已经有了）。
func (h *Handler) applyImport(
	ctx context.Context,
	actor demo.Actor,
	ref catalogRef,
	body api.ModelImportApply,
	fields []string,
) (api.Model, bool, error) {
	item, err := h.catalogItem(ctx, ref)
	if err != nil {
		return api.Model{}, false, err
	}

	var out api.Model
	created := false
	err = h.demos.InTx(ctx, func(tx pgx.Tx) error {
		q := h.queries.WithTx(tx)

		target, err := h.diffTarget(ctx, ref)
		if err != nil {
			return err
		}

		var modelID int64
		before := map[string]string{}
		if target == nil {
			// 新建：slug 用上游名字生成，撞了就加后缀（创建后不可变，所以一次定死）。
			slug, err := uniqueModelSlug(ctx, q, cleanCatalogName(item.Name))
			if err != nil {
				return err
			}
			name := strings.TrimSpace(derefString(body.Name))
			if name == "" {
				name = cleanCatalogName(item.Name)
			}
			var vendor pgtype.Text
			if body.Vendor != nil && strings.TrimSpace(*body.Vendor) != "" {
				vendor = pgtype.Text{String: strings.TrimSpace(*body.Vendor), Valid: true}
			} else if item.VendorSlug.Valid && item.VendorSlug.String != "" {
				vendor = pgtype.Text{String: item.VendorSlug.String, Valid: true}
			}
			// 新建同样只写勾选的：`description` 没勾就留空，等下一轮导入或人工填。
			// 「反正刚建的行都是空的、顺手把上游说明灌进去」听着无害，实际是
			// 把「我只想建个条目」变成「同时接受了上游的营销话术」。
			desc := ""
			if _, ok := selectedFields(body.Fields)["description"]; ok {
				desc = item.Description
			}
			id, err := q.CreateModelFromCatalog(ctx, store.CreateModelFromCatalogParams{
				Slug:        slug,
				Name:        name,
				Vendor:      vendor,
				Description: desc,
			})
			if err != nil {
				return fmt.Errorf("建模型失败: %w", err)
			}
			modelID = id
			created = true
			before["status"] = ""
		} else {
			modelID = target.ID
			before["status"] = target.Status
			before["name"] = target.Name
		}

		if err := h.writeSelectedSpecs(ctx, q, modelID, item, fields); err != nil {
			return err
		}

		// 认领：让「这条上游条目 ↔ 这个本地模型」成为事实。唯一键保证一条上游条目
		// 只能被一个模型认领，所以重复导入是覆盖而不是新增。
		var canonical pgtype.Text
		if ref.CanonicalID != "" {
			canonical = pgtype.Text{String: ref.CanonicalID, Valid: true}
		}
		if err := q.UpsertModelOffering(ctx, store.UpsertModelOfferingParams{
			ModelID:      modelID,
			Authority:    ref.Source,
			ProviderID:   ref.ProviderID,
			ProviderName: item.ProviderName,
			ExternalID:   ref.ExternalID,
			CanonicalID:  canonical,
			IsOfficial:   ref.ProviderID == vendorPrefix(ref.CanonicalID),
		}); err != nil {
			return fmt.Errorf("记上游引用失败: %w", err)
		}

		after, err := json.Marshal(map[string]any{
			"source":      ref.Source,
			"provider_id": ref.ProviderID,
			"external_id": ref.ExternalID,
			"fields":      fields,
		})
		if err != nil {
			return fmt.Errorf("序列化留痕失败: %w", err)
		}
		beforeJSON, err := json.Marshal(before)
		if err != nil {
			return fmt.Errorf("序列化留痕前态失败: %w", err)
		}
		if err := q.InsertAuditLog(ctx, store.InsertAuditLogParams{
			ActorID:   pgtype.Int8{Int64: actor.AccountID, Valid: true},
			ActorRole: actor.Role,
			Action:    "create",
			EntityID:  modelID,
			Before:    beforeJSON,
			After:     after,
			Reason:    "",
		}); err != nil {
			return fmt.Errorf("写审计失败: %w", err)
		}

		row, err := q.GetModelByID(ctx, modelID)
		if err != nil {
			return fmt.Errorf("回读模型失败: %w", err)
		}
		out = modelOfDetailRow(row, nil)
		return nil
	})
	if err != nil {
		return api.Model{}, false, err
	}
	return out, created, nil
}

// selectedFields 把勾选清单摊成集合。
func selectedFields(fields []string) map[string]struct{} {
	out := make(map[string]struct{}, len(fields))
	for _, f := range fields {
		out[f] = struct{}{}
	}
	return out
}

// writeSelectedSpecs 写「models 上的两列 + model_specs 上的档案列」。
//
// `name` / `description` 落 `models`（它们是公开身份，不是档案）；其余落 `model_specs`。
// 逐字段来源写 `model_spec_fields`：下次同步靠它判断哪些字段可以覆盖。
func (h *Handler) writeSelectedSpecs(
	ctx context.Context,
	q *store.Queries,
	modelID int64,
	item store.GetCatalogItemRow,
	fields []string,
) error {
	selected := make(map[string]struct{}, len(fields))
	for _, f := range fields {
		selected[f] = struct{}{}
	}

	// 先在内存里摊出「字段 → 值」，再挑出模型列与档案列各自要写的东西。
	values := map[string]string{
		"name":            cleanCatalogName(item.Name),
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

	needModelRow := false
	modelName := ""
	modelDesc := ""
	if _, ok := selected["name"]; ok && values["name"] != "" {
		needModelRow = true
		modelName = values["name"]
	}
	if _, ok := selected["description"]; ok && values["description"] != "" {
		needModelRow = true
		modelDesc = values["description"]
	}
	if needModelRow {
		current, err := q.GetModelByID(ctx, modelID)
		if err != nil {
			return fmt.Errorf("读模型失败: %w", err)
		}
		if modelName == "" {
			modelName = current.Name
		}
		if modelDesc == "" {
			modelDesc = current.Description
		}
		if err := q.UpdateModelFromCatalog(ctx, store.UpdateModelFromCatalogParams{
			ID:          modelID,
			Name:        modelName,
			Description: modelDesc,
			Vendor:      pgtype.Text{String: item.VendorSlug.String, Valid: item.VendorSlug.Valid},
		}); err != nil {
			return fmt.Errorf("更新模型失败: %w", err)
		}
	}

	// 档案列（model_specs）：只写被勾的那几个，其余留空由 COALESCE 保住旧值。
	params := store.UpsertModelSpecsParams{ModelID: modelID, Source: "models.dev"}
	touchedSpec := false
	if _, ok := selected["contextWindow"]; ok {
		params.ContextWindow = item.ContextWindow
		touchedSpec = true
	}
	if _, ok := selected["maxOutput"]; ok {
		params.MaxOutput = item.MaxOutput
		touchedSpec = true
	}
	if _, ok := selected["costIn"]; ok {
		params.CostIn = item.CostIn
		touchedSpec = true
	}
	if _, ok := selected["costOut"]; ok {
		params.CostOut = item.CostOut
		touchedSpec = true
	}
	if _, ok := selected["releaseDate"]; ok {
		params.ReleaseDate = item.ReleaseDate
		touchedSpec = true
	}
	if _, ok := selected["knowledgeCutoff"]; ok {
		params.KnowledgeCutoff = item.KnowledgeCutoff
		touchedSpec = true
	}
	if _, ok := selected["openWeights"]; ok {
		params.OpenWeights = item.OpenWeights
		touchedSpec = true
	}
	if _, ok := selected["modalityIn"]; ok {
		params.ModalitiesIn = item.ModalitiesIn
		touchedSpec = true
	}
	if _, ok := selected["modalityOut"]; ok {
		params.ModalitiesOut = item.ModalitiesOut
		touchedSpec = true
	}
	if touchedSpec {
		if err := q.UpsertModelSpecs(ctx, params); err != nil {
			return fmt.Errorf("写模型档案失败: %w", err)
		}
	}

	// 逐字段来源：即使值没变也记一行——下次 diff 要据此判断「这个字段是谁写的」。
	for _, f := range fields {
		if err := q.UpsertSpecField(ctx, store.UpsertSpecFieldParams{
			ModelID: modelID,
			Field:   f,
			Source:  "models.dev",
		}); err != nil {
			return fmt.Errorf("记字段来源失败: %w", err)
		}
	}
	return nil
}

// cleanCatalogName 去掉上游展示名里的渠道标记。
//
// `deepseek-v4-flash:free` 这类后缀是**渠道**信息，不是模型名；留在库里会让
// 「同一个模型」在列表里出现两条看起来不同的记录。
func cleanCatalogName(name string) string {
	name = strings.TrimSpace(name)
	for _, marker := range []string{":free", "(free)", " (free)"} {
		name = strings.TrimSuffix(name, marker)
	}
	return strings.TrimSpace(name)
}

// vendorPrefix 取 canonical 的厂商前缀（`deepseek/deepseek-v4-flash` → `deepseek`）。
func vendorPrefix(canonical string) string {
	if i := strings.Index(canonical, "/"); i > 0 {
		return canonical[:i]
	}
	return ""
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// uniqueModelSlug 生成一个没被占用的 slug。
//
// slug 是公开 URL 的一部分、**创建后不可变**，所以这里必须一次定死。撞了就加数字后缀，
// 试满 20 次仍撞则退到 `model-<随机>`：极端情况也不让导入失败。
func uniqueModelSlug(ctx context.Context, q *store.Queries, name string) (string, error) {
	base := demo.SlugifyForImport(name)
	if base == "" {
		base = "model"
	}
	for i := 1; i <= 20; i++ {
		candidate := base
		if i > 1 {
			candidate = fmt.Sprintf("%s-%d", base, i)
		}
		taken, err := q.ModelSlugTaken(ctx, candidate)
		if err != nil {
			return "", fmt.Errorf("查 slug 失败: %w", err)
		}
		if !taken {
			return candidate, nil
		}
	}
	return "", errors.New("slug 冲突太多次，导入失败")
}
