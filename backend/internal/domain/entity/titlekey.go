package entity

import (
	"strings"
	"unicode"
)

// TitleKey / TitleNorm 是题目名的派生键（实体域 §3.8 / §3.9）。
//
// # 已知差距
//
// 设计稿要求 NFKC 归一 + casefold。标准库没有这两个操作，引入 x/text 需要新依赖，
// 这一版先实现「保守」子集：去分隔符/控制符/标点/符号（白名单除外）、小写、
// 折叠连续点。对中文题目与常见英文题目足够；全角/兼容字符与 ß 这类 casefold
// 差异尚未覆盖，登记在 docs/已知松点.md。
//
// # 为什么单独成包
//
// title_key 是存储列：写入时算、读取时不重算。函数一旦变化就是数据迁移，
// 所以必须只有一份实现；迁移脚本以后也应该 import 这里。

// keyPunctWhitelist 是 title_key 保留的标点：它们承载语义（C++ / C# / GPT-4o）。
var keyPunctWhitelist = map[rune]bool{
	'+': true, '#': true, '.': true, '-': true, '_': true, '@': true,
}

// TitleKey 计算硬去重键。
func TitleKey(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case unicode.IsSpace(r), unicode.IsControl(r), unicode.Is(unicode.Cf, r):
			continue
		case unicode.IsPunct(r), unicode.IsSymbol(r):
			if !keyPunctWhitelist[r] {
				continue
			}
		}
		b.WriteRune(unicode.ToLower(r))
	}
	key := b.String()
	for strings.Contains(key, "..") {
		key = strings.ReplaceAll(key, "..", ".")
	}
	return key
}

// TitleNorm 是软提示键：比 TitleKey 更激进，只留字母、数字与 CJK。
func TitleNorm(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}
