package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/api"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/identity"
)

// 「为你推荐」：种子是这个人自己的浏览与评分，现算。
//
// 和「相关作品」一样，这一组用例必须自己造信号：种子作品里几乎没有分类与模型，
// 纯靠种子数据的话推荐永远是空的，测试会退化成「断言空数组」。
//
// 这一组还额外依赖身份：浏览与评分都按 `viewer_key` 落库，所以每个用例都要有
// 一个真实登录用户，并且浏览/评分要用同一个人的会话去写——否则种子是空的。

func newRecommendationHandler(t *testing.T) (*Handler, *identity.Service, *pgxpool.Pool) {
	t.Helper()
	return newSortHandler(t)
}

// seedView 用某个人的身份记一次浏览。
//
// 走接口而不是直接写表：`viewer_key` 的算法住在 identity.Service 里，
// 测试自己拼一遍等于把那份实现复制到测试里，改一处就悄悄失配。
func seedView(t *testing.T, h *Handler, slug, sid string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/demos/"+slug+"/view", nil)
	if sid != "" {
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: sid})
	}
	rec := httptest.NewRecorder()
	NewRouter(h, nil, nil).ServeHTTP(rec, req)
	if rec.Code != 204 && rec.Code != 200 {
		t.Fatalf("记浏览 %s = %d，想要 204；响应体：%s", slug, rec.Code, rec.Body.String())
	}
}

func seedRating(t *testing.T, h *Handler, slug, sid string, score int) {
	t.Helper()
	if rec := putRating(t, h, slug, sid, score); rec.Code != 200 {
		t.Fatalf("评分 %s = %d，想要 200；响应体：%s", slug, rec.Code, rec.Body.String())
	}
}

func recommendations(t *testing.T, h *Handler, sid, query string) api.RecommendationList {
	t.Helper()
	rec := getWithSession(t, h, "/api/v1/recommendations"+query, sid)
	if rec.Code != 200 {
		t.Fatalf("推荐 = %d，想要 200；响应体：%s", rec.Code, rec.Body.String())
	}
	var body api.RecommendationList
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	return body
}

func recommendedSlugs(t *testing.T, h *Handler, sid, query string) []string {
	t.Helper()
	body := recommendations(t, h, sid, query)
	out := make([]string, 0, len(body.Items))
	for _, it := range body.Items {
		out = append(out, it.Slug)
	}
	return out
}

// 种子作品的分类会带出同分类的候选。
func TestIntegrationRecommendationsUseSeedSignals(t *testing.T) {
	h, idSvc, pool := newRecommendationHandler(t)
	reader, sid := registerVerified(t, pool, idSvc, registerInputFor(t, "爱看的人"))
	t.Cleanup(func() { cleanupAuthor(t, pool, reader.Sub) })
	writer, writerSid := registerVerified(t, pool, idSvc, registerInputFor(t, "写的人"))
	t.Cleanup(func() { cleanupAuthor(t, pool, writer.Sub) })
	resetPublished(t, pool)

	seed := publishRated(t, h, pool, writerSid, "我看过的", ratingStats{}, 0)
	neighbor := publishRated(t, h, pool, writerSid, "同分类的邻居", ratingStats{}, 0)
	unrelated, unrelatedSid := registerVerified(t, pool, idSvc, registerInputFor(t, "无关作者"))
	t.Cleanup(func() { cleanupAuthor(t, pool, unrelated.Sub) })
	other := publishRated(t, h, pool, unrelatedSid, "不相干的作品", ratingStats{}, 0)

	classes := classificationIDs(t, pool, "game")
	attachClassifications(t, pool, seed, classes)
	attachClassifications(t, pool, neighbor, classes)

	seedView(t, h, seed, sid)

	got := recommendedSlugs(t, h, sid, "")
	if len(got) != 1 || got[0] != neighbor {
		t.Fatalf("推荐 = %v，想要 [%s]（同分类；%s 与种子无共同信号）", got, neighbor, other)
	}
}

// 看过或打过分的不再推荐：推荐里出现刚看完的那件，读者会觉得系统没在听。
func TestIntegrationRecommendationsSkipSeen(t *testing.T) {
	h, idSvc, pool := newRecommendationHandler(t)
	reader, sid := registerVerified(t, pool, idSvc, registerInputFor(t, "反复看的人"))
	t.Cleanup(func() { cleanupAuthor(t, pool, reader.Sub) })
	writer, writerSid := registerVerified(t, pool, idSvc, registerInputFor(t, "作者二"))
	t.Cleanup(func() { cleanupAuthor(t, pool, writer.Sub) })
	resetPublished(t, pool)

	seed := publishRated(t, h, pool, writerSid, "种子", ratingStats{}, 0)
	first := publishRated(t, h, pool, writerSid, "已经看过的", ratingStats{}, 0)
	second := publishRated(t, h, pool, writerSid, "还没看过的", ratingStats{}, 0)

	seedView(t, h, seed, sid)
	seedView(t, h, first, sid)

	got := recommendedSlugs(t, h, sid, "")
	if len(got) != 1 || got[0] != second {
		t.Fatalf("推荐 = %v，想要 [%s]（%s 已经看过）", got, second, first)
	}
}

// 自己的作品不推荐给自己：作者不需要系统介绍他自己的东西。
func TestIntegrationRecommendationsSkipOwnDemos(t *testing.T) {
	h, idSvc, pool := newRecommendationHandler(t)
	author, sid := registerVerified(t, pool, idSvc, registerInputFor(t, "自产自销"))
	t.Cleanup(func() { cleanupAuthor(t, pool, author.Sub) })
	resetPublished(t, pool)

	mine := publishRated(t, h, pool, sid, "我的第一件", ratingStats{}, 0)
	alsoMine := publishRated(t, h, pool, sid, "我的第二件", ratingStats{}, 0)
	seedView(t, h, mine, sid)

	if got := recommendedSlugs(t, h, sid, ""); len(got) != 0 {
		t.Fatalf("推荐 = %v，想要空数组（两件都是自己的作品）", got)
	}
	_ = alsoMine
}

// 没有任何种子时返回空数组，不是错误。
func TestIntegrationRecommendationsColdStart(t *testing.T) {
	h, idSvc, pool := newRecommendationHandler(t)
	fresh, sid := registerVerified(t, pool, idSvc, registerInputFor(t, "新来的"))
	t.Cleanup(func() { cleanupAuthor(t, pool, fresh.Sub) })
	resetPublished(t, pool)

	writer, writerSid := registerVerified(t, pool, idSvc, registerInputFor(t, "别人三"))
	t.Cleanup(func() { cleanupAuthor(t, pool, writer.Sub) })
	publishRated(t, h, pool, writerSid, "别人的作品", ratingStats{}, 0)

	body := recommendations(t, h, sid, "")
	if body.Items == nil {
		t.Fatal("冷启动时 items 应当是空数组而不是 null")
	}
	if len(body.Items) != 0 {
		t.Fatalf("冷启动推荐 = %v，想要空数组", recommendedSlugs(t, h, sid, ""))
	}
}

// 未登录一律 401：匿名访客的键是 HMAC(ip)，拿它做个性化等于把 NAT 后面的人当成一个人。
func TestIntegrationRecommendationsRequireLogin(t *testing.T) {
	h, _, pool := newRecommendationHandler(t)
	resetPublished(t, pool)

	rec := getWithSession(t, h, "/api/v1/recommendations", "")
	if rec.Code != 401 {
		t.Fatalf("未登录推荐 = %d，想要 401；响应体：%s", rec.Code, rec.Body.String())
	}
}

// limit 越界回 400，不静默截断。
func TestIntegrationRecommendationsLimit(t *testing.T) {
	h, idSvc, pool := newRecommendationHandler(t)
	reader, sid := registerVerified(t, pool, idSvc, registerInputFor(t, "挑刺的人"))
	t.Cleanup(func() { cleanupAuthor(t, pool, reader.Sub) })
	resetPublished(t, pool)

	for _, query := range []string{"?limit=0", "?limit=13"} {
		rec := getWithSession(t, h, "/api/v1/recommendations"+query, sid)
		if rec.Code != 400 {
			t.Fatalf("%s = %d，想要 400", query, rec.Code)
		}
	}
}

// 评分也算种子：不打浏览也算。
func TestIntegrationRecommendationsUseRatingSeeds(t *testing.T) {
	h, idSvc, pool := newRecommendationHandler(t)
	reader, sid := registerVerified(t, pool, idSvc, registerInputFor(t, "只打分的人"))
	t.Cleanup(func() { cleanupAuthor(t, pool, reader.Sub) })
	writer, writerSid := registerVerified(t, pool, idSvc, registerInputFor(t, "作者四"))
	t.Cleanup(func() { cleanupAuthor(t, pool, writer.Sub) })
	resetPublished(t, pool)

	seed := publishRated(t, h, pool, writerSid, "我打过分的", ratingStats{}, 0)
	neighbor := publishRated(t, h, pool, writerSid, "同模型的邻居", ratingStats{}, 0)
	// 「不相干的」必须换一个作者：同作者 +1 会把它带进来，用例就压不住「模型信号」这条判据了。
	stranger, strangerSid := registerVerified(t, pool, idSvc, registerInputFor(t, "路人作者"))
	t.Cleanup(func() { cleanupAuthor(t, pool, stranger.Sub) })
	publishRated(t, h, pool, strangerSid, "不相干的", ratingStats{}, 0)

	attachModelByName(t, pool, seed, "推荐信号模型")
	attachModelByName(t, pool, neighbor, "推荐信号模型")
	seedRating(t, h, seed, sid, 5)

	got := recommendedSlugs(t, h, sid, "")
	if len(got) != 1 || got[0] != neighbor {
		t.Fatalf("推荐 = %v，想要 [%s]（打分也是种子）", got, neighbor)
	}
}
