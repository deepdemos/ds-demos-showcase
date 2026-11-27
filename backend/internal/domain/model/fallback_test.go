package model

import "testing"

// 兜底名判据（模型域 §4.7.5）：这些名字表示「说不清具体是哪个型号」，
// 由系统维护（resolution ∈ family/unknown/guess），不允许管理员手填建出来。
//
// 拒绝的理由不是洁癖：一条 unspecified 手填成 active/exact，等于把
// 「作者不知道用了什么」改写成「本站确认它就是 unspecified 这个型号」，
// 之后所有引用都指到它身上，真实型号反而查不出来。
func TestFallbackModelName(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		// 文档里点名的那几个形态。
		{"unspecified", true},
		{"Unspecified", true},
		{"  unspecified  ", true},
		{"x-unknown", true},
		{"x-family", true},
		{"x-guess", true},
		// 厂商侧「未知型号」的兜底实体。
		{"deepseek-unknown", true},
		{"acme-unknown", true},
		{"DEEPSEEK-UNKNOWN", true},

		// 只是长得像：正常模型名不该被拒（误拒会让管理员退回「导入」那条更重的路）。
		{"", false},
		{"unknown-model", false},
		{"unspecified-model", false},
		{"x-family-2", false},
		{"deepseek-v4-flash", false},
		{"deepseek", false},
		{"unknown", false},
		{"family", false},
		// 兜底判据只管「整个名字就是兜底形态」；带空格的名字是人话，不是兜底。
		{"deepseek unknown", false},
	}
	for _, c := range cases {
		if got := FallbackModelName(c.name); got != c.want {
			t.Errorf("FallbackModelName(%q) = %v，想要 %v", c.name, got, c.want)
		}
	}
}
