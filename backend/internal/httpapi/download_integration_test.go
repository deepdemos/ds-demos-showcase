package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/platform/storage"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

// 下载是「错了会静默多给或少给」的那一类：放得太开等于 hidden 作品谁都能下，
// 收得太紧等于正常作品下不了。所以每条分支都钉一个用例。
//
// 这里不起真库的那一半（纯判定）在 download_test.go 里；
// 本文件覆盖「真库 + 真磁盘」的路径，包括计数是否真的落库。

// dlWorld 描述一次下载测试需要的一个作品。
type dlWorld struct {
	slug       string
	demoType   string
	singleFile string // 非空表示单文件作品
	lifecycle  string
	moderation string
	visibility string // 渠道可见性；空串表示不建渠道行
	withSource bool   // 是否在磁盘上放 source.zip
	// hasPublished：这行是否**有过**对外版本（迁移 0014 的 published_revision_id）。
	// 与 visibility 是两件事，显式写出来让每条数据自己说清它代表什么状态。
	hasPublished bool
}

func seedDownloadWorld(t *testing.T, pool *pgxpool.Pool, dir string) map[string]dlWorld {
	t.Helper()
	ctx := context.Background()

	for _, q := range []string{
		"DELETE FROM demo_stats", "DELETE FROM demo_channels", "DELETE FROM demo_revisions", "DELETE FROM demos",
		"DELETE FROM web_sessions", "DELETE FROM users", "DELETE FROM accounts",
	} {
		if _, err := pool.Exec(ctx, q); err != nil {
			t.Fatalf("%s 失败: %v", q, err)
		}
	}

	mkAccount(t, pool, dlAuthorSub, dlAuthorSID, "it-download-author", "user")

	var authorUserID int64
	if err := pool.QueryRow(ctx, `SELECT id FROM users WHERE sub = $1`, dlAuthorSub).Scan(&authorUserID); err != nil {
		t.Fatalf("取作者 users.id 失败: %v", err)
	}

	specs := []dlWorld{
		// 正常可见的多文件作品：能下，下的是 source.zip。
		{slug: "dl-zip", demoType: "zip", lifecycle: "published", moderation: "approved", visibility: "listed", withSource: true, hasPublished: true},
		// unlisted：不上列表，但知道地址就能下（直链档位）。
		{slug: "dl-unlisted", demoType: "zip", lifecycle: "published", moderation: "approved", visibility: "unlisted", withSource: true, hasPublished: true},
		// hidden：对外不可见，必须 404。
		{slug: "dl-hidden", demoType: "zip", lifecycle: "published", moderation: "approved", visibility: "hidden", withSource: true, hasPublished: true},
		// 没发布：404。
		//
		// 这两条**故意不建渠道行**：触发器 `trg_demos_guard` 拒绝给未审过的作品上架渠道
		// （「作品 65 的审核状态是 pending，不能上架渠道 deep」），所以
		// 「pending/rejected + listed 渠道」这个组合在库里根本造不出来。
		// 判定要写对（单测里用假行覆盖了），但集成测试只能造可达状态。
		{slug: "dl-draft", demoType: "zip", lifecycle: "draft", moderation: "pending", withSource: true},
		// 发布但没审过：404（组合不可达，同理见上）。
		{slug: "dl-rejected", demoType: "zip", lifecycle: "published", moderation: "rejected", withSource: true},
		// 单文件作品：下的是 index.svg 而不是 source.zip。
		{slug: "dl-single", demoType: "web", singleFile: "svg", lifecycle: "published", moderation: "approved", visibility: "listed", hasPublished: true},
		// 外链作品：400，不给假的本站包。
		{slug: "dl-link", demoType: "link", lifecycle: "published", moderation: "approved", visibility: "listed", hasPublished: true},
		// 已归档：墓碑态不提供下载。
		{slug: "dl-archived", demoType: "zip", lifecycle: "archived", moderation: "approved", visibility: "listed", withSource: true, hasPublished: true},
		// 复审中：审核态被退回 pending，但对外版本仍是上一版——作者改稿期间
		// 线上作品必须还能下载，否则「审核」就变成了「下线」。
		{slug: "dl-under-review", demoType: "zip", lifecycle: "published", moderation: "pending", visibility: "listed", withSource: true, hasPublished: true},
	}

	world := map[string]dlWorld{}
	for _, s := range specs {
		var demoID int64
		err := pool.QueryRow(ctx, `
			INSERT INTO demos (slug, title, description, moderation_state, lifecycle_state, author_id)
			VALUES ($1, $2, '简介', $3, $4, $5)
			RETURNING id`,
			s.slug, "标题 "+s.slug, s.moderation, s.lifecycle, authorUserID).Scan(&demoID)
		if err != nil {
			t.Fatalf("插入 %s 失败: %v", s.slug, err)
		}

		var revID int64
		if err := pool.QueryRow(ctx, `
			INSERT INTO demo_revisions (demo_id, rev_no, demo_type, single_file, source, manifest_hash, file_count)
			VALUES ($1, 1, $2, NULLIF($3,''), 'upload', repeat('a', 64), 2) RETURNING id`,
			demoID, s.demoType, s.singleFile).Scan(&revID); err != nil {
			t.Fatalf("插入 %s 的版本失败: %v", s.slug, err)
		}
		// 已发布的作品要有一个对外版本指针（迁移 0014）：没它渠道上不去，
		// 详情/预览/下载的公开读路径也拿不到内容。
		// 草稿与「发布但没审过」的作品按真实情况留空——它们从没对外过。
		if _, err := pool.Exec(ctx, `
			UPDATE demos
			SET current_revision_id = $1::bigint,
			    published_revision_id = CASE WHEN $3 THEN $1::bigint ELSE NULL END
			WHERE id = $2`, revID, demoID, s.hasPublished); err != nil {
			t.Fatalf("设置 %s 的当前版本失败: %v", s.slug, err)
		}
		if s.visibility != "" {
			if _, err := pool.Exec(ctx, `
				INSERT INTO demo_channels (demo_id, channel, visibility, lang)
				VALUES ($1, 'deep', $2, 'zh')`, demoID, s.visibility); err != nil {
				t.Fatalf("插入 %s 的渠道失败: %v", s.slug, err)
			}
		}

		root := filepath.Join(dir, "demos", s.slug, "r1")
		if err := os.MkdirAll(root, 0o755); err != nil {
			t.Fatalf("建目录失败: %v", err)
		}
		if s.withSource {
			if err := os.WriteFile(filepath.Join(root, "source.zip"), []byte("PK\x03\x04zip-bytes-"+s.slug), 0o644); err != nil {
				t.Fatalf("写 %s 的源码包失败: %v", s.slug, err)
			}
		}
		if s.singleFile != "" {
			if err := os.WriteFile(filepath.Join(root, "index."+s.singleFile), []byte("<svg xmlns=\"http://www.w3.org/2000/svg\"/>"), 0o644); err != nil {
				t.Fatalf("写 %s 的单文件失败: %v", s.slug, err)
			}
		}

		world[s.slug] = s
	}

	return world
}

const (
	dlAuthorSub = "ITDOWNLOADAUTHOR000000000AA"
	dlAuthorSID = "it-download-author-session"
)

// TestIntegrationDownload 覆盖「真库 + 真磁盘」的下载路径与计数。
func TestIntegrationDownload(t *testing.T) {
	pool := integrationPool(t)
	dir := t.TempDir()
	seedDownloadWorld(t, pool, dir)
	handler := newDownload(t, pool, dir)

	// 1) 正常作品：200 + attachment + 真字节。
	response := doDownload(t, handler, "dl-zip")
	if response.Code != http.StatusOK {
		t.Fatalf("dl-zip 期望 200，实际 %d", response.Code)
	}
	if got := response.Header().Get("Content-Disposition"); got != `attachment; filename="dl-zip-v1.zip"` {
		t.Fatalf("Content-Disposition 不对: %q", got)
	}
	if got := response.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("缺 nosniff: %q", got)
	}
	if body := response.Body.String(); body != "PK\x03\x04zip-bytes-dl-zip" {
		t.Fatalf("字节不对: %q", body)
	}

	// 1b) 有 Content-Length：没有它，传输靠 chunked 收尾，两边都不知道还剩多少，
	// 一个包传到一半断线时客户端看到的是一个"正常结束"的短文件。
	if got := response.Header().Get("Content-Length"); got != strconv.Itoa(len("PKzip-bytes-dl-zip")) {
		t.Fatalf("Content-Length 不对: %q", got)
	}

	// 2) 计数真的落库了，而且只加一次。
	var downloads int64
	if err := pool.QueryRow(context.Background(),
		`SELECT download_count FROM demo_stats s JOIN demos d ON d.id = s.demo_id WHERE d.slug = 'dl-zip'`).
		Scan(&downloads); err != nil {
		t.Fatalf("读下载计数失败: %v", err)
	}
	if downloads != 1 {
		t.Fatalf("期望 download_count=1，实际 %d", downloads)
	}
	// 再下一次，应当变成 2（UPSERT 的 +1 分支）。
	doDownload(t, handler, "dl-zip")
	if err := pool.QueryRow(context.Background(),
		`SELECT download_count FROM demo_stats s JOIN demos d ON d.id = s.demo_id WHERE d.slug = 'dl-zip'`).
		Scan(&downloads); err != nil {
		t.Fatalf("读下载计数失败: %v", err)
	}
	if downloads != 2 {
		t.Fatalf("期望 download_count=2，实际 %d", downloads)
	}

	// 3) unlisted 可直链下载。
	if got := doDownload(t, handler, "dl-unlisted"); got.Code != http.StatusOK {
		t.Fatalf("dl-unlisted 期望 200，实际 %d", got.Code)
	}

	// 复审中：审核态 pending，但对外版本仍在 → 照常能下。
	// 这条钉的是「作者改稿不该让线上作品下不了」——迁移 0014 之前它是 404。
	if got := doDownload(t, handler, "dl-under-review"); got.Code != http.StatusOK {
		t.Fatalf("dl-under-review 期望 200，实际 %d", got.Code)
	}

	// 4) 单文件作品下原文件。
	response = doDownload(t, handler, "dl-single")
	if response.Code != http.StatusOK {
		t.Fatalf("dl-single 期望 200，实际 %d", response.Code)
	}
	if got := response.Header().Get("Content-Disposition"); got != `attachment; filename="dl-single-v1.svg"` {
		t.Fatalf("单文件下载名不对: %q", got)
	}

	// 5) 剩下的都该失败。
	for _, slug := range []string{"dl-hidden", "dl-draft", "dl-rejected", "dl-archived"} {
		if got := doDownload(t, handler, slug); got.Code != http.StatusNotFound {
			t.Fatalf("%s 期望 404，实际 %d", slug, got.Code)
		}
	}
	if got := doDownload(t, handler, "dl-link"); got.Code != http.StatusBadRequest {
		t.Fatalf("dl-link 期望 400，实际 %d", got.Code)
	}
	if got := doDownload(t, handler, "dl-nope"); got.Code != http.StatusNotFound {
		t.Fatalf("不存在的 slug 期望 404，实际 %d", got.Code)
	}

	// 6) 失败路径不该写计数。dl-hidden 一次都不该有行。
	var hiddenCount int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM demo_stats s JOIN demos d ON d.id = s.demo_id WHERE d.slug = 'dl-hidden'`).
		Scan(&hiddenCount); err != nil {
		t.Fatalf("查 dl-hidden 计数失败: %v", err)
	}
	if hiddenCount != 0 {
		t.Fatalf("被拒的下载不该计数，实际有 %d 行", hiddenCount)
	}
}

// TestIntegrationDownloadMissingFile 覆盖「行说要给、盘上没有」：
// DEMO-040 要求明确失败，而不是回一个空文件。
func TestIntegrationDownloadMissingFile(t *testing.T) {
	pool := integrationPool(t)
	dir := t.TempDir()
	seedDownloadWorld(t, pool, dir)
	handler := newDownload(t, pool, dir)

	// 把 dl-zip 的 source.zip 删掉，模拟文件缺失。
	if err := os.Remove(filepath.Join(dir, "demos", "dl-zip", "r1", "source.zip")); err != nil {
		t.Fatalf("删文件失败: %v", err)
	}

	response := doDownload(t, handler, "dl-zip")
	if response.Code != http.StatusNotFound {
		t.Fatalf("文件缺失期望 404，实际 %d", response.Code)
	}
	if response.Body.Len() == 0 {
		t.Fatalf("404 不该是空响应")
	}
	// 没有真的给出文件，就不该计数。
	var downloads int64
	err := pool.QueryRow(context.Background(),
		`SELECT download_count FROM demo_stats s JOIN demos d ON d.id = s.demo_id WHERE d.slug = 'dl-zip'`).
		Scan(&downloads)
	if err == nil {
		t.Fatalf("文件缺失不该计数，实际 download_count=%d", downloads)
	}
}

func newDownload(t *testing.T, pool *pgxpool.Pool, dir string) *Download {
	t.Helper()
	local, err := storage.NewLocal(dir)
	if err != nil {
		t.Fatalf("建本地存储失败: %v", err)
	}
	return NewDownload(store.New(pool), local)
}

// doDownload 走真实路由：chi.URLParam 依赖路由上下文，直接调 handler 取不到 slug。
func doDownload(t *testing.T, h *Download, slug string) *httptest.ResponseRecorder {
	t.Helper()
	response := httptest.NewRecorder()
	NewRouter(&stub{}, nil, h).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/download/"+slug, nil))
	return response
}
