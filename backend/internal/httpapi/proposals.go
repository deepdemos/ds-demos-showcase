package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/api"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/demo"
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

	row, err := h.queries.GetProposalByID(ctx, int64(id))
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, api.NotFound, "提案不存在")
		return
	}
	if err != nil {
		unavailable(w, err)
		return
	}
	if row.Status != "pending" {
		writeError(w, http.StatusConflict, api.Conflict, "提案已经处理过了")
		return
	}

	switch body.Decision {
	case api.ProposalDecisionDecisionReject:
		reason := ""
		if body.Reason != nil {
			reason = strings.TrimSpace(*body.Reason)
		}
		rejected, err := h.queries.RejectProposal(ctx, store.RejectProposalParams{
			ID:           int64(id),
			ReviewedBy:   pgtype.Int8{Int64: actor.UserID, Valid: actor.UserID != 0},
			RejectReason: pgtype.Text{String: reason, Valid: reason != ""},
		})
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusConflict, api.Conflict, "提案已经处理过了")
			return
		}
		if err != nil {
			unavailable(w, err)
			return
		}

		before, _ := json.Marshal(map[string]string{"status": row.Status})
		after, _ := json.Marshal(map[string]string{"status": rejected.Status, "reason": reason})
		if err := h.queries.InsertAuditLog(ctx, store.InsertAuditLogParams{
			ActorID:    pgtype.Int8{Int64: actor.UserID, Valid: actor.UserID != 0},
			ActorRole:  actor.Role,
			Action:     "review",
			EntityType: "proposal",
			EntityID:   int64(id),
			Before:     before,
			After:      after,
			Reason:     reason,
		}); err != nil {
			// 状态已经落库；审计补写失败只记日志，不能回 503 让调用方重试（会撞 409）。
			// 严格口径需要同一事务，这一轮保持与 demo 审核一致的「先改后记」。
			slog.Warn("写提案审核审计失败", "proposal", id, "err", err)
		}
		writeJSON(w, http.StatusOK, api.ProposalReviewResult{
			Id: int(rejected.ID), Status: api.ProposalReviewResultStatusRejected,
		})
	case api.ProposalDecisionDecisionApprove:
		writeError(w, http.StatusUnprocessableEntity, api.InvalidParam,
			"提案通过需要按 kind 执行正式写入，执行器尚未接入")
	default:
		writeError(w, http.StatusBadRequest, api.InvalidParam, "decision 只能是 approve 或 reject")
	}
}
