package demo

import "testing"

// 形态表是投稿校验与形态矩阵用例的**唯一数据源**，所以它自己也要被钉住：
// 谁改坏了 stagedName / isRootIndex，两处会同时错，只靠矩阵看不出来。

func TestSubmitShapesCoverEveryKind(t *testing.T) {
	shapes := SubmitShapes()

	// zip 一种 + 单文件若干种。
	if len(shapes) != 1+len(singleFileKinds) {
		t.Fatalf("形态数 = %d，想要 1+%d（新增格式时请确认它进了单文件表）", len(shapes), len(singleFileKinds))
	}

	seen := map[string]bool{}
	for _, s := range shapes {
		if seen[s.Kind] {
			t.Fatalf("形态 %q 重复出现", s.Kind)
		}
		seen[s.Kind] = true
		if s.EntryPath == "" {
			t.Fatalf("形态 %q 没有入口文件名", s.Kind)
		}
		if s.FileName == "" {
			t.Fatalf("形态 %q 没有示例文件名", s.Kind)
		}
	}

	// zip 在多文件一侧：single_file 为空，入口是根目录入口。
	if !seen["zip"] {
		t.Fatal("形态表里没有 zip")
	}

	// 每种接受的扩展名都要在表里出现，否则矩阵会漏测那种格式。
	for ext := range singleFileKinds {
		kind := ext[1:] // 去掉点
		if !seen[kind] {
			t.Fatalf("扩展名 %s 对应形态 %q 没进形态表", ext, kind)
		}
	}
}

func TestStagedName(t *testing.T) {
	cases := []struct{ in, want string }{
		{"index.html", "index.html"},
		{"index.htm", "index.html"}, // 归一：MIME 判断不分叉
		{"photo.SVG", "index.svg"},  // 扩展名大小写不敏感
		{"weird.name.htm", "index.html"},
	}
	for _, c := range cases {
		if got := stagedName(c.in); got != c.want {
			t.Errorf("stagedName(%q) = %q，想要 %q", c.in, got, c.want)
		}
	}
}

func TestSingleFileKind(t *testing.T) {
	ok := map[string]string{".html": "html", ".htm": "html", ".svg": "svg", ".SVG": "svg"}
	for ext, want := range ok {
		got, accepted := singleFileKind(ext)
		if !accepted || got != want {
			t.Errorf("singleFileKind(%q) = %q/%v，想要 %q/true", ext, got, accepted, want)
		}
	}
	if _, accepted := singleFileKind(".zip"); accepted {
		t.Error("zip 不该被当成单文件形态：它走解压分支")
	}
}

func TestIsRootIndex(t *testing.T) {
	yes := []string{"index.html", "index.htm"}
	no := []string{"dist/index.html", "index.svg", "index_html", ""}
	for _, rel := range yes {
		if !isRootIndex(rel) {
			t.Errorf("isRootIndex(%q) = false，想要 true", rel)
		}
	}
	for _, rel := range no {
		if isRootIndex(rel) {
			t.Errorf("isRootIndex(%q) = true，想要 false（只有根目录的入口才算）", rel)
		}
	}
}
