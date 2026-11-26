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

func (s *stub) ListMyDemos(_ http.ResponseWriter, _ *http.Request, _ api.ListMyDemosParams) {
	s.hits = append(s.hits, "my-demos")
}

func (s *stub) SyncModelCatalog(_ http.ResponseWriter, _ *http.Request) {
	s.hits = append(s.hits, "sync")
}

func (s *stub) ListDemoTypes(_ http.ResponseWriter, _ *http.Request) {
	s.hits = append(s.hits, "demo-types")
}

func (s *stub) ListModels(_ http.ResponseWriter, _ *http.Request, _ api.ListModelsParams) {
	s.hits = append(s.hits, "models")
}

func (s *stub) SearchUpstreamModels(_ http.ResponseWriter, _ *http.Request, _ api.SearchUpstreamModelsParams) {
	s.hits = append(s.hits, "upstream-search")
}

func (s *stub) ListRankings(_ http.ResponseWriter, _ *http.Request, _ api.ListRankingsParams) {
	s.hits = append(s.hits, "rankings")
}

func (s *stub) ListRelatedDemos(_ http.ResponseWriter, _ *http.Request, _ string, _ api.ListRelatedDemosParams) {
	s.hits = append(s.hits, "related")
}

func (s *stub) ListRecommendations(_ http.ResponseWriter, _ *http.Request, _ api.ListRecommendationsParams) {
	s.hits = append(s.hits, "recommendations")
}

func (s *stub) PutDemoRating(_ http.ResponseWriter, _ *http.Request, slug string) {
	s.hits = append(s.hits, "rate:"+slug)
}

func (s *stub) DeleteDemoRating(_ http.ResponseWriter, _ *http.Request, slug string) {
	s.hits = append(s.hits, "unrate:"+slug)
}

func (s *stub) AddDemoChannel(_ http.ResponseWriter, _ *http.Request, _ string) {
	s.hits = append(s.hits, "addchannel")
}

func (s *stub) ListDemoChannels(_ http.ResponseWriter, _ *http.Request, _ string) {
	s.hits = append(s.hits, "listchannels")
}

func (s *stub) RemoveDemoChannel(_ http.ResponseWriter, _ *http.Request, _ string, _ string) {
	s.hits = append(s.hits, "removechannel")
}

func (s *stub) ListClassifications(_ http.ResponseWriter, _ *http.Request) {
	s.hits = append(s.hits, "classifications")
}

func (s *stub) ListDemoTimeline(_ http.ResponseWriter, _ *http.Request, _ string) {
	s.hits = append(s.hits, "timeline")
}

func (s *stub) GetModel(_ http.ResponseWriter, _ *http.Request, _ string) {
	s.hits = append(s.hits, "model")
}

func (s *stub) GetModelSyncStatus(_ http.ResponseWriter, _ *http.Request) {
	s.hits = append(s.hits, "sync-status")
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

func (s *stub) ApplyModelImportBatch(_ http.ResponseWriter, _ *http.Request) {
	s.hits = append(s.hits, "import-apply-batch")
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

func (s *stub) GetUserProfile(_ http.ResponseWriter, _ *http.Request, username string) {
	s.hits = append(s.hits, "user:"+username)
}

func (s *stub) ListUserDemos(_ http.ResponseWriter, _ *http.Request, username string, _ api.ListUserDemosParams) {
	s.hits = append(s.hits, "user-demos:"+username)
}

func (s *stub) GetMyHistory(_ http.ResponseWriter, _ *http.Request, _ api.GetMyHistoryParams) {
	s.hits = append(s.hits, "my-history")
}

func (s *stub) PostAuthRegister(http.ResponseWriter, *http.Request) {
	s.hits = append(s.hits, "register")
}

func (s *stub) DeleteMyHistory(http.ResponseWriter, *http.Request) {
	s.hits = append(s.hits, "delete-my-history")
}

func (s *stub) UpdateMyHistorySettings(http.ResponseWriter, *http.Request) {
	s.hits = append(s.hits, "history-settings")
}

func (s *stub) ListCollections(http.ResponseWriter, *http.Request) {
	s.hits = append(s.hits, "collections")
}

func (s *stub) CreateCollection(http.ResponseWriter, *http.Request) {
	s.hits = append(s.hits, "create-collection")
}

func (s *stub) GetCollection(_ http.ResponseWriter, _ *http.Request, id int) {
	s.hits = append(s.hits, "collection:"+strconv.Itoa(id))
}

func (s *stub) UpdateCollection(_ http.ResponseWriter, _ *http.Request, id int) {
	s.hits = append(s.hits, "update-collection:"+strconv.Itoa(id))
}

func (s *stub) DeleteCollection(_ http.ResponseWriter, _ *http.Request, id int) {
	s.hits = append(s.hits, "delete-collection:"+strconv.Itoa(id))
}

func (s *stub) AddCollectionItem(_ http.ResponseWriter, _ *http.Request, id int) {
	s.hits = append(s.hits, "add-item:"+strconv.Itoa(id))
}

func (s *stub) RemoveCollectionItem(_ http.ResponseWriter, _ *http.Request, id int, slug string) {
	s.hits = append(s.hits, "remove-item:"+strconv.Itoa(id)+":"+slug)
}

func (s *stub) GetCollectionStatus(_ http.ResponseWriter, _ *http.Request, params api.GetCollectionStatusParams) {
	s.hits = append(s.hits, "collection-status:"+params.Slug)
}

func (s *stub) ListNotifications(_ http.ResponseWriter, _ *http.Request, _ api.ListNotificationsParams) {
	s.hits = append(s.hits, "notifications")
}

func (s *stub) GetUnreadNotificationCount(http.ResponseWriter, *http.Request) {
	s.hits = append(s.hits, "unread-count")
}

func (s *stub) MarkNotificationRead(_ http.ResponseWriter, _ *http.Request, id int) {
	s.hits = append(s.hits, "read-notification:"+strconv.Itoa(id))
}

func (s *stub) MarkAllNotificationsRead(http.ResponseWriter, *http.Request) {
	s.hits = append(s.hits, "read-all-notifications")
}

func (s *stub) GetAdminInbox(_ http.ResponseWriter, _ *http.Request, _ api.GetAdminInboxParams) {
	s.hits = append(s.hits, "admin-inbox")
}

func (s *stub) ReviewEntity(_ http.ResponseWriter, _ *http.Request, kind api.ReviewEntityParamsKind, id string) {
	s.hits = append(s.hits, "review-entity:"+string(kind)+":"+id)
}

func (s *stub) ListAdminProposals(_ http.ResponseWriter, _ *http.Request, _ api.ListAdminProposalsParams) {
	s.hits = append(s.hits, "admin-proposals")
}

func (s *stub) GetAdminProposal(_ http.ResponseWriter, _ *http.Request, id int) {
	s.hits = append(s.hits, "admin-proposal:"+strconv.Itoa(id))
}

func (s *stub) ReviewAdminProposal(_ http.ResponseWriter, _ *http.Request, id int) {
	s.hits = append(s.hits, "review-proposal:"+strconv.Itoa(id))
}

func (s *stub) BatchReviewAdminProposals(http.ResponseWriter, *http.Request) {
	s.hits = append(s.hits, "batch-proposals")
}

func (s *stub) SubmitProposal(http.ResponseWriter, *http.Request) {
	s.hits = append(s.hits, "submit-proposal")
}

func (s *stub) ImportBenchmarks(http.ResponseWriter, *http.Request) {
	s.hits = append(s.hits, "import-benchmarks")
}

func (s *stub) ListBenchmarks(http.ResponseWriter, *http.Request) {
	s.hits = append(s.hits, "benchmarks")
}

func (s *stub) GetBenchmarkOverview(http.ResponseWriter, *http.Request) {
	s.hits = append(s.hits, "benchmark-overview")
}

func (s *stub) GetBenchmark(_ http.ResponseWriter, _ *http.Request, slug string) {
	s.hits = append(s.hits, "benchmark:"+slug)
}

func (s *stub) WithdrawProposal(_ http.ResponseWriter, _ *http.Request, id int) {
	s.hits = append(s.hits, "withdraw-proposal:"+strconv.Itoa(id))
}

func (s *stub) GetDemoAppeal(_ http.ResponseWriter, _ *http.Request, slug string) {
	s.hits = append(s.hits, "demo-appeal:"+slug)
}

func (s *stub) SubmitDemoAppeal(_ http.ResponseWriter, _ *http.Request, slug string) {
	s.hits = append(s.hits, "submit-appeal:"+slug)
}

func (s *stub) ListAdminAppeals(_ http.ResponseWriter, _ *http.Request, _ api.ListAdminAppealsParams) {
	s.hits = append(s.hits, "admin-appeals")
}

func (s *stub) ReviewAdminAppeal(_ http.ResponseWriter, _ *http.Request, id int) {
	s.hits = append(s.hits, "review-appeal:"+strconv.Itoa(id))
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
		{http.MethodGet, "/api/v1/users/it-author", "user:it-author"},
		{http.MethodGet, "/api/v1/users/it-author/demos", "user-demos:it-author"},
		{http.MethodGet, "/api/v1/me/history", "my-history"},
		{http.MethodDelete, "/api/v1/me/history", "delete-my-history"},
		{http.MethodPut, "/api/v1/me/history/settings", "history-settings"},
		{http.MethodGet, "/api/v1/collections", "collections"},
		{http.MethodPost, "/api/v1/collections", "create-collection"},
		{http.MethodGet, "/api/v1/collections/7", "collection:7"},
		{http.MethodPatch, "/api/v1/collections/7", "update-collection:7"},
		{http.MethodDelete, "/api/v1/collections/7", "delete-collection:7"},
		{http.MethodPost, "/api/v1/collections/7/items", "add-item:7"},
		{http.MethodDelete, "/api/v1/collections/7/items/demo-a", "remove-item:7:demo-a"},
		{http.MethodGet, "/api/v1/me/collections/status?slug=demo-a", "collection-status:demo-a"},
		{http.MethodGet, "/api/v1/notifications", "notifications"},
		{http.MethodGet, "/api/v1/notifications/unread-count", "unread-count"},
		{http.MethodPost, "/api/v1/notifications/read-all", "read-all-notifications"},
		{http.MethodPost, "/api/v1/notifications/5/read", "read-notification:5"},
		{http.MethodGet, "/api/v1/admin/inbox", "admin-inbox"},
		// demo 的具体路径由旧端点（兼容别名）接住；泛化路由用另一个 kind 验证存在。
		{http.MethodPost, "/api/v1/admin/reviews/proposal/9", "review-entity:proposal:9"},
		{http.MethodGet, "/api/v1/admin/proposals", "admin-proposals"},
		{http.MethodGet, "/api/v1/admin/proposals/7", "admin-proposal:7"},
		{http.MethodPost, "/api/v1/admin/proposals/7/review", "review-proposal:7"},
		{http.MethodPost, "/api/v1/admin/proposals/batch", "batch-proposals"},
		{http.MethodPost, "/api/v1/proposals", "submit-proposal"},
		{http.MethodPost, "/api/v1/proposals/12/withdraw", "withdraw-proposal:12"},
		{http.MethodPost, "/api/v1/admin/benchmarks/import", "import-benchmarks"},
		{http.MethodGet, "/api/v1/benchmarks", "benchmarks"},
		{http.MethodGet, "/api/v1/benchmarks/swe-bench", "benchmark:swe-bench"},
		// 静态段必须压过 /{slug}：写成 "benchmark:overview" 就说明被参数路由抢走了。
		{http.MethodGet, "/api/v1/benchmarks/overview", "benchmark-overview"},
		// 同上：/api/v1/models/upstream 必须命中静态路由，而不是 getModel 的 {slug}。
		// 写成 "detail:upstream" 就说明上游搜索被 /api/v1/models/{slug} 抢走了。
		{http.MethodGet, "/api/v1/models/upstream", "upstream-search"},
		{http.MethodGet, "/api/v1/demos/demo-a/appeal", "demo-appeal:demo-a"},
		{http.MethodPost, "/api/v1/demos/demo-a/appeal", "submit-appeal:demo-a"},
		{http.MethodGet, "/api/v1/admin/appeals", "admin-appeals"},
		{http.MethodPost, "/api/v1/admin/appeals/7/review", "review-appeal:7"},
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
