package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/api"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/demo"
)

// 作品评分端点：`PUT` / `DELETE /api/v1/demos/{slug}/rating`。
//
// # 为什么打分是 PUT 不是 POST
//
// 「我对这件作品的分数是 4」是一个**状态**，不是一条流水。PUT 语义（同一个请求
// 重发多少次，结果一样）正好对上「一人一票，改分是 UPDATE」，重试不用担心刷票。
//
// # 为什么匿名也放行
//
// 见 domain/demo/rating.go 的 Rate：要求先注册才能说一句「这个不错」，
// 等于把最轻的一次表态推到最重的门槛后面。
//
// # 为什么撤销返回 200 而不是 204
//
// 撤销之后界面要重画汇总（平均分可能变了），所以带上当前的 `Rating`。
// 没打过分的调用同样是 200——`DELETE` 本来就该幂等。

const ratingTimeout = 5 * time.Second

// PutDemoRating 处理 `PUT /api/v1/demos/{slug}/rating`。
func (h *Handler) PutDemoRating(w http.ResponseWriter, r *http.Request, slug string) {
	if h.demos == nil || h.identity == nil {
		writeError(w, http.StatusServiceUnavailable, api.Unavailable, "服务暂时不可用")
		return
	}

	var body api.DemoRatingInput
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, api.InvalidParam, "请求体不是合法的 JSON")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), ratingTimeout)
	defer cancel()

	// 范围校验在领域层（`demo.Rate`）：写两处的话，改了一处另一处还在，
	// 而反向验证会证明「删掉其中一处照样绿」——判断就没被真正压住。
	accountID, viewerKey := h.raterOf(ctx, r)
	rating, err := h.demos.Rate(ctx, slug, viewerKey, accountID, body.Score)
	if err != nil {
		h.writeRatingError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ratingOf(rating))
}

// DeleteDemoRating 处理 `DELETE /api/v1/demos/{slug}/rating`。
func (h *Handler) DeleteDemoRating(w http.ResponseWriter, r *http.Request, slug string) {
	if h.demos == nil || h.identity == nil {
		writeError(w, http.StatusServiceUnavailable, api.Unavailable, "服务暂时不可用")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), ratingTimeout)
	defer cancel()

	_, viewerKey := h.raterOf(ctx, r)
	rating, err := h.demos.Unrate(ctx, slug, viewerKey)
	if err != nil {
		h.writeRatingError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ratingOf(rating))
}

// raterOf 折出「谁在投票」：账号 id（匿名是 0）与去重键。
//
// 与浏览计数共用 identity.Service.ViewerKey——两处各算各的，同一个人在
// 「看过」和「打过分」里就会是两个人。
func (h *Handler) raterOf(ctx context.Context, r *http.Request) (int64, string) {
	var accountID int64
	if account, err := h.identity.Me(ctx, readSessionCookie(r)); err == nil {
		accountID = account.ID
	}
	return accountID, h.identity.ViewerKey(accountID, clientIP(r))
}

func (h *Handler) writeRatingError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, demo.ErrRatingOutOfRange):
		writeError(w, http.StatusBadRequest, api.InvalidParam, "评分必须在 1 到 5 之间")
	case errors.Is(err, demo.ErrNotFound):
		writeError(w, http.StatusNotFound, api.NotFound, "作品不存在")
	default:
		slog.Error("评分链路未分类错误", "err", err)
		writeError(w, http.StatusServiceUnavailable, api.Unavailable, "服务暂时不可用")
	}
}

// ratingOf 把领域结构摊成契约结构。
//
// `count` 在契约里是 int，域名里是 int——保持一致的宽度，别在这里做无声的截断。
func ratingOf(r demo.Rating) api.DemoRating {
	out := api.DemoRating{Average: r.Average, Count: r.Count}
	if r.Mine != nil {
		score := *r.Mine
		out.Mine = &score
	}
	return out
}

// ratingOfDetail 给详情用：读失败时给 0 分 0 票，而不是让详情 503。
func (h *Handler) ratingOfDetail(ctx context.Context, slug, viewerKey string) api.DemoRating {
	return ratingOf(h.demos.RatingOf(ctx, slug, viewerKey))
}
