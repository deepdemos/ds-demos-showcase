package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/api"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

// 作品申诉（#463）。一表一语义：不塞进 proposals。
//
// 作者只能看到自己的申诉；非作者一律 404（与私有收藏夹同一口径，不承认存在）。

const appealReasonMax = 2000

func appealView(id int64, reason, status string, reviewedAt pgtype.Timestamptz, decision string, createdAt pgtype.Timestamptz) api.DemoAppeal {
	out := api.DemoAppeal{
		Id:        int(id),
		Reason:    reason,
		Status:    api.DemoAppealStatus(status),
		CreatedAt: createdAt.Time,
	}
	if reviewedAt.Valid {
		at := reviewedAt.Time
		out.ReviewedAt = &at
	}
	text := decision
	out.DecisionReason = &text
	return out
}

// GetDemoAppeal 对应 GET /api/v1/demos/{slug}/appeal。
func (h *Handler) GetDemoAppeal(w http.ResponseWriter, r *http.Request, slug string) {
	if h.queries == nil || h.identity == nil {
		writeError(w, http.StatusServiceUnavailable, api.Unavailable, "服务暂时不可用")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), queryTimeout)
	defer cancel()

	account, err := h.identity.Me(ctx, readSessionCookie(r))
	if err != nil {
		h.writeIdentityError(w, err)
		return
	}

	row, err := h.queries.GetDemoAppealContext(ctx, slug)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, api.NotFound, "作品不存在")
		return
	}
	if err != nil {
		unavailable(w, err)
		return
	}
	if row.AuthorAccountID == 0 || row.AuthorAccountID != account.ID {
		writeError(w, http.StatusNotFound, api.NotFound, "作品不存在")
		return
	}

	appealRow, err := h.queries.GetLatestDemoAppeal(ctx, row.DemoID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeJSON(w, http.StatusOK, api.DemoAppealState{
			CanSubmit: row.LifecycleState == "removed",
			Appeal:    nil,
		})
		return
	}
	if err != nil {
		unavailable(w, err)
		return
	}

	appeal := appealView(appealRow.ID, appealRow.Reason, appealRow.Status,
		appealRow.ReviewedAt, appealRow.DecisionReason, appealRow.CreatedAt)
	writeJSON(w, http.StatusOK, api.DemoAppealState{
		CanSubmit: row.LifecycleState == "removed" && appealRow.Status != "pending",
		Appeal:    &appeal,
	})
}

// SubmitDemoAppeal 对应 POST /api/v1/demos/{slug}/appeal。
func (h *Handler) SubmitDemoAppeal(w http.ResponseWriter, r *http.Request, slug string) {
	if h.queries == nil || h.identity == nil {
		writeError(w, http.StatusServiceUnavailable, api.Unavailable, "服务暂时不可用")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), queryTimeout)
	defer cancel()

	account, err := h.identity.Me(ctx, readSessionCookie(r))
	if err != nil {
		h.writeIdentityError(w, err)
		return
	}

	var body api.DemoAppealInput
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, api.InvalidParam, "请求体不是合法的 JSON")
		return
	}
	reason := strings.TrimSpace(body.Reason)
	if reason == "" || len([]rune(reason)) > appealReasonMax {
		writeError(w, http.StatusBadRequest, api.InvalidParam, "申诉说明需要 1–2000 个字符")
		return
	}

	row, err := h.queries.GetDemoAppealContext(ctx, slug)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, api.NotFound, "作品不存在")
		return
	}
	if err != nil {
		unavailable(w, err)
		return
	}
	if row.AuthorAccountID == 0 || row.AuthorAccountID != account.ID {
		writeError(w, http.StatusNotFound, api.NotFound, "作品不存在")
		return
	}
	if row.LifecycleState != "removed" {
		writeError(w, http.StatusConflict, api.Conflict, "只有被移除的作品可以申诉")
		return
	}

	created, err := h.queries.CreateDemoAppeal(ctx, store.CreateDemoAppealParams{
		Slug:      slug,
		AccountID: account.ID,
		Reason:    reason,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusConflict, api.Conflict, "已有在途申诉，等处理结束再提交")
		return
	}
	if err != nil {
		unavailable(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, appealView(created.ID, created.Reason, created.Status,
		created.ReviewedAt, created.DecisionReason, created.CreatedAt))
}
