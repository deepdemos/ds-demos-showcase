package storage

import "testing"

func TestKeyLayout(t *testing.T) {
	cases := []struct {
		name string
		got  string
		want string
	}{
		{"files prefix", FilesPrefix("abc", 1), "demos/abc/r1/"},
		{"files key", FilesKey("abc", 3, "assets/app.js"), "demos/abc/r3/assets/app.js"},
		{"source key", SourceKey("abc", 2, "zip"), "demos/abc/r2/source.zip"},
		{"cover key", CoverKey("abc", 10, "png"), "demos/abc/r10/cover.png"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, c.got, c.want)
		}
	}
}

// 版本号换成不同位数时目录仍然唯一——否则 r1 与 r10 会撞。
func TestKeyLayoutVersionsDiffer(t *testing.T) {
	seen := map[string]bool{}
	for _, rev := range []int{0, 1, 2, 9, 10, 11, 99, 100} {
		key := FilesPrefix("abc", rev)
		if seen[key] {
			t.Fatalf("版本 %d 与前一个版本拼出同一个前缀 %q", rev, key)
		}
		seen[key] = true
	}
}

func TestItoa(t *testing.T) {
	cases := map[int]string{0: "0", 1: "1", 9: "9", 10: "10", 42: "42", 100: "100"}
	for in, want := range cases {
		if got := itoa(in); got != want {
			t.Errorf("itoa(%d) = %q, want %q", in, got, want)
		}
	}
}
