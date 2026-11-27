package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/api"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/identity"
)

// 手动新建模型（模型域 §4.7.3）：管理员手填一个名字，就得到一个**直接可用**的模型。
//
// 这组用例钉的是「谁有资格建、建出来是什么形状、什么情况下必须被拒」：
//
//  1. 建出来的模型立刻出现在目录与详情页，状态是 active、名称精确度是 exact；
//  2. 每次新建写一条审计（动作 create、实体 model）；
//  3. slug 由服务端给建议值（纯函数，同一输入稳定），可以改，但改坏了要拒；
//  4. 撞名 / 撞 slug 返回 409 并指向已有那条——不自动加后缀；
//  5. 系统兜底名（unspecified / x-unknown / <厂商>-unknown）一律不许手填；
//  6. 没有 model.create 权限的账号 403。

// postManualModel 走真路由发一次手动新建。
func postManualModel(t *testing.T, h *Handler, sid, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/models", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if sid != "" {
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: sid})
	}
	rec := httptest.NewRecorder()
	NewRouter(h, nil, nil).ServeHTTP(rec, req)
	return rec
}

// getSlugSuggestion 读一次 slug 建议值。
func getSlugSuggestion(t *testing.T, h *Handler, query, sid string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/models/slug-suggestion"+query, nil)
	if sid != "" {
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: sid})
	}
	rec := httptest.NewRecorder()
	NewRouter(h, nil, nil).ServeHTTP(rec, req)
	return rec
}

// purgeManualModel 按 slug 收干净：模型行与它的审计都要删，否则下一次同名的用例
// 会撞在上一轮留下的行上——那种红看起来像实现坏了。
func purgeManualModel(t *testing.T, pool *pgxpool.Pool, slug string) {
	t.Helper()
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `
DELETE FROM audit_log
 WHERE entity_type = 'model'
   AND entity_id IN (SELECT id FROM models WHERE slug = $1)`, slug); err != nil {
		t.Fatalf("清审计失败: %v", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM models WHERE slug = $1`, slug); err != nil {
		t.Fatalf("清模型失败: %v", err)
	}
}

// manualModelBody 拼一个请求体；空字段不写进去（契约里 slug / vendor 都可省略）。
func manualModelBody(fields map[string]string) string {
	parts := make([]string, 0, len(fields))
	for _, key := range []string{"name", "slug", "vendor"} {
		if v, ok := fields[key]; ok {
			parts = append(parts, `"`+key+`":"`+v+`"`)
		}
	}
	return "{" + strings.Join(parts, ",") + "}"
}

// TestIntegrationManualModelCreate 是这张票的主验收：点下去就有一个可用的模型。
func TestIntegrationManualModelCreate(t *testing.T) {
	h, idSvc, pool := newModelTestHandler(t)
	h.WithPool(pool)
	sid := mkAdmin(t, pool, idSvc, "mw")
	ctx := context.Background()

	name := "IT 手动模型 " + uniq()
	rec := postManualModel(t, h, sid, manualModelBody(map[string]string{"name": name, "vendor": "itvendor"}))
	if rec.Code != http.StatusCreated {
		t.Fatalf("新建模型状态码 = %d，想要 201；body=%s", rec.Code, rec.Body.String())
	}
	var created api.Model
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("解析模型失败: %v", err)
	}
	t.Cleanup(func() { purgeManualModel(t, pool, created.Slug) })

	if created.Name != name {
		t.Fatalf("模型名 = %q，想要 %q", created.Name, name)
	}
	// 管理员点下去就是背书：没有候选状态这一步。
	if created.Status != api.ModelStatusActive {
		t.Fatalf("状态 = %q，想要 active", created.Status)
	}
	// 人工建的就是确定的型号，表单不提供分辨率选项。
	if created.Resolution != api.Exact {
		t.Fatalf("名称精确度 = %q，想要 exact", created.Resolution)
	}
	if created.Source != api.Manual {
		t.Fatalf("来源 = %q，想要 manual", created.Source)
	}
	if created.Slug == "" {
		t.Fatal("slug 为空：slug 是公开 URL 的一部分，不能等到第一次读取才生成")
	}
	if created.Vendor != "itvendor" {
		t.Fatalf("厂商 = %q，想要 itvendor", created.Vendor)
	}
	if created.Description != "" {
		t.Fatalf("描述 = %q，想要空串：手动新建不该凭空生成说明", created.Description)
	}

	// 立刻出现在目录与详情页——这是「直接可用」的可执行定义。
	var list api.ModelList
	rec = getWithSession(t, h, "/api/v1/models", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("目录状态码 = %d，想要 200", rec.Code)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("解析目录失败: %v", err)
	}
	found := false
	for _, m := range list.Items {
		if m.Slug == created.Slug {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("新建的模型 %s 没有出现在目录里", created.Slug)
	}

	rec = getWithSession(t, h, "/api/v1/models/"+created.Slug, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("详情页状态码 = %d，想要 200；body=%s", rec.Code, rec.Body.String())
	}

	// 审计：新建写一条 create，实体是 model。
	var action string
	var auditCount int
	rows, err := pool.Query(ctx, `
SELECT a.action
  FROM audit_log a
  JOIN models m ON m.id = a.entity_id
 WHERE a.entity_type = 'model' AND m.slug = $1`, created.Slug)
	if err != nil {
		t.Fatalf("查审计失败: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		if err := rows.Scan(&action); err != nil {
			t.Fatalf("读审计失败: %v", err)
		}
		auditCount++
	}
	if auditCount != 1 {
		t.Fatalf("审计条数 = %d，想要 1", auditCount)
	}
	if action != "create" {
		t.Fatalf("审计动作 = %q，想要 create（复用注册表已有动作，不新增键）", action)
	}
}

// TestIntegrationManualModelCreateSlugSuggestion 表单要预填、也能改；建议值必须与
// 「不传 slug 时服务端实际写下的值」一致，否则表单里看到的是一个根本不存在的地址。
func TestIntegrationManualModelCreateSlugSuggestion(t *testing.T) {
	h, idSvc, pool := newModelTestHandler(t)
	h.WithPool(pool)
	sid := mkAdmin(t, pool, idSvc, "mws")

	var suggestion struct {
		Slug string `json:"slug"`
	}
	rec := getSlugSuggestion(t, h, "?name=IT+Suggest+Model&vendor=itvendor", sid)
	if rec.Code != http.StatusOK {
		t.Fatalf("建议值状态码 = %d，想要 200；body=%s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &suggestion); err != nil {
		t.Fatalf("解析建议值失败: %v", err)
	}
	// 名字折得出 ASCII 就直接用它：可读的 URL 是这条规则的全部目的。
	if suggestion.Slug != "it-suggest-model" {
		t.Fatalf("建议 slug = %q，想要 it-suggest-model", suggestion.Slug)
	}

	// 中文名折不出 ASCII：退「厂商-slug + 名字摘要」，而不是退成 model-2。
	rec = getSlugSuggestion(t, h, "?name=%E6%B7%B1%E5%BA%A6%E6%80%9D%E8%80%83&vendor=deepseek", sid)
	if rec.Code != http.StatusOK {
		t.Fatalf("中文名建议值状态码 = %d，想要 200；body=%s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &suggestion); err != nil {
		t.Fatalf("解析中文名建议值失败: %v", err)
	}
	if !strings.HasPrefix(suggestion.Slug, "deepseek-") {
		t.Fatalf("中文名建议 slug = %q，想要 deepseek-<摘要> 形态（#586 的规则）", suggestion.Slug)
	}

	// 不传 slug 时服务端必须写下的就是建议值：两处一旦分叉，表单里看到的地址是假的。
	name := "IT 建议一致 " + uniq()
	rec = getSlugSuggestion(t, h, "?name="+url.QueryEscape(name), sid)
	var next struct {
		Slug string `json:"slug"`
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("建议值状态码 = %d，想要 200", rec.Code)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &next); err != nil {
		t.Fatalf("解析建议值失败: %v", err)
	}
	rec = postManualModel(t, h, sid, manualModelBody(map[string]string{"name": name}))
	if rec.Code != http.StatusCreated {
		t.Fatalf("新建模型状态码 = %d，想要 201；body=%s", rec.Code, rec.Body.String())
	}
	var created api.Model
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("解析模型失败: %v", err)
	}
	t.Cleanup(func() { purgeManualModel(t, pool, created.Slug) })
	if created.Slug != next.Slug {
		t.Fatalf("实际写入 slug = %q，建议值 = %q", created.Slug, next.Slug)
	}
}

// TestIntegrationManualModelCreateRejectsFallbackNames 兜底名是系统维护的实体，
// 手填会把「说不清用了什么」伪装成「确定了某个型号」。
func TestIntegrationManualModelCreateRejectsFallbackNames(t *testing.T) {
	h, idSvc, pool := newModelTestHandler(t)
	h.WithPool(pool)
	sid := mkAdmin(t, pool, idSvc, "mwf")

	for _, name := range []string{"unspecified", "Unspecified", "x-unknown", "acme-unknown", "x-family", "x-guess"} {
		rec := postManualModel(t, h, sid, manualModelBody(map[string]string{"name": name, "slug": "it-fallback-" + uniq()}))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("兜底名 %q 状态码 = %d，想要 400；body=%s", name, rec.Code, rec.Body.String())
		}
		var e api.Error
		if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil {
			t.Fatalf("解析错误体失败: %v", err)
		}
		if e.Code != api.InvalidParam {
			t.Fatalf("兜底名 %q 错误码 = %q，想要 invalid_param", name, e.Code)
		}
	}

	// 只是「看起来像」不算：这些名字是正常模型名，不该被拒。
	for _, name := range []string{"unknown-model", "unspecified-model", "x-family-2"} {
		slug := "it-ok-" + uniq()
		rec := postManualModel(t, h, sid, manualModelBody(map[string]string{"name": name + " " + slug, "slug": slug}))
		if rec.Code != http.StatusCreated {
			t.Fatalf("正常名字 %q 状态码 = %d，想要 201；body=%s", name, rec.Code, rec.Body.String())
		}
		t.Cleanup(func() { purgeManualModel(t, pool, slug) })
	}
}

// TestIntegrationManualModelCreateRejectsDuplicate 撞名要给 409 并指向已有那条：
// 自动加后缀会造出一个永久难看的展示名，而管理员本想的是「补全这条」。
func TestIntegrationManualModelCreateRejectsDuplicate(t *testing.T) {
	h, idSvc, pool := newModelTestHandler(t)
	h.WithPool(pool)
	sid := mkAdmin(t, pool, idSvc, "mwd")

	name := "IT 撞名模型 " + uniq()
	first := postManualModel(t, h, sid, manualModelBody(map[string]string{"name": name}))
	if first.Code != http.StatusCreated {
		t.Fatalf("首次新建状态码 = %d，想要 201；body=%s", first.Code, first.Body.String())
	}
	var created api.Model
	if err := json.Unmarshal(first.Body.Bytes(), &created); err != nil {
		t.Fatalf("解析模型失败: %v", err)
	}
	t.Cleanup(func() { purgeManualModel(t, pool, created.Slug) })

	// 同名、给一个不同的 slug：撞的是 name 上的唯一键。
	rec := postManualModel(t, h, sid, manualModelBody(map[string]string{"name": name, "slug": "it-dup-" + uniq()}))
	if rec.Code != http.StatusConflict {
		t.Fatalf("同名状态码 = %d，想要 409；body=%s", rec.Code, rec.Body.String())
	}
	var conflict map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &conflict); err != nil {
		t.Fatalf("解析冲突体失败: %v", err)
	}
	if conflict["code"] != "conflict" {
		t.Fatalf("冲突错误码 = %v，想要 conflict", conflict["code"])
	}
	if slug, _ := conflict["existingSlug"].(string); slug != created.Slug {
		t.Fatalf("冲突里的 existingSlug = %v，想要 %q", conflict["existingSlug"], created.Slug)
	}

	// 换个名字、占已有的 slug：撞的是 slug 上的唯一键。
	rec = postManualModel(t, h, sid, manualModelBody(map[string]string{"name": "IT 别的名字 " + uniq(), "slug": created.Slug}))
	if rec.Code != http.StatusConflict {
		t.Fatalf("撞 slug 状态码 = %d，想要 409；body=%s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &conflict); err != nil {
		t.Fatalf("解析冲突体失败: %v", err)
	}
	if slug, _ := conflict["existingSlug"].(string); slug != created.Slug {
		t.Fatalf("撞 slug 时 existingSlug = %v，想要 %q", conflict["existingSlug"], created.Slug)
	}

	// 冲突不该留下半截行：同名只该有一条。
	var count int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM models WHERE name = $1`, name).Scan(&count); err != nil {
		t.Fatalf("查重失败: %v", err)
	}
	if count != 1 {
		t.Fatalf("同名模型条数 = %d，想要 1", count)
	}
}

// TestIntegrationManualModelCreateValidatesSlug 表单可改 slug，所以长度闸必须在服务端。
//
// 「清空输入框」与「根本没填」必须等价：显式传空串不该变成「提交一个空 slug」，
// 也不该报错——表单里把预填值删掉是最常见的动作。
func TestIntegrationManualModelCreateValidatesSlug(t *testing.T) {
	h, idSvc, pool := newModelTestHandler(t)
	h.WithPool(pool)
	sid := mkAdmin(t, pool, idSvc, "mwv")

	for _, slug := range []string{"", "   "} {
		rec := postManualModel(t, h, sid, manualModelBody(map[string]string{"name": "IT 空白 slug " + uniq(), "slug": slug}))
		if rec.Code != http.StatusCreated {
			t.Fatalf("空白 slug %q 状态码 = %d，想要 201（按省略处理）；body=%s", slug, rec.Code, rec.Body.String())
		}
		var created api.Model
		if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
			t.Fatalf("解析模型失败: %v", err)
		}
		t.Cleanup(func() { purgeManualModel(t, pool, created.Slug) })
		if created.Slug == "" {
			t.Fatal("省略 slug 时服务端必须补一个：空 slug 进不了库，也进不了 URL")
		}
	}

	// 超长 slug：上限 128 在库的 ck_models_slug_len 里，服务端要把那条约束翻成 400 而不是 503。
	rec := postManualModel(t, h, sid, manualModelBody(map[string]string{"name": "IT 长 slug " + uniq(), "slug": strings.Repeat("a", 129)}))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("超长 slug 状态码 = %d，想要 400；body=%s", rec.Code, rec.Body.String())
	}
	var e api.Error
	if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil {
		t.Fatalf("解析错误体失败: %v", err)
	}
	if e.Code != api.InvalidParam {
		t.Fatalf("错误码 = %q，想要 invalid_param", e.Code)
	}

	// 名称为空也要拒：name 是这条模型唯一的人话标识。
	rec = postManualModel(t, h, sid, manualModelBody(map[string]string{"slug": "it-noname-" + uniq()}))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("空名称状态码 = %d，想要 400；body=%s", rec.Code, rec.Body.String())
	}
}

// TestIntegrationManualModelCreatePermissions 两个端点都只认 model.create：
// 没这个键的账号连建议值都不该拿到（它是「我要建模型」这条路的入口）。
func TestIntegrationManualModelCreatePermissions(t *testing.T) {
	h, idSvc, pool := newModelTestHandler(t)
	h.WithPool(pool)

	tag := uniq()
	acc, sid := registerVerified(t, pool, idSvc, identity.RegisterInput{
		Email:       "it-mwp-" + tag + "@example.com",
		Password:    "importtest123",
		Username:    "it-mwp-" + tag[:8],
		DisplayName: "手动新建权限测试",
	})
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE sub = $1`, acc.Sub)
		_, _ = pool.Exec(context.Background(), `DELETE FROM accounts WHERE sub = $1`, acc.Sub)
	})

	rec := postManualModel(t, h, sid, manualModelBody(map[string]string{"name": "IT 无权限 " + uniq()}))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("无权限新建状态码 = %d，想要 403；body=%s", rec.Code, rec.Body.String())
	}
	var e api.Error
	if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil {
		t.Fatalf("解析错误体失败: %v", err)
	}
	if e.Code != api.Forbidden {
		t.Fatalf("错误码 = %q，想要 forbidden", e.Code)
	}

	rec = getSlugSuggestion(t, h, "?name=IT+No+Perm", sid)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("无权限读建议值状态码 = %d，想要 403；body=%s", rec.Code, rec.Body.String())
	}

	// 未登录是 401，不是 403：两者含义不同，混起来会让人以为登录坏了。
	rec = postManualModel(t, h, "", manualModelBody(map[string]string{"name": "IT 未登录 " + uniq()}))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("未登录状态码 = %d，想要 401；body=%s", rec.Code, rec.Body.String())
	}
}
