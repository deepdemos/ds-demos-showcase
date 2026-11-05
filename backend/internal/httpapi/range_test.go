package httpapi

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

// 断点续传的解析是这个端点里唯一「错了不会报错、只会给错字节」的地方，
// 所以每种写法都钉一条。
func TestParseByteRange(t *testing.T) {
	cases := []struct {
		name      string
		spec      string
		size      int64
		wantStart int64
		wantEnd   int64
		wantErr   bool
		unsat     bool
	}{
		{name: "前 10 个字节", spec: "bytes=0-9", size: 100, wantStart: 0, wantEnd: 9},
		{name: "从 90 到末尾", spec: "bytes=90-", size: 100, wantStart: 90, wantEnd: 99},
		{name: "只给起点", spec: "bytes=50-", size: 100, wantStart: 50, wantEnd: 99},
		{name: "终点超过文件就截到最后", spec: "bytes=90-10000", size: 100, wantStart: 90, wantEnd: 99},
		{name: "后缀写法取最后 10 个", spec: "bytes=-10", size: 100, wantStart: 90, wantEnd: 99},
		{name: "后缀比文件还大就取全文件", spec: "bytes=-1000", size: 100, wantStart: 0, wantEnd: 99},
		{name: "起点超出文件是 416", spec: "bytes=100-", size: 100, wantErr: true, unsat: true},
		{name: "终点小于起点算语法错", spec: "bytes=50-10", size: 100, wantErr: true},
		{name: "多区间不支持", spec: "bytes=0-9,20-29", size: 100, wantErr: true},
		{name: "单位不是 bytes", spec: "items=0-9", size: 100, wantErr: true},
		{name: "没有分隔符", spec: "bytes=abc", size: 100, wantErr: true},
		{name: "起止都空", spec: "bytes=-", size: 100, wantErr: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := parseByteRange(c.spec, c.size)
			if c.wantErr {
				if err == nil {
					t.Fatalf("期望报错，实际拿到 %+v", got)
				}
				if c.unsat != errors.Is(err, errRangeUnsatisfiable) {
					t.Fatalf("期望 unsatisfiable=%v，实际 err=%v", c.unsat, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("不该报错: %v", err)
			}
			if got.start != c.wantStart || got.end != c.wantEnd {
				t.Fatalf("期望 [%d,%d]，实际 [%d,%d]", c.wantStart, c.wantEnd, got.start, got.end)
			}
		})
	}
}

// 带 Range 的下载必须回 206 + Content-Range，且字节就是那一段。
//
// 这一条用的是「任何 key 都读得到固定内容」的内存存储：字节内容是已知的，
// 所以能逐字比对「取的是不是那一段」，而不是只看状态码。
func TestDownloadServesByteRange(t *testing.T) {
	const body = "0123456789abcdef"

	h := newRangeDownload(t, body)

	req := httptest.NewRequest(http.MethodGet, "/download/demo-slug", nil)
	req.Header.Set("Range", "bytes=4-7")
	rec := httptest.NewRecorder()
	serveRangeDownload(h, rec, req)

	if rec.Code != http.StatusPartialContent {
		t.Fatalf("期望 206，实际 %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Range"); got != "bytes 4-7/16" {
		t.Fatalf("Content-Range 不对: %q", got)
	}
	if got := rec.Header().Get("Content-Length"); got != "4" {
		t.Fatalf("Content-Length 应是 4，实际 %q", got)
	}
	if rec.Body.String() != "4567" {
		t.Fatalf("区间内容不对: %q", rec.Body.String())
	}
}

// 没带 Range 也要声明 `Accept-Ranges: bytes`——浏览器正是看到它才敢续传。
func TestDownloadAdvertisesRanges(t *testing.T) {
	h := newRangeDownload(t, "0123456789abcdef")
	rec := httptest.NewRecorder()
	serveRangeDownload(h, rec, httptest.NewRequest(http.MethodGet, "/download/demo-slug", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d", rec.Code)
	}
	if got := rec.Header().Get("Accept-Ranges"); got != "bytes" {
		t.Fatalf("Accept-Ranges 不对: %q", got)
	}
	if rec.Body.String() != "0123456789abcdef" {
		t.Fatalf("整份内容不对: %q", rec.Body.String())
	}
}

// 起点超出文件要 416 + `bytes */total`，而不是悄悄回一个空 200。
// 后者会让浏览器以为「续传成功、文件就这么多」，得到一个截断的包。
func TestDownloadRangeOutOfBoundsIs416(t *testing.T) {
	h := newRangeDownload(t, "0123456789abcdef")
	req := httptest.NewRequest(http.MethodGet, "/download/demo-slug", nil)
	req.Header.Set("Range", "bytes=99-")
	rec := httptest.NewRecorder()
	serveRangeDownload(h, rec, req)

	if rec.Code != http.StatusRequestedRangeNotSatisfiable {
		t.Fatalf("期望 416，实际 %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Range"); got != "bytes */16" {
		t.Fatalf("416 必须带真实总长，实际 %q", got)
	}
}

// 语法错的 Range 按 RFC 忽略：整份发回去，而不是 400。
// 400 会让一个只是格式怪一点的客户端彻底下不动。
func TestDownloadIgnoresMalformedRange(t *testing.T) {
	h := newRangeDownload(t, "0123456789abcdef")
	req := httptest.NewRequest(http.MethodGet, "/download/demo-slug", nil)
	req.Header.Set("Range", "bytes=garbage")
	rec := httptest.NewRecorder()
	serveRangeDownload(h, rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("期望退化成 200，实际 %d", rec.Code)
	}
	if rec.Body.String() != "0123456789abcdef" {
		t.Fatalf("整份内容不对: %q", rec.Body.String())
	}
}

// ---- 辅助：一份长度已知的存储 + 一个直接打到下载处理器的路由 ----

// fixedStore 返回固定的一段字节，用来逐字比对「取的是不是那一段」。
//
// 注意它返回的是 `*bytes.Reader`（可 Seek），所以走的是 handler 里的
// `io.ReadSeeker` 快路——与本地磁盘实现同一条路径。
type fixedStore struct{ body []byte }

func (s fixedStore) Put(context.Context, string, io.Reader, int64, string) error { return nil }
func (s fixedStore) Delete(context.Context, string) error                        { return nil }
func (s fixedStore) Stat(context.Context, string) (int64, error) {
	return int64(len(s.body)), nil
}
func (s fixedStore) URL(string) (string, bool) { return "", false }
func (s fixedStore) Get(context.Context, string) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(s.body)), nil
}

func newRangeDownload(t *testing.T, body string) *Download {
	t.Helper()
	row := downloadRow("published", "approved", "zip", false, false, false, false)
	return NewDownload(store.New(fakeDB{row: fakeRow{values: downloadValues(row)}}), fixedStore{body: []byte(body)})
}

// serveRangeDownload 走真实路由：`chi.URLParam` 依赖路由上下文，直接调处理器取不到 slug。
func serveRangeDownload(h *Download, rec *httptest.ResponseRecorder, req *http.Request) {
	NewRouter(&stub{}, nil, h).ServeHTTP(rec, req)
}
