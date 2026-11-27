package model

import "strings"

// 兜底实体（fallback）的名字判据（模型域 §4.7.5）。
//
// resolution ∈ family/unknown/guess 的模型表示「说不清具体是哪个型号」，
// 由系统维护：不得有 offerings、不得有档案、不参与目录匹配。管理员手动新建
// 必须把它们挡在门外——一条手填的 unspecified 一旦落成 active/exact，
// 「作者不知道用了什么」就被改写成本站确认的型号，之后所有引用都指向它，
// 真实型号再也查不出来。
//
// 判据只认「整个名字就是兜底形态」的几种写法，不做模糊匹配：unknown-model、
// unspecified-model 这类是正常模型名（有一批私有微调确实这样起名），误拒会让
// 管理员退回「导入上游条目」那条更重的路，反而更容易出错。
//
// 四个形态与文档 §4.7.5 一一对应：unspecified（完全没有型号信息）、
// x-family / x-guess（占位猜测）、<厂商>-unknown（厂商侧未知型号）。
// 这条判据的唯一改动点在 docs/运行手册-可变常量.md §7.7 登记。
func FallbackModelName(name string) bool {
	trimmed := strings.ToLower(strings.TrimSpace(name))
	if trimmed == "" {
		return false
	}
	switch trimmed {
	case "unspecified", "x-family", "x-guess":
		return true
	}
	return strings.HasSuffix(trimmed, "-unknown")
}
