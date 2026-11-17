package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/api"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/identity"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

// 端点上报（#445 的 4d）在 HTTP 层的验收。
//
// 三件事：multipart 里重复的 `modelCalls` 怎么取、400 的正文说什么、
// 以及四处 `r.authority = 'models.dev'` 判据的**反向**用例——端点上报不能
// 让一个模型被读成「上游目录认领过」（那会让「来源」这一列说谎）。
// 前三处（模型页 / 目录 / 按 id 读）在本文件，第四处（版本详情）由
// TestIntegrationEndpointReportOverHTTP 的详情断言钉住。

// submitWithCalls 投稿时带端点上报：每一项是一个 JSON 对象字符串。
func submitWithCalls(t *testing.T, h *Handler, sid, title, body string, slugs, calls []string) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for _, kv := range [][2]string{{"title", title}, {"description", "简介"}, {"demoType", "web"}} {
		if err := mw.WriteField(kv[0], kv[1]); err != nil {
			t.Fatal(err)
		}
	}
	for _, s := range slugs {
		if err := mw.WriteField("modelSlugs", s); err != nil {
			t.Fatal(err)
		}
	}
	for _, c := range calls {
		if err := mw.WriteField("modelCalls", c); err != nil {
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

// endpointDetailBody 是详情里与端点上报有关的那几个字段。
type endpointDetailBody struct {
	Models []struct {
		Slug       string `json:"slug"`
		Source     string `json:"source"`
		Checkpoint *struct {
			Label      string `json:"label"`
			ReleasedOn string `json:"releasedOn"`
		} `json:"checkpoint"`
	} `json:"models"`
}

// TestIntegrationEndpointReportOverHTTP 钉住「上报走投稿表单」这条线上形状：
// 重复的 modelCalls 字段被逐项解析，版本落到这一版的声明上，详情里看得见。
func TestIntegrationEndpointReportOverHTTP(t *testing.T) {
	h, idSvc, pool := newModelTestHandler(t)
	insertModel(t, pool, "it-ep-http", "上报模型HTTP", "active")
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM model_offerings WHERE external_id = 'it-ep-http'`)
	})

	author, sid := registerVerified(t, pool, idSvc, identity.RegisterInput{
		Email:       "it-ep-http@example.com",
		Password:    "test-pass-1234",
		Username:    "it-ep-http",
		DisplayName: "端点上报作者",
	})
	t.Cleanup(func() { cleanupAuthor(t, pool, author.Sub) })

	rec := submitWithCalls(t, h, sid, "端点上报·HTTP", `<!doctype html><p>ep</p>`,
		[]string{"it-ep-http"},
		[]string{`{"ref":"it-ep-http","endpoint":"https://API.Example.com:443/v1/","checkpoint":"1017"}`})
	if rec.Code != http.StatusCreated {
		t.Fatalf("投稿 = %d，想要 201；响应体：%s", rec.Code, rec.Body.String())
	}
	var created struct {
		Slug string `json:"slug"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("解析投稿回执失败: %v", err)
	}

	// 作者读详情：这一版的模型声明上带着上报的版本。
	detail := getDemoAs(t, h, created.Slug, sid)
	if detail.Code != http.StatusOK {
		t.Fatalf("读详情 = %d，想要 200；响应体：%s", detail.Code, detail.Body.String())
	}
	var body endpointDetailBody
	if err := json.Unmarshal(detail.Body.Bytes(), &body); err != nil {
		t.Fatalf("解析详情失败: %v", err)
	}
	if len(body.Models) != 1 {
		t.Fatalf("详情里的模型数 = %d，想要 1", len(body.Models))
	}
	if body.Models[0].Checkpoint == nil || body.Models[0].Checkpoint.Label != "1017" {
		t.Fatalf("详情里的版本 = %+v，想要 1017（上报的版本要落到声明上）", body.Models[0].Checkpoint)
	}
	// 端点上报不是「上游目录认领」：来源仍是 manual。
	if body.Models[0].Source != "manual" {
		t.Fatalf("来源 = %q，想要 manual", body.Models[0].Source)
	}

	// 库里那一行的形状：规范化过的 base_url + exact。
	var providerID, pinning string
	if err := pool.QueryRow(context.Background(), `
		SELECT provider_id, pinning FROM model_offerings
		WHERE authority = 'endpoint' AND external_id = 'it-ep-http'`).Scan(&providerID, &pinning); err != nil {
		t.Fatalf("读调用行失败: %v", err)
	}
	if providerID != "https://api.example.com/v1" {
		t.Fatalf("provider_id = %q，想要规范化后的 https://api.example.com/v1", providerID)
	}
	if pinning != "exact" {
		t.Fatalf("pinning = %q，想要 exact", pinning)
	}
}

// TestIntegrationEndpointReportRejectsOverHTTP 钉住 400 的口径：
// 凭据形状的端点、空名字、不是 JSON、目录里没有的模型，全部 400 且正文说清原因。
func TestIntegrationEndpointReportRejectsOverHTTP(t *testing.T) {
	h, idSvc, pool := newModelTestHandler(t)
	insertModel(t, pool, "it-ep-bad", "上报模型HTTP坏", "active")

	author, sid := registerVerified(t, pool, idSvc, identity.RegisterInput{
		Email:       "it-ep-bad@example.com",
		Password:    "test-pass-5678",
		Username:    "it-ep-bad",
		DisplayName: "端点上报坏作者",
	})
	t.Cleanup(func() { cleanupAuthor(t, pool, author.Sub) })

	unknown := "上报里没收录的模型-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM models WHERE name = $1`, unknown)
	})

	cases := []struct {
		name    string
		calls   []string
		want    string
		notWant string
	}{
		{"凭据形状的端点", []string{`{"ref":"it-ep-bad","endpoint":"https://someone:hunter2@api.example.com/v1"}`}, "不能带用户名或密码", "hunter2"},
		{"空名字", []string{`{"ref":"   "}`}, "缺模型名", ""},
		{"不是 JSON", []string{`endpoint=https://api.example.com/v1`}, "JSON 对象", ""},
		{"目录里没有的模型", []string{`{"ref":"` + unknown + `"}`}, unknown, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := submitWithCalls(t, h, sid, "端点上报·拒收 "+c.name, `<!doctype html><p>x</p>`, nil, c.calls)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("投稿 = %d，想要 400；响应体：%s", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), c.want) {
				t.Fatalf("错误信息里应当有 %q，实际：%s", c.want, rec.Body.String())
			}
			// 400 的正文会进客户端日志与用户截图：拒收凭据的同时不能把凭据回显出来。
			if c.notWant != "" && strings.Contains(rec.Body.String(), c.notWant) {
				t.Fatalf("400 的正文里不该回显凭据（出现了 %q）：%s", c.notWant, rec.Body.String())
			}
		})
	}
}

// TestIntegrationEndpointAuthorityDoesNotClaimCatalogSource 是四处
// `r.authority = 'models.dev'` 判据里前三处的反向用例（第四处见
// TestIntegrationEndpointReportOverHTTP 的详情断言）。
//
// 为什么重要：`source` 是派生值，「有 models.dev 的调用行就是目录认领过的」。
// 端点上报也往同一张表写行，判据一旦写成「有没有调用行」，所有被上报过的模型
// 都会变成「上游目录认领」——来源这一列从此说谎，而且没人会立刻发现。
func TestIntegrationEndpointAuthorityDoesNotClaimCatalogSource(t *testing.T) {
	h, idSvc, pool := newModelTestHandler(t)
	insertModel(t, pool, "it-ep-src", "上报模型来源", "active")
	var modelID int64
	if err := pool.QueryRow(context.Background(),
		`SELECT id FROM models WHERE slug = 'it-ep-src'`).Scan(&modelID); err != nil {
		t.Fatalf("回查模型 id 失败: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM model_offerings WHERE external_id = 'it-ep-src'`)
	})

	author, sid := registerVerified(t, pool, idSvc, identity.RegisterInput{
		Email:       "it-ep-src@example.com",
		Password:    "test-pass-3456",
		Username:    "it-ep-src",
		DisplayName: "端点上报来源作者",
	})
	t.Cleanup(func() { cleanupAuthor(t, pool, author.Sub) })

	rec := submitWithCalls(t, h, sid, "端点上报·来源", `<!doctype html><p>s</p>`,
		[]string{"it-ep-src"},
		[]string{`{"ref":"it-ep-src","endpoint":"https://api.example.com/v1"}`})
	if rec.Code != http.StatusCreated {
		t.Fatalf("投稿 = %d，想要 201；响应体：%s", rec.Code, rec.Body.String())
	}

	// 先证明调用行真的在——否则下面的「没被认领」等于没验。
	var offerings int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM model_offerings WHERE model_id = $1 AND authority = 'endpoint'`,
		modelID).Scan(&offerings); err != nil {
		t.Fatalf("数调用行失败: %v", err)
	}
	if offerings != 1 {
		t.Fatalf("端点调用行 = %d，想要 1", offerings)
	}

	// 1) 模型页（GetModelBySlug）。
	page := httptest.NewRecorder()
	h.GetModel(page, httptest.NewRequest(http.MethodGet, "/api/v1/models/it-ep-src", nil), "it-ep-src")
	if page.Code != http.StatusOK {
		t.Fatalf("模型页 = %d，想要 200；响应体：%s", page.Code, page.Body.String())
	}
	// 模型页的响应是 ModelProfile：模型本体在 model 里，不是平铺的。
	var pageBody struct {
		Model struct {
			Source string `json:"source"`
		} `json:"model"`
	}
	if err := json.Unmarshal(page.Body.Bytes(), &pageBody); err != nil {
		t.Fatalf("解析模型页失败: %v", err)
	}
	if pageBody.Model.Source != "manual" {
		t.Fatalf("模型页来源 = %q，想要 manual（端点上报不是目录认领）", pageBody.Model.Source)
	}

	// 2) 目录（ListModels）。
	list := httptest.NewRecorder()
	h.ListModels(list, httptest.NewRequest(http.MethodGet, "/api/v1/models", nil), api.ListModelsParams{})
	if list.Code != http.StatusOK {
		t.Fatalf("模型目录 = %d，想要 200；响应体：%s", list.Code, list.Body.String())
	}
	var catalog struct {
		Items []struct {
			Slug   string `json:"slug"`
			Source string `json:"source"`
		} `json:"items"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &catalog); err != nil {
		t.Fatalf("解析目录失败: %v", err)
	}
	found := false
	for _, item := range catalog.Items {
		if item.Slug != "it-ep-src" {
			continue
		}
		found = true
		if item.Source != "manual" {
			t.Fatalf("目录里来源 = %q，想要 manual", item.Source)
		}
	}
	if !found {
		t.Fatalf("目录里没找到 it-ep-src")
	}

	// 3) 导入后回读（GetModelByID）：导入流程用它把 id 变成契约对象。
	row, err := store.New(pool).GetModelByID(context.Background(), modelID)
	if err != nil {
		t.Fatalf("按 id 读模型失败: %v", err)
	}
	if row.Source != "manual" {
		t.Fatalf("按 id 读的来源 = %q，想要 manual", row.Source)
	}
}
