// 「拿到连接之后做什么」与「解析失败时打几遍」这两件事单独一个文件。
//
// 为什么拆出来：main.go 里那 10 行 switch 与 main 里那个早退分支，恰好是 2026-11-12
// 那次事故的正中央（「查状态」被接到「推迁移」上、同一个错误打两遍）。拆开之前它们
// 一条用例都够不着 —— os.Exit 让一切都无法断言。拆完之后 main 只剩 os.Exit(cli(...))，
// 其余都能在用例里跑。
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/jackc/pgx/v5"
)

// subcommands 是三个子命令的实现。
//
// 直接写 `switch opt.mode { case modeStatus: status(...) }` 也能跑，但那样用例
// 要么连真库、要么断言不了「选对了哪一个」—— 而这正是事故的形状。
// 收进结构体后，用例可以塞三个只记录调用的假函数，断言 status 模式下**只有** status 被调到。
type subcommands struct {
	status func(ctx context.Context, conn *pgx.Conn, schemaDir, migrateDir string)
	up     func(ctx context.Context, conn *pgx.Conn, schemaDir, migrateDir string)
	seed   func(ctx context.Context, conn *pgx.Conn, seedDir string)
}

// production 是生产用的那一份：三个字段直接指向真正的实现。
var production = subcommands{status: status, up: applyUp, seed: applySeeds}

// run 只负责「按解析出的模式跑哪一个」，返回退出码，不调 os.Exit。
//
// 未知模式原本走 fail()（打印 + os.Exit），现在改成写 stderr 并返回 1 ——
// 这样它也能被用例断言；fail 留给「真的跑起来了但失败了」那些地方。
func run(ctx context.Context, conn *pgx.Conn, opt options, stderr io.Writer, subs subcommands) int {
	switch opt.mode {
	case modeStatus:
		subs.status(ctx, conn, opt.schemaDir, opt.migrateDir)
	case modeUp:
		subs.up(ctx, conn, opt.schemaDir, opt.migrateDir)
	case modeSeed:
		subs.seed(ctx, conn, opt.seedDir)
	default:
		fmt.Fprintf(stderr, "未知 mode %q（只支持 up / status / seed）\n", opt.mode)
		return 1
	}
	return 0
}

// cli 是 main 的全部逻辑，除了 os.Exit 本身：解析参数 →（必要时）连库 → 分派。
//
// 抽出来的唯一理由是「同一个错误只打印一次」这句话此前只写在注释里：用例断言的是
// parseArgs 返回到 alreadyReported，而真正决定不再打第二遍的是下面那四行 errors.As 早退。
// 挪进一个返回 int 的函数之后，用例就能拿 bytes.Buffer 当 stderr 去数次数。
//
// stderr 同时喂给 flag 与本函数自己打的错误 —— 两边必须写同一个地方，
// 否则「打印了几遍」在测试与真机上测的根本不是同一件事。
func cli(argv []string, stderr io.Writer) int {
	opt, err := parseArgsTo(argv, stderr)
	switch {
	case errors.Is(err, flag.ErrHelp):
		// flag 已经把选项打出来了，正常退出。
		return 0
	case err != nil:
		var reported alreadyReported
		if errors.As(err, &reported) {
			// flag.ContinueOnError 自己已经把「错误 + 用法」打到同一个 stderr 了，
			// 这里再打一遍会让同一个错误在终端里出现两次。
			return 1
		}
		fmt.Fprintf(stderr, "%v\n", err)
		return 1
	}

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		fmt.Fprintln(stderr, "DATABASE_URL 未设置")
		return 1
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		fmt.Fprintf(stderr, "连接数据库失败: %v\n", err)
		return 1
	}
	defer conn.Close(context.Background())

	return run(ctx, conn, opt, stderr, production)
}
