package httpapi

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/demo"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/identity"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/platform/storage"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

// 单文件作品（.html / .svg）必须能真的提交成功。
//
// 这条测试存在的理由：落盘走的是 `os.CreateTemp("", "demo-upload-*")`，**临时文件没有
// 扩展名**。类型判断一旦读的是落盘路径而不是客户端文件名，`filepath.Ext` 恒为空串，
// 所有单文件投稿都会 415「不支持的作品包类型」——而 zip 走魔数嗅探不受影响，所以
// 只传 zip 的用例一路绿。用户是点着页面把它点出来的。
func TestIntegrationSubmitSingleFilePackage(t *testing.T) {
	pool := integrationPool(t)
	q := store.New(pool)
	idSvc := integrationIdentity(t, pool)

	files, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("初始化存储失败: %v", err)
	}
	demoSvc := demo.New(pool, q, files, storage.DefaultLimits(), 200<<20, 0, nil)
	h := New(pool, q, idSvc, nil, demoSvc)

	acc, sid, err := idSvc.Register(context.Background(), identity.RegisterInput{
		Email:       "it-submit-single@example.com",
		Password:    "submittest123",
		Username:    "it-submit-single",
		DisplayName: "单文件作者",
	})
	if err != nil {
		t.Fatalf("注册失败: %v", err)
	}
	t.Cleanup(func() {
		ctx := context.Background()
		// 顺序受外键约束：作品先删，账号后删。
		_, _ = pool.Exec(ctx, `DELETE FROM demos WHERE author_sub = $1`, acc.Sub)
		_, _ = pool.Exec(ctx, `DELETE FROM users WHERE sub = $1`, acc.Sub)
		_, _ = pool.Exec(ctx, `DELETE FROM accounts WHERE sub = $1`, acc.Sub)
	})

	cases := []struct {
		name     string
		filename string
		body     string
	}{
		{"svg", "作品.svg", `<svg xmlns="http://www.w3.org/2000/svg" width="4" height="4"/>`},
		{"html", "作品.html", `<!doctype html><p>hi</p>`},
		{"htm", "作品.htm", `<!doctype html><p>hi</p>`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			mw := multipart.NewWriter(&buf)
			if err := mw.WriteField("title", "单文件投稿 "+tc.name); err != nil {
				t.Fatal(err)
			}
			if err := mw.WriteField("demoType", "web"); err != nil {
				t.Fatal(err)
			}
			part, err := mw.CreateFormFile("package", tc.filename)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := part.Write([]byte(tc.body)); err != nil {
				t.Fatal(err)
			}
			if err := mw.Close(); err != nil {
				t.Fatal(err)
			}

			req := httptest.NewRequest(http.MethodPost, "/api/v1/demos", &buf)
			req.Header.Set("Content-Type", mw.FormDataContentType())
			req.AddCookie(&http.Cookie{Name: sessionCookie, Value: sid})

			rec := httptest.NewRecorder()
			h.SubmitDemo(rec, req)

			if rec.Code != http.StatusCreated {
				t.Fatalf("状态码 = %d，想要 201；响应体：%s", rec.Code, rec.Body.String())
			}
		})
	}
}
