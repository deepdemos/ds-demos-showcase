package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/api"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/demo"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/identity"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/platform/storage"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

// 审核端点的鉴权：一个普通用户和一个审核员，同一请求应当拿到不同的状态码。
//
// 这条测试存在的理由：`demo.review` 是审核链路上唯一的门。它写错的方向只有两种，
// 而两种都不会被「正面用例」发现——一是拦得太松（谁都能审），二是拦得太紧（审核员进不去）。
func TestIntegrationReviewPermission(t *testing.T) {
	pool := integrationPool(t)
	q := store.New(pool)
	idSvc := integrationIdentity(t, pool)

	files, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("初始化存储失败: %v", err)
	}
	demoSvc := demo.New(pool, q, files, storage.DefaultLimits(), 200<<20, 0, nil)
	h := New(pool, q, idSvc, nil, demoSvc)

	// 普通用户：能登录，但没有 demo.review。
	userAcc, userSID, err := idSvc.Register(context.Background(), identity.RegisterInput{
		Email:       "it-rev-user@example.com",
		Password:    "reviewtest123",
		Username:    "it-rev-user",
		DisplayName: "普通用户",
	})
	if err != nil {
		t.Fatalf("注册普通用户失败: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE sub = $1`, userAcc.Sub)
		_, _ = pool.Exec(context.Background(), `DELETE FROM accounts WHERE sub = $1`, userAcc.Sub)
	})

	// 审核员：注册之后把角色改掉。角色是 accounts 上的一列，没有别的写入路径。
	modAcc, modSID, err := idSvc.Register(context.Background(), identity.RegisterInput{
		Email:       "it-rev-mod@example.com",
		Password:    "reviewtest123",
		Username:    "it-rev-mod",
		DisplayName: "审核员",
	})
	if err != nil {
		t.Fatalf("注册审核员失败: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE sub = $1`, modAcc.Sub)
		_, _ = pool.Exec(context.Background(), `DELETE FROM accounts WHERE sub = $1`, modAcc.Sub)
	})
	if _, err := pool.Exec(context.Background(),
		`UPDATE accounts SET role = 'moderator' WHERE sub = $1`, modAcc.Sub); err != nil {
		t.Fatalf("提权失败: %v", err)
	}

	cases := []struct {
		name string
		sid  string
		want int
	}{
		{"未登录", "", http.StatusUnauthorized},
		{"普通用户", userSID, http.StatusForbidden},
		{"审核员", modSID, http.StatusOK},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/reviews/demo", nil)
			if tc.sid != "" {
				req.AddCookie(&http.Cookie{Name: sessionCookie, Value: tc.sid})
			}
			rec := httptest.NewRecorder()
			h.ListDemoReviewQueue(rec, req, api.ListDemoReviewQueueParams{})

			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d（body: %s）", rec.Code, tc.want, rec.Body.String())
			}
			if tc.want == http.StatusForbidden && !strings.Contains(rec.Body.String(), "forbidden") {
				t.Fatalf("403 的 code 应为 forbidden，实际 %s", rec.Body.String())
			}
		})
	}
}

// 裁决值不是 approve/reject 时必须 400。
//
// 少了这道校验，decision:"yes" 会因为「不等于 approve」被当成驳回，并且返回 200——
// 审核员以为自己点了通过，实际作品被驳回了。decodeJSON 的 DisallowUnknownFields
// 只管字段名，不管枚举值，所以这条只能在 handler 里判。
func TestIntegrationReviewRejectsUnknownDecision(t *testing.T) {
	pool := integrationPool(t)
	q := store.New(pool)
	idSvc := integrationIdentity(t, pool)

	files, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("初始化存储失败: %v", err)
	}
	demoSvc := demo.New(pool, q, files, storage.DefaultLimits(), 200<<20, 0, nil)
	h := New(pool, q, idSvc, nil, demoSvc)

	acc, sid, err := idSvc.Register(context.Background(), identity.RegisterInput{
		Email:       "it-bad-decision@example.com",
		Password:    "reviewtest123",
		Username:    "it-bad-decision",
		DisplayName: "管理员",
	})
	if err != nil {
		t.Fatalf("注册失败: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE sub = $1`, acc.Sub)
		_, _ = pool.Exec(context.Background(), `DELETE FROM accounts WHERE sub = $1`, acc.Sub)
	})
	if _, err := pool.Exec(context.Background(),
		`UPDATE accounts SET role = 'admin' WHERE sub = $1`, acc.Sub); err != nil {
		t.Fatalf("提权失败: %v", err)
	}

	// 权限先过（admin 有 demo.review），所以这里能走到决定值校验那一步。
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/reviews/demo/x",
		strings.NewReader(`{"decision":"yes"}`))
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: sid})
	rec := httptest.NewRecorder()
	h.ReviewDemo(rec, req, "x")

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法 decision：expected 400，got %d（body=%s）", rec.Code, rec.Body.String())
	}
}
