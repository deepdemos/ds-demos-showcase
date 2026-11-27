package demo

import (
	"context"
	"testing"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/platform/storage"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

// TestIntegrationSubmitHasNoPerAuthorCap 钉住「同一作者可连续提交超过原默认 5 条待审上限的作品」。
//
// 2026-11-16 撤掉了单作者待审上限（原默认 5 条，429 + Retry-After）：它来自 review #198 的
// 防刷建议，没有需求授权（松点第 65 条）。#467 已从 demo.New 签名中移除废弃的占位参数，
// 本用例保留为同一作者连续提交 6 条待审作品并校验全部处于 pending 态；若日后有人重新引入
// 上限 ≤ 5（含原默认 5 条）的单作者硬拒配额，这里会红（更高的阈值无法靠有限条数穷举）。
func TestIntegrationSubmitHasNoPerAuthorCap(t *testing.T) {
	pool := testPool(t)
	files, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("初始化存储失败: %v", err)
	}
	s := New(pool, store.New(pool), files, storage.DefaultLimits(), 200<<20, nil)
	author := mkAuthor(t, pool, "noquota")

	titles := []string{"第一条", "第二条", "第三条", "第四条", "第五条", "第六条"}
	for i, title := range titles {
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
		`SELECT count(*) FROM demos WHERE author_id = $1 AND moderation_state = 'pending'`, author).Scan(&n); err != nil {
		t.Fatalf("查库失败: %v", err)
	}
	if n != len(titles) {
		t.Fatalf("%d 条都该以 pending 态落库，实际 %d 条", len(titles), n)
	}
}
