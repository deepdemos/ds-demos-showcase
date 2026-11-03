package demo

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/platform/storage"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

// newServiceWithPending 与 testService 相同，除了显式指定待审配额。
func newServiceWithPending(t *testing.T, pool *pgxpool.Pool, maxPending int) *Service {
	t.Helper()
	files, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("建本地存储失败: %v", err)
	}
	return New(pool, store.New(pool), files, storage.DefaultLimits(), 200<<20, maxPending, nil)
}

// buildZipLevel 与 buildZip 相同，除了可指定压缩方式（用来证明 manifest 与打包方式无关）。
func buildZipLevel(t *testing.T, files map[string]string, method uint16) string {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: method})
		if err != nil {
			t.Fatalf("造包失败: %v", err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatalf("造包失败: %v", err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("造包失败: %v", err)
	}
	path := filepath.Join(t.TempDir(), "demo.zip")
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatalf("写包失败: %v", err)
	}
	return path
}

// fileHash 是 zip 文件本身的 sha256，用来对照 manifest_hash。
func fileHash(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读包失败: %v", err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
