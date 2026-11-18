package entity

import "testing"

func TestTitleKey(t *testing.T) {
	cases := []struct{ in, want string }{
		{"画一个3D坦克", "画一个3d坦克"},
		{"画一个 3D 坦克", "画一个3d坦克"},
		{"画一个3D坦克！", "画一个3d坦克"},
		{"画一个3D坦克。", "画一个3d坦克"},
		{"《画一个3D坦克》", "画一个3d坦克"},
		{"用 C++ 写排序", "用c++写排序"},
		{"用 C# 写排序", "用c#写排序"},
		{"GPT-4o 画图", "gpt-4o画图"},
		{"a-b", "a-b"},
		{"a_b", "a_b"},
		{"x@y", "x@y"},
		{"hello...world", "hello.world"},
	}
	for _, tc := range cases {
		if got := TitleKey(tc.in); got != tc.want {
			t.Errorf("TitleKey(%q) = %q，想要 %q", tc.in, got, tc.want)
		}
	}
}

func TestTitleNorm(t *testing.T) {
	if got := TitleNorm("画一个 3D 坦克！"); got != "画一个3d坦克" {
		t.Fatalf("TitleNorm = %q", got)
	}
	if got := TitleNorm("Hi There!"); got != "hithere" {
		t.Fatalf("TitleNorm = %q", got)
	}
}
