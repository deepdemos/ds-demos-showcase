package httpapi

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 跨站检查的三种档位与几条边界。
//
// 这一层不看数据库、不看会话：它只回答一个问题——「这个请求自称从哪来，
// 而我们允不允许它改东西」。所以全部用 httptest 直接打中间件。

func newTestLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

// crossSitePost 造一个浏览器会发出的跨站表单提交：非安全方法 + Sec-Fetch-Site: cross-site。
func crossSitePost(target string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, target, strings.NewReader("x=1"))
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	r.Header.Set("Origin", "https://evil.example")
	r.Host = "deepdemos.example"
	return r
}

func sameSitePost(target string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, target, strings.NewReader("x=1"))
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	r.Header.Set("Origin", "https://deepdemos.example")
	r.Host = "deepdemos.example"
	return r
}

// okHandler 记录自己有没有被调用——「拦住了」与「放行了但下游没做事」是两件事。
func okHandler(called *bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		*called = true
		w.WriteHeader(http.StatusOK)
	})
}

func TestParseCSRFMode(t *testing.T) {
	cases := []struct {
		raw     string
		want    CSRFMode
		wantErr bool
	}{
		{"", CSRFLog, false}, // 空 = 默认观察期
		{"log", CSRFLog, false},
		{"enforce", CSRFEnforce, false},
		{"off", CSRFOff, false},
		{"enforcee", "", true}, // 拼错要报错，不能静默降级成默认值
		{"ENFORCE", "", true},  // 大小写也不放过：环境变量里写成大写是常见手误
	}
	for _, c := range cases {
		got, err := ParseCSRFMode(c.raw)
		if c.wantErr {
			if err == nil {
				t.Fatalf("ParseCSRFMode(%q) 应当报错，实际返回 %q", c.raw, got)
			}
			continue
		}
		if err != nil {
			t.Fatalf("ParseCSRFMode(%q) 不该报错：%v", c.raw, err)
		}
		if got != c.want {
			t.Fatalf("ParseCSRFMode(%q) = %q，想要 %q", c.raw, got, c.want)
		}
	}
}

func TestCSRFLogModeRecordsButAllows(t *testing.T) {
	var buf bytes.Buffer
	called := false
	h := WithCSRF(okHandler(&called), CSRFLog, newTestLogger(&buf))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, crossSitePost("/api/v1/demos"))

	if !called {
		t.Fatal("观察期不该拦下请求：下游一次都没被调用")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("观察期应当放行（200），实际 %d", rec.Code)
	}
	logged := buf.String()
	if !strings.Contains(logged, "跨站请求检查未通过") {
		t.Fatalf("观察期必须留下日志，实际日志是 %q", logged)
	}
	// 日志要带判定依据，否则运维看到这一行也不知道该查什么。
	var entry map[string]any
	if err := json.Unmarshal([]byte(strings.SplitN(logged, "\n", 2)[0]), &entry); err != nil {
		t.Fatalf("日志不是合法 JSON：%v（原文 %q）", err, logged)
	}
	if entry["sec_fetch_site"] != "cross-site" {
		t.Fatalf("日志里应带上 sec_fetch_site，实际 %v", entry["sec_fetch_site"])
	}
	if entry["mode"] != "log" {
		t.Fatalf("日志里应带上 mode，实际 %v", entry["mode"])
	}
}

func TestCSRFEnforceBlocksCrossSiteWrite(t *testing.T) {
	var buf bytes.Buffer
	called := false
	h := WithCSRF(okHandler(&called), CSRFEnforce, newTestLogger(&buf))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, crossSitePost("/api/v1/demos"))

	if called {
		t.Fatal("enforce 档下跨站写请求绝不能到达下游")
	}
	if rec.Code != http.StatusForbidden {
		t.Fatalf("跨站写请求应回 403，实际 %d", rec.Code)
	}
	var body struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("响应不是合法 JSON：%v（原文 %q）", err, rec.Body.String())
	}
	if body.Code != "forbidden" {
		t.Fatalf("响应错误码应为 forbidden，实际 %q", body.Code)
	}
}

func TestCSRFEnforceAllowsSameOrigin(t *testing.T) {
	var buf bytes.Buffer
	called := false
	h := WithCSRF(okHandler(&called), CSRFEnforce, newTestLogger(&buf))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, sameSitePost("/api/v1/demos"))

	if !called {
		t.Fatal("同源请求被拦了——那会把正常用户一起挡在门外")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("同源请求应放行，实际 %d", rec.Code)
	}
}

func TestCSRFEnforceAllowsSafeMethodsFromAnywhere(t *testing.T) {
	var buf bytes.Buffer
	called := false
	h := WithCSRF(okHandler(&called), CSRFEnforce, newTestLogger(&buf))

	r := httptest.NewRequest(http.MethodGet, "/api/v1/demos", nil)
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	r.Header.Set("Origin", "https://evil.example")
	r.Host = "deepdemos.example"

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)

	if !called {
		t.Fatal("GET 是安全方法，跨站也必须放行（否则别人的链接点进来就是 403）")
	}
}

func TestCSRFOffDoesNothing(t *testing.T) {
	var buf bytes.Buffer
	called := false
	h := WithCSRF(okHandler(&called), CSRFOff, newTestLogger(&buf))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, crossSitePost("/api/v1/demos"))

	if !called || rec.Code != http.StatusOK {
		t.Fatalf("off 档应当完全不管，实际 called=%v code=%d", called, rec.Code)
	}
	if buf.Len() != 0 {
		t.Fatalf("off 档不该产生日志，实际 %q", buf.String())
	}
}
