package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/api"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/identity"
)

// 公开作者页的读路径（#425）：资料、公开作品、大小写、非 active、未知用户。
//
// 反例是这里的重点：作者页是**不需要登录**的公开端点，任何「区分账号状态」
// 的响应差异都会变成探测器——未知用户、被封用户、格式不合法必须同一句话。

func TestIntegrationPublicProfile(t *testing.T) {
	h, _, idSvc := newIdempotencyHandler(t)
	pool := integrationPool(t)

	author, sid := registerVerified(t, pool, idSvc, identity.RegisterInput{
		Email:       "it-author-page@example.com",
		Password:    "correct-horse-battery",
		Username:    "it-author-page",
		DisplayName: "作者页作者",
	})
	t.Cleanup(func() { cleanupAuthor(t, pool, author.Sub) })

	published := submitHTML(t, h, sid, "已发布的作品", "<h1>published</h1>")
	publishNow(t, pool, published)
	// 第二件只投稿不发布：作者页只该出现公开作品。
	submitHTML(t, h, sid, "还没发布的作品", "<h1>draft</h1>")

	getProfile := func(username string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/users/"+username, nil)
		h.GetUserProfile(rec, req, username)
		return rec
	}

	rec := getProfile(author.Username)
	if rec.Code != http.StatusOK {
		t.Fatalf("作者资料 = %d，想要 200（body=%s）", rec.Code, rec.Body.String())
	}
	var profile api.PublicProfile
	if err := json.Unmarshal(rec.Body.Bytes(), &profile); err != nil {
		t.Fatalf("作者资料不是合法 JSON: %v（body=%s）", err, rec.Body.String())
	}
	if profile.Username != author.Username || profile.DisplayName != "作者页作者" {
		t.Fatalf("资料字段不对: %+v", profile)
	}
	if profile.DemoCount != 1 {
		t.Fatalf("公开作品数 = %d，想要 1（草稿不该计入）", profile.DemoCount)
	}
	if profile.AvatarUrl != nil {
		t.Fatalf("没设头像时 avatarUrl 应为 null，实际 %v", *profile.AvatarUrl)
	}

	// 用户名大小写不敏感：uq_profiles_username 建在 lower(username) 上。
	if rec := getProfile(strings.ToUpper(author.Username)); rec.Code != http.StatusOK {
		t.Fatalf("大写用户名 = %d，想要 200", rec.Code)
	}

	list := func(username string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/users/"+username+"/demos", nil)
		h.ListUserDemos(rec, req, username, api.ListUserDemosParams{})
		return rec
	}
	rec = list(author.Username)
	if rec.Code != http.StatusOK {
		t.Fatalf("公开作品列表 = %d，想要 200（body=%s）", rec.Code, rec.Body.String())
	}
	var listBody api.ProfileDemoList
	if err := json.Unmarshal(rec.Body.Bytes(), &listBody); err != nil {
		t.Fatalf("作品列表不是合法 JSON: %v（body=%s）", err, rec.Body.String())
	}
	if listBody.Total != 1 || len(listBody.Items) != 1 {
		t.Fatalf("公开作品列表 = %d 条 / total %d，想要 1", len(listBody.Items), listBody.Total)
	}
	if listBody.Items[0].Slug != published {
		t.Fatalf("列表里是 %q，想要 %q——草稿不该出现在作者页", listBody.Items[0].Slug, published)
	}
}

// 未知用户、格式不合法、非 active 账号：两个端点都必须 404，且文案一致。
func TestIntegrationPublicProfileUnknownUser(t *testing.T) {
	h, _, _ := newIdempotencyHandler(t)

	// 请求地址用固定的合法形状：这里要钉的是「处理函数怎么判这个名字」，
	// 不是「带空格的名字能不能拼进 URL」（那会让 httptest 先 panic）。
	cases := []string{"nobody-here-xyz", "ab", "bad!name", "bad name", "toolong-username-xyz"}
	for _, username := range cases {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/users/probe", nil)
		h.GetUserProfile(rec, req, username)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("资料（%q）= %d，想要 404", username, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "作者不存在") {
			t.Fatalf("资料（%q）的文案 = %s，想要「作者不存在」", username, rec.Body.String())
		}

		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodGet, "/api/v1/users/probe/demos", nil)
		h.ListUserDemos(rec, req, username, api.ListUserDemosParams{})
		if rec.Code != http.StatusNotFound {
			t.Fatalf("作品列表（%q）= %d，想要 404", username, rec.Code)
		}
	}
}

func TestIntegrationPublicProfileHidesInactive(t *testing.T) {
	h, _, idSvc := newIdempotencyHandler(t)
	pool := integrationPool(t)

	author, _ := registerVerified(t, pool, idSvc, identity.RegisterInput{
		Email:       "it-author-suspended@example.com",
		Password:    "correct-horse-battery",
		Username:    "it-author-susp",
		DisplayName: "受限作者",
	})
	t.Cleanup(func() { cleanupAuthor(t, pool, author.Sub) })

	// suspended 必须同时写 previous_status='active'（ck_accounts_previous_status）。
	if _, err := pool.Exec(t.Context(), `UPDATE accounts SET status = 'suspended', previous_status = 'active' WHERE sub = $1`, author.Sub); err != nil {
		t.Fatalf("改状态失败: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/"+author.Username, nil)
	h.GetUserProfile(rec, req, author.Username)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("受限账号的资料 = %d，想要 404", rec.Code)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/users/"+author.Username+"/demos", nil)
	h.ListUserDemos(rec, req, author.Username, api.ListUserDemosParams{})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("受限账号的作品列表 = %d，想要 404", rec.Code)
	}
}
