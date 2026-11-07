package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/api"
)

// 管理端数据面的三件事，缺一件导入页就写不出来：
// 目录能翻页（8931 条不能只给前 50）、搜索框里的 `%` `_` 按字面匹配、
// 同步状态读得到（否则页面刷新之后不知道镜像有多旧）。

// 目录分页：total 与 items 的口径必须一致，否则最后一页会少几条。
func TestIntegrationCatalogPaging(t *testing.T) {
	h, idSvc, pool := newModelTestHandler(t)
	sid := mkAdmin(t, pool, idSvc, "page")

	u := uniq()
	prov := "it-page-" + u
	for i := 0; i < 5; i++ {
		putCatalogItem(t, pool, prov, fmt.Sprintf("item-%d", i), fmt.Sprintf("分页-%s-%d", u, i), nil)
	}
	// 一条**不匹配搜索词**的无关条目：没有它，即使 total 用错了筛选条件
	// （比如忽略 q）也会算出 5，测试就压不住「两条 SQL 条件必须一致」这条判据。
	// provider 也不能带 u：搜索词也会匹配 provider_name，带了就同样命中。
	putCatalogItem(t, pool, "it-unrelated", "unrelated", "无关条目-zzz", nil)

	// 第一页：2 条，total 仍是 5。
	// 用名字里的公共前缀搜索，而不是整段 u：`无关条目-zzz` 所在 provider 不含 u。
	token := "分页-" + u
	rec := getWithSession(t, h, "/api/v1/admin/models/catalog?q="+token+"&limit=2&offset=0", sid)
	if rec.Code != http.StatusOK {
		t.Fatalf("目录状态码 = %d，想要 200；body=%s", rec.Code, rec.Body.String())
	}
	var first api.ModelCatalogList
	if err := json.Unmarshal(rec.Body.Bytes(), &first); err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(first.Items) != 2 {
		t.Fatalf("第一页条数 = %d，想要 2", len(first.Items))
	}
	if first.Total != 5 {
		t.Fatalf("total = %d，想要 5（既不受 limit 影响，也不能忽略 q）", first.Total)
	}
	if first.Limit != 2 || first.Offset != 0 {
		t.Fatalf("回显 limit/offset = %d/%d，想要 2/0", first.Limit, first.Offset)
	}

	// 最后一页：offset=4 只剩 1 条，且与第一页不重复。
	rec = getWithSession(t, h, "/api/v1/admin/models/catalog?q="+token+"&limit=2&offset=4", sid)
	var last api.ModelCatalogList
	if err := json.Unmarshal(rec.Body.Bytes(), &last); err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(last.Items) != 1 {
		t.Fatalf("最后一页条数 = %d，想要 1", len(last.Items))
	}
	if last.Items[0].ExternalId == first.Items[0].ExternalId {
		t.Fatal("翻页不该重复返回同一条")
	}
	if last.Total != 5 {
		t.Fatalf("最后一页的 total = %d，想要 5", last.Total)
	}

	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(),
			`DELETE FROM model_catalog_items WHERE provider_id = ANY($1)`, []string{prov, "it-unrelated"})
	})
}

// 搜索框里的 `%` 与 `_` 按字面匹配：不转义的话 `%` 会返回整个目录。
func TestIntegrationCatalogSearchTreatsWildcardsLiterally(t *testing.T) {
	h, idSvc, pool := newModelTestHandler(t)
	sid := mkAdmin(t, pool, idSvc, "wild")

	u := uniq()
	prov := "it-wild-" + u
	putCatalogItem(t, pool, prov, "pct", "百分号100%的模型-"+u, nil)
	putCatalogItem(t, pool, prov, "plain", "普通模型-"+u, nil)

	// `%` 只该匹配名字里真含 `%` 的那一条。
	rec := getWithSession(t, h, "/api/v1/admin/models/catalog?q=100%25&limit=50", sid)
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d，想要 200", rec.Code)
	}
	var got api.ModelCatalogList
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	joined := listNames(got)
	if !strings.Contains(joined, "百分号100%的模型") {
		t.Fatalf("应当命中含百分号那条，实际 = %s", joined)
	}
	if strings.Contains(joined, "普通模型") {
		t.Fatalf("`%%` 不该被当成通配符，实际返回 = %s", joined)
	}

	// `_` 同理：它不该匹配任意单个字符。
	rec = getWithSession(t, h, "/api/v1/admin/models/catalog?q=_&limit=50", sid)
	var under api.ModelCatalogList
	if err := json.Unmarshal(rec.Body.Bytes(), &under); err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(under.Items) != 0 {
		t.Fatalf("`_` 不该匹配到任何条目，实际 %d 条：%s", len(under.Items), listNames(under))
	}

	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(),
			`DELETE FROM model_catalog_items WHERE provider_id = ANY($1)`, []string{prov, "it-unrelated"})
	})
}

func listNames(l api.ModelCatalogList) string {
	out := make([]string, 0, len(l.Items))
	for _, it := range l.Items {
		out = append(out, it.Name)
	}
	return strings.Join(out, ",")
}

// 从没同步过 → 200 + status=never（不是 404），这样页面能说「点上面的同步」。
func TestIntegrationModelSyncStatusNever(t *testing.T) {
	h, idSvc, pool := newModelTestHandler(t)
	sid := mkAdmin(t, pool, idSvc, "status-never")

	if _, err := pool.Exec(context.Background(),
		`DELETE FROM model_source_sync WHERE source = 'models.dev'`); err != nil {
		t.Fatalf("清同步状态失败: %v", err)
	}

	rec := getWithSession(t, h, "/api/v1/admin/models/sync", sid)
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d，想要 200；body=%s", rec.Code, rec.Body.String())
	}
	var got api.ModelSyncStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if got.Status != "never" {
		t.Fatalf("status = %q，想要 never", got.Status)
	}
	if got.LastSuccessAt != nil || got.LastAttemptAt != nil {
		t.Fatal("从未同步过时两个时间都该是 null")
	}
	if got.Source != "models.dev" {
		t.Fatalf("source = %q，想要 models.dev", got.Source)
	}
}

// 同步成功之后读得到条数与时间；失败之后 status=failed 且 lastSuccessAt 不被覆盖。
func TestIntegrationModelSyncStatusReadsLedger(t *testing.T) {
	h, idSvc, pool := newModelTestHandler(t)
	sid := mkAdmin(t, pool, idSvc, "status-ok")

	// 造一行「成功」的记账。
	if _, err := pool.Exec(context.Background(), `
INSERT INTO model_source_sync (source, last_attempt_at, last_success_at, payload_hash, item_count, status, error)
VALUES ('models.dev', now(), now(), 'hash-x', 42, 'ok', '')
ON CONFLICT (source) DO UPDATE SET last_attempt_at = now(), last_success_at = now(),
    item_count = 42, status = 'ok', error = ''`); err != nil {
		t.Fatalf("写记账失败: %v", err)
	}
	rec := getWithSession(t, h, "/api/v1/admin/models/sync", sid)
	var ok api.ModelSyncStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &ok); err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if ok.Status != "ok" || ok.Items != 42 {
		t.Fatalf("status/items = %q/%d，想要 ok/42", ok.Status, ok.Items)
	}
	if ok.LastSuccessAt == nil {
		t.Fatal("成功过就该有 lastSuccessAt")
	}
	if ok.Error != nil {
		t.Fatalf("成功时 error 应为 null，实际 %q", *ok.Error)
	}

	// 把它改成失败，但保留 last_success_at（这就是真实写入方的行为）。
	if _, err := pool.Exec(context.Background(), `
UPDATE model_source_sync SET status = 'failed', error = '拉取目录失败: HTTP 500', last_attempt_at = now()
WHERE source = 'models.dev'`); err != nil {
		t.Fatalf("改记账失败: %v", err)
	}
	rec = getWithSession(t, h, "/api/v1/admin/models/sync", sid)
	var bad api.ModelSyncStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &bad); err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if bad.Status != "failed" {
		t.Fatalf("status = %q，想要 failed", bad.Status)
	}
	if bad.Error == nil || !strings.Contains(*bad.Error, "500") {
		t.Fatalf("失败原因要带出来，实际 %v", bad.Error)
	}
	if bad.LastSuccessAt == nil {
		t.Fatal("失败不该清掉 lastSuccessAt——它是判断镜像有多旧的依据")
	}
}
