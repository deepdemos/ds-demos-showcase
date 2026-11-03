package storage

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTestLocal(t *testing.T) *Local {
	t.Helper()
	l, err := NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocal: %v", err)
	}
	return l
}

func TestLocalPutGetStatDelete(t *testing.T) {
	ctx := context.Background()
	l := newTestLocal(t)
	key := FilesKey("demo-a", 1, "index.html")
	body := []byte("<html>hi</html>")

	if err := l.Put(ctx, key, bytes.NewReader(body), int64(len(body)), "text/html"); err != nil {
		t.Fatalf("Put: %v", err)
	}

	size, err := l.Stat(ctx, key)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if size != int64(len(body)) {
		t.Fatalf("Stat size = %d, want %d", size, len(body))
	}

	rc, err := l.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	got, err := io.ReadAll(rc)
	_ = rc.Close()
	if err != nil {
		t.Fatalf("读取: %v", err)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("内容 = %q, want %q", got, body)
	}

	if err := l.Delete(ctx, key); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := l.Stat(ctx, key); !errors.Is(err, ErrNotFound) {
		t.Fatalf("删除后 Stat 应为 ErrNotFound，实际 %v", err)
	}
	// 幂等：再删一次不该报错。
	if err := l.Delete(ctx, key); err != nil {
		t.Fatalf("重复 Delete 应幂等，实际 %v", err)
	}
}

func TestLocalGetMissingIsErrNotFound(t *testing.T) {
	l := newTestLocal(t)
	if _, err := l.Get(context.Background(), FilesKey("nope", 1, "index.html")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("应返回 ErrNotFound，实际 %v", err)
	}
}

// key 由本仓库代码生成，正常不会命中；但这是「上游被改动」时的最后一道闸。
func TestLocalRejectsUnsafeKeys(t *testing.T) {
	ctx := context.Background()
	l := newTestLocal(t)
	bad := []string{
		"",
		"../escape.txt",
		"a/../../escape.txt",
		"/abs/path.txt",
		"C:/windows/x.txt",
		"a//b.txt",
		"a/./b.txt",
		"a\\b.txt",
		"demos/abc/r1/\x00.txt",
	}
	for _, key := range bad {
		t.Run(key, func(t *testing.T) {
			if err := l.Put(ctx, key, strings.NewReader("x"), 1, ""); !errors.Is(err, ErrUnsafeKey) {
				t.Fatalf("Put(%q) 应返回 ErrUnsafeKey，实际 %v", key, err)
			}
		})
	}
}

// Put 声明 size 之后真实写入超过它，必须拒绝——否则调用方算错上限会写爆磁盘。
func TestLocalPutRejectsOverrun(t *testing.T) {
	l := newTestLocal(t)
	err := l.Put(context.Background(), FilesKey("a", 1, "x.bin"), strings.NewReader("0123456789"), 5, "")
	if err == nil {
		t.Fatal("写入超过声明大小时应报错")
	}
	// 失败后不该留下半个文件。
	if _, statErr := l.Stat(context.Background(), FilesKey("a", 1, "x.bin")); !errors.Is(statErr, ErrNotFound) {
		t.Fatalf("失败后目标文件不应存在，实际 %v", statErr)
	}
}

func TestLocalURLNeverDirect(t *testing.T) {
	l := newTestLocal(t)
	if _, ok := l.URL(FilesKey("a", 1, "index.html")); ok {
		t.Fatal("本地存储不该给出直链")
	}
}

func TestLocalPutIsAtomicOverwrite(t *testing.T) {
	ctx := context.Background()
	l := newTestLocal(t)
	key := FilesKey("a", 1, "index.html")

	if err := l.Put(ctx, key, strings.NewReader("first"), 5, ""); err != nil {
		t.Fatalf("首次 Put: %v", err)
	}
	if err := l.Put(ctx, key, strings.NewReader("second"), 6, ""); err != nil {
		t.Fatalf("覆盖 Put: %v", err)
	}
	rc, err := l.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	got, _ := io.ReadAll(rc)
	_ = rc.Close()
	if string(got) != "second" {
		t.Fatalf("覆盖后内容 = %q，应为 second", got)
	}
}

func TestNewLocalRejectsEmptyRoot(t *testing.T) {
	if _, err := NewLocal("  "); err == nil {
		t.Fatal("空根目录应报错")
	}
}

func TestLocalRootIsAbsolute(t *testing.T) {
	l := newTestLocal(t)
	if !filepath.IsAbs(l.Root()) {
		t.Fatalf("Root() = %q，应为绝对路径", l.Root())
	}
	if _, err := os.Stat(l.Root()); err != nil {
		t.Fatalf("Root() 指向的目录应存在: %v", err)
	}
}
