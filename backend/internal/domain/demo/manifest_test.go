package demo

import (
	"archive/zip"
	"context"
	"testing"
)

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
