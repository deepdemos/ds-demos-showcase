package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/demo"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/identity"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/platform/storage"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

// 评分端点的六件事。缺任何一条这个功能就是半空的：
// 记不上分 = 没人能表态；改分不覆盖 = 一个人能刷出十个 5 分；
// 汇总不重算 = 分数永远停在第一次；撤销不减 = 只能升不能降；
// 详情读不到 = 没人能验收；越界不拒 = 界面上的 0.5 分也能写进来。

// newRatingTestHandler 造一个装了真实 demo.Service 的 handler。
func newRatingTestHandler(t *testing.T) (*Handler, *identity.Service, *pgxpool.Pool) {
	t.Helper()
	pool := integrationPool(t)
	q := store.New(pool)
	idSvc := integrationIdentity(t, pool)
	files, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("初始化存储失败: %v", err)
	}
	demoSvc := demo.New(pool, q, files, storage.DefaultLimits(), 200<<20, 0, nil)
	return New(pool, q, idSvc, nil, demoSvc), idSvc, pool
}

// putRating 发一次打分。sid 为空 = 匿名（同一个 IP）。
func putRating(t *testing.T, h *Handler, slug string, sid string, score int) *httptest.ResponseRecorder {
	t.Helper()
	body := fmt.Sprintf(`{"score":%d}`, score)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/demos/"+slug+"/rating", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "203.0.113.7:1234"
	if sid != "" {
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: sid})
	}
	rec := httptest.NewRecorder()
	NewRouter(h, nil, nil).ServeHTTP(rec, req)
	return rec
}

func deleteRating(t *testing.T, h *Handler, slug, sid string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/demos/"+slug+"/rating", nil)
	req.RemoteAddr = "203.0.113.7:1234"
	if sid != "" {
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: sid})
	}
	rec := httptest.NewRecorder()
	NewRouter(h, nil, nil).ServeHTTP(rec, req)
	return rec
}

// getDetailFrom 从指定出口地址读详情。
//
// 匿名调用者的去重键里含 IP 哈希，所以「我打了几分」这件事只有在**同一个 IP**
// 上才读得回来。用 getDemoAs 读会得到 mine=null——那不是实现的问题，是测试自己换了人。
func getDetailFrom(t *testing.T, h *Handler, slug, sid, addr string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/demos/"+slug, nil)
	req.RemoteAddr = addr
	if sid != "" {
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: sid})
	}
	rec := httptest.NewRecorder()
	NewRouter(h, nil, nil).ServeHTTP(rec, req)
	return rec
}

type ratingBody struct {
	Average float64 `json:"average"`
	Count   int     `json:"count"`
	Mine    *int    `json:"mine"`
}

func decodeRating(t *testing.T, rec *httptest.ResponseRecorder) ratingBody {
	t.Helper()
	var got ratingBody
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("响应不是合法 JSON: %v（body=%s）", err, rec.Body.String())
	}
	return got
}

// 打分能记上、能读回、能改。
func TestIntegrationPutDemoRating(t *testing.T) {
	h, idSvc, pool := newRatingTestHandler(t)
	author, authorSID := registerVerified(t, pool, idSvc, identity.RegisterInput{
		Email:       "it-rate-a@example.com",
		Password:    "ratetest1234",
		Username:    "it-rate-a",
		DisplayName: "评分作者",
	})
	t.Cleanup(func() { cleanupAuthor(t, pool, author.Sub) })
	slug := submitHTML(t, h, authorSID, "评分作品", `<!doctype html><p>rate</p>`)
	publishNow(t, pool, slug)

	// 第一次 4 分。
	rec := putRating(t, h, slug, "", 4)
	if rec.Code != http.StatusOK {
		t.Fatalf("打分 = %d，想要 200；body=%s", rec.Code, rec.Body.String())
	}
	got := decodeRating(t, rec)
	if got.Count != 1 || got.Average != 4 {
		t.Fatalf("首次打分后 count/avg = %d/%.2f，想要 1/4", got.Count, got.Average)
	}
	if got.Mine == nil || *got.Mine != 4 {
		t.Fatalf("mine = %v，想要 4", got.Mine)
	}

	// 同一个人改成 2 分：票数还是 1，平均变 2。
	rec = putRating(t, h, slug, "", 2)
	got = decodeRating(t, rec)
	if got.Count != 1 {
		t.Fatalf("改分不该涨票数，实际 %d", got.Count)
	}
	if got.Average != 2 {
		t.Fatalf("改分后平均 = %.2f，想要 2", got.Average)
	}

	// 库里只有一行——一人一票的判据在这里，不在响应体里。
	var rows int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM demo_ratings r JOIN demos d ON d.id = r.demo_id WHERE d.slug = $1`, slug).
		Scan(&rows); err != nil {
		t.Fatalf("查评分行失败: %v", err)
	}
	if rows != 1 {
		t.Fatalf("同一人只该有一行，实际 %d 行", rows)
	}
}

// 匿名与登录是两个人：匿名一票 + 登录一票 = 2 票。
func TestIntegrationRatingAnonymousAndUserAreDistinct(t *testing.T) {
	h, idSvc, pool := newRatingTestHandler(t)
	author, authorSID := registerVerified(t, pool, idSvc, identity.RegisterInput{
		Email:       "it-rate-b@example.com",
		Password:    "ratetest1234",
		Username:    "it-rate-b",
		DisplayName: "评分作者B",
	})
	t.Cleanup(func() { cleanupAuthor(t, pool, author.Sub) })
	slug := submitHTML(t, h, authorSID, "两个人打分的作品", `<!doctype html><p>b</p>`)
	publishNow(t, pool, slug)

	// 匿名 5 分（同一个 IP）。
	if rec := putRating(t, h, slug, "", 5); rec.Code != http.StatusOK {
		t.Fatalf("匿名打分 = %d，想要 200", rec.Code)
	}
	// 登录用户 3 分。
	_, sid := registerVerified(t, pool, idSvc, identity.RegisterInput{
		Email:       "it-rate-b-voter@example.com",
		Password:    "ratetest1234",
		Username:    "it-rate-b-voter",
		DisplayName: "投票人",
	})
	rec := putRating(t, h, slug, sid, 3)
	if rec.Code != http.StatusOK {
		t.Fatalf("登录打分 = %d，想要 200；body=%s", rec.Code, rec.Body.String())
	}
	got := decodeRating(t, rec)
	if got.Count != 2 {
		t.Fatalf("匿名与登录应是两票，实际 %d 票", got.Count)
	}
	if got.Average != 4 {
		t.Fatalf("(5+3)/2 = 4，实际 %.2f", got.Average)
	}
}

// 撤销：票数减一、平均按剩余重算；没打过也回 200（意图已达成）。
func TestIntegrationDeleteDemoRating(t *testing.T) {
	h, idSvc, pool := newRatingTestHandler(t)
	author, authorSID := registerVerified(t, pool, idSvc, identity.RegisterInput{
		Email:       "it-rate-c@example.com",
		Password:    "ratetest1234",
		Username:    "it-rate-c",
		DisplayName: "撤销作者",
	})
	t.Cleanup(func() { cleanupAuthor(t, pool, author.Sub) })
	slug := submitHTML(t, h, authorSID, "撤销评分的作品", `<!doctype html><p>c</p>`)
	publishNow(t, pool, slug)

	if rec := putRating(t, h, slug, "", 5); rec.Code != http.StatusOK {
		t.Fatalf("打分 = %d，想要 200", rec.Code)
	}
	rec := deleteRating(t, h, slug, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("撤销 = %d，想要 200；body=%s", rec.Code, rec.Body.String())
	}
	got := decodeRating(t, rec)
	if got.Count != 0 || got.Average != 0 {
		t.Fatalf("撤销后 count/avg = %d/%.2f，想要 0/0", got.Count, got.Average)
	}
	if got.Mine != nil {
		t.Fatalf("撤销后 mine 应为 null，实际 %v", *got.Mine)
	}

	// 再撤销一次：幂等的 200。
	if rec := deleteRating(t, h, slug, ""); rec.Code != http.StatusOK {
		t.Fatalf("重复撤销应回 200（幂等），实际 %d", rec.Code)
	}
}

// 分数越界 400：契约的 minimum/maximum 是承诺，服务端要自己判。
func TestIntegrationRatingRejectsOutOfRange(t *testing.T) {
	h, idSvc, pool := newRatingTestHandler(t)
	author, authorSID := registerVerified(t, pool, idSvc, identity.RegisterInput{
		Email:       "it-rate-d@example.com",
		Password:    "ratetest1234",
		Username:    "it-rate-d",
		DisplayName: "越界作者",
	})
	t.Cleanup(func() { cleanupAuthor(t, pool, author.Sub) })
	slug := submitHTML(t, h, authorSID, "越界评分的作品", `<!doctype html><p>d</p>`)
	publishNow(t, pool, slug)

	for _, score := range []int{0, 6, -1, 100} {
		if rec := putRating(t, h, slug, "", score); rec.Code != http.StatusBadRequest {
			t.Fatalf("score=%d 应回 400，实际 %d", score, rec.Code)
		}
	}
	// 一条都没写进去。
	var rows int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM demo_ratings r JOIN demos d ON d.id = r.demo_id WHERE d.slug = $1`, slug).
		Scan(&rows); err != nil {
		t.Fatalf("查评分行失败: %v", err)
	}
	if rows != 0 {
		t.Fatalf("越界分数不该落库，实际 %d 行", rows)
	}
}

// 不可见的作品不能被打分：草稿 404，与详情同一判据。
func TestIntegrationRatingRequiresVisibleDemo(t *testing.T) {
	h, idSvc, pool := newRatingTestHandler(t)
	author, authorSID := registerVerified(t, pool, idSvc, identity.RegisterInput{
		Email:       "it-rate-e@example.com",
		Password:    "ratetest1234",
		Username:    "it-rate-e",
		DisplayName: "草稿作者",
	})
	t.Cleanup(func() { cleanupAuthor(t, pool, author.Sub) })
	// 投稿但不发布：还是 draft。
	slug := submitHTML(t, h, authorSID, "草稿作品", `<!doctype html><p>e</p>`)

	if rec := putRating(t, h, slug, "", 5); rec.Code != http.StatusNotFound {
		t.Fatalf("给草稿打分应回 404，实际 %d；body=%s", rec.Code, rec.Body.String())
	}
	if rec := putRating(t, h, "no-such-demo", "", 5); rec.Code != http.StatusNotFound {
		t.Fatalf("给不存在的作品打分应回 404，实际 %d", rec.Code)
	}
}

// 详情里读得到评分，并且带「我打了几分」。
func TestIntegrationDetailCarriesRating(t *testing.T) {
	h, idSvc, pool := newRatingTestHandler(t)
	author, authorSID := registerVerified(t, pool, idSvc, identity.RegisterInput{
		Email:       "it-rate-f@example.com",
		Password:    "ratetest1234",
		Username:    "it-rate-f",
		DisplayName: "详情作者",
	})
	t.Cleanup(func() { cleanupAuthor(t, pool, author.Sub) })
	slug := submitHTML(t, h, authorSID, "带评分的作品", `<!doctype html><p>f</p>`)
	publishNow(t, pool, slug)

	// 没有任何票时是 0 分 0 票，不是 null。
	rec := getDetailFrom(t, h, slug, "", "203.0.113.7:1234")
	if rec.Code != http.StatusOK {
		t.Fatalf("详情 = %d，想要 200", rec.Code)
	}
	var before struct {
		Rating ratingBody `json:"rating"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &before); err != nil {
		t.Fatalf("详情不是合法 JSON: %v", err)
	}
	if before.Rating.Count != 0 || before.Rating.Average != 0 || before.Rating.Mine != nil {
		t.Fatalf("无票时 rating = %+v，想要 0/0/null", before.Rating)
	}

	if rec := putRating(t, h, slug, "", 5); rec.Code != http.StatusOK {
		t.Fatalf("打分 = %d", rec.Code)
	}

	rec = getDetailFrom(t, h, slug, "", "203.0.113.7:1234")
	var after struct {
		Rating ratingBody `json:"rating"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &after); err != nil {
		t.Fatalf("详情不是合法 JSON: %v", err)
	}
	if after.Rating.Count != 1 || after.Rating.Average != 5 {
		t.Fatalf("详情里的评分 = %+v，想要 1 票 5 分", after.Rating)
	}
	if after.Rating.Mine == nil || *after.Rating.Mine != 5 {
		t.Fatalf("同一 IP 再读详情应当看到自己打的 5 分，mine = %v", after.Rating.Mine)
	}
}
