package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/api"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/demo"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/identity"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/platform/storage"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

// 模型声明的四件事：
//
//  1. 目录只出「启用」的模型——候选模型不该出现在投稿表单里。
//  2. 投稿按 slug 声明，声明写进**这一版**（MODEL-007 说版本是事实源）。
//  3. 改稿换模型不改对外可见的声明：作者改稿期间，访客看的仍是已发布那一版。
//  4. 回滚把声明一起带过去，否则「回到上一版」会悄悄丢掉模型。
//
// 第 3、4 条最容易做成「改了库里的数就以为改对了」——它们钉的是版本轴。

func newModelTestHandler(t *testing.T) (*Handler, *identity.Service, *pgxpool.Pool) {
	t.Helper()
	pool := integrationPool(t)
	q := store.New(pool)
	idSvc := integrationIdentity(t, pool)
	files, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("初始化存储失败: %v", err)
	}
	demoSvc := demo.New(pool, q, files, storage.DefaultLimits(), 200<<20, 0, nil)
	return New(pool, q, idSvc, nil, demoSvc), idSvc, pool
}

// insertModel 直接写目录表：`models` 是外部目录，没有写入端点，
// 生产上由导入任务填（见 docs/已知松点.md 第 44 条）。
func insertModel(t *testing.T, pool *pgxpool.Pool, slug, name, status string) {
	t.Helper()
	ctx := context.Background()
	if _, err := pool.Exec(ctx,
		`INSERT INTO models (slug, name, vendor, status) VALUES ($1, $2, '测试厂商', $3)
		 ON CONFLICT (slug) DO UPDATE SET name = excluded.name, status = excluded.status`,
		slug, name, status); err != nil {
		t.Fatalf("插入模型 %s 失败: %v", slug, err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM models WHERE slug = $1`, slug)
	})
}

// submitHTMLWithModels 是 submitHTML 的多选版：同一个 multipart 表单多带 modelSlugs。
// 重复的 key 才是「多选」——`r.FormValue` 只返回第一个值，所以表单参数用
// `mw.WriteField` 逐条写，解析侧走 `formValues`。
func submitHTMLWithModels(t *testing.T, h *Handler, sid, title, body string, modelIDs []string) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if err := mw.WriteField("title", title); err != nil {
		t.Fatal(err)
	}
	if err := mw.WriteField("description", "简介"); err != nil {
		t.Fatal(err)
	}
	if err := mw.WriteField("demoType", "web"); err != nil {
		t.Fatal(err)
	}
	for _, id := range modelIDs {
		if err := mw.WriteField("modelSlugs", id); err != nil {
			t.Fatal(err)
		}
	}
	part, err := mw.CreateFormFile("package", "index.html")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte(body)); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/demos", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: sid})
	rec := httptest.NewRecorder()
	h.SubmitDemo(rec, req, api.SubmitDemoParams{})
	return rec
}

// updateWithModels 发一次只改标题 + 重声明模型的更新。
func updateWithModels(t *testing.T, h *Handler, sid, slug, title string, modelIDs []string) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if err := mw.WriteField("title", title); err != nil {
		t.Fatal(err)
	}
	for _, id := range modelIDs {
		if err := mw.WriteField("modelSlugs", id); err != nil {
			t.Fatal(err)
		}
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPut, "/api/v1/demos/"+slug, &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: sid})
	rec := httptest.NewRecorder()
	NewRouter(h, nil, nil).ServeHTTP(rec, req)
	return rec
}

// modelSlugsOf 从详情/列表响应里抠出模型 slug，顺序无关（目录按 name 排）。
func modelSlugsOf(t *testing.T, rec *httptest.ResponseRecorder) []string {
	t.Helper()
	var body struct {
		Models []struct {
			Slug string `json:"slug"`
		} `json:"models"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("解析详情失败: %v；响应体：%s", err, rec.Body.String())
	}
	out := make([]string, 0, len(body.Models))
	for _, m := range body.Models {
		out = append(out, m.Slug)
	}
	return out
}

// 详情里的模型对象与目录里的是**同一个形状**（同一个 `Model` schema）。
//
// 早先详情只回 slug/name/vendor，目录也差不多——于是「这个模型可不可信」在前端
// 无处可查。现在两处字段面一致，前端只写一套渲染。
func TestIntegrationDetailModelHasCatalogFields(t *testing.T) {
	h, idSvc, pool := newModelTestHandler(t)
	insertModel(t, pool, "it-model-detail", "测试详情模型", "active")

	author, sid := registerVerified(t, pool, idSvc, identity.RegisterInput{
		Email:       "it-model-detail@example.com",
		Password:    "modeltest1234",
		Username:    "it-model-det",
		DisplayName: "模型详情作者",
	})
	t.Cleanup(func() { cleanupAuthor(t, pool, author.Sub) })

	rec := submitHTMLWithModels(t, h, sid, "详情字段面", `<!doctype html><p>d</p>`,
		[]string{"it-model-detail"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("投稿 = %d，想要 201；响应体：%s", rec.Code, rec.Body.String())
	}
	var created struct {
		Slug string `json:"slug"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("解析投稿回执失败: %v", err)
	}

	detail := getDemoAs(t, h, created.Slug, sid)
	if detail.Code != http.StatusOK {
		t.Fatalf("作者读详情 = %d，想要 200；响应体：%s", detail.Code, detail.Body.String())
	}
	var body struct {
		Models []struct {
			Slug           string   `json:"slug"`
			Status         string   `json:"status"`
			Resolution     string   `json:"resolution"`
			MergedIntoSlug string   `json:"mergedIntoSlug"`
			Source         string   `json:"source"`
			Aliases        []string `json:"aliases"`
			Description    string   `json:"description"`
		} `json:"models"`
	}
	if err := json.Unmarshal(detail.Body.Bytes(), &body); err != nil {
		t.Fatalf("解析详情失败: %v", err)
	}
	if len(body.Models) != 1 {
		t.Fatalf("详情里的模型数 = %d，想要 1", len(body.Models))
	}
	m := body.Models[0]
	if m.Status != "active" || m.Resolution != "exact" || m.Source != "manual" {
		t.Fatalf("详情的模型字段面不完整：%+v", m)
	}
	if m.Aliases == nil {
		t.Fatalf("详情的 aliases 是 null，应该是空数组")
	}
}

// 模型目录的字段面。
//
// 契约里的 `status` 字段早先永远等于 `active`——因为查询写死了 `WHERE status='active'`，
// 那等于给了个常量。现在默认出全部状态、可以按 status 筛，筛选责任交回调用方。
func TestIntegrationListModels(t *testing.T) {
	h, _, pool := newModelTestHandler(t)
	insertModel(t, pool, "it-model-active", "测试甲模型", "active")
	insertModel(t, pool, "it-model-candidate", "测试乙模型", "candidate")

	var body struct {
		Items []struct {
			Slug           string   `json:"slug"`
			Name           string   `json:"name"`
			Status         string   `json:"status"`
			Resolution     string   `json:"resolution"`
			MergedIntoSlug string   `json:"mergedIntoSlug"`
			Source         string   `json:"source"`
			Aliases        []string `json:"aliases"`
		} `json:"items"`
	}

	// 1) 不筛状态：两种状态都在，字段面完整。
	rec := httptest.NewRecorder()
	h.ListModels(rec, httptest.NewRequest(http.MethodGet, "/api/v1/models", nil), api.ListModelsParams{})
	if rec.Code != http.StatusOK {
		t.Fatalf("模型目录 = %d，想要 200；响应体：%s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("解析模型目录失败: %v", err)
	}
	seen := map[string]string{}
	for _, m := range body.Items {
		seen[m.Slug] = m.Status
	}
	if _, ok := seen["it-model-active"]; !ok {
		t.Fatalf("启用的模型没出现在目录里：%+v", body.Items)
	}
	if seen["it-model-candidate"] != "candidate" {
		t.Fatalf("候选模型应出现在目录里且 status=candidate，实际 %q", seen["it-model-candidate"])
	}
	if body.Items == nil {
		t.Fatalf("items 是 null，应该是空数组")
	}
	// 没被上游认领过就是 manual；别名没写过就是 []（不是 null）。
	for _, m := range body.Items {
		if m.Slug != "it-model-active" && m.Slug != "it-model-candidate" {
			continue
		}
		if m.Source != "manual" {
			t.Fatalf("%s 的 source = %q，没有 model_external_refs 行时应为 manual", m.Slug, m.Source)
		}
		if m.Aliases == nil {
			t.Fatalf("%s 的 aliases 是 null，应该是空数组", m.Slug)
		}
		if m.Resolution != "exact" {
			t.Fatalf("%s 的 resolution = %q，默认应为 exact", m.Slug, m.Resolution)
		}
	}

	// 2) 按状态筛：只出 active 的（表单那边就是传这个）。
	active := api.ListModelsParamsStatusActive
	rec = httptest.NewRecorder()
	h.ListModels(rec, httptest.NewRequest(http.MethodGet, "/api/v1/models?status=active", nil),
		api.ListModelsParams{Status: &active})
	if rec.Code != http.StatusOK {
		t.Fatalf("按状态筛 = %d，想要 200；响应体：%s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("解析模型目录失败: %v", err)
	}
	for _, m := range body.Items {
		if m.Status != "active" {
			t.Fatalf("筛 active 之后出现了 %s（status=%s）", m.Slug, m.Status)
		}
	}
}

// 合并归宿与别名：deprecated 的模型要指出它并到了谁，别名要能从目录读出来。
func TestIntegrationListModelsShowsMergeAndAliases(t *testing.T) {
	h, _, pool := newModelTestHandler(t)
	insertModel(t, pool, "it-merged-src", "测试旧名模型", "deprecated")
	insertModel(t, pool, "it-merged-dst", "测试新名模型", "active")

	if _, err := pool.Exec(context.Background(),
		`UPDATE models SET merged_into_id = (SELECT id FROM models WHERE slug = 'it-merged-dst')
		  WHERE slug = 'it-merged-src'`); err != nil {
		t.Fatalf("造合并关系失败: %v", err)
	}
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO model_aliases (alias, model_id)
		 VALUES ('it-merged-src-alias', (SELECT id FROM models WHERE slug = 'it-merged-src'))
		 ON CONFLICT (alias) DO NOTHING`); err != nil {
		t.Fatalf("造别名失败: %v", err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(),
			`DELETE FROM model_aliases WHERE alias = 'it-merged-src-alias'`); err != nil {
			t.Logf("清理别名失败: %v", err)
		}
	})

	rec := httptest.NewRecorder()
	h.ListModels(rec, httptest.NewRequest(http.MethodGet, "/api/v1/models", nil), api.ListModelsParams{})
	if rec.Code != http.StatusOK {
		t.Fatalf("模型目录 = %d，想要 200；响应体：%s", rec.Code, rec.Body.String())
	}

	var body struct {
		Items []struct {
			Slug           string   `json:"slug"`
			MergedIntoSlug string   `json:"mergedIntoSlug"`
			Aliases        []string `json:"aliases"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("解析模型目录失败: %v", err)
	}
	var found bool
	for _, m := range body.Items {
		if m.Slug != "it-merged-src" {
			continue
		}
		found = true
		if m.MergedIntoSlug != "it-merged-dst" {
			t.Fatalf("mergedIntoSlug = %q，想要 it-merged-dst", m.MergedIntoSlug)
		}
		if len(m.Aliases) != 1 || m.Aliases[0] != "it-merged-src-alias" {
			t.Fatalf("aliases = %v，想要 [it-merged-src-alias]", m.Aliases)
		}
	}
	if !found {
		t.Fatalf("目录里没找到 it-merged-src")
	}
}

func TestIntegrationSubmitDeclaresModels(t *testing.T) {
	h, idSvc, pool := newModelTestHandler(t)
	insertModel(t, pool, "it-model-active", "测试甲模型", "active")
	insertModel(t, pool, "it-model-candidate", "测试乙模型", "candidate")

	author, sid := registerVerified(t, pool, idSvc, identity.RegisterInput{
		Email:       "it-model@example.com",
		Password:    "modeltest1234",
		Username:    "it-model",
		DisplayName: "模型作者",
	})
	t.Cleanup(func() { cleanupAuthor(t, pool, author.Sub) })

	// 1) 正常声明：作者在详情里看得到自己声明的模型。
	rec := submitHTMLWithModels(t, h, sid, "声明模型的作品", `<!doctype html><p>m</p>`,
		[]string{"it-model-active"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("投稿 = %d，想要 201；响应体：%s", rec.Code, rec.Body.String())
	}
	var created struct {
		Slug string `json:"slug"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("解析投稿回执失败: %v", err)
	}
	got := modelSlugsOf(t, getDemoAs(t, h, created.Slug, sid))
	if len(got) != 1 || got[0] != "it-model-active" {
		t.Fatalf("作者看详情 models = %v，想要 [it-model-active]", got)
	}

	// 2) 不存在的模型：整次投稿失败，而不是「悄悄少声明一个」。
	rec = submitHTMLWithModels(t, h, sid, "声明了不存在的模型", `<!doctype html><p>m</p>`,
		[]string{"it-model-active", "it-model-nope"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("声明不存在的模型 = %d，想要 400；响应体：%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !bytes.Contains([]byte(body), []byte("it-model-nope")) {
		t.Fatalf("400 的文案要点出是哪个模型不对，实际：%s", body)
	}
	// 「模型不存在」是输入问题，不是落库故障：文案里不该出现「落库失败」，
	// 否则作者会以为是我们这边坏了，去做无意义的等待或反馈。
	if bytes.Contains([]byte(body), []byte("落库失败")) {
		t.Fatalf("输入错误被说成了落库失败：%s", body)
	}

	// 3) 候选（未启用）模型：同样拒绝——目录里选不到，接口也不能被绕过。
	rec = submitHTMLWithModels(t, h, sid, "声明候选模型", `<!doctype html><p>m</p>`,
		[]string{"it-model-candidate"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("声明候选模型 = %d，想要 400；响应体：%s", rec.Code, rec.Body.String())
	}

	// 4) 一次都不传：合法，models 是空数组而不是 null。
	slug := submitHTML(t, h, sid, "没有声明模型", `<!doctype html><p>m</p>`)
	detail := getDemoAs(t, h, slug, sid)
	if detail.Code != http.StatusOK {
		t.Fatalf("详情 = %d，想要 200", detail.Code)
	}
	if got := modelSlugsOf(t, detail); len(got) != 0 {
		t.Fatalf("没声明模型的详情 models = %v，想要空", got)
	}
}

func TestIntegrationModelDeclarationFollowsRevision(t *testing.T) {
	h, idSvc, pool := newModelTestHandler(t)
	insertModel(t, pool, "it-model-v1", "测试一号模型", "active")
	insertModel(t, pool, "it-model-v2", "测试二号模型", "active")

	author, sid := registerVerified(t, pool, idSvc, identity.RegisterInput{
		Email:       "it-model-rev@example.com",
		Password:    "modelrev1234",
		Username:    "it-model-rev",
		DisplayName: "版本模型作者",
	})
	t.Cleanup(func() { cleanupAuthor(t, pool, author.Sub) })

	rec := submitHTMLWithModels(t, h, sid, "版本里的模型声明", `<!doctype html><p>v1</p>`,
		[]string{"it-model-v1"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("投稿 = %d，想要 201；响应体：%s", rec.Code, rec.Body.String())
	}
	var created struct {
		Slug string `json:"slug"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("解析投稿回执失败: %v", err)
	}
	slug := created.Slug
	publishNow(t, pool, slug)

	if got := modelSlugsOf(t, getDemoAs(t, h, slug, "")); len(got) != 1 || got[0] != "it-model-v1" {
		t.Fatalf("访客在发布后看 models = %v，想要 [it-model-v1]", got)
	}

	// 作者改成另一个模型：只写进新版本。
	if rec := updateWithModels(t, h, sid, slug, "换过模型的作品", []string{"it-model-v2"}); rec.Code != http.StatusOK {
		t.Fatalf("更新 = %d，想要 200；响应体：%s", rec.Code, rec.Body.String())
	}
	if got := modelSlugsOf(t, getDemoAs(t, h, slug, sid)); len(got) != 1 || got[0] != "it-model-v2" {
		t.Fatalf("作者看当前版本的 models = %v，想要 [it-model-v2]", got)
	}
	// 关键：访客仍看已发布那一版——改稿不该改动线上内容。
	if got := modelSlugsOf(t, getDemoAs(t, h, slug, "")); len(got) != 1 || got[0] != "it-model-v1" {
		t.Fatalf("访客在作者改稿后看 models = %v，想要仍是 [it-model-v1]", got)
	}

	// 回滚到第 1 版：声明要跟着回来，而不是留在第 2 版上。
	if rec := restoreRevision(t, h, sid, slug, "1"); rec.Code != http.StatusCreated && rec.Code != http.StatusOK {
		t.Fatalf("回滚 = %d；响应体：%s", rec.Code, rec.Body.String())
	}
	if got := modelSlugsOf(t, getDemoAs(t, h, slug, sid)); len(got) != 1 || got[0] != "it-model-v1" {
		t.Fatalf("回滚后的 models = %v，想要 [it-model-v1]", got)
	}
}

// 墓碑与已删除的作品不披露模型声明：它和标题一样属于内容。
func TestIntegrationDeletedDemoHidesModels(t *testing.T) {
	h, idSvc, pool := newModelTestHandler(t)
	insertModel(t, pool, "it-model-hidden", "测试隐藏模型", "active")

	author, sid := registerVerified(t, pool, idSvc, identity.RegisterInput{
		Email:       "it-model-del@example.com",
		Password:    "modeldel1234",
		Username:    "it-model-del",
		DisplayName: "删除模型作者",
	})
	t.Cleanup(func() { cleanupAuthor(t, pool, author.Sub) })

	rec := submitHTMLWithModels(t, h, sid, "会被删掉的作品", `<!doctype html><p>d</p>`,
		[]string{"it-model-hidden"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("投稿 = %d，想要 201", rec.Code)
	}
	var created struct {
		Slug string `json:"slug"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("解析投稿回执失败: %v", err)
	}
	publishNow(t, pool, created.Slug)

	if rec := lifecycleCall(t, h, http.MethodDelete, created.Slug, sid, nil); rec.Code != http.StatusOK {
		t.Fatalf("删除 = %d，想要 200；响应体：%s", rec.Code, rec.Body.String())
	}
	if got := modelSlugsOf(t, getDemoAs(t, h, created.Slug, sid)); len(got) != 0 {
		t.Fatalf("已删除作品的 models = %v，想要空", got)
	}
}
