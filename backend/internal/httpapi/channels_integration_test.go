package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/api"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/identity"
)

// 渠道上架 / 撤下（松点 26 的结案用例）。
//
// 这一组必须自己有「已发布」的作品：只有 publishDemo 能让作品变成 published，
// 而 publishDemo 之后再加第二个渠道正是以前做不到、现在要钉住的那件事。

func newChannelHandler(t *testing.T) (*Handler, *identity.Service, *pgxpool.Pool) {
	t.Helper()
	return newSortHandler(t)
}

// publishedDemo 造一件「已发布 + 一个 listed 渠道」的作品，返回 slug。
func publishedDemo(t *testing.T, h *Handler, pool *pgxpool.Pool, sid, title string) string {
	t.Helper()
	slug := submitHTML(t, h, sid, title, `<!doctype html><p>渠道用例</p>`)
	publishNow(t, pool, slug)
	return slug
}

// channelCall 发一次渠道请求。body 为空表示 DELETE。
func channelCall(t *testing.T, h *Handler, method, path, sid string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var payload []byte
	if body != nil {
		var err error
		payload, err = json.Marshal(body)
		if err != nil {
			t.Fatalf("序列化请求体失败: %v", err)
		}
	}
	// 自己拼请求：lifecycleCall 会把路径前缀成 /api/v1/demos/，而渠道端点在 /api/v1/admin 下。
	var r *http.Request
	if payload == nil {
		r = httptest.NewRequest(method, path, nil)
	} else {
		r = httptest.NewRequest(method, path, bytes.NewReader(payload))
		r.Header.Set("Content-Type", "application/json")
	}
	if sid != "" {
		r.AddCookie(&http.Cookie{Name: sessionCookie, Value: sid})
	}
	rec := httptest.NewRecorder()
	NewRouter(h, nil, nil).ServeHTTP(rec, r)
	return rec
}

// channelsOf 直接问库：断言落库结果，而不是相信响应体自述。
func channelsOf(t *testing.T, pool *pgxpool.Pool, slug string) map[string]string {
	t.Helper()
	rows, err := pool.Query(context.Background(), `
		SELECT c.channel, c.visibility
		FROM demo_channels c JOIN demos d ON d.id = c.demo_id
		WHERE d.slug = $1`, slug)
	if err != nil {
		t.Fatalf("查渠道失败: %v", err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var ch, vis string
		if err := rows.Scan(&ch, &vis); err != nil {
			t.Fatalf("读渠道失败: %v", err)
		}
		out[ch] = vis
	}
	return out
}

func TestIntegrationAddSecondChannel(t *testing.T) {
	h, idSvc, pool := newChannelHandler(t)
	admin := mkAdmin(t, pool, idSvc, "ch-a")
	slug := publishedDemo(t, h, pool, admin, "多渠道用例")

	// 第一个渠道由 publishNow 造的 deep/listed；这里追加 astra（en）。
	rec := channelCall(t, h, http.MethodPost, "/api/v1/admin/demos/"+slug+"/channels", admin,
		map[string]any{"channel": "astra", "visibility": "listed", "lang": "en"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("追加第二个渠道 = %d，想要 201：%s", rec.Code, rec.Body.String())
	}
	var res api.DemoChannelResult
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if !res.Created {
		t.Fatalf("新建渠道应当 created=true，实际 %+v", res)
	}

	got := channelsOf(t, pool, slug)
	if len(got) != 2 || got["astra"] != "listed" {
		t.Fatalf("渠道 = %v，想要 deep+astra 两条且 astra=listed", got)
	}

	// 同渠道再调一次：不是新建，而是改可见性。
	rec = channelCall(t, h, http.MethodPost, "/api/v1/admin/demos/"+slug+"/channels", admin,
		map[string]any{"channel": "astra", "visibility": "unlisted", "lang": "en"})
	if rec.Code != http.StatusOK {
		t.Fatalf("改可见性 = %d，想要 200：%s", rec.Code, rec.Body.String())
	}
	res = api.DemoChannelResult{}
	_ = json.Unmarshal(rec.Body.Bytes(), &res)
	if res.Created {
		t.Fatalf("改已有渠道应当 created=false，实际 %+v", res)
	}
	if got := channelsOf(t, pool, slug); got["astra"] != "unlisted" {
		t.Fatalf("astra 可见性 = %q，想要 unlisted", got["astra"])
	}
}

func TestIntegrationAddChannelRejectsWrongLang(t *testing.T) {
	h, idSvc, pool := newChannelHandler(t)
	admin := mkAdmin(t, pool, idSvc, "ch-b")
	slug := publishedDemo(t, h, pool, admin, "渠道语言用例")

	rec := channelCall(t, h, http.MethodPost, "/api/v1/admin/demos/"+slug+"/channels", admin,
		map[string]any{"channel": "astra", "visibility": "listed", "lang": "zh"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("astra + zh = %d，想要 400：%s", rec.Code, rec.Body.String())
	}
}

func TestIntegrationAddChannelRequiresPublished(t *testing.T) {
	h, idSvc, pool := newChannelHandler(t)
	admin := mkAdmin(t, pool, idSvc, "ch-c")
	// 只投稿、不发布：没有对外版本。
	slug := submitHTML(t, h, admin, "没发布过", `<!doctype html><p>x</p>`)

	rec := channelCall(t, h, http.MethodPost, "/api/v1/admin/demos/"+slug+"/channels", admin,
		map[string]any{"channel": "astra", "visibility": "listed", "lang": "en"})
	if rec.Code != http.StatusConflict {
		t.Fatalf("未发布的作品追加渠道 = %d，想要 409：%s", rec.Code, rec.Body.String())
	}
}

func TestIntegrationRemoveChannel(t *testing.T) {
	h, idSvc, pool := newChannelHandler(t)
	admin := mkAdmin(t, pool, idSvc, "ch-d")
	slug := publishedDemo(t, h, pool, admin, "撤渠道用例")

	// 只剩一个渠道且仍是 published：不能撤。
	rec := channelCall(t, h, http.MethodDelete, "/api/v1/admin/demos/"+slug+"/channels/deep", admin, nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("撤最后一个渠道 = %d，想要 409：%s", rec.Code, rec.Body.String())
	}

	// 挂上第二个渠道之后可以撤第一个。
	rec = channelCall(t, h, http.MethodPost, "/api/v1/admin/demos/"+slug+"/channels", admin,
		map[string]any{"channel": "astra", "visibility": "listed", "lang": "en"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("追加渠道 = %d，想要 201", rec.Code)
	}
	rec = channelCall(t, h, http.MethodDelete, "/api/v1/admin/demos/"+slug+"/channels/deep", admin, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("撤掉非唯一渠道 = %d，想要 204：%s", rec.Code, rec.Body.String())
	}
	if got := channelsOf(t, pool, slug); len(got) != 1 || got["astra"] != "listed" {
		t.Fatalf("撤下后渠道 = %v，想要只剩 astra", got)
	}

	// 撤一个本来没有的渠道：404，而不是「悄悄成功」。
	rec = channelCall(t, h, http.MethodDelete, "/api/v1/admin/demos/"+slug+"/channels/nope", admin, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("撤不存在的渠道 = %d，想要 404：%s", rec.Code, rec.Body.String())
	}
}

func TestIntegrationChannelRequiresPublishPermission(t *testing.T) {
	h, idSvc, pool := newChannelHandler(t)
	// 普通作者自己投稿（有会话，但没有 demo.publish）。
	acc, sid := registerVerified(t, pool, idSvc, identity.RegisterInput{
		Email:       "it-ch-e@example.com",
		Password:    "channeltest123",
		Username:    "it-ch-e",
		DisplayName: "普通作者",
	})
	t.Cleanup(func() { cleanupAuthor(t, pool, acc.Sub) })
	slug := submitHTML(t, h, sid, "普通作者", `<!doctype html><p>y</p>`)
	publishNow(t, pool, slug)

	rec := channelCall(t, h, http.MethodPost, "/api/v1/admin/demos/"+slug+"/channels", sid,
		map[string]any{"channel": "astra", "visibility": "listed", "lang": "en"})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("没有 demo.publish 就追加渠道 = %d，想要 403：%s", rec.Code, rec.Body.String())
	}
}

func TestIntegrationAddChannelRequiresApproved(t *testing.T) {
	h, idSvc, pool := newChannelHandler(t)
	admin := mkAdmin(t, pool, idSvc, "ch-f")
	slug := publishedDemo(t, h, pool, admin, "改稿被驳回")

	// 版本分离之后真实存在的状态：线上仍有对外版本，但当前改动被驳回。
	if _, err := pool.Exec(context.Background(),
		`UPDATE demos SET moderation_state = 'rejected' WHERE slug = $1`, slug); err != nil {
		t.Fatalf("造驳回态失败: %v", err)
	}

	rec := channelCall(t, h, http.MethodPost, "/api/v1/admin/demos/"+slug+"/channels", admin,
		map[string]any{"channel": "astra", "visibility": "listed", "lang": "en"})
	if rec.Code != http.StatusConflict {
		t.Fatalf("内容没通过审核就追加渠道 = %d，想要 409：%s", rec.Code, rec.Body.String())
	}
}

// TestIntegrationListDemoChannels 管理端读渠道列表。
//
// 三种「空」必须分开：作品不存在（404）、作品存在但没渠道（200 + []）、
// 没有任何渠道权限（403）。安静返回空数组会让界面把「读不到」画成「没有渠道」。
func TestIntegrationListDemoChannels(t *testing.T) {
	pool := integrationPool(t)
	h, idSvc, _ := newChannelHandler(t)
	author, sid := registerVerified(t, pool, idSvc, registerInputFor(t, "listch"))
	t.Cleanup(func() { cleanupAuthor(t, pool, author.Sub) })

	slug := publishedDemo(t, h, pool, sid, "渠道列表用例")
	adminSid := mkAdmin(t, pool, idSvc, "chlist")

	// 只有 publishDemo 落的那一个渠道。
	rec := channelCall(t, h, http.MethodGet, "/api/v1/admin/demos/"+slug+"/channels", adminSid, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("读渠道列表 = %d，想要 200：%s", rec.Code, rec.Body.String())
	}
	var list api.DemoChannelList
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("解析渠道列表失败: %v", err)
	}
	if len(list.Items) != 1 || list.Items[0].Channel != "deep" {
		t.Fatalf("渠道列表 = %+v，想要只有一个 deep", list.Items)
	}

	// 再加一个渠道：列表要能反映出来，并且按渠道名排序。
	rec = channelCall(t, h, http.MethodPost, "/api/v1/admin/demos/"+slug+"/channels", adminSid,
		map[string]any{"channel": "astra", "visibility": "unlisted", "lang": "en"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("上架 astra = %d，想要 201：%s", rec.Code, rec.Body.String())
	}
	rec = channelCall(t, h, http.MethodGet, "/api/v1/admin/demos/"+slug+"/channels", adminSid, nil)
	list = api.DemoChannelList{}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("解析渠道列表失败: %v", err)
	}
	if len(list.Items) != 2 || list.Items[0].Channel != "astra" || list.Items[1].Channel != "deep" {
		t.Fatalf("渠道列表 = %+v，想要 [astra deep]（按渠道名排序）", list.Items)
	}
	if list.Items[0].Lang != api.DemoChannelLangEn || list.Items[0].Visibility != api.DemoChannelVisibilityUnlisted {
		t.Fatalf("astra 那一行 = %+v，想要 unlisted/en", list.Items[0])
	}

	// 作品存在但没有渠道：空数组，不是 404。
	bare := submitHTML(t, h, sid, "没有渠道的作品", `<!doctype html><p>还没发</p>`)
	rec = channelCall(t, h, http.MethodGet, "/api/v1/admin/demos/"+bare+"/channels", adminSid, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("没有渠道的作品 = %d，想要 200（空数组）", rec.Code)
	}
	if got := rec.Body.String(); got != "{\"items\":[]}\n" {
		t.Fatalf("没有渠道时的响应 = %s，想要 items 是空数组而不是 null", got)
	}

	// 不存在 → 404。
	rec = channelCall(t, h, http.MethodGet, "/api/v1/admin/demos/no-such-demo/channels", adminSid, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("不存在的作品 = %d，想要 404", rec.Code)
	}

	// 没权限 → 403（作者没有 demo.publish）。
	rec = channelCall(t, h, http.MethodGet, "/api/v1/admin/demos/"+slug+"/channels", sid, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("作者读渠道列表 = %d，想要 403", rec.Code)
	}
	// 未登录 → 401。
	rec = channelCall(t, h, http.MethodGet, "/api/v1/admin/demos/"+slug+"/channels", "", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("匿名读渠道列表 = %d，想要 401", rec.Code)
	}
}
