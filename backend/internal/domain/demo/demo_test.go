package demo

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/platform/storage"
)

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"Hello World":      "hello-world",
		"  Trim  Me  ":     "trim-me",
		"a_b.c-d":          "a-b-c-d",
		"UPPER":            "upper",
		"双黑洞合并":            "",
		"双黑洞合并 Black Hole": "black-hole",
		"---":              "",
		"":                 "",
		"x":                "x",
		"a1!@#$b2":         "a1-b2",
	}
	for in, want := range cases {
		if got := slugify(in); got != want {
			t.Errorf("slugify(%q) = %q, want %q", in, got, want)
		}
	}
}

// 纯中文标题压不出 slug，调用方要退回随机 slug——这里只钉住「返回空」这个契约。
func TestSlugifyChineseReturnsEmpty(t *testing.T) {
	if got := slugify("重力迷宫"); got != "" {
		t.Fatalf("纯中文标题应返回空串，实际 %q", got)
	}
}

func TestSlugifyTruncates(t *testing.T) {
	long := strings.Repeat("abcdefghij", 20)
	got := slugify(long)
	if len(got) > slugBaseLimit {
		t.Fatalf("slug 长度 %d 超过上限 %d", len(got), slugBaseLimit)
	}
	if strings.HasSuffix(got, "-") {
		t.Fatalf("截断后不该留下尾部连字符: %q", got)
	}
}

func validInput() SubmitInput {
	return SubmitInput{
		AuthorID:    1,
		Title:       "一个作品",
		Description: "说明",
		DemoType:    "web",
		PackagePath: "/tmp/x.zip",
		PackageSize: 1024,
	}
}

func TestValidateOK(t *testing.T) {
	if err := validInput().validate(); err != nil {
		t.Fatalf("合法输入不该报错: %v", err)
	}
}

func TestValidateRejects(t *testing.T) {
	cases := []struct {
		name  string
		mutit func(*SubmitInput)
	}{
		{"空标题", func(in *SubmitInput) { in.Title = "   " }},
		{"标题过长", func(in *SubmitInput) { in.Title = strings.Repeat("字", 201) }},
		{"描述过长", func(in *SubmitInput) { in.Description = strings.Repeat("字", 5001) }},
		{"缺类型", func(in *SubmitInput) { in.DemoType = "" }},
		{"类型过长", func(in *SubmitInput) { in.DemoType = strings.Repeat("a", 33) }},
		{"类型含大写", func(in *SubmitInput) { in.DemoType = "Web" }},
		{"类型含连字符", func(in *SubmitInput) { in.DemoType = "web-app" }},
		{"缺作者", func(in *SubmitInput) { in.AuthorID = 0 }},
		{"缺文件", func(in *SubmitInput) { in.PackagePath = "" }},
		{"文件为空", func(in *SubmitInput) { in.PackageSize = 0 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := validInput()
			tc.mutit(&in)
			if err := in.validate(); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("应返回 ErrInvalidInput，实际 %v", err)
			}
		})
	}
}

// 解压层的每类「包不合格」都必须翻成 ErrBadPackage（422），
// 不能让 storage 的哨兵错误漏到传输层——那会被当成 503。
func TestMapExtractError(t *testing.T) {
	badPkg := []error{
		storage.ErrTooLarge,
		storage.ErrSuspiciousRatio,
		storage.ErrTooManyMembers,
		storage.ErrUnsafePath,
		storage.ErrEmptyArchive,
	}
	for _, src := range badPkg {
		got := mapExtractError(fmt.Errorf("包装: %w", src))
		if !errors.Is(got, ErrBadPackage) {
			t.Fatalf("%v 应翻成 ErrBadPackage，实际 %v", src, got)
		}
	}
	// 其它错误不该被吞成「用户输入有问题」——那是 500/503。
	if got := mapExtractError(errors.New("磁盘满了")); errors.Is(got, ErrBadPackage) {
		t.Fatal("未知错误不该翻成 ErrBadPackage")
	}
}

func TestSlugifyOnlyPunctuationReturnsEmpty(t *testing.T) {
	// 纯标点与纯空白一样，都不该产出可用 slug。
	for _, in := range []string{"！？", "——", "   ", ""} {
		if got := slugify(in); got != "" {
			t.Fatalf("slugify(%q) = %q，期望空串", in, got)
		}
	}
}
