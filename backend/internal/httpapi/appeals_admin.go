package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/api"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/demo"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

// 管理员申诉列表与裁决（#463 第二半）。

const (
	appealPageSizeDefault = 20
	appealPageSizeMax     = 100
)

// ListAdminAppeals 对应 GET /api/v1/admin/appeals。
func (h *Handler) ListAdminAppeals(w http.ResponseWriter, r *http.Request, params api.ListAdminAppealsParams) {
	if h.queries == nil || h.identity == nil {
		writeError(w, http.StatusServiceUnavailable, api.Unavailable, "服务暂时不可用")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), queryTimeout)
	defer cancel()

	if _, err := h.reviewer(ctx, r); err != nil {
		h.writeReviewError(w, err)
		return
	}

	page := 1
	if params.Page != nil {
		page = *params.Page
	}
	pageSize := appealPageSizeDefault
	if params.PageSize != nil {
		pageSize = *params.PageSize
	}
	if page < 1 || pageSize < 1 || pageSize > appealPageSizeMax {
		writeError(w, http.StatusBadRequest, api.InvalidParam, "page 必须 ≥ 1，pageSize 必须在 1–100")
		return
	}
	status := "pending"
	if params.Status != nil {
		status = string(*params.Status)
	}
	if status != "pending" && status != "approved" && status != "rejected" {
		writeError(w, http.StatusBadRequest, api.InvalidParam, "status 只能是 pending / approved / rejected")
		return
	}

	total, err := h.queries.CountAdminAppeals(ctx, status)
	if err != nil {
		unavailable(w, err)
		return
	}
	rows, err := h.queries.ListAdminAppeals(ctx, store.ListAdminAppealsParams{
		Status: status, RowLimit: int32(pageSize), RowOffset: int32((page - 1) * pageSize),
	})
	if err != nil {
		unavailable(w, err)
		return
	}

	items := make([]api.AdminAppeal, 0, len(rows))
	for _, row := range rows {
		item := api.AdminAppeal{
			Id:             int(row.ID),
			DemoSlug:       row.Slug,
			DemoTitle:      row.DemoTitle,
			Reason:         row.Reason,
			Status:         api.AdminAppealStatus(row.Status),
			AuthorName:     row.AuthorName,
			CreatedAt:      row.CreatedAt.Time,
			DecisionReason: nil,
		}
		if row.ReviewedAt.Valid {
			at := row.ReviewedAt.Time
			item.ReviewedAt = &at
		}
		if row.DecisionReason != "" {
			text := row.DecisionReason
			item.DecisionReason = &text
		}
		items = append(items, item)
	}
	writeJSON(w, http.StatusOK, api.AdminAppealList{
		Items: items, Total: int(total), Page: page, PageSize: pageSize,
	})
}

// ReviewAdminAppeal 对应 POST /api/v1/admin/appeals/{id}/review。
func (h *Handler) ReviewAdminAppeal(w http.ResponseWriter, r *http.Request, id int) {
	if h.queries == nil || h.identity == nil {
		writeError(w, http.StatusServiceUnavailable, api.Unavailable, "服务暂时不可用")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), queryTimeout)
	defer cancel()

	actor, err := h.reviewer(ctx, r)
	if err != nil {
		h.writeReviewError(w, err)
		return
	}

	var body api.AppealDecision
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, api.InvalidParam, "请求体不是合法的 JSON")
		return
	}

	row, err := h.queries.GetAppealForReview(ctx, int64(id))
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, api.NotFound, "申诉不存在")
		return
	}
	if err != nil {
		unavailable(w, err)
		return
	}
	if row.Status != "pending" {
		writeError(w, http.StatusConflict, api.Conflict, "申诉已经处理过了")
		return
	}
	reason := ""
	if body.Reason != nil {
		reason = strings.TrimSpace(*body.Reason)
	}
	if len([]rune(reason)) > 500 {
		writeError(w, http.StatusUnprocessableEntity, api.InvalidParam, "裁决说明最多 500 个字符")
		return
	}

	before, _ := json.Marshal(map[string]string{
		"appeal_status": row.Status, "lifecycle_state": row.LifecycleState,
	})

	switch body.Decision {
	case api.AppealDecisionDecisionApprove:
		if h.pool == nil {
			writeError(w, http.StatusServiceUnavailable, api.Unavailable, "服务暂时不可用")
			return
		}
		tx, err := h.pool.Begin(ctx)
		if err != nil {
			unavailable(w, err)
			return
		}
		defer func() { _ = tx.Rollback(ctx) }()
		q := h.queries.WithTx(tx)

		if _, err := q.ApproveAppealStatus(ctx, store.ApproveAppealStatusParams{
			ID:             int64(id),
			ReviewedBy:     pgtype.Int8{Int64: actor.UserID, Valid: actor.UserID != 0},
			DecisionReason: reason,
		}); errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusConflict, api.Conflict, "作品已不是 removed 状态，申诉不能再通过")
			return
		} else if err != nil {
			unavailable(w, err)
			return
		}

		rows, err := q.TransitionDemoLifecycle(ctx, store.TransitionDemoLifecycleParams{
			DemoID:    row.DemoID,
			FromState: "removed",
			ToState:   "published",
		})
		if err != nil {
			unavailable(w, err)
			return
		}
		if rows == 0 {
			writeError(w, http.StatusConflict, api.Conflict, "作品已不是 removed 状态，申诉不能再通过")
			return
		}
		// 作品动态：恢复也是一次生命周期变化，不能只剩审计（Demo 域 §6.2）。
		if err := q.InsertDemoTimeline(ctx, store.InsertDemoTimelineParams{
			DemoID:  row.DemoID,
			Kind:    "restore",
			Column3: "申诉通过，作品恢复发布",
			ActorID: pgtype.Int8{Int64: actor.AccountID, Valid: actor.AccountID != 0},
		}); err != nil {
			unavailable(w, err)
			return
		}
		if err := h.auditAppealDecision(ctx, q, actor, row, "approved", reason, before); err != nil {
			unavailable(w, err)
			return
		}
		if err := tx.Commit(ctx); err != nil {
			unavailable(w, err)
			return
		}

		// 通知在事务外：失败不改变裁决结果，允许丢一条（已知松点）。
		h.notifyDemoEvent(ctx, row.Slug, "appeal.approved", map[string]string{"reason": reason}, actor.AccountID)
		writeJSON(w, http.StatusOK, api.AppealReviewResult{
			Id: int(id), Status: api.AppealReviewResultStatusApproved, DemoSlug: row.Slug,
		})

	case api.AppealDecisionDecisionReject:
		if h.pool == nil {
			writeError(w, http.StatusServiceUnavailable, api.Unavailable, "服务暂时不可用")
			return
		}
		tx, err := h.pool.Begin(ctx)
		if err != nil {
			unavailable(w, err)
			return
		}
		defer func() { _ = tx.Rollback(ctx) }()
		q := h.queries.WithTx(tx)
		if _, err := q.RejectAppeal(ctx, store.RejectAppealParams{
			ID:             int64(id),
			ReviewedBy:     pgtype.Int8{Int64: actor.UserID, Valid: actor.UserID != 0},
			DecisionReason: reason,
		}); errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusConflict, api.Conflict, "申诉已经处理过了")
			return
		} else if err != nil {
			unavailable(w, err)
			return
		}
		if err := h.auditAppealDecision(ctx, q, actor, row, "rejected", reason, before); err != nil {
			unavailable(w, err)
			return
		}
		if err := tx.Commit(ctx); err != nil {
			unavailable(w, err)
			return
		}
		h.notifyDemoEvent(ctx, row.Slug, "appeal.rejected", map[string]string{"reason": reason}, actor.AccountID)
		writeJSON(w, http.StatusOK, api.AppealReviewResult{
			Id: int(id), Status: api.AppealReviewResultStatusRejected, DemoSlug: row.Slug,
		})

	default:
		writeError(w, http.StatusBadRequest, api.InvalidParam, "decision 只能是 approve 或 reject")
	}
}

func (h *Handler) auditAppealDecision(ctx context.Context, q *store.Queries, actor demo.Actor, row store.GetAppealForReviewRow, afterStatus, reason string, before []byte) error {
	afterState := row.LifecycleState
	if afterStatus == "approved" {
		afterState = "published"
	}
	after, _ := json.Marshal(map[string]string{
		"appeal_status": afterStatus, "lifecycle_state": afterState,
	})
	return q.InsertAuditLog(ctx, store.InsertAuditLogParams{
		ActorID:    pgtype.Int8{Int64: actor.AccountID, Valid: actor.AccountID != 0},
		ActorRole:  actor.Role,
		Action:     "review",
		EntityType: "demo",
		EntityID:   row.DemoID,
		Before:     before,
		After:      after,
		Reason:     reason,
	})
}
