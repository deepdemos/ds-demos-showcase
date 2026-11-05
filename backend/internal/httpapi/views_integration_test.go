package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/demo"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/identity"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/platform/storage"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

// 浏览计数端点的三件事：真的记账、同一天不重复、计数能从详情里读出来。
//
// 这三条缺一条这个功能就是空的：只记账不显示，等于没人能验收；
// 只显示不记账，数字永远 0；不重复那条是 DEMO-041 的原文要求。
func TestIntegrationPostDemoView(t *testing.T) {
	pool := integrationPool(t)
	q := store.New(pool)
	idSvc := integrationIdentity(t, pool)
	files, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("初始化存储失败: %v", err)
	}
	demoSvc := demo.New(pool, q, files, storage.DefaultLimits(), 200<<20, 0, nil)
	h := New(pool, q, idSvc, nil, demoSvc)

	author, authorSID := registerVerified(t, pool, idSvc, identity.RegisterInput{
		Email:       "it-view@example.com",
		Password:    "viewtest1234",
		Username:    "it-view",
		DisplayName: "浏览测试",
	})
	t.Cleanup(func() { cleanupAuthor(t, pool, author.Sub) })

	slug := submitHTML(t, h, authorSID, "浏览计数作品", `<!doctype html><p>view</p>`)
	publishNow(t, pool, slug)

	// 1) 访客看一次：204，计数 1。
	if rec := postView(t, h, slug); rec.Code != http.StatusNoContent {
		t.Fatalf("记浏览 = %d，想要 204；响应体：%s", rec.Code, rec.Body.String())
	}
	if got := viewCountInDB(t, pool, slug); got != 1 {
		t.Fatalf("浏览数应为 1，实际 %d", got)
	}

	// 2) 同一个 IP 再看：204，但计数不变。
	if rec := postView(t, h, slug); rec.Code != http.StatusNoContent {
		t.Fatalf("重复记浏览 = %d，想要 204", rec.Code)
	}
	if got := viewCountInDB(t, pool, slug); got != 1 {
		t.Fatalf("同一天重复浏览不该涨，实际 %d", got)
	}

	// 3) 详情里读得到这个数字——计数不显示就没人能验收。
	detail := getDemoAs(t, h, slug, "")
	if detail.Code != http.StatusOK {
		t.Fatalf("详情 = %d，想要 200", detail.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(detail.Body.Bytes(), &body); err != nil {
		t.Fatalf("解析详情失败: %v", err)
	}
	if body["viewCount"] != float64(1) {
		t.Fatalf("详情 viewCount = %v，想要 1", body["viewCount"])
	}
	if body["downloadCount"] != float64(0) {
		t.Fatalf("详情 downloadCount = %v，想要 0", body["downloadCount"])
	}

	// 4) 不存在的作品也回 204：这个端点被页面自动调用，不该让「作品刚被删」
	//    在前端控制台里刷一片红。真假的可见性判定在详情端点上。
	if rec := postView(t, h, "no-such-demo-slug"); rec.Code != http.StatusNoContent {
		t.Fatalf("不存在的作品 = %d，想要 204", rec.Code)
	}

	// 5) 未发布的作品不计数（作者自己反复打开草稿不该刷高浏览量）。
	draftSlug := submitHTML(t, h, authorSID, "草稿不计数", `<!doctype html><p>draft</p>`)
	if rec := postView(t, h, draftSlug); rec.Code != http.StatusNoContent {
		t.Fatalf("草稿记浏览 = %d，想要 204", rec.Code)
	}
	if got := viewCountInDB(t, pool, draftSlug); got != 0 {
		t.Fatalf("草稿浏览数应为 0，实际 %d", got)
	}
}

// TestIntegrationPostDemoViewRouteUsesPOST 钉住路由形状：契约是 POST。
// 写成 GET 的话，浏览器预取/爬虫就会把浏览量刷起来，而这正是分出来独立端点的原因。
func TestIntegrationPostDemoViewRouteUsesPOST(t *testing.T) {
	router := NewRouter(&stub{}, nil, nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/demos/demo-slug/view", nil))
	if rec.Code == http.StatusNotFound || rec.Code == http.StatusMethodNotAllowed {
		t.Fatalf("POST /api/v1/demos/{slug}/view 路由没挂上，状态码 %d", rec.Code)
	}
	// GET 同一个路径必须是 405（MethodNotAllowed），不能悄悄成功。
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/demos/demo-slug/view", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET /view 应当是 405，实际 %d", rec.Code)
	}
}

func postView(t *testing.T, h *Handler, slug string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	NewRouter(h, nil, nil).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/demos/"+slug+"/view", nil))
	return rec
}

func viewCountInDB(t *testing.T, pool *pgxpool.Pool, slug string) int64 {
	t.Helper()
	var n int64
	if err := pool.QueryRow(context.Background(),
		`SELECT COALESCE(st.view_count, 0) FROM demos d
		 LEFT JOIN demo_stats st ON st.demo_id = d.id WHERE d.slug = $1`, slug).Scan(&n); err != nil {
		t.Fatalf("读浏览数失败: %v", err)
	}
	return n
}
