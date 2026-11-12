package main

// 这一组用例盯着 run.go 里那两处：分派（选哪个子命令）与「同一个错误打几遍」。
//
// 它们此前一条都够不着 —— main 里直接 os.Exit，分派写在 main 的 switch 里。
// 盲审对 PR #411 做了两次变异，两次都证明「改坏了也没人红」：
//
//  1. 把 case modeStatus 下的 status(...) 换成 applyUp(...) —— 也就是把「查状态」
//     接到「推迁移」上，正是 2026-11-12 那次事故的形状。13 条用例全绿。
//  2. 把 main 里 errors.As 的早退改成 && false —— 二进制上同一个错误又打两遍。13 条全绿。
//
// 下面每一条都对应上面一个变异：改坏了，恰好一条变红。

import (
	"bytes"
	"context"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

// recorder 记录三个子命令各自被调了几次。断言「只有该跑的那个跑了」，
// 而不是「参数传对了」—— 后者在参数层已经有 13 条用例管着。
type recorder struct {
	status, up, seed int
	// 记下最后一次收到的目录参数，用来验证分派把 opt 的字段接对了。
	gotSchema, gotMigrate, gotSeedDir string
}

func (r *recorder) subs() subcommands {
	return subcommands{
		status: func(_ context.Context, _ *pgx.Conn, schemaDir, migrateDir string) {
			r.status++
			r.gotSchema, r.gotMigrate = schemaDir, migrateDir
		},
		up: func(_ context.Context, _ *pgx.Conn, schemaDir, migrateDir string) {
			r.up++
			r.gotSchema, r.gotMigrate = schemaDir, migrateDir
		},
		seed: func(_ context.Context, _ *pgx.Conn, seedDir string) {
			r.seed++
			r.gotSeedDir = seedDir
		},
	}
}

// 事故现场的同一种调用：查状态。它必须**只**调到 status。
// 把 run 里 case modeStatus 的实现换成 up，这条就红。
func TestRunStatusDispatchesOnlyStatus(t *testing.T) {
	rec := &recorder{}
	code := run(context.Background(), nil, options{mode: modeStatus, schemaDir: "S", migrateDir: "M"}, &bytes.Buffer{}, rec.subs())
	if code != 0 {
		t.Fatalf("status 正常结束应当返回 0，实际 %d", code)
	}
	if rec.status != 1 || rec.up != 0 || rec.seed != 0 {
		t.Fatalf("status 模式只该调 status，实际 status=%d up=%d seed=%d —— up 被调到就是把只读命令接成了推迁移", rec.status, rec.up, rec.seed)
	}
	if rec.gotSchema != "S" || rec.gotMigrate != "M" {
		t.Fatalf("目录参数没接过去：schema=%q migrate=%q", rec.gotSchema, rec.gotMigrate)
	}
}

func TestRunUpDispatchesOnlyUp(t *testing.T) {
	rec := &recorder{}
	if code := run(context.Background(), nil, options{mode: modeUp}, &bytes.Buffer{}, rec.subs()); code != 0 {
		t.Fatalf("up 正常结束应当返回 0，实际 %d", code)
	}
	if rec.up != 1 || rec.status != 0 || rec.seed != 0 {
		t.Fatalf("up 模式只该调 up，实际 status=%d up=%d seed=%d", rec.status, rec.up, rec.seed)
	}
}

func TestRunSeedDispatchesOnlySeed(t *testing.T) {
	rec := &recorder{}
	if code := run(context.Background(), nil, options{mode: modeSeed, seedDir: "D"}, &bytes.Buffer{}, rec.subs()); code != 0 {
		t.Fatalf("seed 正常结束应当返回 0，实际 %d", code)
	}
	if rec.seed != 1 || rec.status != 0 || rec.up != 0 {
		t.Fatalf("seed 模式只该调 seed，实际 status=%d up=%d seed=%d", rec.status, rec.up, rec.seed)
	}
	if rec.gotSeedDir != "D" {
		t.Fatalf("种子目录没接过去：%q", rec.gotSeedDir)
	}
}

// 解析层不会产出未知模式，但 run 是导出的分岔点，直接传进来时必须拒绝 —— 且**不能**跑任何一个子命令。
func TestRunUnknownModeDispatchesNothing(t *testing.T) {
	rec := &recorder{}
	var errBuf bytes.Buffer
	code := run(context.Background(), nil, options{mode: "down"}, &errBuf, rec.subs())
	if code == 0 {
		t.Fatal("未知模式应当返回非零退出码")
	}
	if rec.status+rec.up+rec.seed != 0 {
		t.Fatalf("未知模式不该跑任何一个子命令，实际 status=%d up=%d seed=%d", rec.status, rec.up, rec.seed)
	}
	if !strings.Contains(errBuf.String(), "未知 mode") {
		t.Fatalf("未知模式要打到 stderr，实际: %q", errBuf.String())
	}
}

// 「同一个错误只打印一次」的下半截：flag 打一遍，早退分支不该再打一遍。
//
// 这条用例看的是 cli，不是 parseArgs —— 因为决定打几遍的从来不是解析结果，
// 而是 main 里那个 errors.As 早退。把那四行改成 && false，这条就红（错误出现两次）。
func TestCLIParseErrorIsPrintedOnce(t *testing.T) {
	for _, argv := range [][]string{{"-bogusflag"}, {"-mode"}} {
		var buf bytes.Buffer
		code := cli(argv, &buf)
		if code == 0 {
			t.Fatalf("%v 是选项写错，应当返回非零退出码", argv)
		}
		out := buf.String()
		// flag.ContinueOnError 的固定前缀；出现两次就说明早退分支没起作用。
		if n := strings.Count(out, "flag provided but not defined"); n > 1 {
			t.Fatalf("%v 的错误在 stderr 里出现了 %d 次，应当只有 1 次（stderr: %q）", argv, n, out)
		}
		if argv[0] == "-bogusflag" && !strings.Contains(out, "flag provided but not defined") {
			t.Fatalf("-bogusflag 的错误没被打印出来，stderr: %q", out)
		}
		if argv[0] == "-mode" && !strings.Contains(out, "flag needs an argument") {
			t.Fatalf("-mode 缺值的错误没被打印出来，stderr: %q", out)
		}
	}
}

// 解析层自己的错误（不是 flag 抛的）也只该出现一次：它们走的是 fmt.Fprintf 那一支。
func TestCLIResolveErrorIsPrintedOnce(t *testing.T) {
	var buf bytes.Buffer
	if code := cli([]string{"status", "up"}, &buf); code == 0 {
		t.Fatal("两个位置参数应当返回非零退出码")
	}
	if n := strings.Count(buf.String(), "最多一个模式参数"); n != 1 {
		t.Fatalf("错误出现了 %d 次，应当正好 1 次（stderr: %q）", n, buf.String())
	}
}

// -h 不是失败：打出用法后返回 0，且不碰数据库（不设 DATABASE_URL 也不该报「未设置」）。
func TestCLIHelpExitsZeroWithoutDatabase(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	var buf bytes.Buffer
	if code := cli([]string{"-h"}, &buf); code != 0 {
		t.Fatalf("-h 应当返回 0，实际 %d（stderr: %q）", code, buf.String())
	}
	if strings.Contains(buf.String(), "DATABASE_URL") {
		t.Fatalf("求帮助不该走到连库那一步，stderr: %q", buf.String())
	}
}

// 参数解析通过、但没有 DSN：应当在连库之前就报清楚，而不是拿空串去连。
func TestCLIReportsMissingDatabaseURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	var buf bytes.Buffer
	if code := cli([]string{"status"}, &buf); code == 0 {
		t.Fatal("没有 DATABASE_URL 应当返回非零退出码")
	}
	if !strings.Contains(buf.String(), "DATABASE_URL 未设置") {
		t.Fatalf("要明确报出 DATABASE_URL 未设置，实际: %q", buf.String())
	}
}

// production 那一行接线必须被看着。
//
// 上面九条用例里，四条 `TestRun*` 都是「注入假的 subcommands 再断言选了谁」，于是 run.go 里
//
//	var production = subcommands{status: status, up: applyUp, seed: applySeeds}
//
// 这一行**没有任何用例经过**。盲审 PR #416 正是从这一行复现了事故：
//
//	production = subcommands{status: applyUp, up: applyUp, seed: applySeeds}
//	→ 21/21 全绿，而 status 模式真的在推迁移
//
// 这不是「理论上可能」：status 与 up 是三个字段里**唯一签名相同**的一对
// （seed 只收一个参数，接错会编译失败），也就是说唯一能悄悄改错的方式，
// 恰好就是 2026-11-12 那次事故的形状。所以这一条直接比函数地址指向谁，
// 而不是再走一遍 run —— 走 run 只能证明 switch 对，证明不了接线对。
func TestProductionWiringIsNotSwapped(t *testing.T) {
	name := func(fn any) string {
		return runtime.FuncForPC(reflect.ValueOf(fn).Pointer()).Name()
	}
	cases := []struct {
		field string
		got   any
		want  string
	}{
		{"status", production.status, ".status"},
		{"up", production.up, ".applyUp"},
		{"seed", production.seed, ".applySeeds"},
	}
	for _, c := range cases {
		if !strings.HasSuffix(name(c.got), c.want) {
			t.Errorf("production.%s 接的是 %s，应当是 %s", c.field, name(c.got), c.want)
		}
	}
}
