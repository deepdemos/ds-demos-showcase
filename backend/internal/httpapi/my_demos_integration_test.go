package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/api"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/identity"
	"github.com/jackc/pgx/v5/pgxpool"
)

// newMyDemosHandler 复用列表测试那套装配：真实库 + 真实 identity + 真实存储。
func newMyDemosHandler(t *testing.T) (*Handler, *identity.Service, *pgxpool.Pool) {
	t.Helper()
	return newSortHandler(t)
}

func myDemos(t *testing.T, h *Handler, sid, query string) (*httptest.ResponseRecorder, api.MyDemoList) {
	t.Helper()
	rec := getWithSession(t, h, "/api/v1/me/demos"+query, sid)
	if rec.Code != http.StatusOK {
		t.Fatalf("我的作品 %s = %d，想要 200；响应体：%s", query, rec.Code, rec.Body.String())
	}
	var body api.MyDemoList
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("解析我的作品失败: %v；响应体：%s", err, rec.Body.String())
	}
	return rec, body
}

func mkAuthorWithDemos(t *testing.T, h *Handler, idSvc *identity.Service, pool *pgxpool.Pool, tag string, n int) (identity.Account, string) {
	t.Helper()
	acc, sid := registerVerified(t, pool, idSvc, identity.RegisterInput{
		Email:       fmt.Sprintf("it-mydemos-%s@example.com", tag),
		Password:    "it-password-1234",
		Username:    fmt.Sprintf("it-mydemos-%s", tag),
		DisplayName: "我的作品作者",
	})
	for i := 1; i <= n; i++ {
		slug := submitHTML(t, h, sid, fmt.Sprintf("%s 的第 %d 件", tag, i), `<!doctype html><p>x</p>`)
		// updated_at 单调递增，翻页顺序才是确定的；用固定间隔而不是 now()，免得同毫秒并列。
		if _, err := pool.Exec(context.Background(),
			`UPDATE demos SET updated_at = created_at + make_interval(secs => $2) WHERE slug = $1`,
			slug, i); err != nil {
			t.Fatalf("推时间失败: %v", err)
		}
	}
	t.Cleanup(func() { cleanupAuthor(t, pool, acc.Sub) })
	return acc, sid
}

func setDemoStates(t *testing.T, pool *pgxpool.Pool, slug, lifecycle, moderation string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		`UPDATE demos SET lifecycle_state = $2, moderation_state = $3 WHERE slug = $1`,
		slug, lifecycle, moderation); err != nil {
		t.Fatalf("改状态失败: %v", err)
	}
}

// TestIntegrationMyDemosListsAllStates 是这条端点的存在理由：作者必须看得到
// 草稿 / 待审 / 已驳回 / 已发布 / 已下架，而不只是「已上架」。
func TestIntegrationMyDemosListsAllStates(t *testing.T) {
	h, idSvc, pool := newMyDemosHandler(t)
	_, sid := mkAuthorWithDemos(t, h, idSvc, pool, "states", 5)

	var slugs []string
	rows, err := pool.Query(context.Background(),
		`SELECT slug FROM demos WHERE author_id = (SELECT id FROM users WHERE sub = $1) ORDER BY id`,
		mustAccountSub(t, h, sid))
	if err != nil {
		t.Fatalf("取 slug 失败: %v", err)
	}
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatalf("扫描 slug 失败: %v", err)
		}
		slugs = append(slugs, s)
	}
	rows.Close()
	if len(slugs) != 5 {
		t.Fatalf("造出 %d 件，想要 5 件", len(slugs))
	}

	setDemoStates(t, pool, slugs[0], "draft", "pending")
	setDemoStates(t, pool, slugs[1], "draft", "rejected")
	setDemoStates(t, pool, slugs[2], "published", "approved")
	publishNow(t, pool, slugs[2])
	setDemoStates(t, pool, slugs[3], "archived", "approved")
	setDemoStates(t, pool, slugs[4], "draft", "pending")

	_, body := myDemos(t, h, sid, "")
	if body.Total != 5 || len(body.Items) != 5 {
		t.Fatalf("我的作品 total/items = %d/%d，想要 5/5", body.Total, len(body.Items))
	}
	got := map[string][2]string{}
	for _, item := range body.Items {
		got[item.Slug] = [2]string{string(item.LifecycleState), string(item.ModerationState)}
	}
	want := map[string][2]string{
		slugs[0]: {"draft", "pending"},
		slugs[1]: {"draft", "rejected"},
		slugs[2]: {"published", "approved"},
		slugs[3]: {"archived", "approved"},
		slugs[4]: {"draft", "pending"},
	}
	for slug, w := range want {
		if got[slug] != w {
			t.Fatalf("%s 的状态 = %v，想要 %v", slug, got[slug], w)
		}
	}
}

// TestIntegrationMyDemosHidesOtherAuthors 是权限边界：这个端点按 session 认人，
// 不接受任何调用方传进来的 id。
func TestIntegrationMyDemosHidesOtherAuthors(t *testing.T) {
	h, idSvc, pool := newMyDemosHandler(t)
	_, sidA := mkAuthorWithDemos(t, h, idSvc, pool, "owner-a", 1)
	_, sidB := mkAuthorWithDemos(t, h, idSvc, pool, "owner-b", 1)

	_, bodyA := myDemos(t, h, sidA, "")
	_, bodyB := myDemos(t, h, sidB, "")
	if len(bodyA.Items) != 1 || len(bodyB.Items) != 1 {
		t.Fatalf("各自应有 1 件，实际 A=%d B=%d", len(bodyA.Items), len(bodyB.Items))
	}
	if bodyA.Items[0].Slug == bodyB.Items[0].Slug {
		t.Fatalf("两个作者拿到了同一件作品 %s", bodyA.Items[0].Slug)
	}
}

func TestIntegrationMyDemosRequiresLogin(t *testing.T) {
	h, _, _ := newMyDemosHandler(t)
	rec := getWithSession(t, h, "/api/v1/me/demos", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("未登录 = %d，想要 401；响应体：%s", rec.Code, rec.Body.String())
	}
}

// TestIntegrationMyDemosHidesErased：擦除对外一律不存在，对作者也不留入口。
func TestIntegrationMyDemosHidesErased(t *testing.T) {
	h, idSvc, pool := newMyDemosHandler(t)
	_, sid := mkAuthorWithDemos(t, h, idSvc, pool, "erased", 2)

	_, before := myDemos(t, h, sid, "")
	gone := before.Items[0].Slug
	// 擦除要同时满足两条约束：时刻有序（ck_demos_time_order），
	// 以及「擦除后不留任何可识别信息」（ck_demos_erased 要求标题/简介/封面全为 NULL）。
	if _, err := pool.Exec(context.Background(), `
UPDATE demos SET deleted_at = GREATEST(now(), created_at),
                 erased_at  = GREATEST(now(), created_at) + interval '1 second',
                 title = NULL, description = NULL,
                 cover_url = NULL, cover_thumb_url = NULL, cover_inline = NULL
 WHERE slug = $1`, gone); err != nil {
		t.Fatalf("擦除失败: %v", err)
	}
	_, after := myDemos(t, h, sid, "")
	if after.Total != 1 {
		t.Fatalf("擦除后 total = %d，想要 1", after.Total)
	}
	for _, item := range after.Items {
		if item.Slug == gone {
			t.Fatalf("已擦除的作品仍出现在我的作品里：%s", gone)
		}
	}
}

// TestIntegrationMyDemosShowsSoftDeleted：软删不改 lifecycle_state，
// 所以只能靠 deletedAt 认出「已移除、还能恢复」，purgeAfter 说明还能恢复多久。
func TestIntegrationMyDemosShowsSoftDeleted(t *testing.T) {
	h, idSvc, pool := newMyDemosHandler(t)
	_, sid := mkAuthorWithDemos(t, h, idSvc, pool, "softdel", 1)
	_, before := myDemos(t, h, sid, "")
	slug := before.Items[0].Slug

	if rec := lifecycleCall(t, h, http.MethodDelete, slug, sid, nil); rec.Code != http.StatusOK {
		t.Fatalf("软删 = %d，想要 200；响应体：%s", rec.Code, rec.Body.String())
	}
	_, after := myDemos(t, h, sid, "")
	if len(after.Items) != 1 {
		t.Fatalf("软删后应仍有 1 件，实际 %d", len(after.Items))
	}
	item := after.Items[0]
	if item.DeletedAt == nil {
		t.Fatalf("软删后 deletedAt 为空——作者无法区分「已移除」与「正常在线」")
	}
	if item.PurgeAfter == nil {
		t.Fatalf("软删后 purgeAfter 为空——界面说不出「还能恢复多久」")
	}
	if !item.PurgeAfter.After(time.Now()) {
		t.Fatalf("purgeAfter = %v，应当还没到点", item.PurgeAfter)
	}
	// 软删不改生命周期：它仍然是删除前的档位。这不是 bug，是判据正交。
	if item.LifecycleState == "removed" {
		t.Logf("软删把 lifecycle_state 改成了 removed（当前实现不改）")
	}
}

func TestIntegrationMyDemosPaginates(t *testing.T) {
	h, idSvc, pool := newMyDemosHandler(t)
	_, sid := mkAuthorWithDemos(t, h, idSvc, pool, "paging", 4)

	_, p1 := myDemos(t, h, sid, "?page=1&pageSize=2")
	_, p2 := myDemos(t, h, sid, "?page=2&pageSize=2")
	if p1.Total != 4 || p2.Total != 4 {
		t.Fatalf("total = %d/%d，想要 4/4", p1.Total, p2.Total)
	}
	if len(p1.Items) != 2 || len(p2.Items) != 2 {
		t.Fatalf("每页条数 = %d/%d，想要 2/2", len(p1.Items), len(p2.Items))
	}
	// updated_at 是递增造的，所以倒序 = 第 4、3 件在前两页。
	if p1.Items[0].Slug == p2.Items[0].Slug || p1.Items[1].Slug == p2.Items[1].Slug {
		t.Fatalf("两页出现了重复条目")
	}
	if !p1.Items[0].UpdatedAt.After(p1.Items[1].UpdatedAt) {
		t.Fatalf("第一页没有按 updatedAt 倒序：%v 不晚于 %v", p1.Items[0].UpdatedAt, p1.Items[1].UpdatedAt)
	}
	if !p2.Items[0].UpdatedAt.After(p2.Items[1].UpdatedAt) {
		t.Fatalf("第二页没有按 updatedAt 倒序")
	}
	if p2.Items[0].UpdatedAt.After(p1.Items[1].UpdatedAt) {
		t.Fatalf("第二页的第一条比第一页的最后一条还新，翻页顺序抖了")
	}
}

func TestIntegrationMyDemosRejectsOversizedPage(t *testing.T) {
	h, idSvc, pool := newMyDemosHandler(t)
	_, sid := mkAuthorWithDemos(t, h, idSvc, pool, "cap", 1)

	rec := getWithSession(t, h, "/api/v1/me/demos?pageSize=201", sid)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("pageSize=201 = %d，想要 400（不静默截断）；响应体：%s", rec.Code, rec.Body.String())
	}
}

// mustAccountSub 从 session 反查 sub：测试里要按 users.sub 找作者自己的行。
func mustAccountSub(t *testing.T, h *Handler, sid string) string {
	t.Helper()
	acc, err := h.identity.Me(context.Background(), sid)
	if err != nil {
		t.Fatalf("取账号失败: %v", err)
	}
	return acc.Sub
}
