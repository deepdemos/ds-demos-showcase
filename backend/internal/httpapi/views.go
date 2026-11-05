package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"time"
)

// 浏览计数端点：`POST /api/v1/demos/{slug}/view`。
//
// 它被前端在详情页渲染成功之后调用一次。所以这里**故意不报错**：
// 作品不存在、今天已经看过、身份未接线——一律 204。
// 理由写在 domain/demo/view.go 的 RecordView 上：这个端点由页面自动调用，
// 给它一个 404 只会让「作品刚被删」这种正常情况在控制台里刷一片红，
// 而真正的可见性判定在详情端点上。
//
// # 为什么不返回计数
//
// 返回「现在的浏览数」看起来方便，但会让这个端点变成一个**写 + 读**的混合体：
// 前端拿它当准实时显示用，缓存与去重就都失效了。计数由详情端点给。

const viewTimeout = 5 * time.Second

// PostDemoView 处理 `POST /api/v1/demos/{slug}/view`。
func (h *Handler) PostDemoView(w http.ResponseWriter, r *http.Request, slug string) {
	if h.demos == nil || h.identity == nil {
		// 未接线（测试或降级部署）：不该让页面因为计数失败而报错。
		w.WriteHeader(http.StatusNoContent)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), viewTimeout)
	defer cancel()

	// 认人失败按匿名处理——这里不是鉴权点。
	var accountID int64
	if account, err := h.identity.Me(ctx, readSessionCookie(r)); err == nil {
		accountID = account.ID
	}

	viewerKey := h.identity.ViewerKey(accountID, clientIP(r))
	if _, err := h.demos.RecordView(ctx, slug, viewerKey); err != nil {
		// 计数失败不该影响用户：他来看作品，不是来给统计表添乱的。
		slog.Warn("记浏览失败", "slug", slug, "err", err)
	}
	w.WriteHeader(http.StatusNoContent)
}
