package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/model"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/platform/catalog"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

// newDueService 造一个服务，上游指向 srv；hits 记录上游被请求了几次。
func newDueService(
	t *testing.T,
	pool *pgxpool.Pool,
	url string,
	hits *int32,
) *model.Service {
	t.Helper()
	q := store.New(pool)
	fetcher := catalog.NewFetcher(url, nil)
	_ = hits
	return model.New(pool, q, fetcher, nil)
}

// setSyncState 直接把记账行写成某个状态，省去「先失败一次」的等待。
func setSyncState(t *testing.T, pool *pgxpool.Pool, status string, attemptAgo, successAgo time.Duration) {
	t.Helper()
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `
		INSERT INTO model_source_sync (source, last_attempt_at, last_success_at, payload_hash, item_count, status, error)
		VALUES ('models.dev', now() - $1::interval, now() - $2::interval, 'x', 1, $3, '')
		ON CONFLICT (source) DO UPDATE SET
			last_attempt_at = now() - $1::interval,
			last_success_at = now() - $2::interval,
			status = $3,
			error = ''`,
		attemptAgo.String(), successAgo.String(), status); err != nil {
		t.Fatalf("写同步状态失败: %v", err)
	}
}

// 上一次失败时，间隔到了 retryGap 就重试——不能等满一个 interval（默认 24 小时），
// 那恰好是最需要自动同步的时候它不动。
func TestIntegrationSyncRetriesSoonAfterFailure(t *testing.T) {
	pool := integrationPool(t)
	resetCatalog(t, pool)

	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		_, _ = w.Write([]byte(syncFixture))
	}))
	defer srv.Close()

	svc := newDueService(t, pool, srv.URL, &hits)

	// 11 分钟前失败过（超过 retryGap），即使 last_success_at 是刚才，也该重试。
	setSyncState(t, pool, model.StatusFailed, 11*time.Minute, time.Minute)
	if err := svc.SyncIfDue(context.Background()); err != nil {
		t.Fatalf("到点重试不该报错: %v", err)
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("失败超过 retryGap 后应当重试一次，实际上游被请求 %d 次", got)
	}

	// 刚刚失败过（retryGap 之内），不该再打上游。
	atomic.StoreInt32(&hits, 0)
	setSyncState(t, pool, model.StatusFailed, time.Minute, time.Minute)
	if err := svc.SyncIfDue(context.Background()); err != nil {
		t.Fatalf("未到重试间隔不该报错: %v", err)
	}
	if got := atomic.LoadInt32(&hits); got != 0 {
		t.Fatalf("retryGap 之内不该重试，实际上游被请求 %d 次", got)
	}
}

// 成功之后按 interval 走：间隔没到就不动，避免每次 5 分钟的 tick 都重下一遍 6 MB。
func TestIntegrationSyncWaitsIntervalAfterSuccess(t *testing.T) {
	pool := integrationPool(t)
	resetCatalog(t, pool)

	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		_, _ = w.Write([]byte(syncFixture))
	}))
	defer srv.Close()

	svc := newDueService(t, pool, srv.URL, &hits)
	setSyncState(t, pool, model.StatusOK, time.Minute, time.Minute)
	if err := svc.SyncIfDue(context.Background()); err != nil {
		t.Fatalf("未到点不该报错: %v", err)
	}
	if got := atomic.LoadInt32(&hits); got != 0 {
		t.Fatalf("成功后间隔没到不该重下，实际上游被请求 %d 次", got)
	}

	// 把间隔改成 0.02 小时（72 秒），last_success_at 推到 2 分钟前 → 到点。
	// 先写一行覆盖值：`site_settings` 默认没有这一行，`UPDATE` 会静默改到 0 行，
	// 于是「设了 0.02 小时」实际上还是 24 小时——第一版就是这么假绿的。
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO site_settings (key, scope, value_type, value)
		VALUES ('modelsdev.sync_interval_hours', 'site', 'number', '0.02'::jsonb)
		ON CONFLICT (key) DO UPDATE SET value = '0.02'::jsonb`); err != nil {
		t.Fatalf("改间隔失败: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `
			DELETE FROM site_settings WHERE key = 'modelsdev.sync_interval_hours'`)
	})
	setSyncState(t, pool, model.StatusOK, 2*time.Minute, 2*time.Minute)
	if err := svc.SyncIfDue(context.Background()); err != nil {
		t.Fatalf("到点同步不该报错: %v", err)
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("间隔到了应当同步一次，实际上游被请求 %d 次", got)
	}
}

// 开关关掉时什么都不做，且回 ErrSyncDisabled（调用方据此静默）。
func TestIntegrationSyncRespectsSwitch(t *testing.T) {
	pool := integrationPool(t)
	resetCatalog(t, pool)

	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		_, _ = w.Write([]byte(syncFixture))
	}))
	defer srv.Close()

	if _, err := pool.Exec(context.Background(), `
		INSERT INTO site_settings (key, scope, value_type, value)
		VALUES ('modelsdev.sync_enabled', 'site', 'boolean', 'false'::jsonb)
		ON CONFLICT (key) DO UPDATE SET value = 'false'::jsonb`); err != nil {
		t.Fatalf("关开关失败: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `
			DELETE FROM site_settings WHERE key = 'modelsdev.sync_enabled'`)
	})

	svc := newDueService(t, pool, srv.URL, &hits)
	err := svc.SyncIfDue(context.Background())
	if err == nil {
		t.Fatal("开关关掉时应当回 ErrSyncDisabled，而不是静默 nil")
	}
	if got := atomic.LoadInt32(&hits); got != 0 {
		t.Fatalf("开关关掉时不该请求上游，实际 %d 次", got)
	}
}
