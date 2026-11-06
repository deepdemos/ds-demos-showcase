package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/identity"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/model"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/platform/catalog"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

// syncFixture 是同步用例的上游 payload（形状与 models.dev 一致，体量缩到 3 条）。
//
// 用 fixture 而不是真拉网：整包 5.96 MB，CI 里断网就红，而且是那种
// 「昨天还好好的」红。「从字节解析」这条路径由它覆盖；网络层用 httptest 覆盖。
const syncFixture = `{
  "deepseek": {
    "id": "deepseek",
    "name": "DeepSeek",
    "models": {
      "deepseek-flash": {
        "id": "deepseek-flash",
        "name": "DeepSeek V4.1 Flash",
        "canonical_model_id": "deepseek/deepseek-v4.1-flash",
        "release_date": "2026-10-17",
        "limit": {"context": 1048576, "output": 393216},
        "cost": {"input": 0.15, "output": 0.6}
      }
    }
  },
  "bothub": {
    "id": "bothub",
    "name": "Bothub",
    "models": {
      "glm-5": {"id": "glm-5", "name": "GLM-5"},
      "glm-5-air": {"id": "glm-5-air", "name": "GLM-5 Air", "canonical_model_id": "zhipuai/glm-5-air"}
    }
  }
}`

// newSyncHandler 造一个装了模型域服务的 handler，上游指向 srv。
func newSyncHandler(t *testing.T, pool *pgxpool.Pool, idSvc *identity.Service, srv *httptest.Server) *Handler {
	t.Helper()
	q := store.New(pool)
	fetcher := catalog.NewFetcher(srv.URL, srv.Client())
	return New(pool, q, idSvc, nil, nil).WithModels(model.New(pool, q, fetcher, nil))
}

// resetCatalog 清空镜像与同步状态，让用例从确定状态开始。
//
// 必须清 `model_source_sync`：`Sync` 会读上一次的 `payload_hash` 来决定「没变就不重写」，
// 留着上一轮的行会让第二次调用直接 skipped——测试就测不到写入。
func resetCatalog(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `DELETE FROM model_catalog_items WHERE source = 'models.dev'`); err != nil {
		t.Fatalf("清空镜像失败: %v", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM model_source_sync WHERE source = 'models.dev'`); err != nil {
		t.Fatalf("清空同步状态失败: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM model_catalog_items WHERE source = 'models.dev'`)
		_, _ = pool.Exec(context.Background(), `DELETE FROM model_source_sync WHERE source = 'models.dev'`)
	})
}

func postSync(t *testing.T, h *Handler, sid string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/models/sync", nil)
	if sid != "" {
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: sid})
	}
	rec := httptest.NewRecorder()
	NewRouter(h, nil, nil).ServeHTTP(rec, req)
	return rec
}

// 一次同步把 payload 摊成镜像行，并记账成 ok。
func TestIntegrationSyncWritesCatalog(t *testing.T) {
	pool := integrationPool(t)
	idSvc := integrationIdentity(t, pool)
	resetCatalog(t, pool)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(syncFixture))
	}))
	defer srv.Close()

	h := newSyncHandler(t, pool, idSvc, srv)
	sid := mkAdmin(t, pool, idSvc, "sync1")

	rec := postSync(t, h, sid)
	if rec.Code != http.StatusOK {
		t.Fatalf("同步应回 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	var got struct {
		Source   string `json:"source"`
		Status   string `json:"status"`
		Items    int64  `json:"items"`
		Skipped  bool   `json:"skipped"`
		SyncedAt string `json:"syncedAt"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("响应不是合法 JSON: %v", err)
	}
	if got.Items != 3 || got.Skipped || got.Status != "ok" || got.Source != "models.dev" {
		t.Fatalf("响应体 = %+v，想要 items=3 skipped=false status=ok source=models.dev", got)
	}
	if got.SyncedAt == "" {
		t.Fatal("syncedAt 不该为空")
	}

	var rows int64
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM model_catalog_items WHERE source = 'models.dev'`).Scan(&rows); err != nil {
		t.Fatalf("查镜像行数失败: %v", err)
	}
	if rows != 3 {
		t.Fatalf("镜像行数 = %d，想要 3", rows)
	}

	var status string
	var itemCount int64
	if err := pool.QueryRow(context.Background(),
		`SELECT status, item_count FROM model_source_sync WHERE source = 'models.dev'`).
		Scan(&status, &itemCount); err != nil {
		t.Fatalf("查同步状态失败: %v", err)
	}
	if status != "ok" || itemCount != 3 {
		t.Fatalf("同步状态 = %q/%d，想要 ok/3", status, itemCount)
	}
}

// payload 没变就不重写：行 id 必须原封不动，否则「整表重写」每天白白漂移一次。
func TestIntegrationSyncSkipsUnchangedPayload(t *testing.T) {
	pool := integrationPool(t)
	idSvc := integrationIdentity(t, pool)
	resetCatalog(t, pool)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(syncFixture))
	}))
	defer srv.Close()

	h := newSyncHandler(t, pool, idSvc, srv)
	sid := mkAdmin(t, pool, idSvc, "sync2")

	if rec := postSync(t, h, sid); rec.Code != http.StatusOK {
		t.Fatalf("第一次同步应回 200，实际 %d", rec.Code)
	}
	var before []int64
	rows, err := pool.Query(context.Background(),
		`SELECT id FROM model_catalog_items WHERE source = 'models.dev' ORDER BY id`)
	if err != nil {
		t.Fatalf("查 id 失败: %v", err)
	}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("扫 id 失败: %v", err)
		}
		before = append(before, id)
	}
	rows.Close()

	rec := postSync(t, h, sid)
	if rec.Code != http.StatusOK {
		t.Fatalf("第二次同步应回 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	var got struct {
		Skipped bool  `json:"skipped"`
		Items   int64 `json:"items"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if !got.Skipped {
		t.Fatal("payload 没变时应当 skipped=true")
	}
	if got.Items != 3 {
		t.Fatalf("skipped 时 items 应沿用上一次的 3，实际 %d", got.Items)
	}

	var after []int64
	rows2, err := pool.Query(context.Background(),
		`SELECT id FROM model_catalog_items WHERE source = 'models.dev' ORDER BY id`)
	if err != nil {
		t.Fatalf("查 id 失败: %v", err)
	}
	for rows2.Next() {
		var id int64
		if err := rows2.Scan(&id); err != nil {
			t.Fatalf("扫 id 失败: %v", err)
		}
		after = append(after, id)
	}
	rows2.Close()
	if len(before) != len(after) || (len(before) > 0 && before[0] != after[0]) {
		t.Fatalf("skipped 这一轮不该重写镜像：before=%v after=%v", before, after)
	}
}

// 上游挂了要保留旧镜像：清空比不更新更糟。
func TestIntegrationSyncKeepsMirrorOnFailure(t *testing.T) {
	pool := integrationPool(t)
	idSvc := integrationIdentity(t, pool)
	resetCatalog(t, pool)

	healthy := true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if !healthy {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(syncFixture))
	}))
	defer srv.Close()

	h := newSyncHandler(t, pool, idSvc, srv)
	sid := mkAdmin(t, pool, idSvc, "sync3")

	if rec := postSync(t, h, sid); rec.Code != http.StatusOK {
		t.Fatalf("先要成功一次，实际 %d", rec.Code)
	}
	var successAt string
	if err := pool.QueryRow(context.Background(),
		`SELECT last_success_at::text FROM model_source_sync WHERE source = 'models.dev'`).
		Scan(&successAt); err != nil {
		t.Fatalf("读 last_success_at 失败: %v", err)
	}

	healthy = false
	rec := postSync(t, h, sid)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("上游 500 应回 502，实际 %d：%s", rec.Code, rec.Body.String())
	}

	var rows int64
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM model_catalog_items WHERE source = 'models.dev'`).Scan(&rows); err != nil {
		t.Fatalf("查镜像行数失败: %v", err)
	}
	if rows != 3 {
		t.Fatalf("失败不该清空镜像：行数 = %d，想要 3", rows)
	}

	var status, errText, successAfter string
	if err := pool.QueryRow(context.Background(),
		`SELECT status, error, last_success_at::text FROM model_source_sync WHERE source = 'models.dev'`).
		Scan(&status, &errText, &successAfter); err != nil {
		t.Fatalf("查同步状态失败: %v", err)
	}
	if status != "failed" {
		t.Fatalf("状态 = %q，想要 failed", status)
	}
	if errText == "" {
		t.Fatal("失败原因要写进 error 列，否则运维只能靠猜")
	}
	if successAfter != successAt {
		t.Fatalf("last_success_at 不该被失败覆盖：%q → %q", successAt, successAfter)
	}
}

// 空目录当成失败：一次「成功」的空同步会清空镜像而运行时毫无异常。
func TestIntegrationSyncRejectsEmptyCatalog(t *testing.T) {
	pool := integrationPool(t)
	idSvc := integrationIdentity(t, pool)
	resetCatalog(t, pool)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	h := newSyncHandler(t, pool, idSvc, srv)
	sid := mkAdmin(t, pool, idSvc, "sync4")

	rec := postSync(t, h, sid)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("空目录应回 502，实际 %d：%s", rec.Code, rec.Body.String())
	}
}

// 权限：未登录 401、普通用户 403。
func TestIntegrationSyncRequiresPermission(t *testing.T) {
	pool := integrationPool(t)
	idSvc := integrationIdentity(t, pool)
	resetCatalog(t, pool)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(syncFixture))
	}))
	defer srv.Close()

	h := newSyncHandler(t, pool, idSvc, srv)

	if rec := postSync(t, h, ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("未登录应回 401，实际 %d", rec.Code)
	}

	acc, sid := registerVerified(t, pool, idSvc, identity.RegisterInput{
		Email:       "it-sync-user@example.com",
		Password:    "synctest1234",
		Username:    "it-sync-user",
		DisplayName: "普通用户",
	})
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE sub = $1`, acc.Sub)
		_, _ = pool.Exec(context.Background(), `DELETE FROM accounts WHERE sub = $1`, acc.Sub)
	})
	if rec := postSync(t, h, sid); rec.Code != http.StatusForbidden {
		t.Fatalf("无 model.import 权限应回 403，实际 %d", rec.Code)
	}
}

// 未配数据库时回 503，而不是 panic。
func TestSyncWithoutDatabaseIs503(t *testing.T) {
	h := New(nil, nil, nil, nil, nil)
	rec := postSync(t, h, "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("未配库应回 503，实际 %d", rec.Code)
	}
}

// 同步失败要能区分「有人在同步」与「同步失败」：前者 409，后者 502。
func TestSyncBusyIsConflict(t *testing.T) {
	if !errors.Is(model.ErrSyncBusy, model.ErrSyncBusy) {
		t.Fatal("哨兵不可比较")
	}
	// 直接验 handler 的映射：拿一个真实锁在别的连接上，再让 handler 去抢。
	pool := integrationPool(t)
	idSvc := integrationIdentity(t, pool)
	resetCatalog(t, pool)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(syncFixture))
	}))
	defer srv.Close()
	h := newSyncHandler(t, pool, idSvc, srv)
	sid := mkAdmin(t, pool, idSvc, "sync5")

	// 用一条独立连接持锁，模拟「另一个实例正在同步」。
	conn, err := pool.Acquire(context.Background())
	if err != nil {
		t.Fatalf("取连接失败: %v", err)
	}
	defer conn.Release()
	var acquired bool
	if err := conn.QueryRow(context.Background(),
		`SELECT pg_try_advisory_lock(7312004411)`).Scan(&acquired); err != nil {
		t.Fatalf("持锁失败: %v", err)
	}
	if !acquired {
		t.Fatal("另一条连接已持有这把锁，用例环境不干净")
	}
	defer func() {
		_, _ = conn.Exec(context.Background(), `SELECT pg_advisory_unlock(7312004411)`)
	}()

	rec := postSync(t, h, sid)
	if rec.Code != http.StatusConflict {
		t.Fatalf("锁被占用时应回 409，实际 %d：%s", rec.Code, rec.Body.String())
	}
}

// 契约里 source 恒为 models.dev；httpapi 抄了一份常量（不 import platform 只为拿字符串）。
func TestCatalogSourceMatchesPlatform(t *testing.T) {
	if catalogSource != catalog.Source {
		t.Fatalf("httpapi 的 %q 与 platform 的 %q 不一致", catalogSource, catalog.Source)
	}
}
