package catalog

import (
	"encoding/json"
	"strings"
	"testing"
)

// fixture 是一小段形状与 upstream 一致的 JSON。
//
// 故意塞进四种真实存在的坏形状：缺 canonical（上游 17% 的条目如此）、缺 name、
// 畸形日期、`:free` 渠道后缀。解析器必须在不报错的前提下给出可落库的值——
// 9000 条里有一条畸形是常态，一条坏数据不该让整次同步失败。
const fixture = `{
  "deepseek": {
    "id": "deepseek",
    "name": "DeepSeek",
    "models": {
      "deepseek-flash": {
        "id": "deepseek-flash",
        "name": "DeepSeek V4.1 Flash",
        "description": "flash",
        "canonical_model_id": "deepseek/deepseek-v4.1-flash",
        "family": "deepseek-flash",
        "knowledge": "2025-05",
        "release_date": "2026-10-17",
        "open_weights": true,
        "tool_call": true,
        "reasoning": true,
        "structured_output": true,
        "attachment": false,
        "limit": {"context": 1048576, "output": 393216},
        "cost": {"input": 0.15, "output": 0.6},
        "modalities": {"input": ["text", "image"], "output": ["text"]}
      },
      "deepseek-free": {
        "id": "deepseek-free",
        "name": "DeepSeek Free:free",
        "canonical_model_id": "deepseek/deepseek-v4.1-flash",
        "release_date": "2026-10-99"
      }
    }
  },
  "bothub": {
    "id": "bothub",
    "name": "Bothub",
    "models": {
      "glm-5": {"id": "glm-5"}
    }
  }
}`

func parseFixture(t *testing.T) []Entry {
	t.Helper()
	entries, err := Parse([]byte(fixture))
	if err != nil {
		t.Fatalf("解析 fixture 失败: %v", err)
	}
	return entries
}

// 解析要给出确定的顺序：同一份字节两次解析必须行序一致，否则「同步是否幂等」
// 与「人工核对 diff」都没有基准。
func TestParseIsStable(t *testing.T) {
	first := parseFixture(t)
	second := parseFixture(t)
	if len(first) != len(second) {
		t.Fatalf("两次解析条数不同: %d vs %d", len(first), len(second))
	}
	for i := range first {
		if first[i].ProviderID != second[i].ProviderID || first[i].ExternalID != second[i].ExternalID {
			t.Fatalf("第 %d 条顺序不稳定: %s/%s vs %s/%s",
				i, first[i].ProviderID, first[i].ExternalID, second[i].ProviderID, second[i].ExternalID)
		}
	}
}

func TestParseProvidersAndModels(t *testing.T) {
	entries := parseFixture(t)
	if len(entries) != 3 {
		t.Fatalf("条目数 = %d，想要 3", len(entries))
	}
	// provider 按 id 排序，所以 bothub 在前。
	if entries[0].ProviderID != "bothub" || entries[0].ProviderName != "Bothub" {
		t.Fatalf("第一条 provider = %q/%q，想要 bothub/Bothub", entries[0].ProviderID, entries[0].ProviderName)
	}
	if entries[1].ProviderID != "deepseek" || entries[1].ExternalID != "deepseek-flash" {
		t.Fatalf("第二条 = %q/%q，想要 deepseek/deepseek-flash", entries[1].ProviderID, entries[1].ExternalID)
	}
}

func TestParseFieldsAreFlattened(t *testing.T) {
	e := parseFixture(t)[1]
	if e.ContextWindow == nil || *e.ContextWindow != 1048576 {
		t.Fatalf("context_window 没摊平: %v", e.ContextWindow)
	}
	if e.MaxOutput == nil || *e.MaxOutput != 393216 {
		t.Fatalf("max_output 没摊平: %v", e.MaxOutput)
	}
	if e.CostIn == nil || *e.CostIn != 0.15 {
		t.Fatalf("cost_in 没摊平: %v", e.CostIn)
	}
	if e.ModalitiesIn != "text,image" || e.ModalitiesOut != "text" {
		t.Fatalf("模态没折成逗号串: %q / %q", e.ModalitiesIn, e.ModalitiesOut)
	}
	if e.SupportsAttachment == nil || *e.SupportsAttachment != 0 {
		t.Fatalf("attachment=false 应折成 0，实际 %v", e.SupportsAttachment)
	}
	if e.SupportsToolCall == nil || *e.SupportsToolCall != 1 {
		t.Fatalf("tool_call=true 应折成 1，实际 %v", e.SupportsToolCall)
	}
	if e.VendorSlug != "deepseek" {
		t.Fatalf("vendor_slug = %q，想要 deepseek", e.VendorSlug)
	}
	if e.ReleaseDate != "2026-10-17" {
		t.Fatalf("release_date = %q，想要 2026-10-17", e.ReleaseDate)
	}
	if e.RawJSON == "" || !strings.Contains(e.RawJSON, "deepseek-flash") {
		t.Fatalf("raw_json 没留在行里: %q", e.RawJSON)
	}
}

// 上游 1325 条没有 canonical（17%）。这类条目必须照常落库，只是 vendor_slug 为空——
// 不能因为没有 canonical 就丢掉条目，那是把「上游收录了但没归类」当成「上游没有」。
func TestParseKeepsEntriesWithoutCanonical(t *testing.T) {
	entries := parseFixture(t)
	last := entries[0]
	if last.ExternalID != "glm-5" {
		t.Fatalf("第一条 = %q，想要 glm-5", last.ExternalID)
	}
	if last.CanonicalID != "" || last.VendorSlug != "" {
		t.Fatalf("没有 canonical 时不该编一个: canonical=%q vendor=%q", last.CanonicalID, last.VendorSlug)
	}
	// 没有 name 时退回外部 id：空名字在界面上是一行看不见的选项。
	if last.Name != "glm-5" {
		t.Fatalf("缺 name 时应退回外部 id，实际 %q", last.Name)
	}
}

// 畸形日期落空串（= SQL NULL），不报错也不硬塞。
func TestParseBadDateBecomesEmpty(t *testing.T) {
	entries := parseFixture(t)
	if entries[0].ReleaseDate != "" {
		t.Fatalf("第一个条目本来就没有日期，得到 %q", entries[0].ReleaseDate)
	}
	if entries[2].ReleaseDate != "" {
		t.Fatalf("`2026-10-99` 不是一天，应当落空，得到 %q", entries[2].ReleaseDate)
	}
	if got := Date("2026-10-99"); got != "" {
		t.Fatalf("畸形日期应当落空: %q", got)
	}
	if got := Date("2026-1-1"); got != "" {
		t.Fatalf("非补零日期应当落空: %q", got)
	}
}

// 空目录要当成错误：一次「成功」的空同步会把现有镜像清空，而运行时看起来一切正常。
func TestParseRejectsEmptyPayload(t *testing.T) {
	for _, raw := range []string{`{}`, `{"p": {"id": "p", "name": "P", "models": {}}}`} {
		if _, err := Parse([]byte(raw)); err == nil {
			t.Fatalf("空目录应当报错: %s", raw)
		}
	}
	if _, err := Parse([]byte("not json")); err == nil {
		t.Fatal("非 JSON 应当报错")
	}
}

func TestVendorSlug(t *testing.T) {
	cases := map[string]string{
		"deepseek/deepseek-v4-flash": "deepseek",
		"openai/gpt-5":               "openai",
		"no-slash":                   "",
		"/leading":                   "",
		"":                           "",
	}
	for in, want := range cases {
		if got := VendorSlug(in); got != want {
			t.Fatalf("VendorSlug(%q) = %q，想要 %q", in, got, want)
		}
	}
}

// Hash 必须只由字节决定：两次相同 payload 要给同一个指纹，否则「没变就不重写」
// 这条保护会永远失效（每次都当成变了）。
func TestHashIsStable(t *testing.T) {
	a := Hash([]byte(fixture))
	b := Hash([]byte(fixture))
	if a != b {
		t.Fatalf("同一份 payload 两次指纹不同: %s vs %s", a, b)
	}
	if a == Hash([]byte(fixture+" ")) {
		t.Fatal("内容变了指纹应当变")
	}
}

// 契约里 `ModelSyncResult.source` 恒为 `models.dev`，与这里的 Source 一致。
// 两边各有一个常量（httpapi 不 import platform 只为拿字符串），所以钉一条测试。
func TestSourceConstant(t *testing.T) {
	if Source != "models.dev" {
		t.Fatalf("Source = %q，契约与 httpapi 都写着 models.dev", Source)
	}
	// fixture 能被 encoding/json 解析成 map（形状检查）。
	var probe map[string]json.RawMessage
	if err := json.Unmarshal([]byte(fixture), &probe); err != nil {
		t.Fatalf("fixture 不是合法 JSON: %v", err)
	}
}
