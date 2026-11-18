package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/api"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/demo"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

// 聚合收件箱（#433 / 治理域 D4）。
//
// 这一轮先做「看得到」：demo 两段队列 + 提案待办与各自计数。提案裁决、统一裁决入口、
// 批量的逐条结果下一轮接入；这里不写一个会假装成功的 approve。
//
// 权限：demo 队列要 demo.review；提案列表额外要 proposal.review。前者由 reviewer 校验，
// 后者单独查——一个只有 demo.review 的自定义角色不该看到提案内容。

const (
	inboxLimitDefault = 20
	inboxLimitMax     = 100
)

func reviewQueueItems(rows []demo.PendingReview, state api.ReviewQueueItemModerationState) []api.ReviewQueueItem {
	items := make([]api.ReviewQueueItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, api.ReviewQueueItem{
			Slug:            row.Slug,
			Title:           nullableString(row.Title),
			Description:     nullableString(row.Description),
			AuthorName:      nullableString(row.AuthorName),
			ModerationState: state,
			SubmittedAt:     row.SubmittedAt,
			PreviewPath:     previewPath(row.Slug, row.CurrentRevNo),
		})
	}
	return items
}

func proposalSummaryTitle(payload []byte) string {
	var data map[string]any
	if len(payload) == 0 || json.Unmarshal(payload, &data) != nil {
		return ""
	}
	for _, key := range []string{"title", "name", "slug"} {
		if value, ok := data[key].(string); ok && value != "" {
			return value
		}
	}
	return ""
}

// GetAdminInbox 对应 GET /api/v1/admin/inbox。
func (h *Handler) GetAdminInbox(w http.ResponseWriter, r *http.Request, params api.GetAdminInboxParams) {
	if h.demos == nil || h.queries == nil || h.identity == nil {
		writeError(w, http.StatusServiceUnavailable, api.Unavailable, "服务暂时不可用")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), queryTimeout)
	defer cancel()

	if _, err := h.reviewer(ctx, r); err != nil {
		h.writeReviewError(w, err)
		return
	}

	limit := int32(inboxLimitDefault)
	if params.Limit != nil {
		limit = int32(*params.Limit)
	}
	if limit < 1 || limit > inboxLimitMax {
		writeError(w, http.StatusBadRequest, api.InvalidParam, "limit 必须在 1–100")
		return
	}

	pendingRows, err := h.demos.ListPendingReviews(ctx, limit)
	if err != nil {
		h.writeReviewError(w, err)
		return
	}
	approvedRows, err := h.demos.ListApprovedUnpublished(ctx, limit)
	if err != nil {
		h.writeReviewError(w, err)
		return
	}

	countDemoPending, err := h.queries.CountInboxDemoPending(ctx)
	if err != nil {
		unavailable(w, err)
		return
	}
	countApproved, err := h.queries.CountInboxApprovedUnpublished(ctx)
	if err != nil {
		unavailable(w, err)
		return
	}

	out := api.AdminInbox{
		DemoPendingCount:         int(countDemoPending),
		ApprovedUnpublishedCount: int(countApproved),
		DemoPending:              reviewQueueItems(pendingRows, api.ReviewQueueItemModerationStatePending),
		ApprovedUnpublished:      reviewQueueItems(approvedRows, api.ReviewQueueItemModerationStateApproved),
		ProposalPendingCount:     0,
		Proposals:                []api.InboxProposal{},
	}

	// 提案摘要只在有 proposal.review 时出现。
	account, err := h.identity.Me(ctx, readSessionCookie(r))
	if err == nil {
		allowed, permErr := h.queries.HasPermission(ctx, store.HasPermissionParams{
			Sub:           account.Sub,
			PermissionKey: "proposal.review",
		})
		if permErr != nil {
			slog.Warn("查提案审核权限失败", "err", permErr)
		} else if allowed {
			countProposals, countErr := h.queries.CountInboxPendingProposals(ctx)
			if countErr != nil {
				unavailable(w, countErr)
				return
			}
			rows, listErr := h.queries.ListInboxPendingProposals(ctx, limit)
			if listErr != nil {
				unavailable(w, listErr)
				return
			}
			out.ProposalPendingCount = int(countProposals)
			for _, row := range rows {
				out.Proposals = append(out.Proposals, api.InboxProposal{
					Id:        int(row.ID),
					Kind:      row.Kind,
					Title:     proposalSummaryTitle(row.Payload),
					Source:    row.Source,
					CreatedAt: row.CreatedAt.Time,
				})
			}
		}
	}

	writeJSON(w, http.StatusOK, out)
}
