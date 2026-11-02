package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/api"
)

// stub 只记录「哪个处理函数被调用了」，用来验证路由确实按契约注册。
type stub struct {
	hits []string
}

func (s *stub) GetHealthz(http.ResponseWriter, *http.Request) {
	s.hits = append(s.hits, "healthz")
}

func (s *stub) ListDemos(_ http.ResponseWriter, _ *http.Request, _ api.ListDemosParams) {
	s.hits = append(s.hits, "list")
}

func (s *stub) GetDemo(_ http.ResponseWriter, _ *http.Request, slug string) {
	s.hits = append(s.hits, "detail:"+slug)
}

func (s *stub) GetMe(http.ResponseWriter, *http.Request) {
	s.hits = append(s.hits, "me")
}

func (s *stub) PostAuthRegister(http.ResponseWriter, *http.Request) {
	s.hits = append(s.hits, "register")
}

// TestRouterUsesContractPaths 守住一条不变式：路由表来自契约，而不是人手写的地址清单。
// 契约改了路径而生成物没重跑、或有人绕过生成物手写路由，这里就会红。
func TestRouterUsesContractPaths(t *testing.T) {
	s := &stub{}
	router := NewRouter(s)

	cases := []struct {
		method string
		path   string
		want   string
	}{
		{http.MethodGet, "/healthz", "healthz"},
		{http.MethodGet, "/api/v1/demos", "list"},
		{http.MethodGet, "/api/v1/demos/some-slug", "detail:some-slug"},
		{http.MethodGet, "/api/v1/me", "me"},
		{http.MethodPost, "/api/v1/auth/register", "register"},
	}

	for _, tc := range cases {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(tc.method, tc.path, nil))
		if response.Code != http.StatusOK {
			t.Fatalf("%s %s: expected 200, got %d", tc.method, tc.path, response.Code)
		}
	}

	if len(s.hits) != len(cases) {
		t.Fatalf("expected %d calls, got %d: %v", len(cases), len(s.hits), s.hits)
	}
	for i, tc := range cases {
		if s.hits[i] != tc.want {
			t.Fatalf("%s: expected handler %q, got %q", tc.path, tc.want, s.hits[i])
		}
	}
}

func TestRouterUnknownPathIs404(t *testing.T) {
	response := httptest.NewRecorder()
	NewRouter(&stub{}).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/nope", nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", response.Code)
	}
}
