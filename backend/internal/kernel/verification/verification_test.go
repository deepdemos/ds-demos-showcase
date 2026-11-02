package verification

import (
	"strings"
	"testing"
)

func TestNewCode_六位十进制且允许前导零(t *testing.T) {
	const n = 2000
	hasLeadingZero := false
	for i := 0; i < n; i++ {
		code, err := newCode()
		if err != nil {
			t.Fatalf("newCode: %v", err)
		}
		if len(code) != CodeDigits {
			t.Fatalf("长度必须是 %d，实际 %q", CodeDigits, code)
		}
		for _, r := range code {
			if r < '0' || r > '9' {
				t.Fatalf("只能有数字，实际 %q", code)
			}
		}
		if strings.HasPrefix(code, "0") {
			hasLeadingZero = true
		}
	}
	// 前导零不代表码「更短」：000123 也是合法码。去掉前导零会把空间缩到 1/10。
	// 2000 次里一次 0 开头都没有，说明补零那一步被写坏了。
	if !hasLeadingZero {
		t.Fatal("2000 次生成里没有出现过前导零，补零逻辑可能失效")
	}
}

func TestIdentifierKey_用途不同则键不同(t *testing.T) {
	s := &Service{secret: []byte("test-secret")}
	a := s.identifierKey(PurposeLoginChallenge, "User@Example.com")
	b := s.identifierKey("email_change", "User@Example.com")
	if a == b {
		t.Fatal("不同用途必须得到不同的键，否则计数与验证会互相污染")
	}
	// 归一化：大小写与首尾空白不影响。
	c := s.identifierKey(PurposeLoginChallenge, "  user@example.COM ")
	if a != c {
		t.Fatal("标识符应归一化后再哈希")
	}
	// 换一把密钥，键就变了（不可逆，无法跨环境比对）。
	other := &Service{secret: []byte("another-secret")}
	if a == other.identifierKey(PurposeLoginChallenge, "User@Example.com") {
		t.Fatal("不同密钥必须得到不同的键")
	}
}

func TestDelimited_跨用途重放必失败(t *testing.T) {
	s := &Service{secret: []byte("test-secret")}
	// 同一串码在两个用途下算出的哈希必须不同——这正是「登录挑战发的码不能拿去改邮箱」的实现。
	if s.hash(s.delimited(PurposeLoginChallenge, "a@b.c", "123456")) ==
		s.hash(s.delimited("email_change", "a@b.c", "123456")) {
		t.Fatal("用途必须参与哈希，否则同一串码可以跨场景重放")
	}
	// 换标识符也必须变。
	if s.hash(s.delimited(PurposeLoginChallenge, "a@b.c", "123456")) ==
		s.hash(s.delimited(PurposeLoginChallenge, "x@y.z", "123456")) {
		t.Fatal("标识符必须参与哈希")
	}
}

func TestNormalize_大小写与空白(t *testing.T) {
	if normalize("  Alice@Example.COM ") != "alice@example.com" {
		t.Fatalf("归一化不对: %q", normalize("  Alice@Example.COM "))
	}
}
