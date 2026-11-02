package identity

import (
	"errors"
	"strings"
	"testing"
)

// Validate 是唯一一处「用户填错了」的判断点。传输层只负责把错误翻成状态码。
func TestRegisterInputValidate(t *testing.T) {
	ok := RegisterInput{
		Email:    "someone@example.com",
		Password: "0123456789",
		Username: "someone",
	}
	if err := ok.Validate(); err != nil {
		t.Fatalf("合法输入被拒: %v", err)
	}

	cases := []struct {
		name   string
		mutate func(*RegisterInput)
		want   string
	}{
		{"邮箱为空", func(in *RegisterInput) { in.Email = "" }, "邮箱格式不对"},
		{"邮箱没有 @", func(in *RegisterInput) { in.Email = "nope" }, "邮箱格式不对"},
		{"邮箱以 @ 开头", func(in *RegisterInput) { in.Email = "@example.com" }, "邮箱格式不对"},
		{"邮箱以 @ 结尾", func(in *RegisterInput) { in.Email = "a@" }, "邮箱格式不对"},
		{"邮箱含空白", func(in *RegisterInput) { in.Email = "a b@example.com" }, "空白字符"},
		{"邮箱过长", func(in *RegisterInput) { in.Email = strings.Repeat("a", 255) + "@b.c" }, "邮箱格式不对"},
		{"密码 9 位", func(in *RegisterInput) { in.Password = "012345678" }, "密码长度"},
		{"密码 129 位", func(in *RegisterInput) { in.Password = strings.Repeat("x", 129) }, "密码长度"},
		{"用户名 2 位", func(in *RegisterInput) { in.Username = "ab" }, "用户名"},
		{"用户名 21 位", func(in *RegisterInput) { in.Username = strings.Repeat("a", 21) }, "用户名"},
		{"用户名含空格", func(in *RegisterInput) { in.Username = "a b" }, "用户名"},
		{"用户名含中文", func(in *RegisterInput) { in.Username = "张三ab" }, "用户名"},
		{"展示名过长", func(in *RegisterInput) { in.DisplayName = strings.Repeat("字", 51) }, "展示名"},
	}

	for _, tc := range cases {
		in := ok
		tc.mutate(&in)
		err := in.Validate()
		if err == nil {
			t.Fatalf("%s：应当被拒", tc.name)
		}
		if !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("%s：应当是 ErrInvalidInput，got %v", tc.name, err)
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s：错误信息应包含 %q，got %q", tc.name, tc.want, err.Error())
		}
	}
}

// 用户名规则必须和数据库的 CHECK 同口径。这里钉的是「哪些能过」，
// 数据库那条 ck 钉的是「哪些能存」——两边不一致时，用户会收到 400 或 500，
// 取决于哪一边更严，而这种不确定性最难查。
func TestUsernamePatternMatchesSchemaCheck(t *testing.T) {
	allowed := []string{"abc", "a_b", "a-b", "Abc", "A1_-", strings.Repeat("a", 20)}
	for _, u := range allowed {
		if !usernamePattern.MatchString(u) {
			t.Fatalf("%q 应当允许", u)
		}
	}
	denied := []string{"ab", strings.Repeat("a", 21), "a b", "a.b", "a@b", "张三", ""}
	for _, u := range denied {
		if usernamePattern.MatchString(u) {
			t.Fatalf("%q 应当拒绝", u)
		}
	}
}
