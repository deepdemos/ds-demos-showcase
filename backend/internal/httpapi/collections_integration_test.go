package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/api"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/identity"
)

// 收藏域（#438 / COL-001 – COL-014）的集成读路径。
//
// 这里钉的是「谁能读、谁能写、什么不能写」：
// 默认夹不可改删、私有夹对非主人 404、猜 id 写 403、只能收藏公开作品、上限读设置域。

func collReq(method, path, sid, body string) *http.Request {
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, path, nil)
	} else {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	}
	if sid != "" {
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: sid})
	}
	return req
}

func decodeCollection[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	if rec.Code != http.StatusOK && rec.Code != http.StatusCreated {
		t.Fatalf("状态 = %d，想要 200/201（body=%s）", rec.Code, rec.Body.String())
	}
	var out T
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("响应不是合法 JSON: %v（body=%s）", err, rec.Body.String())
	}
	return out
}

func mkCollection(t *testing.T, h *Handler, sid, name string) api.Collection {
	t.Helper()
	rec := httptest.NewRecorder()
	h.CreateCollection(rec, collReq(http.MethodPost, "/api/v1/collections", sid,
		`{"name":"`+name+`"}`))
	return decodeCollection[api.Collection](t, rec)
}

func collectionStatus(t *testing.T, h *Handler, sid, slug string) api.CollectionStatus {
	t.Helper()
	rec := httptest.NewRecorder()
	h.GetCollectionStatus(rec, collReq(http.MethodGet, "/api/v1/me/collections/status?slug="+slug, sid, ""),
		api.GetCollectionStatusParams{Slug: slug})
	return decodeCollection[api.CollectionStatus](t, rec)
}

func collectionList(t *testing.T, h *Handler, sid string) api.CollectionList {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ListCollections(rec, collReq(http.MethodGet, "/api/v1/collections", sid, ""))
	return decodeCollection[api.CollectionList](t, rec)
}

func TestIntegrationCollections(t *testing.T) {
	h, _, idSvc := newIdempotencyHandler(t)
	pool := integrationPool(t)

	owner, ownerSID := registerVerified(t, pool, idSvc, identity.RegisterInput{
		Email:       "it-coll-owner@example.com",
		Password:    "correct-horse-battery",
		Username:    "it-coll-owner",
		DisplayName: "收藏主人",
	})
	t.Cleanup(func() { cleanupAuthor(t, pool, owner.Sub) })
	other, otherSID := registerVerified(t, pool, idSvc, identity.RegisterInput{
		Email:       "it-coll-other@example.com",
		Password:    "correct-horse-battery",
		Username:    "it-coll-other",
		DisplayName: "路人",
	})
	t.Cleanup(func() { cleanupAuthor(t, pool, other.Sub) })

	publishedA := submitHTML(t, h, ownerSID, "收藏 A", "<h1>a</h1>")
	publishNow(t, pool, publishedA)
	publishedB := submitHTML(t, h, ownerSID, "收藏 B", "<h1>b</h1>")
	publishNow(t, pool, publishedB)
	draft := submitHTML(t, h, ownerSID, "草稿", "<h1>draft</h1>")

	list := collectionList(t, h, ownerSID)
	if len(list.Items) != 1 || !list.Items[0].IsDefault {
		t.Fatalf("首次打开应有且只有一个默认夹，实际 %+v", list.Items)
	}
	defaultID := list.Items[0].Id

	custom := mkCollection(t, h, ownerSID, "我的夹")
	if custom.IsDefault || custom.Visibility != api.CollectionVisibilityPrivate {
		t.Fatalf("新夹应为私有非默认，实际 %+v", custom)
	}
	// 重名（大小写不敏感）422。
	rec := httptest.NewRecorder()
	h.CreateCollection(rec, collReq(http.MethodPost, "/api/v1/collections", ownerSID, `{"name":"我的夹"}`))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("重名 = %d，想要 422", rec.Code)
	}

	// 默认夹：加作品成功。
	rec = httptest.NewRecorder()
	h.AddCollectionItem(rec, collReq(http.MethodPost, "/api/v1/collections/"+itoa(defaultID)+"/items", ownerSID,
		`{"slug":"`+publishedA+`"}`), defaultID)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("收藏到默认夹 = %d，想要 204（body=%s）", rec.Code, rec.Body.String())
	}
	status := collectionStatus(t, h, ownerSID, publishedA)
	if !status.InDefault || len(status.CollectionIds) != 0 {
		t.Fatalf("状态 = %+v，想要 inDefault=true 且没有自定义夹", status)
	}

	// 同一件作品加进自定义夹：两条路径独立。
	rec = httptest.NewRecorder()
	h.AddCollectionItem(rec, collReq(http.MethodPost, "/api/v1/collections/"+itoa(custom.Id)+"/items", ownerSID,
		`{"slug":"`+publishedA+`"}`), custom.Id)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("收藏到自定义夹 = %d，想要 204", rec.Code)
	}
	if status := collectionStatus(t, h, ownerSID, publishedA); len(status.CollectionIds) != 1 {
		t.Fatalf("自定义夹 id 列表 = %v，想要 1 项", status.CollectionIds)
	}

	// 他人对夹内条目写：403（COL-012）。
	otherBody := "{\"slug\":\"" + publishedA + "\"}"
	rec = httptest.NewRecorder()
	h.AddCollectionItem(rec, collReq(http.MethodPost, "/api/v1/collections/"+itoa(defaultID)+"/items", otherSID, otherBody), defaultID)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("他人加条目 = %d，想要 403", rec.Code)
	}
	rec = httptest.NewRecorder()
	h.RemoveCollectionItem(rec, collReq(http.MethodDelete, "/api/v1/collections/"+itoa(defaultID)+"/items/"+publishedA, otherSID, ""), defaultID, publishedA)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("他人移出 = %d，想要 403", rec.Code)
	}

	// 默认夹不可改 / 不可删。
	rec = httptest.NewRecorder()
	h.UpdateCollection(rec, collReq(http.MethodPatch, "/api/v1/collections/"+itoa(defaultID), ownerSID,
		`{"visibility":"public"}`), defaultID)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("默认夹转公开 = %d，想要 422", rec.Code)
	}
	rec = httptest.NewRecorder()
	h.DeleteCollection(rec, collReq(http.MethodDelete, "/api/v1/collections/"+itoa(defaultID), ownerSID, ""), defaultID)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("删除默认夹 = %d，想要 422", rec.Code)
	}

	// 私有夹：访客 404，主人 200。
	rec = httptest.NewRecorder()
	h.GetCollection(rec, collReq(http.MethodGet, "/api/v1/collections/"+itoa(custom.Id), "", ""), custom.Id)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("访客读私有夹 = %d，想要 404", rec.Code)
	}
	rec = httptest.NewRecorder()
	h.GetCollection(rec, collReq(http.MethodGet, "/api/v1/collections/"+itoa(custom.Id), ownerSID, ""), custom.Id)
	detail := decodeCollection[api.CollectionDetail](t, rec)
	if len(detail.Items) != 1 || detail.Items[0].Slug != publishedA || !detail.Items[0].Accessible {
		t.Fatalf("私有夹详情 = %+v", detail.Items)
	}

	// 公开之后访客可读；他人写仍 403。
	rec = httptest.NewRecorder()
	h.UpdateCollection(rec, collReq(http.MethodPatch, "/api/v1/collections/"+itoa(custom.Id), ownerSID,
		`{"visibility":"public"}`), custom.Id)
	if rec.Code != http.StatusOK {
		t.Fatalf("转公开 = %d，想要 200（body=%s）", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	h.GetCollection(rec, collReq(http.MethodGet, "/api/v1/collections/"+itoa(custom.Id), "", ""), custom.Id)
	if rec.Code != http.StatusOK {
		t.Fatalf("访客读公开夹 = %d，想要 200", rec.Code)
	}
	rec = httptest.NewRecorder()
	h.UpdateCollection(rec, collReq(http.MethodPatch, "/api/v1/collections/"+itoa(custom.Id), otherSID,
		`{"name":"偷改"}`), custom.Id)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("他人写 = %d，想要 403", rec.Code)
	}
	// 转回私有：之前的公开读立刻 404（COL-011）。
	rec = httptest.NewRecorder()
	h.UpdateCollection(rec, collReq(http.MethodPatch, "/api/v1/collections/"+itoa(custom.Id), ownerSID,
		`{"visibility":"private"}`), custom.Id)
	rec = httptest.NewRecorder()
	h.GetCollection(rec, collReq(http.MethodGet, "/api/v1/collections/"+itoa(custom.Id), "", ""), custom.Id)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("转私有后访客读 = %d，想要 404", rec.Code)
	}

	// 只能收藏当前公开作品：草稿 422。
	rec = httptest.NewRecorder()
	h.AddCollectionItem(rec, collReq(http.MethodPost, "/api/v1/collections/"+itoa(defaultID)+"/items", ownerSID,
		`{"slug":"`+draft+`"}`), defaultID)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("收藏草稿 = %d，想要 422", rec.Code)
	}

	// 移出：幂等；作品本身还在。
	rec = httptest.NewRecorder()
	h.RemoveCollectionItem(rec, collReq(http.MethodDelete, "/api/v1/collections/"+itoa(custom.Id)+"/items/"+publishedA, ownerSID, ""), custom.Id, publishedA)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("移出 = %d，想要 204", rec.Code)
	}
	rec = httptest.NewRecorder()
	h.RemoveCollectionItem(rec, collReq(http.MethodDelete, "/api/v1/collections/"+itoa(custom.Id)+"/items/"+publishedA, ownerSID, ""), custom.Id, publishedA)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("重复移出 = %d，想要 204", rec.Code)
	}

	// 删夹不删作品（COL-013）。
	rec = httptest.NewRecorder()
	h.DeleteCollection(rec, collReq(http.MethodDelete, "/api/v1/collections/"+itoa(custom.Id), ownerSID, ""), custom.Id)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("删夹 = %d，想要 204", rec.Code)
	}
	var demos int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM demos WHERE slug = $1`, publishedB).Scan(&demos); err != nil || demos != 1 {
		t.Fatalf("删夹不该删作品：count=%d err=%v", demos, err)
	}
}

func TestIntegrationCollectionsRequiresLogin(t *testing.T) {
	h, _, _ := newIdempotencyHandler(t)

	cases := []struct {
		name string
		do   func(*httptest.ResponseRecorder)
	}{
		{"列表", func(rec *httptest.ResponseRecorder) {
			h.ListCollections(rec, collReq(http.MethodGet, "/api/v1/collections", "", ""))
		}},
		{"新建", func(rec *httptest.ResponseRecorder) {
			h.CreateCollection(rec, collReq(http.MethodPost, "/api/v1/collections", "", `{"name":"x"}`))
		}},
		{"状态", func(rec *httptest.ResponseRecorder) {
			h.GetCollectionStatus(rec, collReq(http.MethodGet, "/api/v1/me/collections/status?slug=x", "", ""),
				api.GetCollectionStatusParams{Slug: "x"})
		}},
	}
	for _, tc := range cases {
		rec := httptest.NewRecorder()
		tc.do(rec)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s 未登录 = %d，想要 401", tc.name, rec.Code)
		}
	}
}

// 上限从设置域读：把两个上限都调成 1，第二次就该 422。
func TestIntegrationCollectionLimits(t *testing.T) {
	h, _, idSvc := newIdempotencyHandler(t)
	pool := integrationPool(t)

	owner, sid := registerVerified(t, pool, idSvc, identity.RegisterInput{
		Email:       "it-coll-limit@example.com",
		Password:    "correct-horse-battery",
		Username:    "it-coll-limit",
		DisplayName: "上限用户",
	})
	t.Cleanup(func() { cleanupAuthor(t, pool, owner.Sub) })

	for key, value := range map[string]string{
		"collection.max_per_account": "1",
		"collection.max_items":       "1",
	} {
		if _, err := pool.Exec(context.Background(), `
INSERT INTO site_settings (key, scope, value_type, value)
VALUES ($1, 'site', 'number', to_jsonb($2::int))
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`, key, value); err != nil {
			t.Fatalf("写上限 %s 失败: %v", key, err)
		}
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM site_settings WHERE key IN ('collection.max_per_account','collection.max_items')`)
	})

	first := mkCollection(t, h, sid, "第一个")
	rec := httptest.NewRecorder()
	h.CreateCollection(rec, collReq(http.MethodPost, "/api/v1/collections", sid, `{"name":"第二个"}`))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("超上限建夹 = %d，想要 422（body=%s）", rec.Code, rec.Body.String())
	}

	slug := submitHTML(t, h, sid, "上限作品", "<h1>limit</h1>")
	publishNow(t, pool, slug)
	rec = httptest.NewRecorder()
	h.AddCollectionItem(rec, collReq(http.MethodPost, "/api/v1/collections/"+itoa(first.Id)+"/items", sid,
		`{"slug":"`+slug+`"}`), first.Id)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("第一次收藏 = %d，想要 204", rec.Code)
	}
	slugB := submitHTML(t, h, sid, "上限作品 B", "<h1>limit b</h1>")
	publishNow(t, pool, slugB)
	rec = httptest.NewRecorder()
	h.AddCollectionItem(rec, collReq(http.MethodPost, "/api/v1/collections/"+itoa(first.Id)+"/items", sid,
		`{"slug":"`+slugB+`"}`), first.Id)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("超过单夹上限 = %d，想要 422", rec.Code)
	}
}

func itoa(value int) string {
	return strconv.Itoa(value)
}
