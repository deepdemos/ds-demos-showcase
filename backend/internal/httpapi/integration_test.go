package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/api"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

// 集成测试：连真库跑一遍真实 SQL。
//
// **默认跳过**——没设 TEST_DATABASE_URL 时不跑，本地不需要装 PostgreSQL。
// CI 里由 postgres service 提供库，并先灌 db/schema/*.sql。
//
// 这是有意的取舍（见 docs/development-workflow.md）：不为少数人要跑的测试强迫所有人装库。
// 代价是可能出现「本地绿、CI 红」，承担方式是 CI 红时先看是不是集成测试。
func integrationPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL 未设置，跳过集成测试（CI 里会设）")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("连接测试库失败: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func seed(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	if _, err := pool.Exec(ctx, "DELETE FROM demos"); err != nil {
		t.Fatalf("清空 demos 失败: %v", err)
	}
	rows := []struct {
		slug, state string
		deleted     bool
		erased      bool
	}{
		{"it-published", "published", false, false},
		{"it-archived", "archived", false, false},
		{"it-draft", "draft", false, false},
		{"it-erased", "published", false, true},
	}
	for _, r := range rows {
		// 被擦除的行必须把内容列全部置空——db/schema/30-demo.sql 的 ck_demos_erased
		// 就是这么要求的（擦除 = 内容清空 + 状态封存）。这里照约束来，不绕过。
		title, description := "标题 "+r.slug, "简介"
		if r.erased {
			title, description = "", ""
		}
		// ck_demos_time_order 要求：erased_at 非空时 deleted_at 必须非空，且 erased_at >= deleted_at。
		// 也就是「先删后擦」——擦除是删除之后的进一步动作。这里照约束构造数据。
		deleted := r.deleted || r.erased
		_, err := pool.Exec(ctx, `
			INSERT INTO demos (slug, title, description, moderation_state, lifecycle_state, deleted_at, erased_at)
			VALUES ($1, NULLIF($2, ''), NULLIF($3, ''), 'approved', $4,
			        CASE WHEN $5 THEN now() ELSE NULL END,
			        CASE WHEN $6 THEN now() ELSE NULL END)`,
			r.slug, title, description, r.state, deleted, r.erased)
		if err != nil {
			t.Fatalf("插入 %s 失败: %v", r.slug, err)
		}
	}
}

func TestIntegrationListAndDetail(t *testing.T) {
	pool := integrationPool(t)
	seed(t, pool)

	h := New(pool, store.New(pool))

	// 列表：只有 published 且未擦除的那一条。
	listRec := httptest.NewRecorder()
	h.ListDemos(listRec, httptest.NewRequest(http.MethodGet, "/api/v1/demos", nil), api.ListDemosParams{})
	var list map[string]any
	if err := json.Unmarshal(listRec.Body.Bytes(), &list); err != nil {
		t.Fatalf("列表响应不是 JSON: %v", err)
	}
	if got := list["total"]; got != float64(1) {
		t.Fatalf("expected total 1（只有 it-published），got %v", got)
	}

	// 详情：三种状态，三种结果。
	cases := []struct {
		slug string
		code int
	}{
		{"it-published", http.StatusOK},
		{"it-archived", http.StatusOK}, // 墓碑
		{"it-draft", http.StatusNotFound},
		{"it-erased", http.StatusNotFound},
		{"it-does-not-exist", http.StatusNotFound},
	}
	for _, tc := range cases {
		rec := httptest.NewRecorder()
		h.GetDemo(rec, httptest.NewRequest(http.MethodGet, "/api/v1/demos/"+tc.slug, nil), tc.slug)
		if rec.Code != tc.code {
			t.Fatalf("%s: expected %d, got %d（body=%s）", tc.slug, tc.code, rec.Code, rec.Body.String())
		}
		if tc.slug == "it-archived" {
			var body map[string]any
			_ = json.Unmarshal(rec.Body.Bytes(), &body)
			if body["title"] != nil {
				t.Fatalf("墓碑态 title 必须为 null，实际 %v", body["title"])
			}
		}
	}
}
