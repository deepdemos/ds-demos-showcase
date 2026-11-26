package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/api"
)

// 上游条目公开搜索（#589）：模型选择器的数据源。
//
// 这组用例钉的是**对外承诺**，不是实现细节：未登录可用、已收录时给站内 slug、
// 关键词里的通配符按字面匹配、最多 20 条、不泄漏导入溯源。
// 「一次查 20 条」与「不排除上游弃用」是设计口径（epic #585 的 §上游条目公开搜索），
// 不是顺手加的便利——选择器要能搜到灰测 / 弃用名，也要一次就能扫完候选。

// getUpstreamSearch 发一次公开搜索请求。sid 传空串就是未登录。
func getUpstreamSearch(t *testing.T, h *Handler, q, sid string) *httptest.ResponseRecorder {
	t.Helper()
	path := "/api/v1/models/upstream"
	if q != "" {
		path += "?q=" + url.QueryEscape(q)
	}
	return getWithSession(t, h, path, sid)
}

func decodeUpstreamSearch(t *testing.T, rec *httptest.ResponseRecorder) api.UpstreamModelList {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d，想要 200；body=%s", rec.Code, rec.Body.String())
	}
	var got api.UpstreamModelList
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("解析失败: %v；body=%s", err, rec.Body.String())
	}
	return got
}

// findByExternalID 在结果里找一条。找不到返回 nil（用例自己决定怎么报）。
func findByExternalID(items []api.UpstreamModel, externalID string) *api.UpstreamModel {
	for i := range items {
		if items[i].ExternalId == externalID {
			return &items[i]
		}
	}
	return nil
}

// 公开只读：不带会话也能搜；返回的字段就是「名称 / 发布方 / 规范标识 / 上游状态 / 已收录 slug」，
// 多一个都算泄漏——「谁导的、什么时候导的」不该出现在公开响应里。
func TestIntegrationUpstreamSearchIsPublic(t *testing.T) {
	h, _, pool := newModelTestHandler(t)

	u := uniq()
	prov := "it-ups-" + u
	putCatalogItem(t, pool, prov, "flash", "公开搜索-"+u, map[string]any{"canonical": "it-ups/" + u})

	rec := getUpstreamSearch(t, h, u, "")
	got := decodeUpstreamSearch(t, rec)

	hit := findByExternalID(got.Items, "flash")
	if hit == nil {
		t.Fatalf("没搜到刚写进去的条目（%s）；body=%s", u, rec.Body.String())
	}
	if hit.Source != "models.dev" {
		t.Fatalf("source = %q，想要 models.dev", hit.Source)
	}
	if hit.ProviderId != prov {
		t.Fatalf("providerId = %q，想要 %q", hit.ProviderId, prov)
	}
	if hit.ProviderName != prov {
		t.Fatalf("providerName = %q，想要 %q", hit.ProviderName, prov)
	}
	if hit.Name != "公开搜索-"+u {
		t.Fatalf("name = %q，想要 公开搜索-%s", hit.Name, u)
	}
	if hit.CanonicalId != "it-ups/"+u {
		t.Fatalf("canonicalId = %q，想要 it-ups/%s", hit.CanonicalId, u)
	}
	if hit.Slug != nil {
		t.Fatalf("未收录的条目不该给 slug，实际 %q", *hit.Slug)
	}

	// 字段集是白名单：多一个键就是「把导入溯源端出来了」。
	var raw struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	allowed := map[string]bool{
		"source": true, "providerId": true, "providerName": true, "externalId": true,
		"name": true, "canonicalId": true, "upstreamStatus": true, "slug": true,
	}
	for _, item := range raw.Items {
		for key := range item {
			if !allowed[key] {
				t.Fatalf("公开响应里出现了不该有的字段 %q（只允许名称 / 发布方 / 规范标识 / 上游状态 / 已收录 slug）", key)
			}
		}
	}
}

// 已收录的条目给站内 slug，未收录给 null。这是选择器把条目提到第一层的唯一依据。
func TestIntegrationUpstreamSearchMarksClaimedSlug(t *testing.T) {
	h, _, pool := newModelTestHandler(t)

	u := uniq()
	prov := "it-upsl-" + u
	slug := "it-upsl-model-" + u
	insertModel(t, pool, slug, "已收录模型-"+u, "active")
	putCatalogItem(t, pool, prov, "claimed", "已收录条目-"+u, nil)
	putCatalogItem(t, pool, prov, "open", "未收录条目-"+u, nil)

	// 认领行：这条上游条目 → 那个站内模型（导入链路写的就是这一行）。
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO model_offerings (authority, provider_id, provider_name, external_id, model_id, is_official)
		 SELECT 'models.dev', $1, $1, 'claimed', id, true FROM models WHERE slug = $2`,
		prov, slug); err != nil {
		t.Fatalf("写认领行失败: %v", err)
	}

	got := decodeUpstreamSearch(t, getUpstreamSearch(t, h, u, ""))

	claimed := findByExternalID(got.Items, "claimed")
	if claimed == nil {
		t.Fatalf("没搜到已收录那条（%s）", u)
	}
	if claimed.Slug == nil || *claimed.Slug != slug {
		t.Fatalf("已收录条目的 slug = %v，想要 %q", claimed.Slug, slug)
	}

	open := findByExternalID(got.Items, "open")
	if open == nil {
		t.Fatalf("没搜到未收录那条（%s）", u)
	}
	if open.Slug != nil {
		t.Fatalf("未收录条目的 slug = %q，想要 null", *open.Slug)
	}
}

// 关键词里的 % _ \ 按字面匹配：不转义的话 % 与 _ 会变成通配符，
// 而「搜一个百分号」会变成「把整份镜像吐回来」。
//
// 每对条目只差一个字符：字面量那条带真字符，陪跑那条带普通字符。
// 转义坏了的时候陪跑那条会被通配符命中，断言立刻红。
func TestIntegrationUpstreamSearchTreatsWildcardsLiterally(t *testing.T) {
	h, _, pool := newModelTestHandler(t)

	u := uniq()
	prov := "it-upsw-" + u
	putCatalogItem(t, pool, prov, "pct-lit", "百分号%-"+u, nil)
	putCatalogItem(t, pool, prov, "pct-any", "百分号X-"+u, nil)
	putCatalogItem(t, pool, prov, "und-lit", "下划线_-"+u, nil)
	putCatalogItem(t, pool, prov, "und-any", "下划线X-"+u, nil)
	putCatalogItem(t, pool, prov, "bsl-lit", "反斜杠\\-"+u, nil)
	putCatalogItem(t, pool, prov, "bsl-any", "反斜杠-"+u, nil)

	cases := []struct {
		q, want, not string
	}{
		{"百分号%-" + u, "pct-lit", "pct-any"},
		{"下划线_-" + u, "und-lit", "und-any"},
		{"反斜杠\\-" + u, "bsl-lit", "bsl-any"},
	}
	for _, tc := range cases {
		got := decodeUpstreamSearch(t, getUpstreamSearch(t, h, tc.q, ""))
		if findByExternalID(got.Items, tc.want) == nil {
			t.Fatalf("q=%q 该命中 %s，实际 = %s", tc.q, tc.want, namesOfUpstream(got.Items))
		}
		if findByExternalID(got.Items, tc.not) != nil {
			t.Fatalf("q=%q 把 %s 也命中了——通配符没被按字面转义，实际 = %s",
				tc.q, tc.not, namesOfUpstream(got.Items))
		}
	}
}

// 不分页 = 只给前 20 条。多给一条就是契约说谎（描述里写死了 20）。
func TestIntegrationUpstreamSearchCapsAtTwenty(t *testing.T) {
	h, _, pool := newModelTestHandler(t)

	u := uniq()
	prov := "it-upsc-" + u
	for i := 0; i < 21; i++ {
		putCatalogItem(t, pool, prov, fmt.Sprintf("cap-%02d", i),
			fmt.Sprintf("上限-%s-%02d", u, i), nil)
	}

	got := decodeUpstreamSearch(t, getUpstreamSearch(t, h, "上限-"+u, ""))
	if len(got.Items) != 20 {
		t.Fatalf("返回条数 = %d，想要 20（21 条命中里只给前 20）", len(got.Items))
	}
}

// 默认不排除上游已弃用：上游 deprecated 是「厂商不再维护」，
// 与本站 status='deprecated'（已合并）是两套语义。选择器要能搜到灰测 / 弃用名。
func TestIntegrationUpstreamSearchIncludesUpstreamDeprecated(t *testing.T) {
	h, _, pool := newModelTestHandler(t)

	u := uniq()
	prov := "it-upsd-" + u
	putCatalogItem(t, pool, prov, "dead", "上游弃用-"+u,
		map[string]any{"upstreamStatus": "deprecated"})

	got := decodeUpstreamSearch(t, getUpstreamSearch(t, h, u, ""))
	hit := findByExternalID(got.Items, "dead")
	if hit == nil {
		t.Fatalf("上游弃用的条目被吞掉了（%s）；实际 = %s", u, namesOfUpstream(got.Items))
	}
	if hit.UpstreamStatus != "deprecated" {
		t.Fatalf("upstreamStatus = %q，想要 deprecated（原样交回去，由调用方决定怎么标）",
			hit.UpstreamStatus)
	}
}

// 关键词超过契约声明的 100 字回 400，而不是静默截断——
// 截断会让用户以为搜的是全部，搜不到时还会怀疑「明明有这几个字」。
func TestIntegrationUpstreamSearchRejectsLongKeyword(t *testing.T) {
	h, _, _ := newModelTestHandler(t)

	rec := getUpstreamSearch(t, h, strings.Repeat("模", maxSearchLen+1), "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("超长关键词状态码 = %d，想要 400；body=%s", rec.Code, rec.Body.String())
	}
	var apiErr api.Error
	if err := json.Unmarshal(rec.Body.Bytes(), &apiErr); err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if apiErr.Code != api.InvalidParam {
		t.Fatalf("错误码 = %q，想要 invalid_param", apiErr.Code)
	}
}

func namesOfUpstream(items []api.UpstreamModel) string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.ExternalId+"="+item.Name)
	}
	return strings.Join(out, " | ")
}
