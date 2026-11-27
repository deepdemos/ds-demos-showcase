// Package slug 把展示名折成 URL 片段。
//
// 只有「折叠」这一件事：小写、只留 a-z 与 0-9、其余一律当分隔符收成一个连字符、
// 去掉首尾连字符。折不出任何 ASCII 时返回空串，由调用方决定兜底形态。
//
// 它是**纯文本机制**，所以放 platform；用它的两个域各自保留自己的业务规则：
// 作品撞名时补随机后缀、模型折不出时退名字摘要。长度上限与兜底判据**故意不在这里**——
// 把上限也塞进来，将来谁调上限都会悄悄改掉另一个域的 URL 形状。
package slug

import "strings"

// Fold 把名字折成小写连字符形态；折不出 ASCII 时返回空串。
//
// 「折不出」不是异常：纯中文名、纯标点名都会走到这里。调用方必须把空串当成
// 「这个名字没有可读形态」处理，而不是硬造一个连字符开头的串。
func Fold(name string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		default:
			// 其余一律当分隔符：空格、标点、中文都归到这里。
			// 丢掉它们会把 "a1!b2" 变成 "a1b2"（两个词粘一起），加个连字符更诚实。
			if b.Len() > 0 && !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}
