package demo

import (
	"context"
	"testing"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/platform/storage"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

// TestIntegrationSubmitHasNoPerAuthorCap 钉住「投稿写路径无单作者待审上限」这件事。
//
// 2026-11-16 撤掉了单作者待审上限（429 + Retry-After）：它来自 review #198 的
// 防刷建议，没有需求授权（松点第 65 条）。#467 已从 demo.New 签名中移除废弃的占位参数，
// 本用例保留为直接断言同一作者可连续提交两条待审作品；若日后有人重新引入单作者硬拒配额，
// 这里会红。
func TestIntegrationSubmitHasNoPerAuthorCap(t *testing.T) {
	pool := testPool(t)
	files, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("初始化存储失败: %v", err)
	}
	s := New(pool, store.New(pool), files, storage.DefaultLimits(), 200<<20, nil)
	author := mkAuthor(t, pool, "noquota")

	for i, title := range []string{"第一条", "第二条"} {
		pkg := buildZip(t, map[string]string{"index.html": "<html>" + title + "</html>"})
		if _, err := s.Submit(context.Background(), SubmitInput{
			AuthorID: author, Title: title, DemoType: "web",
			PackagePath: pkg, PackageSize: fileSize(t, pkg),
		}); err != nil {
			t.Fatalf("第 %d 条投稿不该被挡（待审上限已撤），实际 %v", i+1, err)
		}
	}

	var n int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM demos WHERE author_id = $1`, author).Scan(&n); err != nil {
		t.Fatalf("查库失败: %v", err)
	}
	if n != 2 {
		t.Fatalf("两条都该落库，实际 %d 条", n)
	}
}
