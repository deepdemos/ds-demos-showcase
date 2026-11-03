package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/demo"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/identity"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/platform/storage"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

// 发布端点的鉴权：`demo.publish` 只绑了 admin（审核员没有）。
//
// 这条测试钉的是「发布与审核是两个权限」这个决定：审核员来了也必须 403。
// 如果哪天有人图省事把两者合并，这里会先红，而不是等到某个审核员
// 把一条没上首页资格的作品挂出去之后才发现。
func TestIntegrationPublishPermission(t *testing.T) {
	pool := integrationPool(t)
	q := store.New(pool)
	idSvc := integrationIdentity(t, pool)
	files, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("初始化存储失败: %v", err)
	}
	demoSvc := demo.New(pool, q, files, storage.DefaultLimits(), 200<<20, 0, nil)
	h := New(pool, q, idSvc, nil, demoSvc)

	mk := func(tag string) (string, string) {
		acc, sid, err := idSvc.Register(context.Background(), identity.RegisterInput{
			Email:       "it-pub-" + tag + "@example.com",
			Password:    "publishtest123",
			Username:    "it-pub-" + tag,
			DisplayName: "发布测试" + tag,
		})
		if err != nil {
			t.Fatalf("注册 %s 失败: %v", tag, err)
		}
		t.Cleanup(func() {
			_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE sub = $1`, acc.Sub)
			_, _ = pool.Exec(context.Background(), `DELETE FROM accounts WHERE sub = $1`, acc.Sub)
		})
		return acc.Sub, sid
	}

	userSub, userSID := mk("user")
	_ = userSub
	modSub, modSID := mk("mod")
	if _, err := pool.Exec(context.Background(),
		`UPDATE accounts SET role = 'moderator' WHERE sub = $1`, modSub); err != nil {
		t.Fatalf("提权失败: %v", err)
	}
	adminSub, adminSID := mk("admin")
	if _, err := pool.Exec(context.Background(),
		`UPDATE accounts SET role = 'admin' WHERE sub = $1`, adminSub); err != nil {
		t.Fatalf("提权失败: %v", err)
	}

	cases := []struct {
		name string
		sid  string
		want int
	}{
		{"未登录", "", http.StatusUnauthorized},
		{"普通用户", userSID, http.StatusForbidden},
		{"审核员没有发布权", modSID, http.StatusForbidden},
		// admin 有权限，但没有这条作品 → 404（证明它过了鉴权、走到底了）。
		{"admin", adminSID, http.StatusNotFound},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/demos/no-such/publish",
				strings.NewReader(`{"channel":"deep","visibility":"listed"}`))
			req.Header.Set("Content-Type", "application/json")
			if tc.sid != "" {
				req.AddCookie(&http.Cookie{Name: sessionCookie, Value: tc.sid})
			}
			rec := httptest.NewRecorder()
			h.PublishDemo(rec, req, "no-such")

			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d（body: %s）", rec.Code, tc.want, rec.Body.String())
			}
			if tc.want == http.StatusForbidden && !strings.Contains(rec.Body.String(), "forbidden") {
				t.Fatalf("403 的 code 应为 forbidden，实际 %s", rec.Body.String())
			}
		})
	}
}
