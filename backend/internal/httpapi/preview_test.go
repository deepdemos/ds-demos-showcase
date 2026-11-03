package httpapi

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ===== 纯函数 =====
//
// 预览的可见性判定要连库，单独放集成测试；这里测的是「不需要库、
// 但错了会静默出错」的那几处：路径拆分、内容类型、缓存策略、base 注入。

func TestSplitPreviewPath(t *testing.T) {
	cases := []struct {
		rest      string
		rev       int
		rel       string
		versioned bool
	}{
		{"", 0, "", false},
		{"index.html", 0, "index.html", false},
		{"sub/about.html", 0, "sub/about.html", false},
		{"v12/index.html", 12, "index.html", true},
		{"v12", 12, "", true},
		{"v12/", 12, "", true},
		{"v12/a/b.css", 12, "a/b.css", true},
		// 不是版本段：`r` 后面不是数字，或数字不合法
		{"v0/index.html", 0, "v0/index.html", false},
		{"v-1/index.html", 0, "v-1/index.html", false},
		{"videos/x.html", 0, "videos/x.html", false},
		{"v/index.html", 0, "v/index.html", false},
	}
	for _, c := range cases {
		rev, rel, versioned := splitPreviewPath(c.rest)
		if rev != c.rev || rel != c.rel || versioned != c.versioned {
			t.Errorf("splitPreviewPath(%q) = (%d,%q,%v)，期望 (%d,%q,%v)",
				c.rest, rev, rel, versioned, c.rev, c.rel, c.versioned)
		}
	}
}

func TestContentTypeOf(t *testing.T) {
	cases := map[string]string{
		"index.html":       "text/html; charset=utf-8",
		"INDEX.HTM":        "text/html; charset=utf-8",
		"a.css":            "text/css; charset=utf-8",
		"a.js":             "text/javascript; charset=utf-8",
		"data.json":        "application/json; charset=utf-8",
		"logo.svg":         "image/svg+xml",
		"p.png":            "image/png",
		"m.wasm":           "application/wasm",
		"sub/deep/x.webp":  "image/webp",
		"weird.unknownext": "application/octet-stream",
		"noext":            "application/octet-stream",
	}
	for in, want := range cases {
		if got := contentTypeOf(in); got != want {
			t.Errorf("contentTypeOf(%q) = %q，期望 %q", in, got, want)
		}
	}
}

func TestCacheControlOf(t *testing.T) {
	// 版本化 + 已发布：内容永不改变，可以永久缓存。
	if got := cacheControlOf(true, true); !strings.Contains(got, "immutable") {
		t.Errorf("已发布+版本化应为 immutable，实际 %q", got)
	}
	// 未发布的任何形式都不许中间层缓存：草稿会变。
	for _, versioned := range []bool{true, false} {
		if got := cacheControlOf(false, versioned); !strings.Contains(got, "no-store") {
			t.Errorf("未发布(v=%v)应为 no-store，实际 %q", versioned, got)
		}
	}
	// 当前版本别名已发布：短缓存，不能 immutable。
	got := cacheControlOf(true, false)
	if strings.Contains(got, "immutable") {
		t.Errorf("当前版本别名不应 immutable，实际 %q", got)
	}
	if !strings.Contains(got, "max-age=60") {
		t.Errorf("当前版本别名应短缓存，实际 %q", got)
	}
}

func TestInjectBase(t *testing.T) {
	base := `<base href="/preview/x/v1/">`

	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "有 head",
			in:   "<!doctype html><html><head><title>t</title></head><body></body></html>",
			want: "<!doctype html><html><head>" + base + "<title>t</title></head><body></body></html>",
		},
		{
			name: "HEAD 大写",
			in:   "<HTML><HEAD></HEAD><BODY></BODY></HTML>",
			want: "<HTML><HEAD>" + base + "</HEAD><BODY></BODY></HTML>",
		},
		{
			name: "head 带属性",
			in:   `<html><head lang="zh"><meta charset="utf-8"></head></html>`,
			want: `<html><head lang="zh">` + base + `<meta charset="utf-8"></head></html>`,
		},
		{
			name: "没有 head，插在 html 之后",
			in:   "<html><body>hi</body></html>",
			want: "<html>" + base + "<body>hi</body></html>",
		},
		{
			name: "连 html 都没有，放最前",
			in:   "<p>bare</p>",
			want: base + "<p>bare</p>",
		},
	}
	for _, c := range cases {
		got := string(injectBase([]byte(c.in), "/preview/x/v1/"))
		if got != c.want {
			t.Errorf("%s:\n 得到 %q\n 期望 %q", c.name, got, c.want)
		}
	}
}

func TestInjectBaseKeepsBytesAfterInsertion(t *testing.T) {
	// 注入不能吃掉已有内容：中文、注释、脚本里的 `<head` 字样都要原样保留。
	in := "<html><head></head><body><!-- <head> 是注释 --><script>var s='<head>'</script>中文</body></html>"
	got := string(injectBase([]byte(in), "/p/v1/"))
	if !strings.Contains(got, "中文") || !strings.Contains(got, "<!-- <head> 是注释 -->") {
		t.Fatalf("注入后丢了内容: %q", got)
	}
	// 只会插一次。
	if n := strings.Count(got, "<base href="); n != 1 {
		t.Fatalf("应恰好注入一次，实际 %d 次", n)
	}
}

func TestPreviewRejectsNonGet(t *testing.T) {
	p := NewPreview(nil, nil, nil)
	for _, m := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		w := httptest.NewRecorder()
		p.ServeHTTP(w, httptest.NewRequest(m, "/preview/x/index.html", nil))
		if w.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s 应为 405，实际 %d", m, w.Code)
		}
		if allow := w.Header().Get("Allow"); !strings.Contains(allow, "GET") {
			t.Errorf("%s 的 Allow 头缺失或不含 GET：%q", m, allow)
		}
	}
}

func TestPreviewWithoutStorageIs404(t *testing.T) {
	// 未接线的部署（queries/files 为 nil）不能 500，也不能 200。
	w := httptest.NewRecorder()
	NewPreview(nil, nil, nil).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/preview/x/index.html", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("应为 404，实际 %d", w.Code)
	}
	if bytes.Contains(w.Body.Bytes(), []byte("panic")) {
		t.Fatalf("响应体不该含 panic 字样: %q", w.Body.String())
	}
}
