package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/api"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/demo"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/model"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

// 手动新建模型（模型域 §4.7.3）。
//
// # 为什么直接 active，不给候选选项
//
// 这个动作没有任何上游依据：名字是人填的，点下去就是**本站背书**。候选
// （candidate）的语义是「有人提了、等核实」——那是提案那条路。把两种「谁背书」
// 塞进一个下拉，等于让操作者自己决定这句话算不算数；而这两个状态在读取侧
// 完全一样（都进目录、都能被声明），选错了没有任何人会发现。
//
// # 为什么分辨率固定 exact
//
// family / unknown / guess 是系统维护的兜底实体（§4.7.5）：它们表示「说不清具体
// 是哪个型号」。人工建一条 exact 是「我确定它是这个」，建一条 unknown 毫无意义
// ——真要表达说不清，走兜底实体那条由系统维护的路径。
//
// # 为什么撞名返回 409 而不是自动加后缀
//
// slug 进 URL 且创建后不可变。自动加后缀（deepseek-chat-2）会造出一个永久难看的
// 展示名，而管理员真正想的多半是「补全那一条」。所以 409 里带上已有那条的 slug，
// 界面据此给「去那条补全」的入口。

// modelConflict 是「名字或 slug 已被占用」，带着已有那条的行。
//
// 单独一个类型而不是哨兵错误：409 的响应体要带 existingSlug，那是数据不是文案，
// 塞进 message 就只能靠调用方抠字符串。
type modelConflict struct {
	existing store.FindModelByNameOrSlugRow
	sameName bool
}

func (e *modelConflict) Error() string {
	return "模型已存在：" + e.existing.Slug
}

// modelConflictMessage 是这条冲突的人话。
//
// 两条写路径都要这句话：手动新建写进 409 的 message，提案批准写进裁决失败的文案。
// 各写一遍会出现「同一个冲突两种说法」，而这两种说法正是操作者判断「是不是该去
// 已有那条补全、还是干脆换名字」的唯一依据。
func modelConflictMessage(conflict *modelConflict) string {
	slug := conflict.existing.Slug
	if conflict.sameName {
		return "已经有同名模型「" + conflict.existing.Name + "」（/models/" + slug + "）。要补全它请走导入；确实要建另一条，请换个名字"
	}
	return "slug「" + slug + "」已经被「" + conflict.existing.Name + "」占用（/models/" + slug + "），换个 slug 或直接用那条"
}

// writeModelConflict 写 409：给已有那条的 slug，让界面能跳过去。
func (h *Handler) writeModelConflict(w http.ResponseWriter, conflict *modelConflict) {
	slug := conflict.existing.Slug
	writeJSON(w, http.StatusConflict, api.Error{
		Code:         api.Conflict,
		Message:      modelConflictMessage(conflict),
		ExistingSlug: &slug,
	})
}

// modelFieldViolation 把库层的输入形状错误翻成 400 / 409。
//
// 长度与唯一性都交给库判（ck_models_slug_len / ck_models_name_len /
// ck_models_vendor_len / 两个 UNIQUE）：上限写在 DDL 里，在 Go 里再抄一份迟早
// 与库分叉——分叉的表现是「接口说可以、落库报 500」。
func modelFieldViolation(err error) (int, api.ErrorCode, string, bool) {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return 0, "", "", false
	}
	switch pgErr.Code {
	case "23505":
		return http.StatusConflict, api.Conflict, "同名或同 slug 的模型已经存在", true
	case "23514":
		switch pgErr.ConstraintName {
		case "ck_models_name_len":
			return http.StatusBadRequest, api.InvalidParam, "模型名要 1–128 个字", true
		case "ck_models_slug_len":
			return http.StatusBadRequest, api.InvalidParam, "slug 要 1–128 个字符", true
		case "ck_models_vendor_len":
			return http.StatusBadRequest, api.InvalidParam, "厂商名太长（上限 64 个字符）", true
		}
		return http.StatusBadRequest, api.InvalidParam, "输入超出了模型字段的长度上限", true
	}
	return 0, "", "", false
}

// CreateModel 对应 POST /api/v1/admin/models。
func (h *Handler) CreateModel(w http.ResponseWriter, r *http.Request) {
	if h.pool == nil {
		writeError(w, http.StatusServiceUnavailable, api.Unavailable, "服务暂时不可用")
		return
	}
	actor, err := h.requirePermission(r.Context(), r, permModelCreate)
	if err != nil {
		h.writePermissionError(w, err, permModelCreate)
		return
	}

	var body api.ModelManualCreate
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, api.InvalidParam, "请求体不是合法的 JSON")
		return
	}

	name := strings.TrimSpace(body.Name)
	if name == "" {
		writeError(w, http.StatusBadRequest, api.InvalidParam, "模型名不能为空")
		return
	}
	// 兜底名由系统维护：手填一条 unspecified 会把「说不清用了什么」改写成
	// 「本站确认它是这个型号」，之后所有引用都指向它，真实型号反而查不出来。
	if model.FallbackModelName(name) {
		writeError(w, http.StatusBadRequest, api.InvalidParam,
			"「"+name+"」是系统维护的兜底名（表示说不清具体型号），不能手动新建；要收录真实型号请走导入")
		return
	}

	// 显式给空串与不给等价：表单里清空 slug 输入框，不该变成「提交一个空 slug」。
	// 具体的值在事务里才算：厂商可能是实体（`vendorId`），只有先解析出厂商名，
	// 才能算出与「表单里看到的建议值」一致的那个值。
	slug := strings.TrimSpace(derefString(body.Slug))

	ctx, cancel := context.WithTimeout(r.Context(), queryTimeout)
	defer cancel()

	created, err := h.insertManualModel(ctx, actor, name, slug, body)
	if err != nil {
		// 厂商输入的两条形状错误先摊开：它们与「模型撞名」不是同一件事，
		// 混进同一个 409 / 400 文案会让人不知道要改哪一处。
		switch {
		case errors.Is(err, errVendorAmbiguous):
			writeError(w, http.StatusBadRequest, api.InvalidParam,
				"vendorId 与 vendor 只能给一个：选已有厂商，或者新建一家")
			return
		case errors.Is(err, errVendorNotFound):
			writeError(w, http.StatusBadRequest, api.InvalidParam, "vendorId 指向的厂商不存在")
			return
		}
		var vendorConflict *vendorSlugConflict
		if errors.As(err, &vendorConflict) {
			writeVendorConflict(w, vendorConflict)
			return
		}
		var conflict *modelConflict
		if errors.As(err, &conflict) {
			h.writeModelConflict(w, conflict)
			return
		}
		if status, code, msg, ok := modelFieldViolation(err); ok {
			// 唯一键在「查重」与「插入」之间被抢走（另一个管理员同时提交）：
			// 补一次查询，让 409 仍然带得上 existingSlug，而不是退化成一句没有出路的文案。
			if status == http.StatusConflict {
				row, lookErr := h.queries.FindModelByNameOrSlug(ctx, store.FindModelByNameOrSlugParams{
					Name: name,
					Slug: slug,
				})
				if lookErr == nil {
					h.writeModelConflict(w, &modelConflict{existing: row, sameName: row.Name == name})
					return
				}
			}
			writeError(w, status, code, msg)
			return
		}
		unavailable(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

// modelCreate 是「建一条模型」的全部输入。
//
// Status 是唯一随写路径变化的字段：手动新建是 active（管理员点下去就是背书），
// 收录提案批准是 candidate（有人提了、等核实）。其余字段两条路完全一致——
// `VendorID` 只有手动新建会填（#588：选已有厂商或就地新建），提案批准那边只写文本列。
type modelCreate struct {
	Name     string
	Slug     string
	Vendor   string
	VendorID pgtype.Int8
	Status   string
}

// createModelInTx 在调用方已开启的事务里「查重 → 建行 → 写 create 审计」，返回新行 id。
//
// 为什么查重与审计必须和建行写在同一处：它们决定的不是「这一行怎么填」，而是
// 「这算不算一条新建的模型」——查重漏了会建出两条同名行，审计漏了会留下一条
// 没有来源的模型。两条写路径对这两件事的要求逐字相同，只有状态不同。
func (h *Handler) createModelInTx(
	ctx context.Context,
	q *store.Queries,
	actor demo.Actor,
	in modelCreate,
) (int64, error) {
	existing, err := q.FindModelByNameOrSlug(ctx, store.FindModelByNameOrSlugParams{
		Name: in.Name,
		Slug: in.Slug,
	})
	switch {
	case err == nil:
		return 0, &modelConflict{existing: existing, sameName: existing.Name == in.Name}
	case !errors.Is(err, pgx.ErrNoRows):
		return 0, err
	}

	var vendorColumn pgtype.Text
	if in.Vendor != "" {
		vendorColumn = pgtype.Text{String: in.Vendor, Valid: true}
	}

	// 复用导入路径那条 INSERT：slug / name / vendor / vendor_id / status / resolution=exact
	// 的形状三条路一致，再开一条 INSERT INTO models 就是第二个写入点。
	id, err := q.CreateModelFromCatalog(ctx, store.CreateModelFromCatalogParams{
		Slug:        in.Slug,
		Name:        in.Name,
		Vendor:      vendorColumn,
		VendorID:    in.VendorID,
		Status:      in.Status,
		Description: "",
	})
	if err != nil {
		return 0, err
	}

	before, err := json.Marshal(map[string]string{"status": ""})
	if err != nil {
		return 0, err
	}
	// after 只留「这条凭什么可信」用得上的字段：状态、名称精确度、标识与厂商。
	// 不存整行——审计日志会跟着变胖，而真正要回答的是「谁在什么时候把它变成了什么」。
	//
	// `vendor_id` 只在真的挂了厂商时出现：没有这个键 = 厂商未标注（#588）。
	// 写成一个空值反而会让人以为「挂了一个 id 为空的厂商」。
	afterFields := map[string]any{
		"slug":       in.Slug,
		"name":       in.Name,
		"vendor":     in.Vendor,
		"status":     in.Status,
		"resolution": "exact",
	}
	if in.VendorID.Valid {
		afterFields["vendor_id"] = in.VendorID.Int64
	}
	after, err := json.Marshal(afterFields)
	if err != nil {
		return 0, err
	}
	if err := q.InsertAuditLog(ctx, store.InsertAuditLogParams{
		ActorID:    pgtype.Int8{Int64: actor.AccountID, Valid: true},
		ActorRole:  actor.Role,
		Action:     "create",
		EntityType: "model",
		EntityID:   id,
		Before:     before,
		After:      after,
		Reason:     "",
	}); err != nil {
		return 0, err
	}
	return id, nil
}

// insertManualModel 在一个事务里「查重 → 建行 → 写审计 → 回读」。
//
// 四步必须在同一个事务里：审计与实体是一条账，分开写会出现「有模型没留痕」或
// 「有留痕没模型」——两者都只能靠人肉猜。
func (h *Handler) insertManualModel(
	ctx context.Context,
	actor demo.Actor,
	name, slug string,
	body api.ModelManualCreate,
) (api.Model, error) {
	tx, err := h.pool.Begin(ctx)
	if err != nil {
		return api.Model{}, err
	}
	// 提交成功后再 Rollback 是无害的 no-op；漏掉它才是泄漏连接的形状。
	defer func() { _ = tx.Rollback(ctx) }()

	q := h.queries.WithTx(tx)

	// 厂商先定（#588）：就地新建要插一行，而那一行必须与模型行同生共死——模型撞名
	// 回滚时不能留下一条没有任何模型指向的厂商。
	vendorRef, err := h.resolveModelVendor(ctx, q, body)
	if err != nil {
		return api.Model{}, err
	}
	if slug == "" {
		// 建议值就是这里写下的值（同一个纯函数、同一份输入：展示名 + 厂商名）。
		// 厂商名取**实体**的，而不是请求里那个可能没给的文本——否则「选了已有厂商」
		// 这条路的预填值与实际写入值会分叉。
		slug = model.Slug(name, vendorRef.Name)
	}

	// 查重 / 建行 / 写 create 审计仍是 #587 那一处（createModelInTx）：提案批准走
	// 同一个函数，只差状态这一列。厂商两个字段一起给——`vendor_id` 是权威，
	// 文本列是过渡期的展示缓存（同值）。
	id, err := h.createModelInTx(ctx, q, actor, modelCreate{
		Name:     name,
		Slug:     slug,
		Vendor:   vendorRef.Name,
		VendorID: vendorRef.ID,
		// 手动新建由管理员点下去，是背书；候选那条语义属于提案批准。
		Status: "active",
	})
	if err != nil {
		return api.Model{}, err
	}

	row, err := q.GetModelByID(ctx, id)
	if err != nil {
		return api.Model{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return api.Model{}, err
	}
	return modelOfDetailRow(row, nil), nil
}

// SuggestModelSlug 对应 GET /api/v1/admin/models/slug-suggestion。
//
// 只跑算法、不查唯一性：同一个输入永远得到同一个值，撞了由新建端点回 409。
// 在建议这一步加 -2 后缀，等于让表单里显示一个「提交后会变」的地址——
// 而 slug 一旦写进 URL 就不可变，用户看到的和拿到的是两回事。
//
// 单独一个端点而不是让前端算：slug 规则是公开 URL 的一部分，两处实现迟早分叉，
// 分叉的表现正是「表单里看到的地址和实际写下地址不一样」。
func (h *Handler) SuggestModelSlug(w http.ResponseWriter, r *http.Request, params api.SuggestModelSlugParams) {
	if _, err := h.requirePermission(r.Context(), r, permModelCreate); err != nil {
		h.writePermissionError(w, err, permModelCreate)
		return
	}
	name := strings.TrimSpace(params.Name)
	if name == "" {
		writeError(w, http.StatusBadRequest, api.InvalidParam, "name 不能为空")
		return
	}
	vendor := strings.TrimSpace(derefString(params.Vendor))
	writeJSON(w, http.StatusOK, api.ModelSlugSuggestion{Slug: model.Slug(name, vendor)})
}
