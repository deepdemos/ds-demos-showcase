package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strconv"
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

func (s *stub) ListModels(_ http.ResponseWriter, _ *http.Request, _ api.ListModelsParams) {
	s.hits = append(s.hits, "models")
}

func (s *stub) ListClassifications(_ http.ResponseWriter, _ *http.Request) {
	s.hits = append(s.hits, "classifications")
}

func (s *stub) ListModelCatalog(_ http.ResponseWriter, _ *http.Request, _ api.ListModelCatalogParams) {
	s.hits = append(s.hits, "catalog")
}

func (s *stub) PreviewModelImport(_ http.ResponseWriter, _ *http.Request) {
	s.hits = append(s.hits, "import-preview")
}

func (s *stub) ApplyModelImport(_ http.ResponseWriter, _ *http.Request) {
	s.hits = append(s.hits, "import-apply")
}

func (s *stub) UpdateDemo(_ http.ResponseWriter, _ *http.Request, _ string) {}

func (s *stub) ArchiveDemo(_ http.ResponseWriter, _ *http.Request, _ string) {}

func (s *stub) UnarchiveDemo(_ http.ResponseWriter, _ *http.Request, _ string) {}

func (s *stub) DeleteDemo(_ http.ResponseWriter, _ *http.Request, _ string) {}

func (s *stub) RestoreDemo(_ http.ResponseWriter, _ *http.Request, _ string) {}

func (s *stub) ListDemoRevisions(_ http.ResponseWriter, _ *http.Request, slug string) {
	s.hits = append(s.hits, "revisions:"+slug)
}

func (s *stub) RestoreDemoRevision(_ http.ResponseWriter, _ *http.Request, slug string, revNo int) {
	s.hits = append(s.hits, "restore:"+slug+":"+strconv.Itoa(revNo))
}

func (s *stub) PostDemoView(_ http.ResponseWriter, _ *http.Request, slug string) {
	s.hits = append(s.hits, "view:"+slug)
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

func (s *stub) PostAuthVerifyEmail(http.ResponseWriter, *http.Request) {
	s.hits = append(s.hits, "verify-email")
}

func (s *stub) PostAuthVerifyEmailResend(http.ResponseWriter, *http.Request) {
	s.hits = append(s.hits, "verify-email-resend")
}

func (s *stub) PostAuthLogin(http.ResponseWriter, *http.Request) {
	s.hits = append(s.hits, "login")
}

func (s *stub) PostAuthLogout(http.ResponseWriter, *http.Request) {
	s.hits = append(s.hits, "logout")
}

func (s *stub) PostAuthEmailCode(http.ResponseWriter, *http.Request) {
	s.hits = append(s.hits, "email-code")
}

func (s *stub) PostAuthPasswordResetCode(http.ResponseWriter, *http.Request) {
	s.hits = append(s.hits, "reset-code")
}

func (s *stub) ListDemoReviewQueue(_ http.ResponseWriter, _ *http.Request, _ api.ListDemoReviewQueueParams) {
	s.hits = append(s.hits, "review-queue")
}

func (s *stub) ReviewDemo(_ http.ResponseWriter, _ *http.Request, slug string) {
	s.hits = append(s.hits, "review:"+slug)
}

func (s *stub) PublishDemo(_ http.ResponseWriter, _ *http.Request, slug string) {
	s.hits = append(s.hits, "publish:"+slug)
}

func (s *stub) PostAuthPasswordReset(http.ResponseWriter, *http.Request) {
	s.hits = append(s.hits, "reset")
}

func (s *stub) SubmitDemo(_ http.ResponseWriter, _ *http.Request, _ api.SubmitDemoParams) {
	s.hits = append(s.hits, "submit")
}

// TestRouterUsesContractPaths 守住一条不变式：路由表来自契约，而不是人手写的地址清单。
// 契约改了路径而生成物没重跑、或有人绕过生成物手写路由，这里就会红。
func TestRouterUsesContractPaths(t *testing.T) {
	s := &stub{}
	router := NewRouter(s, nil, nil)

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
		{http.MethodPost, "/api/v1/auth/login", "login"},
		{http.MethodPost, "/api/v1/auth/email-code", "email-code"},
		{http.MethodPost, "/api/v1/auth/password-reset/code", "reset-code"},
		{http.MethodPost, "/api/v1/auth/password-reset", "reset"},
		{http.MethodPost, "/api/v1/auth/logout", "logout"},
		{http.MethodPost, "/api/v1/demos", "submit"},
		{http.MethodGet, "/api/v1/admin/reviews/demo", "review-queue"},
		{http.MethodPost, "/api/v1/admin/reviews/demo/some-slug", "review:some-slug"},
		{http.MethodPost, "/api/v1/admin/demos/some-slug/publish", "publish:some-slug"},
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
	NewRouter(&stub{}, nil, nil).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/nope", nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", response.Code)
	}
}
