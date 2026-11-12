package httpapi

// 按条更新这条路径的用例：只有内容真的变了的行才写，行 id 与 fetched_at 都不该被
// 「又跑了一次同步」这件事打扰。
//
// 与 model_sync_integration_test.go 分开放，是因为那里已经 500 行往上：两个文件
// 各自讲一件事——那边讲「同步整体怎么跑」，这边讲「一行何时才算被写过」。

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/identity"
)

// syncFixtureRenamed 与 syncFixture 只差一个字段：`glm-5` 的展示名。
//
// 用「改一个字段」而不是「换一份目录」来演上游变化：真实世界绝大多数变化就是
// 某个字段变了，而「整表重写」与「按条更新」的差别恰恰在这个场景下最明显——
// 前者换掉全部行 id，后者只有被改的那一行动。
const syncFixtureRenamed = `{
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
      "glm-5": {"id": "glm-5", "name": "GLM-5 (renamed)"},
      "glm-5-air": {"id": "glm-5-air", "name": "GLM-5 Air", "canonical_model_id": "zhipuai/glm-5-air"}
    }
  }
}`

// syncFixtureMinusAir 与 syncFixture 只差一条：上游下架了 `glm-5-air`。
//
// 其余两条**逐字照抄**，包括 canonical_model_id / release_date / limit / cost。
// 少抄一个字段，「内容没变」这个前提就假了——被漏掉的字段会被当成「上游删了这个值」，
// fetched_at 跟着前进，用例就会红在一个与被测行为无关的地方。
const syncFixtureMinusAir = `{
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
      "glm-5": {"id": "glm-5", "name": "GLM-5"}
    }
  }
}`

// catalogRow 是「镜像里这一条现在长什么样」的最小切片。
type catalogRow struct {
	ID        int64
	Name      string
	FetchedAt time.Time
}

// readCatalog 按 `provider_id/external_id` 索引镜像行。
func readCatalog(t *testing.T, pool *pgxpool.Pool) map[string]catalogRow {
	t.Helper()
	rows, err := pool.Query(context.Background(), `
		SELECT provider_id || '/' || external_id, id, name, fetched_at
		FROM model_catalog_items WHERE source = 'models.dev'`)
	if err != nil {
		t.Fatalf("读镜像失败: %v", err)
	}
	defer rows.Close()
	out := map[string]catalogRow{}
	for rows.Next() {
		var key string
		var r catalogRow
		if err := rows.Scan(&key, &r.ID, &r.Name, &r.FetchedAt); err != nil {
			t.Fatalf("扫镜像行失败: %v", err)
		}
		out[key] = r
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("遍历镜像失败: %v", err)
	}
	return out
}

// newMutableSyncHandler 造一个上游内容可换的 handler。
//
// 用 atomic.Value 而不是裸变量：CI 跑 `-race`，而这里 handler 在 httptest 的
// goroutine 里读、测试主体在主 goroutine 里写。
func newMutableSyncHandler(t *testing.T, pool *pgxpool.Pool, idSvc *identity.Service) (*Handler, func(string)) {
	t.Helper()
	var payload atomic.Value
	payload.Store(syncFixture)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(payload.Load().(string)))
	}))
	t.Cleanup(srv.Close)
	return newSyncHandler(t, pool, idSvc, srv), func(next string) { payload.Store(next) }
}

// 上游改了一条：被改的那行 id 不变、值更新、fetched_at 前进；其余行连 fetched_at 都不动。
//
// 这条是「按条更新」的核心承诺，也是它取代「先清空再灌」的唯一理由。没有它，
// 整表重写同样能通过「写进去了 3 条」那种断言。
func TestIntegrationSyncKeepsRowIDsWhenOneEntryChanges(t *testing.T) {
	pool := integrationPool(t)
	idSvc := integrationIdentity(t, pool)
	resetCatalog(t, pool)

	h, setPayload := newMutableSyncHandler(t, pool, idSvc)
	sid := mkAdmin(t, pool, idSvc, "sync6")

	if rec := postSync(t, h, sid); rec.Code != http.StatusOK {
		t.Fatalf("第一次同步应回 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	before := readCatalog(t, pool)
	if len(before) != 3 {
		t.Fatalf("第一次同步后应有 3 行，实际 %d", len(before))
	}

	setPayload(syncFixtureRenamed)
	rec := postSync(t, h, sid)
	if rec.Code != http.StatusOK {
		t.Fatalf("改一条之后的同步应回 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	var got struct {
		Skipped bool  `json:"skipped"`
		Items   int64 `json:"items"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got.Skipped {
		t.Fatal("payload 变了，不该 skipped=true")
	}
	if got.Items != 3 {
		t.Fatalf("items = %d，想要 3", got.Items)
	}

	after := readCatalog(t, pool)
	if len(after) != 3 {
		t.Fatalf("同步后应仍是 3 行，实际 %d", len(after))
	}
	for key, was := range before {
		now, ok := after[key]
		if !ok {
			t.Fatalf("%s 在同步后消失了", key)
		}
		if now.ID != was.ID {
			t.Fatalf("%s 的行 id 变了：%d → %d——按条更新的意义就是这里不该变", key, was.ID, now.ID)
		}
	}

	changed := after["bothub/glm-5"]
	if changed.Name != "GLM-5 (renamed)" {
		t.Fatalf("被改的那条 name = %q，想要 GLM-5 (renamed)", changed.Name)
	}
	if !changed.FetchedAt.After(before["bothub/glm-5"].FetchedAt) {
		t.Fatalf("被改的那条 fetched_at 应当前进：%v → %v",
			before["bothub/glm-5"].FetchedAt, changed.FetchedAt)
	}

	// 没变的行连 fetched_at 都不该动：它记的是「这一行内容最后一次变化」的时间，
	// 不是「这次同步跑过」的时间。少了这道闸门，一天一次同步会把整张表的
	// fetched_at 原地刷新一遍，「上游改了」与「我们同步过」就又混成一个时间点了。
	for _, key := range []string{"deepseek/deepseek-flash", "bothub/glm-5-air"} {
		if !after[key].FetchedAt.Equal(before[key].FetchedAt) {
			t.Fatalf("%s 内容没变，fetched_at 却从 %v 跳到 %v",
				key, before[key].FetchedAt, after[key].FetchedAt)
		}
	}
}

// 上游下架一条：那行消失，其余行的 id 与 fetched_at 都不动。
func TestIntegrationSyncDropsRowsMissingUpstream(t *testing.T) {
	pool := integrationPool(t)
	idSvc := integrationIdentity(t, pool)
	resetCatalog(t, pool)

	h, setPayload := newMutableSyncHandler(t, pool, idSvc)
	sid := mkAdmin(t, pool, idSvc, "sync7")

	if rec := postSync(t, h, sid); rec.Code != http.StatusOK {
		t.Fatalf("第一次同步应回 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	before := readCatalog(t, pool)
	if len(before) != 3 {
		t.Fatalf("第一次同步后应有 3 行，实际 %d", len(before))
	}

	setPayload(syncFixtureMinusAir)
	if rec := postSync(t, h, sid); rec.Code != http.StatusOK {
		t.Fatalf("下架一条之后的同步应回 200，实际 %d：%s", rec.Code, rec.Body.String())
	}

	after := readCatalog(t, pool)
	if len(after) != 2 {
		t.Fatalf("同步后应剩 2 行，实际 %d", len(after))
	}
	if _, ok := after["bothub/glm-5-air"]; ok {
		t.Fatal("上游已经下架的 glm-5-air 还留在镜像里")
	}
	for _, key := range []string{"deepseek/deepseek-flash", "bothub/glm-5"} {
		if after[key].ID != before[key].ID {
			t.Fatalf("%s 的行 id 在删别的行时被换掉了：%d → %d", key, before[key].ID, after[key].ID)
		}
		if !after[key].FetchedAt.Equal(before[key].FetchedAt) {
			t.Fatalf("%s 内容没变，fetched_at 却变了：%v → %v",
				key, before[key].FetchedAt, after[key].FetchedAt)
		}
	}
}

// 上游把可空字段从「没给」改成「给了值」，必须算一次变化。
//
// 这条盯的是 payload 里空串与 null 的区别：`jsonb_populate_recordset` 原样取 JSON 的值，
// 空串落进 `canonical_id` 就成了一条「规范名是空字符串」的记录，与 NULL（没给）
// 是两回事。真那样的话，界面上「待确认厂商」那一档会永远空着，而谁都看不出原因。
func TestIntegrationSyncTreatsNullAndEmptyAsAbsent(t *testing.T) {
	pool := integrationPool(t)
	idSvc := integrationIdentity(t, pool)
	resetCatalog(t, pool)

	h, setPayload := newMutableSyncHandler(t, pool, idSvc)
	sid := mkAdmin(t, pool, idSvc, "sync8")

	if rec := postSync(t, h, sid); rec.Code != http.StatusOK {
		t.Fatalf("第一次同步应回 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	// 文本类可空列：没给就应当是 NULL，不是 ''。
	//
	// date 列不在这条里——`release_date = ''` 在 PostgreSQL 里直接是类型错误
	// （`invalid input syntax for type date: ""`），不是「比出 false」。它单独判 IS NULL。
	var emptyText int
	if err := pool.QueryRow(context.Background(), `
		SELECT count(*) FROM model_catalog_items
		WHERE source = 'models.dev'
		  AND (canonical_id = '' OR vendor_slug = '' OR family = ''
		       OR knowledge_cutoff = '' OR model_type = '' OR upstream_status = '') `).Scan(&emptyText); err != nil {
		t.Fatalf("查文本空串行数失败: %v", err)
	}
	if emptyText != 0 {
		t.Fatalf("有 %d 行把「上游没给」在文本列上存成了空串；可空列应当存 NULL", emptyText)
	}
	// date 列单独判：fixture 里只有 deepseek-flash 给了日期，另外两条必须留空（NULL）。
	// 空串落进 date 列不是「存了个空值」，是整次同步直接报错回滚（SQLSTATE 22007）。
	var dated int
	if err := pool.QueryRow(context.Background(), `
		SELECT count(*) FROM model_catalog_items
		WHERE source = 'models.dev' AND release_date IS NOT NULL `).Scan(&dated); err != nil {
		t.Fatalf("查日期列行数失败: %v", err)
	}
	if dated != 1 {
		t.Fatalf("有 %d 行 release_date 不是 NULL，fixture 里只给了 1 条日期", dated)
	}
	var gotDate string
	if err := pool.QueryRow(context.Background(), `
		SELECT release_date::text FROM model_catalog_items
		WHERE source = 'models.dev' AND external_id = 'deepseek-flash' `).Scan(&gotDate); err != nil {
		t.Fatalf("查 deepseek-flash 的日期失败: %v", err)
	}
	if gotDate != "2026-10-17" {
		t.Fatalf("release_date = %q，想要 2026-10-17", gotDate)
	}

	// 上游这次把 canonical_model_id 补上了 → 必须被认成变化。
	setPayload(`{
  "bothub": {
    "id": "bothub",
    "name": "Bothub",
    "models": {"glm-5": {"id": "glm-5", "name": "GLM-5", "canonical_model_id": "zhipuai/glm-5"}}
  }
}`)
	if rec := postSync(t, h, sid); rec.Code != http.StatusOK {
		t.Fatalf("补 canonical 之后的同步应回 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	var canonical, vendor string
	if err := pool.QueryRow(context.Background(), `
		SELECT COALESCE(canonical_id, '<null>'), COALESCE(vendor_slug, '<null>')
		FROM model_catalog_items WHERE source = 'models.dev' AND external_id = 'glm-5'`).
		Scan(&canonical, &vendor); err != nil {
		t.Fatalf("查 glm-5 失败: %v", err)
	}
	if canonical != "zhipuai/glm-5" || vendor != "zhipuai" {
		t.Fatalf("canonical/vendor = %q/%q，想要 zhipuai/glm-5 与 zhipuai", canonical, vendor)
	}
}
