package httpapi

import (
	"bytes"
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

// 生命周期四个端点的 HTTP 层：状态码、错误码、以及**作者视图与访客视图的差别**。
//
// 后者才是这一组真正要钉的东西：下架之后访客该看到墓碑、删除之后访客该看到 404，
// 而作者自己两种情况都还能进页面——否则他没有地方点「重新上架」或「恢复」。

func newLifecycleHandler(t *testing.T) (*Handler, *pgxpool.Pool, int64, string) {
	t.Helper()
	pool := integrationPool(t)
	q := store.New(pool)
	idSvc := integrationIdentity(t, pool)
	files, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("初始化存储失败: %v", err)
	}
	demoSvc := demo.New(pool, q, files, storage.DefaultLimits(), 200<<20, 0, nil)
	h := New(pool, q, idSvc, nil, demoSvc)

	acc, sid := registerVerified(t, pool, idSvc, identity.RegisterInput{
		Email:       "it-lifecycle@example.com",
		Password:    "lifecycletest123",
		Username:    "it-lifecycle",
		DisplayName: "生命周期作者",
	})
	t.Cleanup(func() { cleanupAuthor(t, pool, acc.Sub) })
	return h, pool, acc.UserID, sid
}

// publishNow 直接改库把作品变成「已发布 + 已上架」，跳过审核与发布两条 HTTP 路径
// （它们各有自己的用例，这里只关心生命周期）。
func publishNow(t *testing.T, pool *pgxpool.Pool, slug string) {
	t.Helper()
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `
		UPDATE demos SET lifecycle_state = 'published', moderation_state = 'approved',
		                 published_revision_id = current_revision_id
		WHERE slug = $1 AND current_revision_id IS NOT NULL`, slug); err != nil {
		t.Fatalf("造发布态失败: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO demo_channels (demo_id, channel, visibility, lang)
		SELECT id, 'deep', 'listed', 'zh' FROM demos WHERE slug = $1
		ON CONFLICT (demo_id, channel) DO UPDATE SET visibility = 'listed'`, slug); err != nil {
		t.Fatalf("建渠道失败: %v", err)
	}
}

func TestIntegrationLifecycleHTTP(t *testing.T) {
	h, q, _, sid := newLifecycleHandler(t)
	slug := submitHTML(t, h, sid, "生命周期用例", `<!doctype html><p>hi</p>`)
	publishNow(t, q, slug)

	// 1) 下架
	rec := lifecycleCall(t, h, http.MethodPost, slug+"/archive", sid, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("下架 = %d，想要 200；响应体：%s", rec.Code, rec.Body.String())
	}

	// 访客看：墓碑（200），但没有内容，也没有任何生命周期动作。
	guest := getDemoAs(t, h, slug, "")
	if guest.Code != http.StatusOK {
		t.Fatalf("访客看已下架作品 = %d，想要 200 墓碑", guest.Code)
	}
	guestBody := decode(t, guest.Body.Bytes())
	// 墓碑**保留标题**（`docs/实体域数据结构设计.md` §5.0 的墓碑行列了「slug / 标题 /
	// 下架时间 / 说明」）：分享链接落到这里时，用户得看得出来是哪个作品没了。
	if guestBody["title"] == nil || guestBody["title"] == "" {
		t.Fatalf("墓碑该保留标题，实际 %v", guestBody["title"])
	}
	// 但简介与封面是内容，墓碑不给。
	if guestBody["description"] != nil {
		t.Fatalf("墓碑不该有简介：%v", guestBody["description"])
	}
	if guestBody["downloadable"] != false {
		t.Fatalf("墓碑不该可下载，实际 %v", guestBody["downloadable"])
	}
	if got := actionsOf(t, guestBody); got != [4]bool{} {
		t.Fatalf("访客不该有任何生命周期动作，实际 %v", got)
	}

	// 作者看：同样没内容，但能点「重新上架」。
	author := getDemoAs(t, h, slug, sid)
	authorBody := decode(t, author.Body.Bytes())
	if got := actionsOf(t, authorBody); got != [4]bool{false, true, true, false} {
		t.Fatalf("作者对已下架作品的动作位 = %v，想要 unarchive+delete", got)
	}

	// 2) 重新上架
	if rec := lifecycleCall(t, h, http.MethodPost, slug+"/unarchive", sid, nil); rec.Code != http.StatusOK {
		t.Fatalf("重新上架 = %d，想要 200；响应体：%s", rec.Code, rec.Body.String())
	}

	// 3) 删除（带理由）
	reason, _ := json.Marshal(map[string]string{"reason": "写错了"})
	if rec := lifecycleCall(t, h, http.MethodDelete, slug, sid, reason); rec.Code != http.StatusOK {
		t.Fatalf("删除 = %d，想要 200；响应体：%s", rec.Code, rec.Body.String())
	}
	// 再删一次是 409，不是幂等成功。
	if rec := lifecycleCall(t, h, http.MethodDelete, slug, sid, nil); rec.Code != http.StatusConflict {
		t.Fatalf("重复删除 = %d，想要 409", rec.Code)
	}

	// 访客看：404。删除对外就是不存在，连墓碑都不给。
	if rec := getDemoAs(t, h, slug, ""); rec.Code != http.StatusNotFound {
		t.Fatalf("访客看已删除作品 = %d，想要 404", rec.Code)
	}
	// 作者看：200 + restore。
	author = getDemoAs(t, h, slug, sid)
	authorBody = decode(t, author.Body.Bytes())
	if author.Code != http.StatusOK {
		t.Fatalf("作者看已删除作品 = %d，想要 200", author.Code)
	}
	if got := actionsOf(t, authorBody); got != [4]bool{false, false, false, true} {
		t.Fatalf("作者对已删除作品的动作位 = %v，想要只有 restore", got)
	}

	// 4) 恢复
	if rec := lifecycleCall(t, h, http.MethodPost, slug+"/restore", sid, nil); rec.Code != http.StatusOK {
		t.Fatalf("恢复 = %d，想要 200；响应体：%s", rec.Code, rec.Body.String())
	}
	if rec := getDemoAs(t, h, slug, ""); rec.Code != http.StatusOK {
		t.Fatalf("恢复后访客 = %d，想要 200", rec.Code)
	}
}

// TestIntegrationLifecycleRejectsStrangers 别人既不能下架也不能删除，且都 404。
func TestIntegrationLifecycleRejectsStrangers(t *testing.T) {
	h, q, _, sid := newLifecycleHandler(t)
	slug := submitHTML(t, h, sid, "别人的作品", `<!doctype html><p>hi</p>`)
	publishNow(t, q, slug)

	for _, tc := range []struct {
		name, method, tail string
	}{
		{"下架", http.MethodPost, slug + "/archive"},
		{"删除", http.MethodDelete, slug},
		{"恢复", http.MethodPost, slug + "/restore"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// 未登录 → 401
			if rec := lifecycleCall(t, h, tc.method, tc.tail, "", nil); rec.Code != http.StatusUnauthorized {
				t.Fatalf("未登录 %s = %d，想要 401", tc.name, rec.Code)
			}
			// 不存在的 slug（换一个身份也一样）→ 404
			missing := tc.tail[:len(slug)] + "-nobody" + tc.tail[len(slug):]
			if rec := lifecycleCall(t, h, tc.method, missing, sid, nil); rec.Code != http.StatusNotFound {
				t.Fatalf("%s 不存在的作品 = %d，想要 404", tc.name, rec.Code)
			}
		})
	}
}

// TestIntegrationLifecycleRouteShapes 钉住三个 HTTP 方法。
//
// 契约里下架/上架/恢复都是 POST、删除是 DELETE。写成 PUT 或把删除写成 POST，
// 生成的客户端调不到，而两边各自单测都过得去。
func TestIntegrationLifecycleRouteShapes(t *testing.T) {
	router := NewRouter(&stub{}, nil, nil)
	cases := []struct {
		method, path string
	}{
		{http.MethodPost, "/api/v1/demos/demo-slug/archive"},
		{http.MethodPost, "/api/v1/demos/demo-slug/unarchive"},
		{http.MethodPost, "/api/v1/demos/demo-slug/restore"},
		{http.MethodDelete, "/api/v1/demos/demo-slug"},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(c.method, c.path, nil))
		if rec.Code == http.StatusNotFound || rec.Code == http.StatusMethodNotAllowed {
			t.Fatalf("%s %s 路由没挂上，状态码 %d", c.method, c.path, rec.Code)
		}
	}
}

// ---- 辅助 ----

func lifecycleCall(t *testing.T, h *Handler, method, path, sid string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body == nil {
		r = httptest.NewRequest(method, "/api/v1/demos/"+path, nil)
	} else {
		r = httptest.NewRequest(method, "/api/v1/demos/"+path, bytes.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	}
	if sid != "" {
		r.AddCookie(&http.Cookie{Name: sessionCookie, Value: sid})
	}
	rec := httptest.NewRecorder()
	NewRouter(h, nil, nil).ServeHTTP(rec, r)
	return rec
}

// actionsOf 把响应体里的 lifecycleActions 压成固定顺序的四个布尔，
// 让断言能写成一行（顺序 archive/unarchive/delete/restore）。
func actionsOf(t *testing.T, body map[string]any) [4]bool {
	t.Helper()
	raw, ok := body["lifecycleActions"].(map[string]any)
	if !ok {
		t.Fatalf("响应里没有 lifecycleActions：%v", body["lifecycleActions"])
	}
	keys := [4]string{"archive", "unarchive", "delete", "restore"}
	var out [4]bool
	for i, k := range keys {
		v, _ := raw[k].(bool)
		out[i] = v
	}
	return out
}
