package demo

import (
	"context"
	"testing"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/platform/storage"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

// TestIntegrationSubmitHasNoPerAuthorCap 钉住「第 6 个参数被忽略」这件事。
//
// 2026-11-16 撤掉了单作者待审上限（429 + Retry-After）：它来自 review #198 的
// 防刷建议，没有需求授权。但 demo.New 的签名里还留着那个参数——一次删干净要动
// 19 处调用点，会越过 pr-guard 的 20 文件上限（删除记在 #467，松点第 65 条）。
//
// 这条用例把「留着也不生效」写成断言：传 1 也照样能连投两条。如果哪天有人把这个
// 参数接回配额判定，这里会红——而不是等到线上有人投第二条被挡住才发现。
func TestIntegrationSubmitHasNoPerAuthorCap(t *testing.T) {
	pool := testPool(t)
	files, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("初始化存储失败: %v", err)
	}
	// 第 6 参故意传 1：旧语义下第二条会被 ErrTooManyPending 挡住。
	s := New(pool, store.New(pool), files, storage.DefaultLimits(), 200<<20, 1, nil)
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
