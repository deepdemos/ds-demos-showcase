package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/platform/storage"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

// 内容寻址（零拷贝）之后，预览与下载有两条读取路径：
//
//  1. 清单优先——`demo_revision_files` → `blobs.storage_key`；
//  2. 目录回退——没有清单行的旧数据仍按 `demos/{slug}/r{n}/{rel}` 读。
//
// 回退那条已经被既有的预览/下载集成测试覆盖（它们的种子就是铺目录、不写清单）。
// 这里专门钉「清单优先」，并且**故意不放目录文件**——这样一旦回退逻辑抢先命中，
// 用例会以 404 失败，而不是悄悄通过。

func blobKeyForTest(sha string) string { return "blobs/" + sha[:2] + "/" + sha }

func writeBlob(t *testing.T, root, body string) (sha, key string) {
	t.Helper()
	sum := sha256.Sum256([]byte(body))
	sha = hex.EncodeToString(sum[:])
	key = blobKeyForTest(sha)
	full := filepath.Join(root, filepath.FromSlash(key))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("建 blob 目录失败: %v", err)
	}
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		t.Fatalf("写 blob 失败: %v", err)
	}
	return sha, key
}

// seedManifestWorld 造一件「已发布 + 已审 + 有 deep/listed 渠道」的作品，
// 它唯一的内容是一份清单行指向的 blob，磁盘上没有 r1 目录。
func seedManifestWorld(t *testing.T, pool *pgxpool.Pool, root, slug, body string) {
	t.Helper()
	ctx := context.Background()

	for _, q := range []string{
		"DELETE FROM demo_revision_files", "DELETE FROM blobs",
		"DELETE FROM demo_stats", "DELETE FROM demo_channels", "DELETE FROM demo_revisions",
		"DELETE FROM demos", "DELETE FROM web_sessions", "DELETE FROM users", "DELETE FROM accounts",
	} {
		if _, err := pool.Exec(ctx, q); err != nil {
			t.Fatalf("%s 失败: %v", q, err)
		}
	}

	var accountID int64
	if err := pool.QueryRow(ctx,
		`INSERT INTO accounts (sub, status, role) VALUES ($1, 'active', 'user') RETURNING id`,
		"it-manifest-author").Scan(&accountID); err != nil {
		t.Fatalf("建账号失败: %v", err)
	}
	var authorUserID int64
	if err := pool.QueryRow(ctx, `INSERT INTO users (sub) VALUES ($1) RETURNING id`,
		"it-manifest-author").Scan(&authorUserID); err != nil {
		t.Fatalf("建 users 投影失败: %v", err)
	}

	var demoID int64
	var revID int64
	err := pool.QueryRow(ctx, `
		INSERT INTO demos (slug, title, description, moderation_state, lifecycle_state, author_id)
		VALUES ($1, '清单作品', '简介', 'approved', 'published', $2) RETURNING id`,
		slug, authorUserID).Scan(&demoID)
	if err != nil {
		t.Fatalf("建作品失败: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO demo_revisions (demo_id, rev_no, demo_type, source, manifest_hash, file_count)
		VALUES ($1, 1, 'web', 'upload', repeat('a', 64), 1) RETURNING id`,
		demoID).Scan(&revID); err != nil {
		t.Fatalf("建版本失败: %v", err)
	}
	// 两个指针都指向这一版：published_revision_id 决定「对外内容」，
	// current_revision_id 决定「作者视图看哪一版」。
	if _, err := pool.Exec(ctx,
		`UPDATE demos SET current_revision_id = $1, published_revision_id = $1 WHERE id = $2`,
		revID, demoID); err != nil {
		t.Fatalf("设版本指针失败: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO demo_channels (demo_id, channel, visibility, lang)
		VALUES ($1, 'deep', 'listed', 'zh')`, demoID); err != nil {
		t.Fatalf("建渠道失败: %v", err)
	}

	sha, key := writeBlob(t, root, body)
	if _, err := pool.Exec(ctx, `
		INSERT INTO blobs (sha256, size, mime, storage_key, backend)
		VALUES ($1, $2, 'text/html', $3, 'local')`, sha, int64(len(body)), key); err != nil {
		t.Fatalf("登记 blob 失败: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO demo_revision_files (revision_id, path, blob_hash, size, mime)
		VALUES ($1, 'index.html', $2, $3, 'text/html')`, revID, sha, int64(len(body))); err != nil {
		t.Fatalf("写清单失败: %v", err)
	}
}

func TestIntegrationPreviewServesFromBlobManifest(t *testing.T) {
	pool := integrationPool(t)
	root := t.TempDir()
	const body = "<!doctype html><html><head><title>来自 blob</title></head><body>blob</body></html>"
	seedManifestWorld(t, pool, root, "mf-preview", body)

	local, err := storage.NewLocal(root)
	if err != nil {
		t.Fatalf("建本地存储失败: %v", err)
	}
	p := NewPreview(store.New(pool), local, integrationIdentity(t, pool))

	response := previewGet(p, "/preview/mf-preview/v1/index.html", "")
	if response.Code != http.StatusOK {
		t.Fatalf("清单命中时期望 200，实际 %d", response.Code)
	}
	if got := response.Body.String(); !strings.Contains(got, "<title>来自 blob</title>") {
		t.Fatalf("内容不是从 blob 读的: %q", got)
	}

	// 同一作品的目录布局是空的（seedManifestWorld 没写 r1/），
	// 所以上面那条 200 只可能来自清单——回退逻辑没有抢先命中。
	if _, err := local.Stat(context.Background(), storage.FilesKey("mf-preview", 1, "index.html")); err == nil {
		t.Fatal("这条用例不该在 r1 目录里放文件，否则验不出「清单优先」")
	}
}

func TestIntegrationDownloadServesFromBlobManifest(t *testing.T) {
	pool := integrationPool(t)
	root := t.TempDir()
	const body = "PK\x03\x04 blobs 里的源包"
	seedManifestWorld(t, pool, root, "mf-download", body)

	// 把清单里的路径换成「原件」那一行：多文件作品的下载目标是 source.zip。
	ctx := context.Background()
	if _, err := pool.Exec(ctx,
		`UPDATE demo_revision_files SET path = 'source.zip' WHERE path = 'index.html'`); err != nil {
		t.Fatalf("改清单路径失败: %v", err)
	}

	local, err := storage.NewLocal(root)
	if err != nil {
		t.Fatalf("建本地存储失败: %v", err)
	}
	h := NewDownload(store.New(pool), local)

	response := httptest.NewRecorder()
	NewRouter(&stub{}, nil, h).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/download/mf-download", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("清单命中时期望 200，实际 %d", response.Code)
	}
	if got := response.Body.String(); got != body {
		t.Fatalf("下载内容不对: %q", got)
	}
	if cd := response.Header().Get("Content-Disposition"); cd != `attachment; filename="mf-download-v1.zip"` {
		t.Fatalf("下载名不对: %q", cd)
	}
}
