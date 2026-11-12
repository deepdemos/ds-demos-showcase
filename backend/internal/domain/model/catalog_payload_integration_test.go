package model

// 这一条把「加了一列但没写进 payload」从「撞上才响」变成「加完就红，并说清为什么」。
//
// 背景见 docs/已知松点.md 第 62 条：`UpsertCatalogItems` 的闸门是「整行减去 id / source /
// fetched_at 再比」，新加的列一出生就参与比较；payload 里没有这个键时 EXCLUDED 拿到 NULL，
// 而既有行拿到列的默认值，两边不等——每轮同步把 9000 行全判成「变了」。
// 症状是 fetched_at 全表前进，离「你少写了一列」隔着两层推断。
//
// 这里直接对表结构：`model_catalog_items` 的列集合必须正好等于
// {id, source, fetched_at} ∪ upsertRow 的 json tag 集合。多一列就在这条用例上红，
// 报错里点名那一列。

import (
	"context"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// payloadColumns 是 upsertRow 覆盖的列（按 json tag 取）。
func payloadColumns(t *testing.T) map[string]bool {
	t.Helper()
	typ := reflect.TypeOf(upsertRow{})
	out := make(map[string]bool, typ.NumField())
	for i := 0; i < typ.NumField(); i++ {
		tag := typ.Field(i).Tag.Get("json")
		name, _, _ := strings.Cut(tag, ",")
		if name == "" || name == "-" {
			t.Fatalf("upsertRow 的第 %d 个字段没有 json tag：%s", i, typ.Field(i).Name)
		}
		out[name] = true
	}
	return out
}

// 与表结构对账：列名集合必须与 payload 键集合 + 由调用方给的三列完全一致。
func TestIntegrationCatalogPayloadCoversEveryColumn(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL 未设置，跳过集成测试（CI 里会设）")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("连接测试库失败: %v", err)
	}
	defer pool.Close()

	rows, err := pool.Query(context.Background(), `
		SELECT column_name FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'model_catalog_items'`)
	if err != nil {
		t.Fatalf("读表结构失败: %v", err)
	}
	defer rows.Close()
	var cols []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			t.Fatalf("扫列名失败: %v", err)
		}
		cols = append(cols, c)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("遍历列名失败: %v", err)
	}
	if len(cols) == 0 {
		t.Fatal("model_catalog_items 一列都没读到——表不存在，还是没跑迁移？")
	}

	// 这三列不由 payload 提供：id 是自增主键，source 由查询里的参数给，
	// fetched_at 由 Go 侧传进来的时间戳给。
	fromCaller := map[string]bool{"id": true, "source": true, "fetched_at": true}
	payload := payloadColumns(t)

	var missing, extra []string
	for _, c := range cols {
		if !fromCaller[c] && !payload[c] {
			missing = append(missing, c)
		}
	}
	byCol := make(map[string]bool, len(cols))
	for _, c := range cols {
		byCol[c] = true
	}
	for c := range payload {
		if !byCol[c] {
			extra = append(extra, c)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)

	if len(missing) > 0 {
		t.Fatalf("model_catalog_items 有 %d 列没被 payload 覆盖：%v。\n"+
			"新加的列要在 backend/internal/domain/model/catalog_payload.go 的 upsertRow 里补上，\n"+
			"否则它是 NULL 进 EXCLUDED、默认值留在既有行上，闸门每轮都把整表判成「变了」（松点 62）。",
			len(missing), missing)
	}
	if len(extra) > 0 {
		t.Fatalf("payload 里有 %d 个键在表里没有对应列：%v——\n"+
			"jsonb_populate_recordset 会静默忽略它们，于是这一列永远不写。", len(extra), extra)
	}
}
