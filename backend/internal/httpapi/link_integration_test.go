package httpapi

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/api"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/platform/storage"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

// submitLink 用 externalUrl 投稿（不带 package）。
func submitLink(t *testing.T, h *Handler, sid, title, url string) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	_ = mw.WriteField("title", title)
	_ = mw.WriteField("description", "外链作品")
	_ = mw.WriteField("demoType", "link")
	_ = mw.WriteField("externalUrl", url)
	_ = mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/demos", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if sid != "" {
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: sid})
	}
	rec := httptest.NewRecorder()
	h.SubmitDemo(rec, req, api.SubmitDemoParams{})
	return rec
}

// 外链作品的整条路：收一条只有地址的版本、详情能看到类型、预览 302 到外面。
func TestIntegrationSubmitLinkDemo(t *testing.T) {
	h, idSvc, pool := newModelTestHandler(t)
	acc, sid := registerVerified(t, pool, idSvc, registerInputFor(t, "link"))

	// 迁移 0026 才把 link 的 `url` 形态登记上；没跑迁移的库这里会 400。
	rec := submitLink(t, h, sid, "我的外链作品", "https://example.com/demo")
	if rec.Code != http.StatusCreated {
		t.Fatalf("外链投稿 = %d，想要 201：%s", rec.Code, rec.Body.String())
	}
	var submitted api.SubmissionResult
	if err := json.Unmarshal(rec.Body.Bytes(), &submitted); err != nil {
		t.Fatalf("回执不是 JSON：%v", err)
	}
	slug := submitted.Slug

	// 作者能看到类型是 link，且没有可下载的字节。
	detail := decodeDetail(t, getDemoAs(t, h, slug, sid))
	if detail.DemoType != "link" {
		t.Fatalf("详情里的 demoType = %q，想要 link", detail.DemoType)
	}
	if detail.Downloadable {
		t.Fatalf("外链作品不该可下载")
	}
	_ = acc

	// 预览 302 到外部地址，且不能缓存（地址可以被作者改）。
	previewReq := httptest.NewRequest(http.MethodGet, "/preview/"+slug, nil)
	previewReq.AddCookie(&http.Cookie{Name: sessionCookie, Value: sid})
	// 预览要自己的 identity 才能认作者会话：NewRouter 的第三个位置给的是
	// 独立的 Preview 实例，直接复用测试里那份。
	files, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("建本地存储失败: %v", err)
	}
	preview := NewPreview(store.New(pool), files, idSvc)
	previewRec := httptest.NewRecorder()
	NewRouter(h, preview, nil).ServeHTTP(previewRec, previewReq)
	if previewRec.Code != http.StatusFound {
		t.Fatalf("外链预览 = %d，想要 302", previewRec.Code)
	}
	if got := previewRec.Header().Get("Location"); got != "https://example.com/demo" {
		t.Fatalf("Location = %q，想要 https://example.com/demo", got)
	}
	if got := previewRec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q，想要 no-store", got)
	}
}

// 文件与地址互斥，两边都给要被挡。
func TestIntegrationSubmitRejectsFileAndURL(t *testing.T) {
	h, idSvc, pool := newModelTestHandler(t)
	_, sid := registerVerified(t, pool, idSvc, registerInputFor(t, "link-both"))

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	_ = mw.WriteField("title", "两个都给")
	_ = mw.WriteField("demoType", "link")
	_ = mw.WriteField("externalUrl", "https://example.com/x")
	fw, _ := mw.CreateFormFile("package", "index.html")
	_, _ = fw.Write([]byte("<html><body>x</body></html>"))
	_ = mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/demos", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: sid})
	rec := httptest.NewRecorder()
	h.SubmitDemo(rec, req, api.SubmitDemoParams{})

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("同时给文件与地址 = %d，想要 400：%s", rec.Code, rec.Body.String())
	}
}

// 地址只收 http/https：javascript: 会把「点一个作品」变成执行脚本。
func TestIntegrationSubmitRejectsNonHTTPScheme(t *testing.T) {
	h, idSvc, pool := newModelTestHandler(t)
	_, sid := registerVerified(t, pool, idSvc, registerInputFor(t, "link-scheme"))

	rec := submitLink(t, h, sid, "危险地址", "javascript:alert(1)")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("javascript: 地址 = %d，想要 400：%s", rec.Code, rec.Body.String())
	}
}

// 类型与形态绑定对外链同样生效：给 web 类型一个地址要 400。
func TestIntegrationSubmitLinkRequiresLinkType(t *testing.T) {
	h, idSvc, pool := newModelTestHandler(t)
	_, sid := registerVerified(t, pool, idSvc, registerInputFor(t, "link-type"))

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	_ = mw.WriteField("title", "类型不对")
	_ = mw.WriteField("demoType", "web")
	_ = mw.WriteField("externalUrl", "https://example.com/y")
	_ = mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/demos", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: sid})
	rec := httptest.NewRecorder()
	h.SubmitDemo(rec, req, api.SubmitDemoParams{})

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("web 类型给地址 = %d，想要 400：%s", rec.Code, rec.Body.String())
	}
}
