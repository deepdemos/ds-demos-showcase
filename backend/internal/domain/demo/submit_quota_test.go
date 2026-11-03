package demo

import (
	"archive/zip"
	"context"
	"errors"
	"testing"
)

// TestIntegrationSubmitQuota 证明「待审配额」在解压之前就把人挡住，
// 而不是先付出 500 MiB 解压成本再说不行。
func TestIntegrationSubmitQuota(t *testing.T) {
	pool := testPool(t)
	s := newServiceWithPending(t, pool, 1)
	author := mkAuthor(t, pool, "quota")

	pkg1 := buildZip(t, map[string]string{"index.html": "<html>1</html>"})
	if _, err := s.Submit(context.Background(), SubmitInput{
		AuthorID: author, Title: "第一条", DemoType: "web",
		PackagePath: pkg1, PackageSize: fileSize(t, pkg1),
	}); err != nil {
		t.Fatalf("第一条应成功: %v", err)
	}

	// 第二条：配额上限 1，应当被 ErrTooManyPending 挡住。
	pkg2 := buildZip(t, map[string]string{"index.html": "<html>2</html>"})
	_, err := s.Submit(context.Background(), SubmitInput{
		AuthorID: author, Title: "第二条", DemoType: "web",
		PackagePath: pkg2, PackageSize: fileSize(t, pkg2),
	})
	if !errors.Is(err, ErrTooManyPending) {
		t.Fatalf("超配额应返回 ErrTooManyPending（→429），实际 %v", err)
	}

	var n int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM demos WHERE author_id = $1`, author).Scan(&n); err != nil {
		t.Fatalf("查库失败: %v", err)
	}
	if n != 1 {
		t.Fatalf("被配额拒绝的投稿不该留行，实际 %d 条", n)
	}
}

// TestIntegrationManifestIsCanonical 证明 manifest_hash 与「怎么打包」无关：
// 同样的文件内容、不同的 zip 压缩方式（这里用不同压缩级别）应当得到同一个值。
func TestIntegrationManifestIsCanonical(t *testing.T) {
	pool := testPool(t)
	s := testService(t, pool)
	author := mkAuthor(t, pool, "manifest")

	files := map[string]string{
		"index.html": "<html>same</html>",
		"style.css":  "body{color:red}",
		"sub/app.js": "console.log(1)",
	}
	pkg1 := buildZipLevel(t, files, zip.Store)
	pkg2 := buildZipLevel(t, files, zip.Deflate)

	r1, err := s.Submit(context.Background(), SubmitInput{
		AuthorID: author, Title: "包一", DemoType: "zip",
		PackagePath: pkg1, PackageSize: fileSize(t, pkg1),
	})
	if err != nil {
		t.Fatalf("第一条应成功: %v", err)
	}
	r2, err := s.Submit(context.Background(), SubmitInput{
		AuthorID: author, Title: "包二", DemoType: "zip",
		PackagePath: pkg2, PackageSize: fileSize(t, pkg2),
	})
	if err != nil {
		t.Fatalf("第二条应成功: %v", err)
	}

	hashOf := func(slug string) string {
		var h string
		if err := pool.QueryRow(context.Background(), `
			SELECT r.manifest_hash FROM demo_revisions r
			JOIN demos d ON d.id = r.demo_id WHERE d.slug = $1`, slug).Scan(&h); err != nil {
			t.Fatalf("取 manifest 失败: %v", err)
		}
		return h
	}
	h1, h2 := hashOf(r1.Slug), hashOf(r2.Slug)
	if h1 != h2 {
		t.Fatalf("同样的内容、不同打包方式应得到同一个 manifest_hash，实际 %s != %s", h1, h2)
	}
	// 与 zip 字节哈希不同：这一条用来防止有人把实现改回「算包文件摘要」。
	if h1 == fileHash(t, pkg1) {
		t.Fatal("manifest_hash 不该等于 zip 文件本身的 sha256")
	}
}
