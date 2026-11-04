package demo

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/platform/storage"
)

// 单文件（html / svg）的类型只能从**客户端文件名**判断，不能从落盘路径判断。
//
// 线上落盘走 os.CreateTemp("", "demo-upload-*")，路径长这样：
//
//	C:\Users\…\Temp\demo-upload-3141592653
//
// 没有扩展名。曾经这里读的是 PackagePath，于是 filepath.Ext 恒为空串，
// 所有单文件投稿都撞到 default 分支返回「不支持的作品包类型」（HTTP 415）——
// 而 zip 走的是魔数嗅探，不受影响，所以集成测试一路绿。
func TestStageSingleFileTakesExtensionFromPackageName(t *testing.T) {
	dir := t.TempDir()
	pkgPath := filepath.Join(dir, "demo-upload-3141592653") // 故意不带扩展名
	if err := os.WriteFile(pkgPath, []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`), 0o600); err != nil {
		t.Fatal(err)
	}

	// 单文件分支不碰数据库与对象存储，传 nil 是安全的；只有 zip 分支会用到 limits。
	s := New(nil, nil, nil, storage.Limits{}, 1<<20, 0, slog.New(slog.NewTextHandler(io.Discard, nil)))

	st, cleanup, err := s.stage(context.Background(), SubmitInput{
		PackagePath: pkgPath,
		PackageName: "作品.svg",
	})
	defer cleanup()
	if err != nil {
		t.Fatalf("stage 返回错误：%v（落盘路径没有扩展名不该影响类型判断）", err)
	}
	if st.singleFile != "svg" {
		t.Fatalf("singleFile = %q，想要 svg", st.singleFile)
	}
	if st.entryPath != "index.svg" {
		t.Fatalf("entryPath = %q，想要 index.svg", st.entryPath)
	}
	if len(st.files) != 1 || st.files[0] != "index.svg" {
		t.Fatalf("files = %v，想要 [index.svg]", st.files)
	}
}

// 拿不到扩展名时仍要拒绝，且是 ErrUnsupportedPackage（不是 500）。
func TestStageRejectsUnknownExtension(t *testing.T) {
	dir := t.TempDir()
	pkgPath := filepath.Join(dir, "demo-upload-2718281828")
	if err := os.WriteFile(pkgPath, []byte("#!/bin/sh\necho hi\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	s := New(nil, nil, nil, storage.Limits{}, 1<<20, 0, slog.New(slog.NewTextHandler(io.Discard, nil)))

	_, cleanup, err := s.stage(context.Background(), SubmitInput{
		PackagePath: pkgPath,
		PackageName: "run.sh",
	})
	defer cleanup()
	if !errors.Is(err, ErrUnsupportedPackage) {
		t.Fatalf("err = %v，想要 ErrUnsupportedPackage", err)
	}
}

// .htm 统一重命名成 .html，避免预览时 MIME 判断分叉。
func TestStageNormalizesHtmToHTML(t *testing.T) {
	dir := t.TempDir()
	pkgPath := filepath.Join(dir, "demo-upload-1618033988")
	if err := os.WriteFile(pkgPath, []byte("<!doctype html><p>hi</p>"), 0o600); err != nil {
		t.Fatal(err)
	}

	s := New(nil, nil, nil, storage.Limits{}, 1<<20, 0, slog.New(slog.NewTextHandler(io.Discard, nil)))

	st, cleanup, err := s.stage(context.Background(), SubmitInput{
		PackagePath: pkgPath,
		PackageName: "page.htm",
	})
	defer cleanup()
	if err != nil {
		t.Fatalf("stage 返回错误：%v", err)
	}
	if st.singleFile != "html" || st.entryPath != "index.html" {
		t.Fatalf("singleFile=%q entryPath=%q，想要 html / index.html", st.singleFile, st.entryPath)
	}
}
