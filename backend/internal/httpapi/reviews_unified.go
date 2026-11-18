package httpapi

import (
	"net/http"

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
	case api.Demo:
		h.ReviewDemo(w, r, id)
	default:
		writeError(w, http.StatusBadRequest, api.InvalidParam, "kind 只能是 demo（提案审核尚未接入）")
	}
}
