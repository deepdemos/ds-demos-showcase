package main

import (
	"errors"
	"flag"
	"strings"
	"testing"
)

// 事故现场：`go run ./cmd/migrate status` 被当成 up 执行，把 0016 推上了开发库。
// 那时的实现只读 -mode，位置参数被 flag.Parse 静默丢掉、默认值 "up" 生效。
// 下面这条用例就是那个现场的同一种调用形态（位置参数，不带 -mode）。
func TestParseArgsPositionalStatusIsStatus(t *testing.T) {
	opt, err := parseArgs([]string{"status"})
	if err != nil {
		t.Fatalf("位置参数 status 应当被接受，实际报错: %v", err)
	}
	if opt.mode != modeStatus {
		t.Fatalf("`migrate status` 必须是只读的 status，实际 mode=%q —— 这正是会改库的那个缺陷", opt.mode)
	}
}

func TestParseArgsFlagStatusIsStatus(t *testing.T) {
	opt, err := parseArgs([]string{"-mode", "status"})
	if err != nil {
		t.Fatalf("flag 写法应当被接受: %v", err)
	}
	if opt.mode != modeStatus {
		t.Fatalf("mode = %q，想要 status", opt.mode)
	}
}

// 两种写法都给且说同一件事时放行：文档、package.json、容器 entrypoint 里两种写法都存在。
func TestParseArgsAgreeingFormsAreAccepted(t *testing.T) {
	for _, argv := range [][]string{
		{"-mode", "up", "up"},
		{"-mode", "seed", "seed"},
	} {
		opt, err := parseArgs(argv)
		if err != nil {
			t.Fatalf("%v 应当被接受: %v", argv, err)
		}
		want := argv[len(argv)-1]
		if opt.mode != want {
			t.Fatalf("%v 解析出 mode=%q，想要 %q", argv, opt.mode, want)
		}
	}
}

// 选项写在模式后面时 flag 根本看不见它（解析停在第一个非选项）。与其静默忽略，
// 不如报错并给出正确写法——静默忽略正是这次事故的形状。
func TestParseArgsRejectsFlagsAfterMode(t *testing.T) {
	for _, argv := range [][]string{
		{"up", "-mode", "up"},
		{"seed", "-seeds", "/tmp/seeds"},
	} {
		_, err := parseArgs(argv)
		if err == nil {
			t.Fatalf("%v 应当报错：写在模式后面的选项会被 flag 静默丢弃", argv)
		}
		if !strings.Contains(err.Error(), "选项必须写在模式之前") {
			t.Fatalf("%v 的错误信息要说清顺序，实际: %v", argv, err)
		}
	}
}

// 矛盾就报错，不猜哪个是对的：猜错的方向正好是「本该只读却写了库」。
func TestParseArgsConflictingFormsAreRejected(t *testing.T) {
	_, err := parseArgs([]string{"-mode", "up", "status"})
	if err == nil {
		t.Fatal("位置参数与 -mode 矛盾时应当报错")
	}
	if !strings.Contains(err.Error(), "矛盾") {
		t.Fatalf("错误信息要说明是模式矛盾，实际: %v", err)
	}
}

// 没有默认模式。省略模式曾经等于 up，那是这次事故的根因之一。
func TestParseArgsRequiresMode(t *testing.T) {
	_, err := parseArgs(nil)
	if err == nil {
		t.Fatal("一个模式都不给时应当报错，而不是默认 up")
	}
	if errors.Is(err, flag.ErrHelp) {
		t.Fatal("缺模式不是 -h 请求帮助")
	}
}

func TestParseArgsRejectsUnknownMode(t *testing.T) {
	for _, mode := range []string{"down", "migrate", "UP", ""} {
		if mode == "" {
			continue
		}
		if _, err := parseArgs([]string{mode}); err == nil {
			t.Fatalf("未知模式 %q 应当报错", mode)
		}
	}
}

func TestParseArgsRejectsExtraPositional(t *testing.T) {
	if _, err := parseArgs([]string{"status", "up"}); err == nil {
		t.Fatal("两个位置参数应当报错")
	}
}

// -h 走 flag 的标准路径：打出用法后正常返回，不算失败。
func TestParseArgsHelpIsNotAnError(t *testing.T) {
	if _, err := parseArgs([]string{"-h"}); !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("-h 应当返回 flag.ErrHelp，实际: %v", err)
	}
}

// 目录参数照旧可覆盖，且与模式解耦。
func TestParseArgsKeepsDirectoryOverrides(t *testing.T) {
	opt, err := parseArgs([]string{"-seeds", "/tmp/seeds", "-schema", "/tmp/schema", "seed"})
	if err != nil {
		t.Fatalf("目录覆盖解析失败: %v", err)
	}
	if opt.mode != modeSeed || opt.seedDir != "/tmp/seeds" || opt.schemaDir != "/tmp/schema" {
		t.Fatalf("解析结果不对: %+v", opt)
	}
}

// flag.ContinueOnError 自己会把「错误 + 用法」打到 stderr；main 再 fail 一次会让同一个
// 错误在终端里出现两遍。这条用例钉住「被打包成 alreadyReported 而不是裸 err」。
func TestParseArgsFlagErrorIsMarkedAlreadyReported(t *testing.T) {
	for _, argv := range [][]string{{"-bogusflag"}, {"-mode"}} {
		_, err := parseArgs(argv)
		if err == nil {
			t.Fatalf("%v 应当报错", argv)
		}
		var reported alreadyReported
		if !errors.As(err, &reported) {
			t.Fatalf("%v 的错误应当被标记成 alreadyReported（否则会被打印两遍），实际: %T %v", argv, err, err)
		}
		if errors.Is(err, flag.ErrHelp) {
			t.Fatalf("%v 是选项写错，不是请求帮助", argv)
		}
	}
}

// 求帮助与顺序无关：写在哪里都该看到用法，而不是「选项必须写在模式之前」。
func TestParseArgsHelpAfterModeIsHelp(t *testing.T) {
	for _, argv := range [][]string{{"status", "-h"}, {"up", "--help"}, {"-h"}} {
		if _, err := parseArgs(argv); !errors.Is(err, flag.ErrHelp) {
			t.Fatalf("%v 应当返回 flag.ErrHelp，实际: %v", argv, err)
		}
	}
}

// `--` 是选项结束符，不是「写错位置的选项」。
func TestParseArgsDoubleDashIsNotMisplacedFlag(t *testing.T) {
	if _, err := parseArgs([]string{"status", "--"}); err != nil {
		t.Fatalf("\"status --\" 不该报错，实际: %v", err)
	}
	opt, err := parseArgs([]string{"--", "status"})
	if err != nil {
		t.Fatalf("\"-- status\" 应当被接受，实际: %v", err)
	}
	if opt.mode != modeStatus {
		t.Fatalf("mode = %q，想要 status", opt.mode)
	}
}
