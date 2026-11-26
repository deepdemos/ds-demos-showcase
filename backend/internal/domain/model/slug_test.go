package model

import (
	"strings"
	"testing"
)

// 纯 ASCII 名折成小写连字符。期望值是手写的字面量，不由被测代码推导——
// 否则这条用例只是在复述实现，永远不会与实现对不上。
func TestSlugFoldsASCIIName(t *testing.T) {
	if got, want := Slug("DeepSeek V4 Flash", "deepseek"), "deepseek-v4-flash"; got != want {
		t.Fatalf("Slug(DeepSeek V4 Flash) = %q，想要 %q", got, want)
	}
}

// 纯中文名折不出 ASCII，退「厂商-slug + 名字摘要」——不再退化成 model / model-2。
//
// 这里只钉形状与确定性，不在测试里再算一遍摘要：那等于把实现抄第二遍，
// 摘要算法一改两边一起改，用例永远绿（同义反复）。
func TestSlugChineseNameFallsBackToVendorAndDigest(t *testing.T) {
	got := Slug("深度求索旗舰版", "deepseek")
	if !strings.HasPrefix(got, "deepseek-") {
		t.Fatalf("中文名应带厂商前缀，实际 %q", got)
	}
	digest := strings.TrimPrefix(got, "deepseek-")
	if !isLowerHex(digest) || len(digest) != 6 {
		t.Fatalf("摘要应是 6 位小写十六进制，实际 %q（完整 slug %q）", digest, got)
	}
	if again := Slug("深度求索旗舰版", "deepseek"); again != got {
		t.Fatalf("同一输入两次产出不同：%q / %q", got, again)
	}
	if other := Slug("另一款国产模型", "deepseek"); other == got {
		t.Fatalf("不同名字不该撞同一个 slug，两个都是 %q", got)
	}
	if strings.HasPrefix(got, "model") {
		t.Fatalf("有厂商时不该退成 model 系列，实际 %q", got)
	}
}

// 没有厂商、名字又全无信息（纯标点 / 纯空白）时，slug 就是名字摘要本身：
// 不加前缀、不以连字符开头或结尾，也不是空串（空串落库会违反 ck_models_slug_len）。
func TestSlugWithoutVendorFallsBackToDigestOnly(t *testing.T) {
	for _, name := range []string{"！？", "——", "   ", "", "  ··  "} {
		got := Slug(name, "")
		if !isLowerHex(got) || len(got) != 6 {
			t.Fatalf("Slug(%q) = %q，期望就是 6 位小写十六进制摘要", name, got)
		}
		if strings.HasPrefix(got, "-") || strings.HasSuffix(got, "-") {
			t.Fatalf("Slug(%q) = %q，首尾不该残留连字符", name, got)
		}
		if again := Slug(name, ""); again != got {
			t.Fatalf("同一输入两次产出不同：%q / %q", got, again)
		}
		// 厂商有值时哪怕名字全是标点，也要走厂商前缀。
		if withVendor := Slug(name, "deepseek"); !strings.HasPrefix(withVendor, "deepseek-") {
			t.Fatalf("Slug(%q, deepseek) = %q，应带厂商前缀", name, withVendor)
		}
	}
}

// 超长名截断到上限；截断点正好落在分隔符上时不能留下末尾连字符。
func TestSlugTruncatesLongName(t *testing.T) {
	long := strings.Repeat("abcdefghij", 20) // 200 个字母，折完原样保留
	got := Slug(long, "")
	if len(got) > SlugMaxLen {
		t.Fatalf("长度 %d 超过上限 %d：%q", len(got), SlugMaxLen, got)
	}
	if got != long[:SlugMaxLen] {
		t.Fatalf("截断后应是前 %d 个字符，实际 %q", SlugMaxLen, got)
	}

	// 第 SlugMaxLen 个字符正好是分隔符：`aaa…a` + ` b` 折成 `aaa…a-b`，截断后只剩 a。
	cutOnDash := strings.Repeat("a", SlugMaxLen-1) + " b"
	if got := Slug(cutOnDash, ""); got != strings.Repeat("a", SlugMaxLen-1) {
		t.Fatalf("截断点落在分隔符上时应去掉末尾连字符，实际 %q（%d 字符）", got, len(got))
	}
}

// 厂商名很长时不能把摘要挤掉——摘要被截掉就等于退回了「无身份的兜底」。
func TestSlugKeepsDigestWhenVendorIsLong(t *testing.T) {
	got := Slug("深度求索旗舰版", strings.Repeat("v", 100))
	if len(got) > SlugMaxLen {
		t.Fatalf("长度 %d 超过上限 %d", len(got), SlugMaxLen)
	}
	if len(got) <= 6 || got[len(got)-7] != '-' || !isLowerHex(got[len(got)-6:]) {
		t.Fatalf("末尾应完整保留「-摘要」，实际 %q", got)
	}
}

func isLowerHex(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}
