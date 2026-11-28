package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/api"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/demo"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/entity"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

// 提案审核（#433 / ENT-016 – ENT-024）。
//
// 这一轮只做「看得清 + 能驳回」：拒绝只改 proposals 并写审计，不产生任何正式数据
// （ENT-023）；approve 需要按 kind 的 execute 矩阵，未接入前明确 422，绝不假装成功。

const (
	proposalPageSizeDefault = 20
	proposalPageSizeMax     = 100
)

var proposalStatuses = map[string]bool{
	"": true, "pending": true, "approved": true, "rejected": true,
	"withdrawn": true, "superseded": true,
}

var proposalSources = map[string]bool{
	"": true, "user": true, "admin": true, "ai": true,
	"inferred": true, "external": true, "imported": true,
}

func (h *Handler) proposalReviewer(ctx context.Context, r *http.Request) (demo.Actor, error) {
	if h.identity == nil || h.queries == nil {
		return demo.Actor{}, errNotWired
	}
	account, err := h.identity.Me(ctx, readSessionCookie(r))
	if err != nil {
		return demo.Actor{}, err
	}
	ok, err := h.queries.HasPermission(ctx, store.HasPermissionParams{
		Sub:           account.Sub,
		PermissionKey: "proposal.review",
	})
	if err != nil {
		return demo.Actor{}, err
	}
	if !ok {
		return demo.Actor{}, errForbidden
	}
	return demo.Actor{AccountID: account.ID, UserID: account.UserID, Role: account.Role}, nil
}

func proposalPayloadMap(payload []byte) map[string]any {
	if len(payload) == 0 {
		return map[string]any{}
	}
	var data map[string]any
	if json.Unmarshal(payload, &data) != nil {
		return map[string]any{}
	}
	return data
}

func proposalTitle(payload []byte) string {
	for _, key := range []string{"title", "name", "slug"} {
		if value, ok := proposalPayloadMap(payload)[key].(string); ok && value != "" {
			return value
		}
	}
	return ""
}

// ListAdminProposals 对应 GET /api/v1/admin/proposals。
func (h *Handler) ListAdminProposals(w http.ResponseWriter, r *http.Request, params api.ListAdminProposalsParams) {
	if h.queries == nil || h.identity == nil {
		writeError(w, http.StatusServiceUnavailable, api.Unavailable, "服务暂时不可用")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), queryTimeout)
	defer cancel()

	if _, err := h.proposalReviewer(ctx, r); err != nil {
		h.writeReviewError(w, err)
		return
	}

	page := 1
	if params.Page != nil {
		page = *params.Page
	}
	pageSize := proposalPageSizeDefault
	if params.PageSize != nil {
		pageSize = *params.PageSize
	}
	if page < 1 || pageSize < 1 || pageSize > proposalPageSizeMax {
		writeError(w, http.StatusBadRequest, api.InvalidParam, "page 必须 ≥ 1，pageSize 必须在 1–100")
		return
	}

	status := "pending"
	if params.Status != nil {
		status = string(*params.Status)
	}
	kind := ""
	if params.Kind != nil {
		kind = strings.TrimSpace(*params.Kind)
	}
	source := ""
	if params.Source != nil {
		source = string(*params.Source)
	}
	if !proposalStatuses[status] || !proposalSources[source] {
		writeError(w, http.StatusBadRequest, api.InvalidParam, "status / source 取值不合法")
		return
	}

	total, err := h.queries.CountProposalsForAdmin(ctx, store.CountProposalsForAdminParams{
		Status: status, Kind: kind, Source: source,
	})
	if err != nil {
		unavailable(w, err)
		return
	}
	rows, err := h.queries.ListProposalsForAdmin(ctx, store.ListProposalsForAdminParams{
		Status: status, Kind: kind, Source: source,
		RowLimit: int32(pageSize), RowOffset: int32((page - 1) * pageSize),
	})
	if err != nil {
		unavailable(w, err)
		return
	}

	items := make([]api.ProposalSummary, 0, len(rows))
	for _, row := range rows {
		items = append(items, api.ProposalSummary{
			Id:        int(row.ID),
			Kind:      row.Kind,
			Status:    api.ProposalSummaryStatus(row.Status),
			Source:    api.ProposalSummarySource(row.Source),
			Title:     proposalTitle(row.Payload),
			CreatedAt: row.CreatedAt.Time,
			UpdatedAt: row.UpdatedAt.Time,
		})
	}
	writeJSON(w, http.StatusOK, api.ProposalList{
		Items: items, Total: int(total), Page: page, PageSize: pageSize,
	})
}

func proposalDetail(row store.GetProposalByIDRow) api.ProposalDetail {
	payload := proposalPayloadMap(row.Payload)
	out := api.ProposalDetail{
		Id:        int(row.ID),
		Kind:      row.Kind,
		Status:    api.ProposalDetailStatus(row.Status),
		Payload:   &payload,
		Source:    api.ProposalDetailSource(row.Source),
		CreatedAt: row.CreatedAt.Time,
		UpdatedAt: row.UpdatedAt.Time,
	}
	if row.Confidence.Valid {
		value := row.Confidence.Float32
		out.Confidence = &value
	}
	if row.CreatedBy.Valid {
		id := int(row.CreatedBy.Int64)
		out.CreatedBy = &id
	}
	if row.ReviewedBy.Valid {
		id := int(row.ReviewedBy.Int64)
		out.ReviewedBy = &id
	}
	if row.ReviewedAt.Valid {
		at := row.ReviewedAt.Time
		out.ReviewedAt = &at
	}
	if row.RejectReason.Valid {
		reason := row.RejectReason.String
		out.RejectReason = &reason
	}
	if row.ResultKind.Valid {
		kind := row.ResultKind.String
		out.ResultKind = &kind
	}
	if row.ResultID.Valid {
		id := int(row.ResultID.Int64)
		out.ResultId = &id
	}
	return out
}

// GetAdminProposal 对应 GET /api/v1/admin/proposals/{id}。
func (h *Handler) GetAdminProposal(w http.ResponseWriter, r *http.Request, id int) {
	if h.queries == nil || h.identity == nil {
		writeError(w, http.StatusServiceUnavailable, api.Unavailable, "服务暂时不可用")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), queryTimeout)
	defer cancel()

	if _, err := h.proposalReviewer(ctx, r); err != nil {
		h.writeReviewError(w, err)
		return
	}

	row, err := h.queries.GetProposalByID(ctx, int64(id))
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, api.NotFound, "提案不存在")
		return
	}
	if err != nil {
		unavailable(w, err)
		return
	}
	writeJSON(w, http.StatusOK, proposalDetail(row))
}

// proposalFailure 是提案裁决的失败态：带上 HTTP 状态与机器码，
// 单条入口直接写响应，批量入口把 message 塞进对应行。
type proposalFailure struct {
	status  int
	code    api.ErrorCode
	message string
}

func (f *proposalFailure) Error() string { return f.message }

func proposalFail(status int, code api.ErrorCode, message string) *proposalFailure {
	return &proposalFailure{status: status, code: code, message: message}
}

// reviewProposalOne 是单条与批量共用的裁决核心。
func (h *Handler) reviewProposalOne(ctx context.Context, actor demo.Actor, id int64, decision api.ProposalDecisionDecision, reason *string) (api.ProposalReviewResult, *proposalFailure) {
	row, err := h.queries.GetProposalByID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return api.ProposalReviewResult{}, proposalFail(http.StatusNotFound, api.NotFound, "提案不存在")
	}
	if err != nil {
		return api.ProposalReviewResult{}, proposalFail(http.StatusServiceUnavailable, api.Unavailable, "服务暂时不可用")
	}
	if row.Status != "pending" {
		return api.ProposalReviewResult{}, proposalFail(http.StatusConflict, api.Conflict, "提案已经处理过了")
	}

	switch decision {
	case api.ProposalDecisionDecisionReject:
		text := ""
		if reason != nil {
			text = strings.TrimSpace(*reason)
		}
		if len([]rune(text)) > 500 {
			return api.ProposalReviewResult{}, proposalFail(http.StatusUnprocessableEntity, api.InvalidParam, "驳回原因最多 500 个字符")
		}
		rejected, err := h.queries.RejectProposal(ctx, store.RejectProposalParams{
			ID:           id,
			ReviewedBy:   pgtype.Int8{Int64: actor.UserID, Valid: actor.UserID != 0},
			RejectReason: pgtype.Text{String: text, Valid: text != ""},
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return api.ProposalReviewResult{}, proposalFail(http.StatusConflict, api.Conflict, "提案已经处理过了")
		}
		if err != nil {
			return api.ProposalReviewResult{}, proposalFail(http.StatusServiceUnavailable, api.Unavailable, "服务暂时不可用")
		}
		h.auditProposalReview(ctx, actor, id, row.Status, "rejected", "", 0, text)
		return api.ProposalReviewResult{Id: int(rejected.ID), Status: api.ProposalReviewResultStatusRejected}, nil

	case api.ProposalDecisionDecisionApprove:
		switch row.Kind {
		case "new_task":
			payload := proposalPayloadMap(row.Payload)
			title, _ := payload["title"].(string)
			brief, _ := payload["brief"].(string)
			category, _ := payload["category"].(string)
			title = strings.TrimSpace(title)
			if title == "" {
				return api.ProposalReviewResult{}, proposalFail(http.StatusUnprocessableEntity, api.InvalidParam, "new_task 的标题不能为空")
			}
			titleKey := entity.TitleKey(title)
			approved, err := h.queries.ApproveNewTaskProposal(ctx, store.ApproveNewTaskProposalParams{
				ID:         id,
				TitleKey:   titleKey,
				Slug:       taskSlug(title),
				Title:      title,
				TitleNorm:  entity.TitleNorm(title),
				Brief:      strings.TrimSpace(brief),
				Category:   pgtype.Text{String: strings.TrimSpace(category), Valid: strings.TrimSpace(category) != ""},
				CreatedBy:  pgtype.Int8{Int64: actor.UserID, Valid: actor.UserID != 0},
				ReviewedBy: pgtype.Int8{Int64: actor.UserID, Valid: actor.UserID != 0},
			})
			if err != nil {
				if isUniqueViolation(err) {
					return api.ProposalReviewResult{}, proposalFail(http.StatusConflict, api.Conflict, "同名题目刚被另一条提案创建，请重试")
				}
				return api.ProposalReviewResult{}, proposalFail(http.StatusServiceUnavailable, api.Unavailable, "服务暂时不可用")
			}
			if !approved.Approved {
				return api.ProposalReviewResult{}, proposalFail(http.StatusConflict, api.Conflict, "提案已经处理过了")
			}
			h.auditProposalReview(ctx, actor, id, row.Status, "approved", "task", approved.TaskID, "")
			return api.ProposalReviewResult{Id: int(id), Status: api.ProposalReviewResultStatusApproved}, nil

		case "new_skill":
			payload := proposalPayloadMap(row.Payload)
			slug, _ := payload["slug"].(string)
			name, _ := payload["name"].(string)
			description, _ := payload["description"].(string)
			spec, _ := payload["spec"].(string)
			url, _ := payload["url"].(string)
			slug = strings.TrimSpace(slug)
			name = strings.TrimSpace(name)
			if spec == "" {
				spec = "agentskills.io"
			}
			if spec != "agentskills.io" && spec != "other" {
				return api.ProposalReviewResult{}, proposalFail(http.StatusUnprocessableEntity, api.InvalidParam, "new_skill 的 spec 只能是 agentskills.io 或 other")
			}
			if slug == "" || name == "" {
				return api.ProposalReviewResult{}, proposalFail(http.StatusUnprocessableEntity, api.InvalidParam, "new_skill 的 slug 与 name 不能为空")
			}
			approved, err := h.queries.ApproveNewSkillProposal(ctx, store.ApproveNewSkillProposalParams{
				ID:          id,
				Slug:        slug,
				Name:        name,
				Description: description,
				Url:         pgtype.Text{String: strings.TrimSpace(url), Valid: strings.TrimSpace(url) != ""},
				Spec:        spec,
				ReviewedBy:  pgtype.Int8{Int64: actor.UserID, Valid: actor.UserID != 0},
			})
			if err != nil {
				return api.ProposalReviewResult{}, proposalFail(http.StatusServiceUnavailable, api.Unavailable, "服务暂时不可用")
			}
			if !approved.Approved {
				return api.ProposalReviewResult{}, proposalFail(http.StatusConflict, api.Conflict, "提案已经处理过了")
			}
			h.auditProposalReview(ctx, actor, id, row.Status, "approved", "skill", approved.SkillID, "")
			return api.ProposalReviewResult{Id: int(id), Status: api.ProposalReviewResultStatusApproved}, nil

		case "new_classification":
			payload := proposalPayloadMap(row.Payload)
			dimension, _ := payload["dimension"].(string)
			slug, _ := payload["slug"].(string)
			label, _ := payload["label"].(string)
			description, _ := payload["description"].(string)
			slug = strings.TrimSpace(slug)
			label = strings.TrimSpace(label)
			switch dimension {
			case "genre", "technique", "subject":
			default:
				return api.ProposalReviewResult{}, proposalFail(http.StatusUnprocessableEntity, api.InvalidParam, "new_classification 的 dimension 只能是 genre / technique / subject")
			}
			if slug == "" {
				return api.ProposalReviewResult{}, proposalFail(http.StatusUnprocessableEntity, api.InvalidParam, "提案缺少 slug")
			}
			if label == "" {
				label = slug
			}
			approved, err := h.queries.ApproveClassificationProposal(ctx, store.ApproveClassificationProposalParams{
				ID:          id,
				Dimension:   dimension,
				Slug:        slug,
				Label:       label,
				Description: description,
				ReviewedBy:  pgtype.Int8{Int64: actor.UserID, Valid: actor.UserID != 0},
			})
			if err != nil {
				return api.ProposalReviewResult{}, proposalFail(http.StatusServiceUnavailable, api.Unavailable, "服务暂时不可用")
			}
			if !approved.Approved {
				return api.ProposalReviewResult{}, proposalFail(http.StatusConflict, api.Conflict, "提案已经处理过了")
			}
			h.auditProposalReview(ctx, actor, id, row.Status, "approved", "classification", approved.ClassificationID, "")
			return api.ProposalReviewResult{Id: int(id), Status: api.ProposalReviewResultStatusApproved}, nil

		case "new_benchmark":
			payload := proposalPayloadMap(row.Payload)
			name, _ := payload["name"].(string)
			name = strings.TrimSpace(name)
			if name == "" {
				return api.ProposalReviewResult{}, proposalFail(http.StatusUnprocessableEntity, api.InvalidParam, "new_benchmark 的名称不能为空")
			}
			publisher, _ := payload["publisher"].(string)
			homepage, _ := payload["homepage_url"].(string)
			description, _ := payload["description"].(string)
			category, _ := payload["category"].(string)
			category = strings.TrimSpace(category)
			if category == "" {
				// 目录里「测什么」缺省就是 general（`benchmarks.category` 的列默认值同此）。
				// 空串不能进库：列表按逗号枚举解析，空值会让这个条目在按方向筛选时消失。
				category = "general"
			}
			approved, err := h.queries.ApproveNewBenchmarkProposal(ctx, store.ApproveNewBenchmarkProposalParams{
				ID:          id,
				Slug:        benchmarkSlug(name),
				Name:        name,
				Publisher:   strings.TrimSpace(publisher),
				HomepageUrl: strings.TrimSpace(homepage),
				Category:    category,
				Description: strings.TrimSpace(description),
				ReviewedBy:  pgtype.Int8{Int64: actor.UserID, Valid: actor.UserID != 0},
			})
			if err != nil {
				if isUniqueViolation(err) {
					return api.ProposalReviewResult{}, proposalFail(http.StatusConflict, api.Conflict, "同名基准条目刚被另一条提案创建，请重试")
				}
				return api.ProposalReviewResult{}, proposalFail(http.StatusServiceUnavailable, api.Unavailable, "服务暂时不可用")
			}
			if !approved.Approved {
				return api.ProposalReviewResult{}, proposalFail(http.StatusConflict, api.Conflict, "提案已经处理过了")
			}
			h.auditProposalReview(ctx, actor, id, row.Status, "approved", "benchmark", approved.BenchmarkID, "")
			return api.ProposalReviewResult{Id: int(id), Status: api.ProposalReviewResultStatusApproved}, nil

		case "new_model":
			return h.approveNewModelProposal(ctx, actor, id)

		default:
			return api.ProposalReviewResult{}, proposalFail(http.StatusUnprocessableEntity, api.InvalidParam,
				"该 kind 的执行器尚未接入（目前只接 new_benchmark / new_classification / new_model / new_skill / new_task）")
		}

	default:
		return api.ProposalReviewResult{}, proposalFail(http.StatusBadRequest, api.InvalidParam, "decision 只能是 approve 或 reject")
	}
}

// ReviewAdminProposal 对应 POST /api/v1/admin/proposals/{id}/review。
func (h *Handler) ReviewAdminProposal(w http.ResponseWriter, r *http.Request, id int) {
	if h.queries == nil || h.identity == nil {
		writeError(w, http.StatusServiceUnavailable, api.Unavailable, "服务暂时不可用")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), queryTimeout)
	defer cancel()

	actor, err := h.proposalReviewer(ctx, r)
	if err != nil {
		h.writeReviewError(w, err)
		return
	}

	var body api.ProposalDecision
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, api.InvalidParam, "请求体不是合法的 JSON")
		return
	}

	result, failure := h.reviewProposalOne(ctx, actor, int64(id), body.Decision, body.Reason)
	if failure != nil {
		writeError(w, failure.status, failure.code, failure.message)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// BatchReviewAdminProposals 对应 POST /api/v1/admin/proposals/batch（GOV-010 / ENT-022）。
func (h *Handler) BatchReviewAdminProposals(w http.ResponseWriter, r *http.Request) {
	if h.queries == nil || h.identity == nil {
		writeError(w, http.StatusServiceUnavailable, api.Unavailable, "服务暂时不可用")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), queryTimeout)
	defer cancel()

	actor, err := h.proposalReviewer(ctx, r)
	if err != nil {
		h.writeReviewError(w, err)
		return
	}

	var body api.ProposalBatchReview
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, api.InvalidParam, "请求体不是合法的 JSON")
		return
	}
	if len(body.Ids) == 0 || len(body.Ids) > 50 {
		writeError(w, http.StatusBadRequest, api.InvalidParam, "ids 需要 1–50 条")
		return
	}
	seen := make(map[int]bool, len(body.Ids))
	for _, id := range body.Ids {
		if id < 1 || seen[id] {
			writeError(w, http.StatusBadRequest, api.InvalidParam, "ids 里有重复或非法编号")
			return
		}
		seen[id] = true
	}
	if body.Decision != api.ProposalBatchReviewDecisionApprove && body.Decision != api.ProposalBatchReviewDecisionReject {
		writeError(w, http.StatusBadRequest, api.InvalidParam, "decision 只能是 approve 或 reject")
		return
	}

	results := make([]api.ProposalBatchItem, 0, len(body.Ids))
	for _, rawID := range body.Ids {
		decision := api.ProposalDecisionDecision(body.Decision)
		result, failure := h.reviewProposalOne(ctx, actor, int64(rawID), decision, body.Reason)
		item := api.ProposalBatchItem{Id: rawID, Ok: failure == nil}
		if failure != nil {
			message := failure.message
			item.Error = &message
		} else {
			status := api.ProposalBatchItemStatus(result.Status)
			item.Status = &status
		}
		results = append(results, item)
	}
	writeJSON(w, http.StatusOK, api.ProposalBatchResult{Results: results})
}

// auditProposalReview 写提案审核审计；失败只记日志（状态已经落库）。
func (h *Handler) auditProposalReview(ctx context.Context, actor demo.Actor, id int64, beforeStatus, afterStatus, resultKind string, resultID int64, reason string) {
	before, _ := json.Marshal(map[string]string{"status": beforeStatus})
	after := map[string]any{"status": afterStatus}
	if resultKind != "" {
		after["result_kind"] = resultKind
		after["result_id"] = resultID
	}
	if reason != "" {
		after["reason"] = reason
	}
	afterJSON, _ := json.Marshal(after)
	if err := h.queries.InsertAuditLog(ctx, store.InsertAuditLogParams{
		ActorID:    pgtype.Int8{Int64: actor.AccountID, Valid: actor.AccountID != 0},
		ActorRole:  actor.Role,
		Action:     "review",
		EntityType: "proposal",
		EntityID:   id,
		Before:     before,
		After:      afterJSON,
		Reason:     reason,
	}); err != nil {
		slog.Warn("写提案审核审计失败", "proposal", id, "err", err)
	}
}

// taskSlug 用标题生成 slug；纯中文标题折不出 ASCII 时退化成随机后缀。
func taskSlug(title string) string {
	if base := demo.SlugifyForImport(title); base != "" {
		return "task-" + base
	}
	buf := make([]byte, 4)
	if _, err := rand.Read(buf); err != nil {
		return "task"
	}
	return "task-" + hex.EncodeToString(buf)
}
