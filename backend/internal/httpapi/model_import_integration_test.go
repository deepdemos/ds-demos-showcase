package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/api"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/identity"
)

// 模型导入（模型域 §4.6 / §4.7.4）：两步、只写勾选的、新建是 active。
//
// 这组用例钉的是「导入不会把手写的东西冲掉」这件事的每一块凭据：
// preview 的三条不可勾（值相同 / 人工写过 / 被锁）、apply 只写 fields、
// 以及「候选条目被点过之后才叫 active」这个状态语义。

// putCatalogItem 往 models.dev 本地镜像里放一条上游条目。
//
// 镜像由同步任务填（模型域 §4.6.1），这里直接写是为了把「同步」排除在用例边界外。
func putCatalogItem(t *testing.T, pool *pgxpool.Pool, providerID, externalID, name string, opts map[string]any) {
	t.Helper()
	source := "models.dev"
	if v, ok := opts["source"].(string); ok {
		source = v
	}
	canonical := name
	if v, ok := opts["canonical"].(string); ok {
		canonical = v
	}
	status := "active"
	if v, ok := opts["upstreamStatus"].(string); ok {
		status = v
	}
	desc := "上游描述"
	if v, ok := opts["description"].(string); ok {
		desc = v
	}

	_, err := pool.Exec(context.Background(), `
INSERT INTO model_catalog_items
    (source, provider_id, provider_name, external_id, name, canonical_id,
     vendor_slug, description, modalities_in, modalities_out,
     context_window, max_output, cost_in, cost_out, upstream_status)
VALUES ($1, $2, $2, $3, $4, $5, $6, $7, 'text', 'text', 128000, 8192, 0.5, 1.5, $8)
ON CONFLICT (source, provider_id, external_id) DO UPDATE
   SET name = EXCLUDED.name, description = EXCLUDED.description,
       context_window = EXCLUDED.context_window, upstream_status = EXCLUDED.upstream_status`,
		source, providerID, externalID, name, canonical, providerID, desc, status)
	if err != nil {
		t.Fatalf("写镜像条目失败: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(),
			`DELETE FROM model_catalog_items WHERE source = $1 AND provider_id = $2 AND external_id = $3`,
			source, providerID, externalID)
	})
}

// mkAdmin 建一个 admin 账号并返回会话 id。
func mkAdmin(t *testing.T, pool *pgxpool.Pool, idSvc *identity.Service, tag string) string {
	t.Helper()
	acc, sid := registerVerified(t, pool, idSvc, identity.RegisterInput{
		Email:       "it-imp-" + tag + "@example.com",
		Password:    "importtest123",
		Username:    "it-imp-" + tag,
		DisplayName: "导入测试" + tag,
	})
	if _, err := pool.Exec(context.Background(),
		`UPDATE accounts SET role = 'admin' WHERE sub = $1`, acc.Sub); err != nil {
		t.Fatalf("提权失败: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE sub = $1`, acc.Sub)
		_, _ = pool.Exec(context.Background(), `DELETE FROM accounts WHERE sub = $1`, acc.Sub)
	})
	return sid
}

func getWithSession(t *testing.T, h *Handler, path, sid string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if sid != "" {
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: sid})
	}
	rec := httptest.NewRecorder()
	NewRouter(h, nil, nil).ServeHTTP(rec, req)
	return rec
}

func postJSONWithSession(t *testing.T, h *Handler, path, sid, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if sid != "" {
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: sid})
	}
	rec := httptest.NewRecorder()
	NewRouter(h, nil, nil).ServeHTTP(rec, req)
	return rec
}

// TestIntegrationModelCatalogSearch 目录搜索：默认滤掉上游已弃用，显式要求时能带上。
//
// 上游弃用（厂商不再维护）与我们自己的 `merged_into_id` 是两套语义，
// 所以两个开关也分开：`includeDeprecated` 只管前者。
func TestIntegrationModelCatalogSearch(t *testing.T) {
	h, idSvc, pool := newModelTestHandler(t)
	putCatalogItem(t, pool, "it-prov", "it-cat-live", "IT Catalog Live", nil)
	purgeImportLeftovers(t, pool, "it-prov", "it-cat-live")
	putCatalogItem(t, pool, "it-prov", "it-cat-dead", "IT Catalog Dead", map[string]any{"upstreamStatus": "deprecated"})
	purgeImportLeftovers(t, pool, "it-prov", "it-cat-dead")

	sid := mkAdmin(t, pool, idSvc, "cat")

	rec := getWithSession(t, h, "/api/v1/admin/models/catalog?q=IT+Catalog", sid)
	if rec.Code != http.StatusOK {
		t.Fatalf("目录搜索状态码 = %d，想要 200；body=%s", rec.Code, rec.Body.String())
	}
	var list api.ModelCatalogList
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	names := make([]string, 0, len(list.Items))
	for _, it := range list.Items {
		names = append(names, it.Name)
	}
	joined := strings.Join(names, ",")
	if strings.Contains(joined, "IT Catalog Dead") {
		t.Fatalf("默认不该返回上游已弃用的条目，实际 = %s", joined)
	}
	if !strings.Contains(joined, "IT Catalog Live") {
		t.Fatalf("该返回在用条目，实际 = %s", joined)
	}

	rec = getWithSession(t, h, "/api/v1/admin/models/catalog?q=IT+Catalog&includeDeprecated=true", sid)
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(list.Items) < 2 {
		t.Fatalf("includeDeprecated=true 时条数 = %d，想要 ≥2", len(list.Items))
	}
}

// TestIntegrationModelImportPreviewCreates 未被认领的条目 → mode=create，没有 target。
func TestIntegrationModelImportPreviewCreates(t *testing.T) {
	h, idSvc, pool := newModelTestHandler(t)
	putCatalogItem(t, pool, "it-prov", "it-cat-new", "IT Catalog New", map[string]any{"canonical": "itprov/IT-Catalog-New"})
	purgeImportLeftovers(t, pool, "it-prov", "it-cat-new")
	sid := mkAdmin(t, pool, idSvc, "prev")

	body := `{"source":"models.dev","providerId":"it-prov","externalId":"it-cat-new","canonicalId":"itprov/IT-Catalog-New"}`
	rec := postJSONWithSession(t, h, "/api/v1/admin/models/import/preview", sid, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("preview 状态码 = %d，想要 200；body=%s", rec.Code, rec.Body.String())
	}
	var pv api.ModelImportPreview
	if err := json.Unmarshal(rec.Body.Bytes(), &pv); err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if pv.Mode != "create" {
		t.Fatalf("mode = %q，想要 create", pv.Mode)
	}
	if pv.Target != nil {
		t.Fatalf("新建时不该有 target")
	}
	if len(pv.Fields) != 12 {
		t.Fatalf("字段数 = %d，想要 12", len(pv.Fields))
	}
	for _, f := range pv.Fields {
		if f.Field == "name" && !f.Selectable {
			t.Fatalf("新建时 name 应该可勾（上游有值、本地为空）")
		}
	}
}

// purgeImportLeftovers 清掉某条上游条目此前认领过的本地模型。
//
// 必须**在用例开始时**清，而不是只在 t.Cleanup 里清：上一轮如果红在中途
// （或被人为破坏跑过一次），认领行会留下来，下一次 apply 就会走「补全」而不是
// 「新建」——用例于是去断言 status=active，却拿到上一轮遗留的 candidate。
// 这个坑真实发生过：反向验证故意把新建写成 candidate 跑红一次之后，
// 还原代码仍然红，红在遗留数据上。
func purgeImportLeftovers(t *testing.T, pool *pgxpool.Pool, providerID, externalID string) {
	t.Helper()
	ctx := context.Background()
	rows, err := pool.Query(ctx, `
SELECT model_id FROM model_offerings
 WHERE authority = 'models.dev' AND provider_id = $1 AND external_id = $2`, providerID, externalID)
	if err != nil {
		t.Fatalf("查遗留认领失败: %v", err)
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			t.Fatalf("读遗留认领失败: %v", err)
		}
		ids = append(ids, id)
	}
	rows.Close()
	for _, id := range ids {
		for _, stmt := range []string{
			`DELETE FROM model_spec_fields WHERE model_id = $1`,
			`DELETE FROM model_specs WHERE model_id = $1`,
			`DELETE FROM model_offerings WHERE model_id = $1`,
			`DELETE FROM model_aliases WHERE model_id = $1`,
			`DELETE FROM demo_revision_models WHERE model_id = $1`,
			`DELETE FROM audit_log WHERE entity_id = $1 AND entity_type = 'model'`,
			`DELETE FROM models WHERE id = $1`,
		} {
			if _, err := pool.Exec(ctx, stmt, id); err != nil {
				t.Fatalf("清理遗留模型失败: %v", err)
			}
		}
	}
}

// TestIntegrationModelImportPreviewMarksUnselectable 三条不可勾各有来源：
// 值相同、人工写过、被锁。
func TestIntegrationModelImportPreviewMarksUnselectable(t *testing.T) {
	h, idSvc, pool := newModelTestHandler(t)
	putCatalogItem(t, pool, "it-prov", "it-cat-diff", "IT Diff Name", map[string]any{
		"canonical":   "itprov/it-cat-diff",
		"description": "上游说明",
	})
	purgeImportLeftovers(t, pool, "it-prov", "it-cat-diff")
	sid := mkAdmin(t, pool, idSvc, "diff")

	// 先导入一次建出本地模型，再把本地 description 改成人工撰写并锁住 contextWindow。
	apply := `{"source":"models.dev","providerId":"it-prov","externalId":"it-cat-diff","canonicalId":"itprov/it-cat-diff","fields":["name","description","contextWindow"]}`
	rec := postJSONWithSession(t, h, "/api/v1/admin/models/import/apply", sid, apply)
	if rec.Code != http.StatusOK {
		t.Fatalf("apply 状态码 = %d，想要 200；body=%s", rec.Code, rec.Body.String())
	}
	var created api.Model
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if created.Status != api.ModelStatusActive {
		t.Fatalf("导入新建的模型 status 应为 active，实际 = %q", created.Status)
	}

	var modelID int64
	if err := pool.QueryRow(context.Background(),
		`SELECT id FROM models WHERE slug = $1`, created.Slug).Scan(&modelID); err != nil {
		t.Fatalf("查模型 id 失败: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM model_spec_fields WHERE model_id = $1`, modelID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM model_specs WHERE model_id = $1`, modelID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM model_offerings WHERE model_id = $1`, modelID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM audit_log WHERE entity_id = $1 AND entity_type = 'model'`, modelID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM models WHERE id = $1`, modelID)
	})

	// 人工把 description 改成自己的话（并标成 manual），锁住 contextWindow。
	if _, err := pool.Exec(context.Background(),
		`UPDATE models SET description = '人工写的说明' WHERE id = $1`, modelID); err != nil {
		t.Fatalf("改说明失败: %v", err)
	}
	for field, src := range map[string]string{"description": "manual", "contextWindow": "models.dev"} {
		if _, err := pool.Exec(context.Background(), `
INSERT INTO model_spec_fields (model_id, field, source, locked) VALUES ($1, $2, $3, $4)
ON CONFLICT (model_id, field) DO UPDATE SET source = EXCLUDED.source, locked = EXCLUDED.locked`,
			modelID, field, src, field == "contextWindow"); err != nil {
			t.Fatalf("写字段来源失败: %v", err)
		}
	}

	// 把上游值改得与本地不同：否则 `up == cur` 那条判据也会让 contextWindow 不可勾，
	// 用例就变成「无论锁不锁都不勾」，锁这一条根本没被压住（第一次写就踩了这个坑——
	// 去掉 `!isLocked` 之后仍然 PASS）。
	if _, err := pool.Exec(context.Background(), `
UPDATE model_catalog_items SET context_window = 256000
 WHERE source = 'models.dev' AND provider_id = 'it-prov' AND external_id = 'it-cat-diff'`); err != nil {
		t.Fatalf("改上游值失败: %v", err)
	}

	body := `{"source":"models.dev","providerId":"it-prov","externalId":"it-cat-diff","canonicalId":"itprov/it-cat-diff"}`
	rec = postJSONWithSession(t, h, "/api/v1/admin/models/import/preview", sid, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("preview 状态码 = %d；body=%s", rec.Code, rec.Body.String())
	}
	var pv api.ModelImportPreview
	if err := json.Unmarshal(rec.Body.Bytes(), &pv); err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if pv.Mode != "complete" {
		t.Fatalf("mode = %q，想要 complete", pv.Mode)
	}
	byField := map[string]api.ModelImportField{}
	for _, f := range pv.Fields {
		byField[f.Field] = f
	}
	if f := byField["name"]; f.Selectable {
		t.Fatalf("上游与本地的名字相同，不该可勾：%+v", f)
	}
	if f := byField["description"]; f.Selectable {
		t.Fatalf("本地说明是人工写的（source=manual），不该可勾：%+v", f)
	}
	if f := byField["contextWindow"]; f.Selectable {
		t.Fatalf("字段被锁了，不该可勾：%+v", f)
	} else if f.Upstream == f.Current {
		t.Fatalf("上游值与本地相同，这条用例压不住「锁」这条判据：%+v", f)
	}
}

// TestIntegrationModelImportApplyWritesSelectedOnly 只写勾选的字段，没勾的保持原样。
func TestIntegrationModelImportApplyWritesSelectedOnly(t *testing.T) {
	h, idSvc, pool := newModelTestHandler(t)
	putCatalogItem(t, pool, "it-prov", "it-cat-partial", "IT Partial", map[string]any{
		"canonical":   "itprov/it-cat-partial",
		"description": "上游说明",
	})
	purgeImportLeftovers(t, pool, "it-prov", "it-cat-partial")
	sid := mkAdmin(t, pool, idSvc, "part")

	apply := `{"source":"models.dev","providerId":"it-prov","externalId":"it-cat-partial","canonicalId":"itprov/it-cat-partial","fields":["contextWindow"]}`
	rec := postJSONWithSession(t, h, "/api/v1/admin/models/import/apply", sid, apply)
	if rec.Code != http.StatusOK {
		t.Fatalf("apply 状态码 = %d；body=%s", rec.Code, rec.Body.String())
	}
	var m api.Model
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("解析失败: %v", err)
	}

	var modelID int64
	var desc string
	if err := pool.QueryRow(context.Background(),
		`SELECT id, description FROM models WHERE slug = $1`, m.Slug).Scan(&modelID, &desc); err != nil {
		t.Fatalf("查模型失败: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM model_spec_fields WHERE model_id = $1`, modelID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM model_specs WHERE model_id = $1`, modelID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM model_offerings WHERE model_id = $1`, modelID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM audit_log WHERE entity_id = $1 AND entity_type = 'model'`, modelID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM models WHERE id = $1`, modelID)
	})

	if desc != "" {
		t.Fatalf("没勾 description 却把上游说明写进去了：%q", desc)
	}

	// 认领行的 is_official：canonical 前缀与 provider 一致才算官方条目。
	// `it-prov` vs `itprov/...` 不一致 → false。这条不写，`IsOfficial` 那条判据
	// 就没有用例压着（反向验证时改成恒 true 也仍然 PASS，就是证据）。
	var isOfficial bool
	if err := pool.QueryRow(context.Background(), `
SELECT is_official FROM model_offerings WHERE model_id = $1`, modelID).Scan(&isOfficial); err != nil {
		t.Fatalf("查认领行失败: %v", err)
	}
	if isOfficial {
		t.Fatalf("provider 与 canonical 前缀不一致时应记 is_official=false")
	}
	var cw *int
	if err := pool.QueryRow(context.Background(),
		`SELECT context_window FROM model_specs WHERE model_id = $1`, modelID).Scan(&cw); err != nil {
		t.Fatalf("查档案失败: %v", err)
	}
	if cw == nil || *cw != 128000 {
		t.Fatalf("勾了 contextWindow 却没写进去：%v", cw)
	}
}

// TestIntegrationModelImportCreateWritesOnlySelected 新建时也只写勾选的字段。
//
// 这一条是被实现抓出来的：第一版 `CreateModelFromCatalog` 无条件带上了上游
// `description`，理由是「新行本来就是空的」。但勾选清单是调用方看过 diff 之后的
// 决定，新建与补全没有区别——没勾 description 就不该有 description。
func TestIntegrationModelImportCreateWritesOnlySelected(t *testing.T) {
	h, idSvc, pool := newModelTestHandler(t)
	putCatalogItem(t, pool, "it-prov", "it-cat-create", "IT Create Only", map[string]any{
		"canonical":   "itprov/it-cat-create",
		"description": "上游说明",
	})
	purgeImportLeftovers(t, pool, "it-prov", "it-cat-create")
	sid := mkAdmin(t, pool, idSvc, "createonly")

	apply := `{"source":"models.dev","providerId":"it-prov","externalId":"it-cat-create","canonicalId":"itprov/it-cat-create","fields":["contextWindow"]}`
	rec := postJSONWithSession(t, h, "/api/v1/admin/models/import/apply", sid, apply)
	if rec.Code != http.StatusOK {
		t.Fatalf("apply 状态码 = %d；body=%s", rec.Code, rec.Body.String())
	}
	var m api.Model
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("解析失败: %v", err)
	}

	var modelID int64
	var desc string
	if err := pool.QueryRow(context.Background(),
		`SELECT id, description FROM models WHERE slug = $1`, m.Slug).Scan(&modelID, &desc); err != nil {
		t.Fatalf("查模型失败: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM model_spec_fields WHERE model_id = $1`, modelID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM model_specs WHERE model_id = $1`, modelID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM model_offerings WHERE model_id = $1`, modelID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM audit_log WHERE entity_id = $1 AND entity_type = 'model'`, modelID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM models WHERE id = $1`, modelID)
	})
	if desc != "" {
		t.Fatalf("新建时没勾 description 却写进了上游说明：%q", desc)
	}
}

// TestIntegrationModelImportRejectsEmptyFields 一个字段都没勾 → 400，不静默成功。
func TestIntegrationModelImportRejectsEmptyFields(t *testing.T) {
	h, idSvc, pool := newModelTestHandler(t)
	putCatalogItem(t, pool, "it-prov", "it-cat-empty", "IT Empty", nil)
	purgeImportLeftovers(t, pool, "it-prov", "it-cat-empty")
	sid := mkAdmin(t, pool, idSvc, "empty")

	body := `{"source":"models.dev","providerId":"it-prov","externalId":"it-cat-empty","fields":[]}`
	rec := postJSONWithSession(t, h, "/api/v1/admin/models/import/apply", sid, body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("空勾选状态码 = %d，想要 400；body=%s", rec.Code, rec.Body.String())
	}
}

// TestIntegrationModelImportPermissions 未登录 401、非管理员 403。
func TestIntegrationModelImportPermissions(t *testing.T) {
	h, idSvc, pool := newModelTestHandler(t)

	rec := getWithSession(t, h, "/api/v1/admin/models/catalog", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("未登录状态码 = %d，想要 401", rec.Code)
	}

	acc, sid := registerVerified(t, pool, idSvc, identity.RegisterInput{
		Email:       "it-imp-plain@example.com",
		Password:    "importtest123",
		Username:    "it-imp-plain",
		DisplayName: "普通用户",
	})
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE sub = $1`, acc.Sub)
		_, _ = pool.Exec(context.Background(), `DELETE FROM accounts WHERE sub = $1`, acc.Sub)
	})
	rec = getWithSession(t, h, "/api/v1/admin/models/catalog", sid)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("普通用户状态码 = %d，想要 403；body=%s", rec.Code, rec.Body.String())
	}
}
