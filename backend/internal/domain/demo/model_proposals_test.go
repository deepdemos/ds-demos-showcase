package demo

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateModelProposalsSuccessAndDedupe(t *testing.T) {
	raw := []string{
		`{"name":" DeepSeek  V4 Preview ","vendor":"DeepSeek","providerId":"deepseek","externalId":"deepseek-v4-preview"}`,
		// 同一次请求内大小写 / 空格变体：应当按规范化名去重，只留第一条。
		`{"name":"deepseek v4 preview","vendor":"Other"}`,
		// 纯手填（无上游三元组、无厂商）。
		`{"name":"内部灰测模型 A"}`,
	}
	got, err := validateModelProposals(raw)
	if err != nil {
		t.Fatalf("合法提案不应报错: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("去重后条数 = %d，想要 2；got=%+v", len(got), got)
	}
	if got[0].Name != "DeepSeek  V4 Preview" ||
		got[0].NormalizedName != "deepseek v4 preview" ||
		got[0].Vendor != "DeepSeek" ||
		got[0].Source != "models.dev" ||
		got[0].ProviderID != "deepseek" ||
		got[0].ExternalID != "deepseek-v4-preview" {
		t.Fatalf("第 1 条规整结果不对: %+v", got[0])
	}
	if got[1].Name != "内部灰测模型 A" || got[1].Source != "" || got[1].ProviderID != "" {
		t.Fatalf("第 2 条规整结果不对: %+v", got[1])
	}
}

func TestValidateModelProposalsRejectsInvalidInputs(t *testing.T) {
	tooMany := make([]string, maxModelProposals+1)
	for i := range tooMany {
		tooMany[i] = `{"name":"M"}`
	}
	cases := []struct {
		name string
		raw  []string
	}{
		{"非 JSON", []string{`not-json`}},
		{"未知字段", []string{`{"name":"M","provider_id":"deepseek"}`}},
		{"尾部多余内容", []string{`{"name":"M"}{"name":"N"}`}},
		{"空名称", []string{`{"name":"   "}`}},
		{"名称超长", []string{`{"name":"` + strings.Repeat("a", maxModelNameLen+1) + `"}`}},
		{"厂商超长", []string{`{"name":"M","vendor":"` + strings.Repeat("v", maxProposalVendorLen+1) + `"}`}},
		{"只给 providerId 缺 externalId", []string{`{"name":"M","providerId":"deepseek"}`}},
		{"只给 externalId 缺 providerId", []string{`{"name":"M","externalId":"m-1"}`}},
		{"只给 source 缺三元组", []string{`{"name":"M","source":"models.dev"}`}},
		{"单条 JSON 超长", []string{`{"name":"M","vendor":"` + strings.Repeat("x", maxProposalJSONLen) + `"}`}},
		{"名称含 NUL 字符", []string{`{"name":"foo\u0000bar"}`}},
		{"厂商含 NUL 字符", []string{`{"name":"M","vendor":"v\u00001"}`}},
		{"上游标识含 NUL 字符", []string{`{"name":"M","providerId":"p\u00001","externalId":"e1"}`}},
		{"非法 UTF-8", []string{"{\"name\":\"\xff\"}"}},
		{"条数超上限", tooMany},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := validateModelProposals(tc.raw)
			if !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("%s: err = %v，想要 ErrInvalidInput", tc.name, err)
			}
		})
	}
}
