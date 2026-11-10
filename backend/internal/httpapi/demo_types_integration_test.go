package httpapi

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/api"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/demo"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

// submitTyped 用指定类型与文件名投一件作品，返回记录器。
//
// 与 submitHTML 的区别只有一个：类型与文件名由调用方给。
// 校验「类型收不收这种文件」必须能自由组合这两者，否则测不出来。
func submitTyped(t *testing.T, h *Handler, sid, demoType, fileName string, content []byte) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for k, v := range map[string]string{"title": "类型校验 " + fileName, "description": "简介", "demoType": demoType} {
		if err := mw.WriteField(k, v); err != nil {
			t.Fatalf("写字段 %s 失败: %v", k, err)
		}
	}
	part, err := mw.CreateFormFile("package", fileName)
	if err != nil {
		t.Fatalf("建文件字段失败: %v", err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatalf("写文件内容失败: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("收尾 multipart 失败: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/demos", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if sid != "" {
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: sid})
	}
	rec := httptest.NewRecorder()
	h.SubmitDemo(rec, req, api.SubmitDemoParams{})
	return rec
}

// godotZip 造一个 Godot 导出包的形状：入口 + 引擎 + 资源包。
//
// 判据只看扩展名，所以这里不需要真的可运行的 wasm——但三个文件都得在，
// 否则测的就不是「按包内结构识别」而是别的什么。
func godotZip(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	files := map[string]string{
		"index.html": "<!doctype html><html><body><canvas id=c></canvas></body></html>",
		"game.js":    "// 引擎 glue",
		"game.wasm":  "wasm-bytes",
		"game.pck":   "pck-bytes",
	}
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("建 zip 条目失败: %v", err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatalf("写 zip 条目失败: %v", err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("收尾 zip 失败: %v", err)
	}
	return buf.Bytes()
}

// TestIntegrationListDemoTypes 目录要能直接驱动投稿页：类型、中文名、允许的形态。
func TestIntegrationListDemoTypes(t *testing.T) {
	h, _, pool := newSortHandler(t)
	_ = pool

	rec := httptest.NewRecorder()
	h.ListDemoTypes(rec, httptest.NewRequest(http.MethodGet, "/api/v1/demo-types", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d，想要 200（%s）", rec.Code, rec.Body.String())
	}
	var got api.DemoTypeList
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}

	byCode := map[string]api.DemoType{}
	for _, item := range got.Items {
		byCode[item.Code] = item
	}
	web, ok := byCode["web"]
	if !ok {
		t.Fatal("目录里没有 web 类型")
	}
	if !slices.Contains(web.Formats, "single:html") || !slices.Contains(web.Formats, "zip:html") {
		t.Fatalf("web 允许的形态 = %v，应当含 single:html 与 zip:html", web.Formats)
	}
	godot, ok := byCode["godot"]
	if !ok {
		t.Fatal("目录里没有 godot 类型")
	}
	if !slices.Equal(godot.Formats, []string{"zip:godot"}) {
		t.Fatalf("godot 允许的形态 = %v，想要 [zip:godot]", godot.Formats)
	}
	// 还没有上传路径的类型必须给空数组而不是 null：界面据此标「不可选」。
	if link, ok := byCode["link"]; ok {
		if link.Formats == nil {
			t.Fatal("link 的 formats 是 null，想要空数组")
		}
		if len(link.Formats) != 0 {
			t.Fatalf("link 的 formats = %v，还没有上传路径应当是空的", link.Formats)
		}
	}
	if image, ok := byCode["image"]; ok {
		if !slices.Equal(image.Formats, []string{"single:png", "single:jpg", "single:webp"}) {
			t.Fatalf("image 允许的形态 = %v，想要 [single:png single:jpg single:webp]", image.Formats)
		}
	}
	if model, ok := byCode["model"]; ok {
		if !slices.Equal(model.Formats, []string{"single:glb"}) {
			t.Fatalf("model 允许的形态 = %v，想要 [single:glb]", model.Formats)
		}
	}
}

// TestIntegrationSubmitRejectsMismatchedShape 类型与真实形态不符时必须拒绝，且说清原因。
func TestIntegrationSubmitRejectsMismatchedShape(t *testing.T) {
	h, idSvc, pool := newSortHandler(t)
	acc, sid := registerVerified(t, pool, idSvc, registerInputFor(t, "type-mismatch"))
	t.Cleanup(func() { cleanupAuthor(t, pool, acc.Sub) })

	// 选「压缩包作品」却给一个单文件 HTML。
	rec := submitTyped(t, h, sid, "zip", "index.html", []byte("<!doctype html><p>x</p>"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("类型与形态不符 = %d，想要 400（%s）", rec.Code, rec.Body.String())
	}
	var body api.Error
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body.Message == "" || !bytes.Contains([]byte(body.Message), []byte("不收这种文件")) {
		t.Fatalf("错误文案 = %q，应当说明类型不收这种文件", body.Message)
	}

	// 选「网页作品」却给一个 Godot 导出包。
	rec2 := submitTyped(t, h, sid, "web", "bundle.zip", godotZip(t))
	if rec2.Code != http.StatusBadRequest {
		t.Fatalf("godot 包投 web 类型 = %d，想要 400（%s）", rec2.Code, rec2.Body.String())
	}
}

// TestIntegrationSubmitAcceptsGodotZip 同一份包，选对类型就应当通过。
func TestIntegrationSubmitAcceptsGodotZip(t *testing.T) {
	h, idSvc, pool := newSortHandler(t)
	acc, sid := registerVerified(t, pool, idSvc, registerInputFor(t, "type-godot"))
	t.Cleanup(func() { cleanupAuthor(t, pool, acc.Sub) })

	rec := submitTyped(t, h, sid, "godot", "bundle.zip", godotZip(t))
	if rec.Code != http.StatusCreated {
		t.Fatalf("godot 类型投 Godot 包 = %d，想要 201（%s）", rec.Code, rec.Body.String())
	}
}

// TestIntegrationSubmitRejectsUnknownType 类型是主数据，编一个不存在的不该默默落库。
func TestIntegrationSubmitRejectsUnknownType(t *testing.T) {
	h, idSvc, pool := newSortHandler(t)
	acc, sid := registerVerified(t, pool, idSvc, registerInputFor(t, "type-unknown"))
	t.Cleanup(func() { cleanupAuthor(t, pool, acc.Sub) })

	rec := submitTyped(t, h, sid, "nope", "index.html", []byte("<!doctype html><p>x</p>"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("未知类型 = %d，想要 400（%s）", rec.Code, rec.Body.String())
	}
}

// TestIntegrationKnownFormatsMatchDatabase 代码里的格式词汇与库里的行不能漂。
//
// 漂了会怎样：作者投一种库里登记了、代码却不认识的格式，或者反过来——
// 代码认识但没登记，于是「明明支持却选不到」。两边都是沉默的错误。
func TestIntegrationKnownFormatsMatchDatabase(t *testing.T) {
	_, _, pool := newSortHandler(t)
	ctx := t.Context()

	rows, err := store.New(pool).ListDemoTypes(ctx)
	if err != nil {
		t.Fatalf("读类型目录失败: %v", err)
	}
	known := demo.KnownFormats()
	knownSet := map[string]bool{}
	for _, f := range known {
		knownSet[f] = true
	}
	for _, row := range rows {
		for _, f := range row.Formats {
			if !knownSet[f] {
				t.Fatalf("类型 %s 登记了代码不认识的形态 %q；代码认识的是 %v", row.Code, f, known)
			}
		}
	}
}
