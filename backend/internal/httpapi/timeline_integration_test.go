package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/api"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/demo"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/identity"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/platform/storage"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

// 作品动态的**读路径**：只有作者能读、倒序、带操作人显示名、擦除之后连作者也读不到。
//
// 写入侧另有测试钉着（`lifecycle_trail_integration_test.go` 断言下架/恢复会写进出，
// `TestIntegrationSoftDeleteWritesAuditOnly` 断言软删只写审计不写时间线）。
// 这里直接往表里插行：读路径的判据是「谁能看、按什么顺序、字段对不对」，
// 让每条用例先跑一遍审核发布链路只会把失败原因搅在一起。

func newTimelineHandler(t *testing.T) (*Handler, *identity.Service, *pgxpool.Pool) {
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

func getTimeline(t *testing.T, h *Handler, slug, sid string) (int, api.DemoTimeline) {
	t.Helper()
	rec := getWithSession(t, h, "/api/v1/demos/"+slug+"/timeline", sid)
	var out api.DemoTimeline
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("解析动态失败: %v；响应体：%s", err, rec.Body.String())
		}
	}
	return rec.Code, out
}

// insertTimeline 直接写一行动态，返回 demo 的 id。
func insertTimeline(t *testing.T, pool *pgxpool.Pool, slug, kind, message string, actorUserID *int64, at time.Time) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `
INSERT INTO demo_timeline (demo_id, kind, message, actor_id, created_at)
SELECT d.id, $2, NULLIF($3, ''), $4, $5 FROM demos d WHERE d.slug = $1`,
		slug, kind, message, actorUserID, at); err != nil {
		t.Fatalf("写时间线（%s）失败: %v", kind, err)
	}
}

func userIDForSub(t *testing.T, pool *pgxpool.Pool, sub string) int64 {
	t.Helper()
	var id int64
	if err := pool.QueryRow(context.Background(),
		`SELECT id FROM users WHERE sub = $1`, sub).Scan(&id); err != nil {
		t.Fatalf("取 users.id 失败: %v", err)
	}
	return id
}

func TestIntegrationDemoTimelineAuthorOnly(t *testing.T) {
	h, idSvc, pool := newTimelineHandler(t)

	author, sid := registerVerified(t, pool, idSvc, identity.RegisterInput{
		Email: "timeline-author@example.com", Password: "timeline-author-pw",
		Username: "timeline-author", DisplayName: "动态作者",
	})
	t.Cleanup(func() { cleanupAuthor(t, pool, author.Sub) })
	stranger, otherSid := registerVerified(t, pool, idSvc, identity.RegisterInput{
		Email: "timeline-stranger@example.com", Password: "timeline-stranger-pw",
		Username: "timeline-stranger", DisplayName: "路人",
	})
	t.Cleanup(func() { cleanupAuthor(t, pool, stranger.Sub) })

	slug := submitHTML(t, h, sid, "动态测试", "<h1>hi</h1>")

	authorUserID := userIDForSub(t, pool, author.Sub)
	now := time.Now()
	// 两条：先通过（有操作人），再发布（系统动作，没有操作人）。
	insertTimeline(t, pool, slug, "approve", "内容合格", &authorUserID, now.Add(-2*time.Minute))
	insertTimeline(t, pool, slug, "publish", "发布到 deep", nil, now.Add(-1*time.Minute))

	code, got := getTimeline(t, h, slug, sid)
	if code != http.StatusOK {
		t.Fatalf("作者读动态 = %d，想要 200", code)
	}
	if len(got.Items) != 2 {
		t.Fatalf("动态条数 = %d，想要 2（%+v）", len(got.Items), got.Items)
	}
	// 倒序：后发生的在前。
	if got.Items[0].Kind != "publish" || got.Items[1].Kind != "approve" {
		t.Fatalf("顺序 = [%s, %s]，想要 [publish, approve]（按时间倒序）",
			got.Items[0].Kind, got.Items[1].Kind)
	}
	// 发布那条没有操作人 → actorName 为 null（不要用「系统」掩盖事实）。
	if got.Items[0].ActorName != nil {
		t.Fatalf("系统动作的 actorName = %q，想要 null", *got.Items[0].ActorName)
	}
	if m := got.Items[0].Message; m == nil || *m != "发布到 deep" {
		t.Fatalf("发布那条的 message = %v，想要「发布到 deep」", m)
	}
	// 通过那条带操作人显示名。
	if got.Items[1].ActorName == nil || *got.Items[1].ActorName != "动态作者" {
		t.Fatalf("通过那条的 actorName = %v，想要「动态作者」", got.Items[1].ActorName)
	}
	if got.Items[0].At.IsZero() || !got.Items[0].At.After(got.Items[1].At) {
		t.Fatalf("时刻不对：publish=%v approve=%v", got.Items[0].At, got.Items[1].At)
	}

	// 非作者：404（不是 403——403 会承认这个 slug 存在）。
	if code, _ := getTimeline(t, h, slug, otherSid); code != http.StatusNotFound {
		t.Fatalf("非作者读动态 = %d，想要 404", code)
	}
	// 未登录：同样 404，不区分「你需要登录」。
	if code, _ := getTimeline(t, h, slug, ""); code != http.StatusNotFound {
		t.Fatalf("未登录读动态 = %d，想要 404", code)
	}
	// 审核员也读不到：D1 的裁决是只给作者。
	reviewerSID := mkAdmin(t, pool, idSvc, "timeline-mod")
	if code, _ := getTimeline(t, h, slug, reviewerSID); code != http.StatusNotFound {
		t.Fatalf("审核员读动态 = %d，想要 404（D1：只给作者）", code)
	}
}

// 擦除之后对作者也不存在：与详情、预览同一口径。
func TestIntegrationDemoTimelineGoneAfterErase(t *testing.T) {
	h, idSvc, pool := newTimelineHandler(t)

	author, sid := registerVerified(t, pool, idSvc, identity.RegisterInput{
		Email: "timeline-erase@example.com", Password: "timeline-erase-pw",
		Username: "timeline-erase", DisplayName: "擦除作者",
	})
	t.Cleanup(func() { cleanupAuthor(t, pool, author.Sub) })

	slug := submitHTML(t, h, sid, "会被擦除的作品", "<h1>bye</h1>")
	// 擦除必须先软删：`ck_demos_time_order` 要求 erased_at >= deleted_at，
	// 而且 erased_at 非空时 deleted_at 也必须非空。时间都取 GREATEST(now(), created_at)，
	// 与生产写入同口径（时钟回拨时不会把这条约束撞红）。
	if _, err := pool.Exec(context.Background(), `
UPDATE demos
SET deleted_at = GREATEST(now(), created_at),
    erased_at  = GREATEST(now(), created_at),
    updated_at = GREATEST(now(), created_at),
    title = NULL, description = NULL
WHERE slug = $1`, slug); err != nil {
		t.Fatalf("标记擦除失败: %v", err)
	}

	if code, _ := getTimeline(t, h, slug, sid); code != http.StatusNotFound {
		t.Fatalf("擦除后作者读动态 = %d，想要 404", code)
	}
}

// 没有动作的作品返回空数组（不是 null、不是 404）：契约里 items 是必填数组，
// 前端据此渲染「还没有动作」，而不是当成错误。
func TestIntegrationDemoTimelineEmpty(t *testing.T) {
	h, idSvc, pool := newTimelineHandler(t)

	author, sid := registerVerified(t, pool, idSvc, identity.RegisterInput{
		Email: "timeline-empty@example.com", Password: "timeline-empty-pw",
		Username: "timeline-empty", DisplayName: "空动态作者",
	})
	t.Cleanup(func() { cleanupAuthor(t, pool, author.Sub) })

	slug := submitHTML(t, h, sid, "还没有动作", "<h1>fresh</h1>")
	rec := getWithSession(t, h, "/api/v1/demos/"+slug+"/timeline", sid)
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d，想要 200", rec.Code)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if string(raw["items"]) != "[]" {
		t.Fatalf("items = %s，想要 []（空数组而不是 null）", raw["items"])
	}
}
