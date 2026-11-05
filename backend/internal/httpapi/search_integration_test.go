package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/api"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

// 搜索是「契约声明了、后端曾经没读」的那一类：它不会崩，只会悄悄不生效。
// 所以每条分支都钉死：标题命中、简介命中、都不命中、空白、超长。
func TestIntegrationSearchDemos(t *testing.T) {
	pool := integrationPool(t)
	seed(t, pool)
	ctx := context.Background()

	// 种子里四条作品的简介都是「简介」，先给一条换成能搜到的内容。
	if _, err := pool.Exec(ctx,
		`UPDATE demos SET description = '关于零拷贝的一次尝试' WHERE slug = 'it-published'`); err != nil {
		t.Fatalf("改简介失败: %v", err)
	}

	// 再造一条已发布的作品，标题里带「探针」，用来验标题命中。
	// 版本行必须先有：迁移 0014 之后「能上架渠道」的前提是有对外版本，
	// 而对外版本来自 current_revision_id——没有版本的 demos 行压根发不出去。
	//
	// 分两条语句写：数据修改型 CTE 之间互相看不见对方的写入（同一快照），
	// 一条语句里 INSERT demos → INSERT revisions → UPDATE demos 会 UPDATE 到 0 行。
	var probeID int64
	if err := pool.QueryRow(ctx, `
		INSERT INTO demos (slug, title, description, moderation_state, lifecycle_state)
		VALUES ('it-probe', '探针作品', '另一份简介', 'approved', 'draft')
		RETURNING id`).Scan(&probeID); err != nil {
		t.Fatalf("插入探针作品失败: %v", err)
	}
	var probeRev int64
	if err := pool.QueryRow(ctx, `
		INSERT INTO demo_revisions (demo_id, rev_no, demo_type, source)
		VALUES ($1, 1, 'web', 'upload') RETURNING id`, probeID).Scan(&probeRev); err != nil {
		t.Fatalf("插入探针版本失败: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE demos SET current_revision_id = $1 WHERE id = $2`, probeRev, probeID); err != nil {
		t.Fatalf("设置当前版本失败: %v", err)
	}
	publishNow(t, pool, "it-probe")

	h := New(pool, store.New(pool), integrationIdentity(t, pool), nil, nil)

	cases := []struct {
		name      string
		q         *string
		wantCode  int
		wantTotal int
		wantTitle string // 空表示不校验
		wantIn    string
	}{
		{name: "不带关键词", q: nil, wantCode: http.StatusOK, wantTotal: 2},
		{name: "空串等同没传", q: strPtr(""), wantCode: http.StatusOK, wantTotal: 2},
		{name: "全空格等同没传", q: strPtr("   "), wantCode: http.StatusOK, wantTotal: 2},
		{name: "标题命中", q: strPtr("探针"), wantCode: http.StatusOK, wantTotal: 1, wantTitle: "探针作品", wantIn: "title"},
		{name: "简介命中", q: strPtr("零拷贝"), wantCode: http.StatusOK, wantTotal: 1, wantIn: "description"},
		{name: "大小写与前后空格不影响", q: strPtr("  探针  "), wantCode: http.StatusOK, wantTotal: 1, wantTitle: "探针作品"},
		{name: "没有命中", q: strPtr("这个关键词不存在"), wantCode: http.StatusOK, wantTotal: 0},
		{name: "超长回 400", q: strPtr(repeatRune('搜', 101)), wantCode: http.StatusBadRequest},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			h.ListDemos(rec, httptest.NewRequest(http.MethodGet, "/api/v1/demos", nil),
				api.ListDemosParams{Q: tc.q})

			if rec.Code != tc.wantCode {
				t.Fatalf("状态码 = %d，想要 %d；响应体：%s", rec.Code, tc.wantCode, rec.Body.String())
			}
			if tc.wantCode != http.StatusOK {
				return
			}

			var body struct {
				Items []map[string]any `json:"items"`
				Total int              `json:"total"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("响应不是 JSON: %v", err)
			}
			if body.Total != tc.wantTotal {
				t.Fatalf("total = %d，想要 %d", body.Total, tc.wantTotal)
			}
			// total 与列表条数必须一致——两者是两条 SQL，口径漏一边就会对不上，
			// 表现出来是「共 2 个作品」但下面只列 1 条。
			if len(body.Items) > body.Total {
				t.Fatalf("列表 %d 条超过 total %d", len(body.Items), body.Total)
			}
			if tc.wantTitle != "" {
				if len(body.Items) != 1 {
					t.Fatalf("期望 1 条结果，实际 %d 条", len(body.Items))
				}
				if got := body.Items[0]["title"]; got != tc.wantTitle {
					t.Fatalf("title = %v，想要 %q", got, tc.wantTitle)
				}
			}
			if tc.wantIn != "" {
				if len(body.Items) != 1 {
					t.Fatalf("期望 1 条结果，实际 %d 条", len(body.Items))
				}
				if got := body.Items[0]["matchedIn"]; got != tc.wantIn {
					t.Fatalf("matchedIn = %v，想要 %q", got, tc.wantIn)
				}
			}
		})
	}
}

func strPtr(s string) *string { return &s }

func repeatRune(r rune, n int) string {
	out := make([]rune, n)
	for i := range out {
		out[i] = r
	}
	return string(out)
}
