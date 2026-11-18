package httpapi

import (
	"context"
	"net/http"
	"strconv"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/api"
)

// 统一审核裁决入口（GOV-006 / #433）。
//
// review_kinds 注册表决定「有哪些审核种类」，这个 handler 只做分派：
//   - demo：id 是 slug，委托给已有的 demo 裁决逻辑（含权限、状态机、通知）。
//   - proposal：下一轮接入；现在明确拒绝，而不是假装成功。
//
// 旧路径 /admin/reviews/demo/{slug} 保留为兼容别名；前端已改走这里。
func (h *Handler) ReviewEntity(w http.ResponseWriter, r *http.Request, kind api.ReviewEntityParamsKind, id string) {
	switch kind {
	case api.ReviewEntityParamsKindDemo:
		h.ReviewDemo(w, r, id)
	case api.ReviewEntityParamsKindProposal:
		proposalID, err := strconv.Atoi(id)
		if err != nil || proposalID < 1 {
			writeError(w, http.StatusBadRequest, api.InvalidParam, "proposal 的 id 必须是正整数")
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
		result, failure := h.reviewProposalOne(ctx, actor, int64(proposalID), body.Decision, body.Reason)
		if failure != nil {
			writeError(w, failure.status, failure.code, failure.message)
			return
		}
		kindValue := api.ReviewResultKind(kind)
		writeJSON(w, http.StatusOK, api.ReviewResult{
			Slug:            id,
			ModerationState: api.ReviewResultModerationState(result.Status),
			Kind:            &kindValue,
		})
	default:
		writeError(w, http.StatusBadRequest, api.InvalidParam, "kind 只能是 demo 或 proposal")
	}
}
