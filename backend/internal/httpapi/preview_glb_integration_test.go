package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/api"
)

// TestIntegrationPreviewGLBServesViewer 三维模型要包一层查看页，字节走 `?raw=1`。
//
// 地址从「不带文件名」的那条跳转里拿：单文件作品落库时入口被归一成 `index.<ext>`，
// 作者上传时的文件名不保留。测「用户真正会打到的那条路径」比猜文件名可靠。
func TestIntegrationPreviewGLBServesViewer(t *testing.T) {
	h, p, idSvc, pool := newShapeHandler(t)
	acc, sid := registerVerified(t, pool, idSvc, registerInputFor(t, "glb"))
	t.Cleanup(func() { cleanupAuthor(t, pool, acc.Sub) })

	body := []byte("glTF" + uniq())
	rec := submitTyped(t, h, sid, "model", "model.glb", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("投稿 glb（model 类型）= %d，想要 201；body=%s", rec.Code, rec.Body.String())
	}
	var submitted api.SubmissionResult
	if err := json.Unmarshal(rec.Body.Bytes(), &submitted); err != nil {
		t.Fatalf("解析投稿回执失败: %v", err)
	}

	first := previewGet(p, "/preview/"+submitted.Slug, sid)
	if first.Code != http.StatusFound {
		t.Fatalf("不带文件名的预览 = %d，想要 302；body=%q", first.Code, first.Body.String())
	}
	loc := first.Header().Get("Location")
	if !strings.HasSuffix(loc, ".glb") {
		t.Fatalf("跳转目标 = %q，应当指向 .glb 入口", loc)
	}

	viewer := previewGet(p, loc, sid)
	if viewer.Code != http.StatusOK {
		t.Fatalf("查看页 = %d，想要 200；body=%s", viewer.Code, viewer.Body.String())
	}
	if ct := viewer.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("查看页 Content-Type = %q，想要 text/html", ct)
	}
	page := viewer.Body.String()
	for _, want := range []string{"<model-viewer", "?raw=1", modelViewerPath} {
		if !strings.Contains(page, want) {
			t.Fatalf("查看页里缺少 %q；实际：%s", want, page)
		}
	}
	if csp := viewer.Header().Get("Content-Security-Policy"); strings.Contains(csp, "sandbox") {
		t.Fatalf("查看页不该带 sandbox（会挡住模块脚本）：%q", csp)
	}

	raw := previewGet(p, loc+"?raw=1", sid)
	if raw.Code != http.StatusOK {
		t.Fatalf("原始字节 = %d，想要 200；body=%s", raw.Code, raw.Body.String())
	}
	if ct := raw.Header().Get("Content-Type"); ct != "model/gltf-binary" {
		t.Fatalf("原始字节 Content-Type = %q，想要 model/gltf-binary", ct)
	}
	if raw.Body.String() != string(body) {
		t.Fatalf("原始字节 = %q，想要 %q", raw.Body.String(), body)
	}
}

// TestIntegrationPreviewServesPlayerAsset 播放器资产必须能从服务端取到：它跟二进制走，
// 部署不需要额外的静态目录。
func TestIntegrationPreviewServesPlayerAsset(t *testing.T) {
	rec := httptest.NewRecorder()
	servePreviewAssets(rec, httptest.NewRequest(http.MethodGet, modelViewerPath, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("播放器资产 = %d，想要 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/javascript") {
		t.Fatalf("播放器资产 Content-Type = %q", ct)
	}
	if rec.Body.Len() < 500_000 {
		t.Fatalf("播放器资产只有 %d 字节，像是没打包进来", rec.Body.Len())
	}
}

// TestIntegrationSubmitShapeRequiresMatchingType 形态与类型不符要 400——三维与图片同理。
func TestIntegrationSubmitShapeRequiresMatchingType(t *testing.T) {
	h, _, idSvc, pool := newShapeHandler(t)
	acc, sid := registerVerified(t, pool, idSvc, registerInputFor(t, "glb-type"))
	t.Cleanup(func() { cleanupAuthor(t, pool, acc.Sub) })

	if rec := submitTyped(t, h, sid, "web", "model.glb", []byte("glTF")); rec.Code != http.StatusBadRequest {
		t.Fatalf("glb 投 web = %d，想要 400", rec.Code)
	}
	if rec := submitTyped(t, h, sid, "model", "model.glb", []byte("glTF")); rec.Code != http.StatusCreated {
		t.Fatalf("glb 投 model = %d，想要 201；body=%s", rec.Code, rec.Body.String())
	}
	if rec := submitTyped(t, h, sid, "image", "shot.png", []byte("\x89PNG\r\n\x1a\n")); rec.Code != http.StatusCreated {
		t.Fatalf("png 投 image = %d，想要 201；body=%s", rec.Code, rec.Body.String())
	}
}
