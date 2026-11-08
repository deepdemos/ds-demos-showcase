package httpapi

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/api"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/demo"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/identity"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/platform/storage"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

// # 投稿形态 × 读路径 的形态矩阵
//
// 这里存在的原因是一次真实事故：单文件 SVG 投稿成功，但预览页 404——预览把入口
// 硬编码成 index.html，而这类作品的入口叫 index.svg。当时所有门禁都绿：
// 预览不在契约里（数不到）、反例矩阵里它只有一条可见性用例（不测形态）、
// 而预览的 fixture 里每条作品的入口都是 index.html（形态只有一个取值）。
//
// 所以这张表的意义不是「多测几条」，而是**把形态维度变成显式的一维**：
// 数据源是 `demo.SubmitShapes()`，而它读的是 `singleFileKinds`——投稿校验也读同一份。
// 新增一种文件格式只改那一处，这张表就会自动多一行，于是「新格式的预览能不能打开」
// 这件事不可能没人测过。

// newShapeHandler 返回装配好的 handler 与挂在**同一份存储**上的预览服务。
//
// 共用存储是必须的：预览读的是投稿写进去的那批字节，两边各自 t.TempDir()
// 的话矩阵会全部 404——而且会红在「文件不存在」这种与形态无关的地方上。
func newShapeHandler(t *testing.T) (*Handler, *Preview, *identity.Service, *pgxpool.Pool) {
	t.Helper()
	pool := integrationPool(t)
	q := store.New(pool)
	idSvc := integrationIdentity(t, pool)
	files, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("初始化存储失败: %v", err)
	}
	demoSvc := demo.New(pool, q, files, storage.DefaultLimits(), 200<<20, 0, nil)
	return New(pool, q, idSvc, nil, demoSvc), NewPreview(q, files, idSvc), idSvc, pool
}

// submitShape 按给定形态投稿，返回 slug。
func submitShape(t *testing.T, h *Handler, sid, title, fileName string, content []byte) string {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for k, v := range map[string]string{"title": title, "description": "简介", "demoType": "web"} {
		if err := mw.WriteField(k, v); err != nil {
			t.Fatalf("写字段 %s 失败: %v", k, err)
		}
	}
	part, err := mw.CreateFormFile("package", fileName)
	if err != nil {
		t.Fatalf("建文件字段失败: %v", err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatalf("写文件内容失败: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("收尾 multipart 失败: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/demos", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: sid})
	rec := httptest.NewRecorder()
	h.SubmitDemo(rec, req, api.SubmitDemoParams{})
	if rec.Code != http.StatusCreated {
		t.Fatalf("投稿（%s）= %d，想要 201；响应体：%s", fileName, rec.Code, rec.Body.String())
	}
	var out api.SubmissionResult
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("解析投稿回执失败: %v", err)
	}
	return out.Slug
}

// shapeContent 造出这种形态的字节内容。
func shapeContent(t *testing.T, shape demo.SubmitShape) []byte {
	t.Helper()
	switch shape.SingleFile {
	case "html":
		return []byte("<!doctype html><html><head><title>形态矩阵</title></head><body>hi</body></html>")
	case "svg":
		return []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="8" height="8"><rect width="8" height="8"/></svg>`)
	case "":
		// zip 包：根目录一个入口 + 一个静态资源，确认多文件也能打开。
		var buf bytes.Buffer
		zw := zip.NewWriter(&buf)
		entry, err := zw.Create("index.html")
		if err != nil {
			t.Fatalf("建 zip 条目失败: %v", err)
		}
		if _, err := entry.Write([]byte("<!doctype html><html><head><title>zip 形态</title></head><body>hi</body></html>")); err != nil {
			t.Fatalf("写 zip 条目失败: %v", err)
		}
		css, err := zw.Create("style.css")
		if err != nil {
			t.Fatalf("建 zip 条目失败: %v", err)
		}
		if _, err := css.Write([]byte("body{margin:0}")); err != nil {
			t.Fatalf("写 zip 条目失败: %v", err)
		}
		if err := zw.Close(); err != nil {
			t.Fatalf("收尾 zip 失败: %v", err)
		}
		return buf.Bytes()
	default:
		t.Fatalf("未知形态 %q：新增格式时请在这里补上它的字节内容", shape.Kind)
		return nil
	}
}

// wantContentType 是这种形态落库后入口文件应有的 Content-Type。
func wantContentType(shape demo.SubmitShape) string {
	if shape.SingleFile == "svg" {
		return "image/svg+xml"
	}
	return "text/html"
}

// TestIntegrationSubmitShapeMatrix 对**每一种投稿形态**逐一打四条读路径。
//
// 矩阵的每一格都必须有值：某一形态读不出来，就是「投稿成功但打不开」，
// 而这正是它要钉的东西。
func TestIntegrationSubmitShapeMatrix(t *testing.T) {
	shapes := demo.SubmitShapes()
	if len(shapes) < 4 {
		t.Fatalf("投稿形态只有 %d 种（%v），zip + html + htm + svg 至少四种", len(shapes), shapes)
	}

	h, p, idSvc, pool := newShapeHandler(t)
	acc, sid := registerVerified(t, pool, idSvc, identity.RegisterInput{
		Email: "shape-matrix@example.com", Password: "shape-matrix-password",
		Username: "shape-matrix", DisplayName: "形态矩阵",
	})
	t.Cleanup(func() { cleanupAuthor(t, pool, acc.Sub) })

	for _, shape := range shapes {
		t.Run(shape.Kind, func(t *testing.T) {
			title := "形态-" + shape.Kind
			slug := submitShape(t, h, sid, title, shape.FileName, shapeContent(t, shape))
			t.Cleanup(func() {
				ctx := context.Background()
				for _, q := range []string{
					`DELETE FROM demo_channels WHERE demo_id = (SELECT id FROM demos WHERE slug = $1)`,
					`DELETE FROM demo_revisions WHERE demo_id = (SELECT id FROM demos WHERE slug = $1)`,
					`DELETE FROM demos WHERE slug = $1`,
				} {
					if _, err := pool.Exec(ctx, q, slug); err != nil {
						t.Logf("清理 %s 失败: %v", slug, err)
					}
				}
			})

			// 1) 详情：作者看得到自己的草稿。
			if rec := getDemoAs(t, h, slug, sid); rec.Code != http.StatusOK {
				t.Fatalf("详情 = %d，想要 200；body=%s", rec.Code, rec.Body.String())
			}

			// 2) 预览（不带文件名）：302 到这一版的入口，跟过去是 200。
			first := previewGet(p, "/preview/"+slug, sid)
			if first.Code != http.StatusFound {
				t.Fatalf("预览（不带文件名）= %d，想要 302；body=%q", first.Code, first.Body.String())
			}
			loc := first.Header().Get("Location")
			wantSuffix := fmt.Sprintf("/v1/%s", shape.EntryPath)
			if !strings.HasSuffix(loc, wantSuffix) {
				t.Fatalf("跳转目标 = %q，想要以 %q 结尾（入口应取 entry_path）", loc, wantSuffix)
			}
			served := previewGet(p, loc, sid)
			if served.Code != http.StatusOK {
				t.Fatalf("跟随后 = %d，想要 200；body=%q", served.Code, served.Body.String())
			}
			if ct := served.Header().Get("Content-Type"); !strings.HasPrefix(ct, wantContentType(shape)) {
				t.Fatalf("Content-Type = %q，想要 %s", ct, wantContentType(shape))
			}

			// 3) 预览（带版本号、不带文件名）：同样跳过去。
			if rec := previewGet(p, fmt.Sprintf("/preview/%s/v1/", slug), sid); rec.Code != http.StatusFound {
				t.Fatalf("预览（带版本号）= %d，想要 302", rec.Code)
			}

			// 4) 预览（带版本号 + 文件名）：这一格是最终落点，必须 200。
			direct := fmt.Sprintf("/preview/%s/v1/%s", slug, shape.EntryPath)
			rec := previewGet(p, direct, sid)
			if rec.Code != http.StatusOK {
				t.Fatalf("%s = %d，想要 200；body=%q", direct, rec.Code, rec.Body.String())
			}
			if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, wantContentType(shape)) {
				t.Fatalf("%s 的 Content-Type = %q，想要 %s", direct, ct, wantContentType(shape))
			}
		})
	}
}

// 不带文件名的预览一律 302，并且优先跳「当前版本」（作者视角）而不是对外版本。
func TestIntegrationPreviewEntryRedirect(t *testing.T) {
	pool := integrationPool(t)
	dir := t.TempDir()
	seedPreviewWorld(t, pool, dir)
	p := newPreview(t, pool, dir)

	// 作者看自己的草稿：跳到当前版本 r1 的真实入口。
	rec := previewGet(p, "/preview/pv-draft", pvAuthorSID)
	if rec.Code != http.StatusFound {
		t.Fatalf("状态码 = %d，想要 302", rec.Code)
	}
	if loc, want := rec.Header().Get("Location"), "/preview/pv-draft/v1/index.html"; loc != want {
		t.Fatalf("Location = %q，想要 %q", loc, want)
	}
	// 跳转目标随改稿变化（当前版本会变），所以不能给长缓存。
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("Cache-Control = %q，想要 no-store", cc)
	}

	// 带版本号 + 不带文件名：跳到那一版自己的入口。
	rec = previewGet(p, "/preview/pv-draft/v1/", pvAuthorSID)
	if loc, want := rec.Header().Get("Location"), "/preview/pv-draft/v1/index.html"; loc != want {
		t.Fatalf("带版本号的 Location = %q，想要 %q", loc, want)
	}

	// 匿名看草稿：跳转之前就该 404——「入口修好」不等于「草稿公开」。
	if rec := previewGet(p, "/preview/pv-draft", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("匿名看草稿 = %d，想要 404", rec.Code)
	}
}
