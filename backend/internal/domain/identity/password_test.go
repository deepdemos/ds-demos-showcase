package identity

import (
	"strings"
	"testing"
)

func TestHashPasswordUsesArgon2idPHCFormat(t *testing.T) {
	encoded, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("哈希失败: %v", err)
	}
	// 参数必须真的写进去——用当前常量验老哈希的前提就是「参数随哈希存」。
	if !strings.HasPrefix(encoded, "$argon2id$v=19$m=65536,t=3,p=2$") {
		t.Fatalf("PHC 前缀不对: %s", encoded)
	}
	if got := len(strings.Split(encoded, "$")); got != 6 {
		t.Fatalf("PHC 应该是 6 段（含前导空段），got %d: %s", got, encoded)
	}
}

func TestHashPasswordIsSalted(t *testing.T) {
	a, _ := HashPassword("same password")
	b, _ := HashPassword("same password")
	if a == b {
		t.Fatal("两次哈希相同 —— 盐没有生效")
	}
}

func TestVerifyPassword(t *testing.T) {
	encoded, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("哈希失败: %v", err)
	}

	if ok, err := VerifyPassword("correct horse battery staple", encoded); err != nil || !ok {
		t.Fatalf("正确密码应当通过：ok=%v err=%v", ok, err)
	}
	if ok, err := VerifyPassword("wrong password", encoded); err != nil || ok {
		t.Fatalf("错误密码应当失败：ok=%v err=%v", ok, err)
	}
}

// 老哈希用老参数验：把 m 调低之后，用旧参数生成的哈希仍要能验通。
// 如果实现改成「用包级常量验」，这条会挂——那正是调参那天全员登不上的原因。
func TestVerifyPasswordUsesParametersFromHash(t *testing.T) {
	weak, err := HashPassword("hunter2hunter2")
	if err != nil {
		t.Fatalf("哈希失败: %v", err)
	}
	// 手工把 m 改成 8MiB（参数变小，但格式合法）
	weaker := strings.Replace(weak, "m=65536,", "m=8192,", 1)
	if weaker == weak {
		t.Fatal("替换没生效，测试本身写错了")
	}
	// 参数变了，哈希必然对不上——这说明 Verify 确实用了哈希里的参数，而不是常量。
	if ok, _ := VerifyPassword("hunter2hunter2", weaker); ok {
		t.Fatal("参数被改过却验通了 —— 说明 Verify 没有使用哈希里的参数")
	}
}

func TestVerifyPasswordRejectsGarbage(t *testing.T) {
	cases := []string{
		"",
		"plaintext",
		"$2b$10$abcdefghijklmnopqrstuv",
		"$argon2id$v=19$m=65536,t=3,p=2$onlyonefield",
		"$argon2id$v=19$m=0,t=3,p=2$c2FsdA$aGFzaA",     // 内存 0
		"$argon2id$v=19$m=65536,t=0,p=2$c2FsdA$aGFzaA", // 迭代 0
		"$argon2id$v=19$m=65536,t=3,p=2$c2FsdA$",       // 空哈希
	}
	for _, c := range cases {
		if ok, err := VerifyPassword("anything", c); ok || err == nil {
			t.Fatalf("%q 应当被拒（ok=%v err=%v）", c, ok, err)
		}
	}
}

func TestNewSubShape(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 64; i++ {
		sub, err := NewSub()
		if err != nil {
			t.Fatalf("生成 sub 失败: %v", err)
		}
		// base32(128 bit) 无填充 = 26 个字符，字母表 A-Z2-7
		if len(sub) != 26 {
			t.Fatalf("sub 长度应为 26，got %d: %s", len(sub), sub)
		}
		for _, r := range sub {
			if !(r >= 'A' && r <= 'Z') && !(r >= '2' && r <= '7') {
				t.Fatalf("sub 含有 base32 字母表外的字符 %q: %s", r, sub)
			}
		}
		if seen[sub] {
			t.Fatalf("sub 重复：%s", sub)
		}
		seen[sub] = true
	}
}

func TestNewSessionIDShape(t *testing.T) {
	id, err := newSessionID()
	if err != nil {
		t.Fatalf("生成会话 ID 失败: %v", err)
	}
	// 32 字节 base64url 无填充 = 43 个字符
	if len(id) != 43 {
		t.Fatalf("会话 ID 长度应为 43，got %d", len(id))
	}
	if strings.ContainsAny(id, "+/=") {
		t.Fatalf("会话 ID 必须是 url-safe 无填充: %s", id)
	}
}
