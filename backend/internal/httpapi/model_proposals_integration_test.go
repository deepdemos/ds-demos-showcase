package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/api"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/demo"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/platform/storage"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

// 随投稿 / 编辑提交模型收录提案（#590）：
//
//  1. 一次投稿可同时声明站内已有模型 + 提多个未入库模型（上游三元组 / 纯手填）→ 各自产生待审提案；
//  2. 同一作品重复提交同名模型（同次请求内变体或编辑再提）不产生重复待审提案；
//  3. 不同作品提同名模型各自产生独立待审提案（去重按「作品 × 规范化模型名」）；
//  4. 管理端提案列表与详情返回来源作品 sourceDemo（slug + title）；
//  5. 畸形 / 缺必填 / 半截上游三元组一律 400 且不落任何作品与提案行；
//  6. 通用提案入口 POST /api/v1/proposals 对 kind='new_model' 一律 422（不开放）。

func submitDemoWithModelProposals(
	t *testing.T,
	h *Handler,
	sid, title string,
	modelSlugs []string,
	modelProposals []string,
) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if err := mw.WriteField("title", title); err != nil {
		t.Fatal(err)
	}
	if err := mw.WriteField("demoType", "web"); err != nil {
		t.Fatal(err)
	}
	for _, s := range modelSlugs {
		if err := mw.WriteField("modelSlugs", s); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range modelProposals {
		if err := mw.WriteField("modelProposals", p); err != nil {
			t.Fatal(err)
		}
	}
	part, err := mw.CreateFormFile("package", "index.html")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte(`<!doctype html><p>model proposal test</p>`)); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/demos", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: sid})
	rec := httptest.NewRecorder()
	NewRouter(h, nil, nil).ServeHTTP(rec, req)
	return rec
}

func updateDemoWithModelProposals(
	t *testing.T,
	h *Handler,
	sid, slug, title string,
	modelSlugs []string,
	modelProposals []string,
) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if err := mw.WriteField("title", title); err != nil {
		t.Fatal(err)
	}
	for _, s := range modelSlugs {
		if err := mw.WriteField("modelSlugs", s); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range modelProposals {
		if err := mw.WriteField("modelProposals", p); err != nil {
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

func TestIntegrationModelProposalsFromSubmitAndUpdate(t *testing.T) {
	pool := integrationPool(t)
	q := store.New(pool)
	idSvc := integrationIdentity(t, pool)
	files, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("初始化存储失败: %v", err)
	}
	demoSvc := demo.New(pool, q, files, storage.DefaultLimits(), 200<<20, 0, nil)
	h := New(pool, q, idSvc, nil, demoSvc)
	ctx := context.Background()

	tag := uniq()
	author, sid := registerVerified(t, pool, idSvc, registerInputFor(t, "mp-author-"+tag[:8]))
	adminSID := mkAdmin(t, pool, idSvc, "mp-adm")
	existingSlug := "it-mp-existing-" + tag
	insertModel(t, pool, existingSlug, "IT Existing "+tag, "active")

	// LIFO 清理：先删 proposals，再删 author（cleanupAuthor 删 demos / users / accounts）。
	t.Cleanup(func() { cleanupAuthor(t, pool, author.Sub) })
	t.Cleanup(func() {
		if _, err := pool.Exec(ctx, `DELETE FROM proposals WHERE created_by = $1`, author.UserID); err != nil {
			t.Errorf("清理模型提案失败: %v", err)
		}
	})

	// 1. 一次投稿：同时声明站内已有模型 + 提交 2 个未入库模型（1 上游三元组 + 1 纯手填）+ 1 个同次重复变体。
	rec := submitDemoWithModelProposals(
		t, h, sid, "模型提案测试作品一",
		[]string{existingSlug},
		[]string{
			`{"name":"DeepSeek V4 Preview","vendor":"DeepSeek","source":"models.dev","providerId":"deepseek","externalId":"deepseek-v4-preview"}`,
			`{"name":"  deepseek   v4  preview ","vendor":"Other"}`,
			`{"name":"自研私有模型 Alpha","vendor":"自研团队"}`,
		},
	)
	if rec.Code != http.StatusCreated {
		t.Fatalf("投稿状态码 = %d，想要 201；body=%s", rec.Code, rec.Body.String())
	}
	var sub1 api.SubmissionResult
	if err := json.Unmarshal(rec.Body.Bytes(), &sub1); err != nil {
		t.Fatalf("解析投稿回执失败: %v", err)
	}
	if sub1.ModelProposalIds == nil || len(*sub1.ModelProposalIds) != 2 {
		t.Fatalf("应新建 2 条提案，实际回执 = %+v", sub1.ModelProposalIds)
	}

	// 核对站内已有模型仍正常挂在第一版声明上（不回退既有行为）。
	var declaredCount int
	if err := pool.QueryRow(ctx, `
SELECT count(*) FROM demo_models dm
JOIN demos d ON d.id = dm.demo_id
JOIN models m ON m.id = dm.model_id
WHERE d.slug = $1 AND m.slug = $2`, sub1.Slug, existingSlug).Scan(&declaredCount); err != nil {
		t.Fatalf("查站内模型声明失败: %v", err)
	}
	if declaredCount != 1 {
		t.Fatalf("站内已有模型声明条数 = %d，想要 1", declaredCount)
	}

	// 2. 改稿同一作品再次提交同名模型 + 新增 1 个新模型 → 同名去重跳过、只新建 1 条。
	rec = updateDemoWithModelProposals(
		t, h, sid, sub1.Slug, "模型提案测试作品一（新版）",
		[]string{existingSlug},
		[]string{
			`{"name":"DEEPSEEK V4 PREVIEW"}`,
			`{"name":"自研私有模型 Beta"}`,
		},
	)
	if rec.Code != http.StatusOK {
		t.Fatalf("改稿状态码 = %d，想要 200；body=%s", rec.Code, rec.Body.String())
	}
	var upd api.SubmissionResult
	if err := json.Unmarshal(rec.Body.Bytes(), &upd); err != nil {
		t.Fatalf("解析改稿回执失败: %v", err)
	}
	if upd.ModelProposalIds == nil || len(*upd.ModelProposalIds) != 1 {
		t.Fatalf("改稿应只新建 1 条提案（另一条同作品去重），实际 = %+v", upd.ModelProposalIds)
	}

	// 3. 另一件作品提交同一个模型名 → 各自产生独立提案（去重按作品 × 模型名）。
	rec = submitDemoWithModelProposals(
		t, h, sid, "模型提案测试作品二",
		nil,
		[]string{`{"name":"DeepSeek V4 Preview","vendor":"DeepSeek"}`},
	)
	if rec.Code != http.StatusCreated {
		t.Fatalf("第二件作品投稿状态码 = %d，想要 201；body=%s", rec.Code, rec.Body.String())
	}
	var sub2 api.SubmissionResult
	if err := json.Unmarshal(rec.Body.Bytes(), &sub2); err != nil {
		t.Fatalf("解析第二件作品回执失败: %v", err)
	}
	if sub2.ModelProposalIds == nil || len(*sub2.ModelProposalIds) != 1 {
		t.Fatalf("不同作品同名模型应独立建 1 条提案，实际 = %+v", sub2.ModelProposalIds)
	}

	// 4. 管理端提案列表与详情必须能看到来源作品 sourceDemo。
	listRec := httptest.NewRecorder()
	h.ListAdminProposals(listRec, proposalReq(http.MethodGet, "/api/v1/admin/proposals?kind=new_model", adminSID, ""),
		api.ListAdminProposalsParams{Kind: strPtr("new_model")})
	if listRec.Code != http.StatusOK {
		t.Fatalf("管理端提案列表 = %d，想要 200；body=%s", listRec.Code, listRec.Body.String())
	}
	var plist api.ProposalList
	if err := json.Unmarshal(listRec.Body.Bytes(), &plist); err != nil {
		t.Fatalf("解析提案列表失败: %v", err)
	}
	firstPropID := (*sub1.ModelProposalIds)[0]
	var foundInList *api.ProposalSummary
	for i := range plist.Items {
		if plist.Items[i].Id == firstPropID {
			foundInList = &plist.Items[i]
			break
		}
	}
	if foundInList == nil {
		t.Fatalf("提案列表未找到 #%d", firstPropID)
	}
	if foundInList.SourceDemo == nil || foundInList.SourceDemo.Slug != sub1.Slug {
		t.Fatalf("列表来源作品不对：%+v，想要 slug=%q", foundInList.SourceDemo, sub1.Slug)
	}

	detailRec := httptest.NewRecorder()
	h.GetAdminProposal(detailRec, proposalReq(http.MethodGet, "/api/v1/admin/proposals/x", adminSID, ""), firstPropID)
	if detailRec.Code != http.StatusOK {
		t.Fatalf("提案详情 = %d，想要 200；body=%s", detailRec.Code, detailRec.Body.String())
	}
	var pdetail api.ProposalDetail
	if err := json.Unmarshal(detailRec.Body.Bytes(), &pdetail); err != nil {
		t.Fatalf("解析提案详情失败: %v", err)
	}
	if pdetail.SourceDemo == nil || pdetail.SourceDemo.Slug != sub1.Slug {
		t.Fatalf("详情来源作品不对：%+v", pdetail.SourceDemo)
	}
	if pdetail.Payload == nil {
		t.Fatalf("提案 payload 为空")
	}
	if (*pdetail.Payload)["provider_id"] != "deepseek" || (*pdetail.Payload)["external_id"] != "deepseek-v4-preview" ||
		(*pdetail.Payload)["demo_slug"] != sub1.Slug {
		t.Fatalf("提案 payload 字段不全：%+v", *pdetail.Payload)
	}
}

func TestIntegrationModelProposalsRejectsInvalidAndGenericEndpoint(t *testing.T) {
	pool := integrationPool(t)
	q := store.New(pool)
	idSvc := integrationIdentity(t, pool)
	files, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("初始化存储失败: %v", err)
	}
	demoSvc := demo.New(pool, q, files, storage.DefaultLimits(), 200<<20, 0, nil)
	h := New(pool, q, idSvc, nil, demoSvc)
	ctx := context.Background()

	tag := uniq()
	author, sid := registerVerified(t, pool, idSvc, registerInputFor(t, "mp-rej-"+tag[:8]))
	t.Cleanup(func() { cleanupAuthor(t, pool, author.Sub) })
	t.Cleanup(func() {
		if _, err := pool.Exec(ctx, `DELETE FROM proposals WHERE created_by = $1`, author.UserID); err != nil {
			t.Errorf("清理提案失败: %v", err)
		}
	})

	// 1. 通用提案入口 POST /api/v1/proposals 不开放 new_model → 422。
	genericRec := submitProposal(t, h, sid, `{"kind":"new_model","payload":{"name":"绕过作品的模型"}}`)
	if genericRec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("通用提案入口提交 new_model 状态码 = %d，想要 422；body=%s", genericRec.Code, genericRec.Body.String())
	}

	// 2. 投稿附带半截上游三元组 → 400，且不落作品与提案。
	badSubmit := submitDemoWithModelProposals(
		t, h, sid, "非法提案投稿",
		nil,
		[]string{`{"name":"Bad Upstream","providerId":"deepseek"}`},
	)
	if badSubmit.Code != http.StatusBadRequest {
		t.Fatalf("半截上游三元组投稿 = %d，想要 400；body=%s", badSubmit.Code, badSubmit.Body.String())
	}

	var demoCount, propCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM demos WHERE author_id = $1`, author.UserID).Scan(&demoCount); err != nil {
		t.Fatalf("查作品数失败: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM proposals WHERE created_by = $1`, author.UserID).Scan(&propCount); err != nil {
		t.Fatalf("查提案数失败: %v", err)
	}
	if demoCount != 0 || propCount != 0 {
		t.Fatalf("被拒请求不该落库，实际 demos=%d proposals=%d", demoCount, propCount)
	}

	// 3. 先成功投一件作品，再改稿时传非法 modelProposals → 400，版本号不变、零新提案。
	okSubmit := submitDemoWithModelProposals(t, h, sid, "正常作品供改稿反例", nil, nil)
	if okSubmit.Code != http.StatusCreated {
		t.Fatalf("前置投稿 = %d，想要 201；body=%s", okSubmit.Code, okSubmit.Body.String())
	}
	var sub api.SubmissionResult
	if err := json.Unmarshal(okSubmit.Body.Bytes(), &sub); err != nil {
		t.Fatalf("解析前置回执失败: %v", err)
	}
	badUpdate := updateDemoWithModelProposals(t, h, sid, sub.Slug, "改稿失败用例", nil, []string{`{"name":""}`})
	if badUpdate.Code != http.StatusBadRequest {
		t.Fatalf("改稿空模型名提案 = %d，想要 400；body=%s", badUpdate.Code, badUpdate.Body.String())
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM proposals WHERE created_by = $1`, author.UserID).Scan(&propCount); err != nil {
		t.Fatalf("查提案数失败: %v", err)
	}
	if propCount != 0 {
		t.Fatalf("改稿被拒后不该落提案，实际 %d", propCount)
	}
}
