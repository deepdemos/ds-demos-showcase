package model

import (
	"encoding/json"
	"testing"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/platform/catalog"
)

// 空串必须序列化成 null，而不是 ""。
//
// 这条不是风格问题：`jsonb_populate_recordset` 原样取 JSON 的值，空串落进可空列
// 就成了一条「规范名是空字符串」的记录，与 NULL（上游没给）是两回事。真那样的话，
// 界面上「待确认厂商」那一档会永远空着，而谁都看不出原因。
//
// 也顺带钉住 release_date：空串喂给 date 列会让整次同步报错回滚（PostgreSQL 报
// `invalid input syntax for type date: ""`），一条畸形日期不该带走 8999 条好数据。
func TestCatalogPayloadTurnsEmptyStringsIntoNull(t *testing.T) {
	entries := []catalog.Entry{
		{
			ProviderID:   "bothub",
			ProviderName: "Bothub",
			ExternalID:   "glm-5",
			Name:         "GLM-5",
			// 其余可空字段全空：上游这条没有 canonical、没有 family、没有日期。
		},
		{
			ProviderID:   "deepseek",
			ProviderName: "DeepSeek",
			ExternalID:   "deepseek-flash",
			Name:         "DeepSeek V4.1 Flash",
			CanonicalID:  "deepseek/deepseek-v4.1-flash",
			VendorSlug:   "deepseek",
			ReleaseDate:  "2026-10-17",
		},
	}

	payload, keys, err := catalogPayload(entries)
	if err != nil {
		t.Fatalf("生成 payload 失败: %v", err)
	}

	var rows []map[string]any
	if err := json.Unmarshal(payload, &rows); err != nil {
		t.Fatalf("payload 不是合法 JSON: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("payload 有 %d 行，想要 2", len(rows))
	}

	// 没有值的六列必须是 JSON null，不是空串。
	for _, col := range []string{
		"canonical_id", "vendor_slug", "family", "release_date",
		"knowledge_cutoff", "model_type", "upstream_status",
	} {
		v, ok := rows[0][col]
		if !ok {
			t.Fatalf("第 0 行缺 %q 这个键——键必须写出来，值可以是 null", col)
		}
		if v != nil {
			t.Fatalf("%s = %#v，想要 null；空串会被当成「上游给了一个空字符串」", col, v)
		}
	}
	// 指针类型的数值列同样：没给就是 null，不是 0。
	for _, col := range []string{
		"context_window", "max_output", "cost_in", "cost_out",
		"open_weights", "supports_tool_call", "supports_reasoning",
		"supports_structured_output", "supports_attachment",
	} {
		if v, ok := rows[0][col]; !ok || v != nil {
			t.Fatalf("%s = %#v（存在=%v），想要 null", col, v, ok)
		}
	}
	// NOT NULL 的文本列保持字符串：那几列的空串本来就是既定落法。
	if v, _ := rows[0]["description"].(string); v != "" {
		t.Fatalf("description = %#v，想要空串", v)
	}

	// 有值的那行照常。
	if v, _ := rows[1]["canonical_id"].(string); v != "deepseek/deepseek-v4.1-flash" {
		t.Fatalf("canonical_id = %#v，想要 deepseek/deepseek-v4.1-flash", v)
	}
	if v, _ := rows[1]["release_date"].(string); v != "2026-10-17" {
		t.Fatalf("release_date = %#v，想要 2026-10-17", v)
	}

	// keys 只带主键两列，且与 payload 一一对应。
	var ks []map[string]any
	if err := json.Unmarshal(keys, &ks); err != nil {
		t.Fatalf("keys 不是合法 JSON: %v", err)
	}
	if len(ks) != len(entries) {
		t.Fatalf("keys 有 %d 条，想要 %d", len(ks), len(entries))
	}
	for i, k := range ks {
		if len(k) != 2 {
			t.Fatalf("keys[%d] 有 %d 个键，想要 2（provider_id / external_id）", i, len(k))
		}
		if k["provider_id"] != entries[i].ProviderID || k["external_id"] != entries[i].ExternalID {
			t.Fatalf("keys[%d] = %#v，与 entries[%d] 对不上", i, k, i)
		}
	}
}
