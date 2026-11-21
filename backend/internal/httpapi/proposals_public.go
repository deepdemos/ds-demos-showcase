package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/api"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/entity"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

// 提案提交与撤回（#524 / 实体域 §4.9）。
//
// # 为什么是白名单，不是「所有 kind 都收」
//
// `proposals.kind` 有 14 个值，但只有一部分是**用户能自填**的：`report` 已作废
// （治理域 §2.2：论坛外置后不建举报队列），`merge_*` / `retag_demo` / `task_match`
// 是管理员或 agent 的活。一次全开等于给站上造一堆没人处理的队列。所以端点通用、
// 白名单放行；放开一个 kind = 往 submittableKinds 加一行 + 补它的 payload 校验。
//
// # 为什么去重只查 pending
//
// 被驳回或撤回之后，同一份内容应该允许重新提交 —— 否则一次手滑就把这个 benchmark
// 永久挡在门外。唯一索引 `uq_proposals_pending_dedupe` 兜住并发；先查一次只是为了
// 在响应里给出原 id（幂等 202，而不是把重复提交当冲突）。

const (
	proposalNameMax        = 128
	proposalPublisherMax   = 64
	proposalDescriptionMax = 2000
	proposalHomepageMax    = 500
	proposalCategoryMax    = 64
)

// submittableKinds 是可被登录用户提交的 kind 白名单。
//
// 放开一个新的 kind 之前先确认两件事：它的 payload 形状收得住（见 normalizeProposalPayload），
// 以及有人会去裁决它（否则提案会一直堆在 pending）。
var submittableKinds = map[string]bool{
	"new_benchmark": true,
}

// benchmarkCategories 是 `benchmarks.category` 的取值域（模型域 §3.7）。
var benchmarkCategories = map[string]bool{
	"coding": true, "math": true, "agent": true, "general": true,
	"multimodal": true, "safety": true, "other": true,
}

// SubmitProposal 对应 POST /api/v1/proposals。
func (h *Handler) SubmitProposal(w http.ResponseWriter, r *http.Request) {
	if h.queries == nil {
		unavailable(w, errors.New("database not configured"))
		return
	}
	account, ctx, cancel, ok := h.requireAccount(w, r)
	if !ok {
		return
	}
	defer cancel()

	var body api.ProposalSubmission
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, api.InvalidParam, err.Error())
		return
	}

	kind := strings.TrimSpace(body.Kind)
	if !submittableKinds[kind] {
		// 422 而不是 400：请求形状是对的，是**这个 kind 还没开放**。
		writeError(w, http.StatusUnprocessableEntity, api.InvalidParam, "这个 kind 尚未开放提交")
		return
	}

	payload, dedupeKey, err := normalizeProposalPayload(kind, body.Payload)
	if err != nil {
		writeError(w, http.StatusBadRequest, api.InvalidParam, err.Error())
		return
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		writeError(w, http.StatusBadRequest, api.InvalidParam, "payload 无法序列化")
		return
	}

	createdBy := pgtype.Int8{Int64: account.UserID, Valid: true}
	dedupe := pgtype.Text{String: dedupeKey, Valid: dedupeKey != ""}
	findParams := store.FindPendingProposalByDedupeParams{Kind: kind, DedupeKey: dedupe}

	if id, err := h.queries.FindPendingProposalByDedupe(ctx, findParams); err == nil {
		writeProposalAccepted(w, id, false)
		return
	} else if !errors.Is(err, pgx.ErrNoRows) {
		unavailable(w, err)
		return
	}

	row, err := h.queries.InsertProposal(ctx, store.InsertProposalParams{
		Kind:      kind,
		Payload:   encoded,
		CreatedBy: createdBy,
		DedupeKey: dedupe,
	})
	if err != nil {
		// 两个人同时提交同一份内容时，唯一索引会先于这里的一次 SELECT 命中。
		// 那不是冲突，是「已经有了」—— 再查一次拿出原 id。
		var pgErr *pgconn.PgError
		// 23505 = unique_violation（与 collections.go / demo/submit.go 同一写法）。
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			if id, findErr := h.queries.FindPendingProposalByDedupe(ctx, findParams); findErr == nil {
				writeProposalAccepted(w, id, false)
				return
			}
		}
		unavailable(w, err)
		return
	}
	writeProposalAccepted(w, row.ID, true)
}

// WithdrawProposal 对应 POST /api/v1/proposals/{id}/withdraw。
func (h *Handler) WithdrawProposal(w http.ResponseWriter, r *http.Request, id int) {
	if h.queries == nil {
		unavailable(w, errors.New("database not configured"))
		return
	}
	account, ctx, cancel, ok := h.requireAccount(w, r)
	if !ok {
		return
	}
	defer cancel()

	if id < 1 {
		writeError(w, http.StatusBadRequest, api.InvalidParam, "提案编号必须是正整数")
		return
	}

	row, err := h.queries.WithdrawProposal(ctx, store.WithdrawProposalParams{
		ID:        int64(id),
		CreatedBy: pgtype.Int8{Int64: account.UserID, Valid: true},
	})
	if err == nil {
		writeJSON(w, http.StatusOK, api.ProposalWithdrawn{
			Id:     int(row.ID),
			Status: api.ProposalWithdrawnStatus("withdrawn"),
		})
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		unavailable(w, err)
		return
	}

	// 0 行有两种原因，必须分开回答：不是你的（404，不承认存在）与已经裁决过（409）。
	existing, getErr := h.queries.GetProposalByID(ctx, int64(id))
	if errors.Is(getErr, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, api.NotFound, "提案不存在")
		return
	}
	if getErr != nil {
		unavailable(w, getErr)
		return
	}
	if !existing.CreatedBy.Valid || existing.CreatedBy.Int64 != account.UserID {
		writeError(w, http.StatusNotFound, api.NotFound, "提案不存在")
		return
	}
	writeError(w, http.StatusConflict, api.Conflict, "提案已经不在待审状态，不能撤回")
}

// writeProposalAccepted 统一 202 的响应体：新建与「已经存在」只差 created 一个布尔。
func writeProposalAccepted(w http.ResponseWriter, id int64, created bool) {
	writeJSON(w, http.StatusAccepted, api.ProposalSubmissionResult{
		Id:      int(id),
		Status:  api.ProposalSubmissionResultStatus("pending"),
		Created: created,
	})
}

// normalizeProposalPayload 按 kind 校验并规整 payload，同时算出去重键。
//
// 与 `ck_proposals_payload` 同源：数据库那条 CHECK 只钉「必须有 name」，报出来的是
// 英文约束名；这里把长度、URL 形状与 category 取值一并收窄，读者才知道该改哪个字段。
func normalizeProposalPayload(kind string, raw map[string]interface{}) (map[string]interface{}, string, error) {
	switch kind {
	case "new_benchmark":
		return normalizeBenchmarkPayload(raw)
	default:
		return nil, "", fmt.Errorf("这个 kind 尚未开放提交")
	}
}

// normalizeBenchmarkPayload 校验 new_benchmark 的形状（#525 的提交侧）。
//
// 只收四类事实：叫什么、谁发布的、在哪能查到、测什么。分数一律不进提案 ——
// 那是站方按白名单源入库的事（#515 决议第 1 条）。
func normalizeBenchmarkPayload(raw map[string]interface{}) (map[string]interface{}, string, error) {
	allowed := map[string]bool{
		"name": true, "publisher": true, "homepage_url": true,
		"description": true, "category": true,
	}
	for key := range raw {
		if !allowed[key] {
			return nil, "", fmt.Errorf("不认识的字段 %q（只接受 name / publisher / homepage_url / description / category）", key)
		}
	}

	name, err := requiredText(raw, "name", proposalNameMax, "名称")
	if err != nil {
		return nil, "", err
	}

	out := map[string]interface{}{"name": name}

	if value, err := optionalText(raw, "publisher", proposalPublisherMax, "发布方"); err != nil {
		return nil, "", err
	} else if value != "" {
		out["publisher"] = value
	}
	if value, err := optionalText(raw, "description", proposalDescriptionMax, "简介"); err != nil {
		return nil, "", err
	} else if value != "" {
		out["description"] = value
	}
	if value, err := optionalURL(raw, "homepage_url", proposalHomepageMax); err != nil {
		return nil, "", err
	} else if value != "" {
		out["homepage_url"] = value
	}
	if value, err := optionalCategory(raw); err != nil {
		return nil, "", err
	} else if value != "" {
		out["category"] = value
	}

	// 去重键：与题目名同一套归一键（实体域 §3.8）。
	// 复用而不是另写一份：两个「差不多」的归一键迟早会给出两种重复判定。
	key := entity.TitleKey(name)
	if key == "" {
		return nil, "", fmt.Errorf("名称去掉符号后为空，换一个写法")
	}
	return out, key, nil
}

// requiredText 取必填字符串并裁到上限。
func requiredText(raw map[string]interface{}, field string, max int, label string) (string, error) {
	value, ok := raw[field]
	if !ok || value == nil {
		return "", fmt.Errorf("%s不能为空", label)
	}
	text, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("%s必须是字符串", label)
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("%s不能为空", label)
	}
	if len([]rune(text)) > max {
		return "", fmt.Errorf("%s最多 %d 字", label, max)
	}
	return text, nil
}

// optionalText 取可选字符串；没给或空串时返回空串（不往 payload 里塞空字段）。
//
// 超长与类型不对都要报错，不能静默丢弃：提交人看到 202，会以为「发布方」已经填上并入库了，
// 而审核员看到的是一条少字段的提案 —— 中间没有任何人知道字段丢了。
func optionalText(raw map[string]interface{}, field string, max int, label string) (string, error) {
	value, exists := raw[field]
	if !exists || value == nil {
		return "", nil
	}
	text, isString := value.(string)
	if !isString {
		return "", fmt.Errorf("%s必须是字符串", label)
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", nil
	}
	if len([]rune(text)) > max {
		return "", fmt.Errorf("%s最多 %d 字", label, max)
	}
	return text, nil
}

// optionalURL 取可选 URL，并只放行 http(s)。
//
// 拒掉 `javascript:` / `data:` 这类不只是洁癖：这个值会被渲染成页面上的链接。
func optionalURL(raw map[string]interface{}, field string, max int) (string, error) {
	value, exists := raw[field]
	if !exists || value == nil {
		return "", nil
	}
	text, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("%s 必须是字符串", field)
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", nil
	}
	if len(text) > max {
		return "", fmt.Errorf("%s 最多 %d 字符", field, max)
	}
	parsed, err := url.Parse(text)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", fmt.Errorf("%s 必须是 http(s) 地址", field)
	}
	return text, nil
}

// optionalCategory 取可选的方向标签；多值用逗号分隔，按固定顺序去重。
//
// 多值是模型域 §3.7 的刻意设计（LiveBench 同时覆盖 coding / math），所以这里
// 不做「只能选一个」的收窄 —— 但每个值都必须在枚举里。
func optionalCategory(raw map[string]interface{}) (string, error) {
	value, exists := raw["category"]
	if !exists || value == nil {
		return "", nil
	}
	text, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("category 必须是字符串")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", nil
	}
	if len(text) > proposalCategoryMax {
		return "", fmt.Errorf("category 最多 %d 字符", proposalCategoryMax)
	}
	seen := map[string]bool{}
	parts := make([]string, 0, 3)
	for _, piece := range strings.Split(text, ",") {
		piece = strings.TrimSpace(piece)
		if piece == "" || seen[piece] {
			continue
		}
		if !benchmarkCategories[piece] {
			return "", fmt.Errorf("category 只能是 coding / math / agent / general / multimodal / safety / other")
		}
		seen[piece] = true
		parts = append(parts, piece)
	}
	return strings.Join(parts, ","), nil
}
