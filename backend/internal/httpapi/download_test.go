package httpapi

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

// 下载端点的纯判定部分：不起库、不起盘，只验「行 → 该不该给、给哪个文件」。
// 真库真磁盘的部分在 download_integration_test.go。

func TestDownloadTarget(t *testing.T) {
	cases := []struct {
		name       string
		slug       string
		rev        int
		singleFile string
		wantName   string
		wantKey    string
	}{
		{
			name:     "多文件作品下原包",
			slug:     "demo-a",
			rev:      3,
			wantName: "demo-a-v3.zip",
			wantKey:  "demos/demo-a/r3/source.zip",
		},
		{
			name:       "单文件 svg 下原文件",
			slug:       "demo-b",
			rev:        1,
			singleFile: "svg",
			wantName:   "demo-b-v1.svg",
			wantKey:    "demos/demo-b/r1/index.svg",
		},
		{
			name:       "单文件 html 下原文件",
			slug:       "demo-c",
			rev:        2,
			singleFile: "html",
			wantName:   "demo-c-v2.html",
			wantKey:    "demos/demo-c/r2/index.html",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			name, key := downloadTarget(c.slug, c.rev, c.singleFile)
			if name != c.wantName {
				t.Fatalf("文件名期望 %q，实际 %q", c.wantName, name)
			}
			if key != c.wantKey {
				t.Fatalf("key 期望 %q，实际 %q", c.wantKey, key)
			}
		})
	}
}

func TestDownloadNilDeps(t *testing.T) {
	// 未接线存储时所有下载返回 404，而不是 500 或 panic。
	response := httptest.NewRecorder()
	NewDownload(nil, nil).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/download/whatever", nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("期望 404，实际 %d", response.Code)
	}
}

func TestDownloadRejectsNonGET(t *testing.T) {
	response := httptest.NewRecorder()
	NewDownload(nil, nil).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/download/whatever", nil))
	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("期望 405，实际 %d", response.Code)
	}
	if allow := response.Header().Get("Allow"); allow != "GET, HEAD" {
		t.Fatalf("Allow 头不对: %q", allow)
	}
}

// TestDownloadVisibilityJudge 只验「不该给的都不给」，不验字节。
// 每条分支一个用例，因为这里放松一条就等于把 hidden 作品变成公开可下。
func TestDownloadVisibilityJudge(t *testing.T) {
	cases := []struct {
		name        string
		lifecycle   string
		moderation  string
		demoType    string // 空串按 zip 算
		hidden      bool   // 渠道不可见
		revErased   bool
		demoErased  bool
		demoDeleted bool
		wantCode    int
	}{
		{name: "正常可见", lifecycle: "published", moderation: "approved", wantCode: http.StatusOK},
		{name: "没发布", lifecycle: "draft", moderation: "approved", wantCode: http.StatusNotFound},
		{name: "发布但没审过", lifecycle: "published", moderation: "rejected", wantCode: http.StatusNotFound},
		{name: "渠道不可见", lifecycle: "published", moderation: "approved", hidden: true, wantCode: http.StatusNotFound},
		{name: "作品被擦除", lifecycle: "published", moderation: "approved", demoErased: true, wantCode: http.StatusNotFound},
		{name: "作品被软删", lifecycle: "published", moderation: "approved", demoDeleted: true, wantCode: http.StatusNotFound},
		{name: "当前版本被擦除", lifecycle: "published", moderation: "approved", revErased: true, wantCode: http.StatusNotFound},
		// 外链作品没有本站文件：「不给」比「给一个假包」重要，所以是 400 而不是 404
		// （404 会被理解成「这个作品不存在」，而它存在，只是内容在别处）。
		{name: "外链作品不给假包", lifecycle: "published", moderation: "approved", demoType: "link", wantCode: http.StatusBadRequest},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			row := downloadRow(c.lifecycle, c.moderation, c.demoType, c.hidden, c.revErased, c.demoErased, c.demoDeleted)
			response := httptest.NewRecorder()
			serveWithRow(t, row, response)
			if response.Code != c.wantCode {
				t.Fatalf("期望 %d，实际 %d", c.wantCode, response.Code)
			}
		})
	}
}

// downloadRow 造一行下载查询结果。配合 memStore 使用：
// 判定放行 → 200；判定拒绝 → 404。两者能区分开。
func downloadRow(lifecycle, moderation, demoType string, hidden, revErased, demoErased, demoDeleted bool) store.GetDemoForDownloadRow {
	if demoType == "" {
		demoType = "zip"
	}
	return store.GetDemoForDownloadRow{
		ID:                1,
		Slug:              "demo-slug",
		LifecycleState:    lifecycle,
		ModerationState:   moderation,
		CurrentRevNo:      1,
		DemoType:          demoType,
		HasVisibleChannel: !hidden,
		RevisionErasedAt:  nullableTS(revErased),
		ErasedAt:          nullableTS(demoErased),
		DeletedAt:         nullableTS(demoDeleted),
	}
}

// serveWithRow 用一行假数据驱动整个处理器，存储换成「所有 key 都存在」的内存实现。
//
// 换成有内容的存储是为了让「判定放行」真的走到 200：
// files 传 nil 的话，放行与拒绝都表现为 404，那就等于没测到判定。
func serveWithRow(t *testing.T, row store.GetDemoForDownloadRow, w *httptest.ResponseRecorder) {
	t.Helper()
	h := NewDownload(store.New(fakeDB{row: fakeRow{values: downloadValues(row)}}), memStore{})
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/download/demo-slug", nil))
}

// memStore 是「任何 key 都读得到一段固定字节」的存储，只给判定用例用。
type memStore struct{}

func (memStore) Put(context.Context, string, io.Reader, int64, string) error { return nil }
func (memStore) Delete(context.Context, string) error                        { return nil }
func (memStore) Stat(context.Context, string) (int64, error)                 { return 6, nil }
func (memStore) URL(string) (string, bool)                                   { return "", false }
func (memStore) Get(context.Context, string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("bytes!")), nil
}

func downloadValues(row store.GetDemoForDownloadRow) []any {
	return []any{
		row.ID, row.Slug, row.LifecycleState, row.ModerationState,
		row.DeletedAt, row.ErasedAt, row.CurrentRevNo,
		row.SingleFile, row.DemoType,
		row.RevisionDeletedAt, row.RevisionErasedAt, row.HasVisibleChannel,
	}
}

// nullableTS 造「有值/无值」的 timestamptz，给上面那张用例表用。
func nullableTS(valid bool) pgtype.Timestamptz {
	return pgtype.Timestamptz{Valid: valid}
}
