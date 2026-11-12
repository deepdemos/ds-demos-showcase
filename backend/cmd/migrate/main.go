// 迁移执行器：把 db/schema 基线 + db/migrations 增量应用到真库。
//
// 用法（在 backend/ 目录下）：
//
//	DATABASE_URL=postgres://... go run ./cmd/migrate status
//	DATABASE_URL=postgres://... go run ./cmd/migrate up
//	DATABASE_URL=postgres://... go run ./cmd/migrate seed
//
// 模式写在位置参数上（上面三行就是推荐写法）；`-mode status` 这种标志写法也认，
// 但两者同时给出时必须说同一件事，矛盾直接报错。
//
// **没有默认模式**：一个都不写就是错误，不会替你选 up。见 resolveMode 的注释。
//
// 为什么用 Go 写而不是 node 脚本：这里已经有 pgx，而 node 侧唯一的依赖是 WASM 版 PGlite
// （连不了真库）。再加一个 `pg` 依赖只为了跑迁移，不划算。
//
// 两段式历史（为什么不是一条链）：
//
//	db/schema/*.sql   —— **冻结基线**。它等价于「0000 号迁移」，永远不再改。
//	db/migrations/    —— 基线之后的所有变化，只增不改。
//
// 把基线也拆成 10 条迁移是一次纯改名，会波及 103 处文档引用；冻结基线能达到同样的效果，
// 代价是接受「历史开头是一份快照」——这在真实项目里是常规做法（squash 到某个点为止）。
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	baselineVersion = 0
	// advisoryLockKey 只要全仓用同一个值，两个迁移器就会互斥。数值本身没有含义。
	advisoryLockKey = 4815162342
)

func main() {
	opt, err := parseArgs(os.Args[1:])
	switch {
	case errors.Is(err, flag.ErrHelp):
		// flag 已经把选项打出来了，正常退出。
		return
	case err != nil:
		var reported alreadyReported
		if errors.As(err, &reported) {
			// flag.ContinueOnError 自己已经把「错误 + 用法」打到 stderr 了，
			// 这里再打一遍会让同一个错误在终端里出现两次。
			os.Exit(1)
		}
		fail("%v", err)
	}

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		fail("DATABASE_URL 未设置")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		fail("连接数据库失败: %v", err)
	}
	defer conn.Close(context.Background())

	switch opt.mode {
	case modeStatus:
		status(ctx, conn, opt.schemaDir, opt.migrateDir)
	case modeUp:
		applyUp(ctx, conn, opt.schemaDir, opt.migrateDir)
	case modeSeed:
		applySeeds(ctx, conn, opt.seedDir)
	default:
		fail("未知 mode %q（只支持 up / status / seed）", opt.mode)
	}
}

// options 是解析完的命令行。字段与顺序无关，只看内容。
type options struct {
	mode       string
	schemaDir  string
	migrateDir string
	seedDir    string
}

const (
	modeUp     = "up"
	modeStatus = "status"
	modeSeed   = "seed"
)

// resolveMode 决定这次跑哪个模式。这是本命令唯一会「改库」的分岔点，所以规则写得比需要的更死：
//
//  1. 位置参数与 -mode 都认，但**必须一致**；不一致直接报错。原先只读 -mode，位置参数被
//     flag.Parse 静默丢掉 —— `migrate status` 于是按默认值 up 执行，一次「查一下跑到第几版」
//     就变成了推迁移。（本仓库的 package.json / README / 容器 entrypoint 都写过这种调用。）
//  2. 一个都不给 = 错误，**没有默认模式**。默认值只在「省略也无害」时才安全；这里省略的代价
//     是 DDL 已经落库、回滚要自己写反向 SQL。宁可打一行用法，也不替人挑一个模式。
func resolveMode(positional []string, flagMode string) (string, error) {
	if len(positional) > 1 {
		return "", fmt.Errorf("最多一个模式参数，收到 %d 个：%v", len(positional), positional)
	}
	var pos string
	if len(positional) == 1 {
		pos = positional[0]
	}
	switch {
	case pos != "" && flagMode != "":
		if pos != flagMode {
			return "", fmt.Errorf("模式参数矛盾：位置参数是 %q，-mode 是 %q", pos, flagMode)
		}
		return pos, nil
	case pos != "":
		return pos, nil
	case flagMode != "":
		return flagMode, nil
	default:
		return "", errors.New("必须给出模式：migrate <up | status | seed>，例如 migrate status")
	}
}

// parseArgs 解析命令行。抽成函数是为了能测：main 里的 os.Exit 让参数规则无法断言，
// 而这次的缺陷恰好就出在参数上。
func parseArgs(argv []string) (options, error) {
	fs := flag.NewFlagSet("migrate", flag.ContinueOnError)
	flagMode := fs.String("mode", "", "up | status | seed（与位置参数等价；两者都给时必须一致）")
	schemaDir := fs.String("schema", "../db/schema", "冻结基线目录")
	migrateDir := fs.String("migrations", "../db/migrations", "增量迁移目录")
	seedDir := fs.String("seeds", "../db/seeds", "开发种子目录")
	if err := fs.Parse(argv); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return options{}, err
		}
		return options{}, alreadyReported{err}
	}
	// flag 解析到**第一个非选项**就停：`migrate seed -seeds X` 里的 -seeds 根本不会被看见，
	// 于是它既不生效也不报错。静默忽略正是这次事故的形状，所以这里明确要求选项写在前面。
	//
	// `--` 是选项结束符，不占位置参数的名额：它出现在模式后面（此时早就不再解析选项了）
	// 不该被当成「选项写错位置」——那会让人以为自己拼错了什么。
	positional := make([]string, 0, len(fs.Args()))
	for _, tok := range fs.Args() {
		switch {
		case tok == "--":
			continue
		case tok == "-h" || tok == "-help" || tok == "--help":
			// 求帮助的意图与顺序无关：写在哪里都该看到用法。
			fs.Usage()
			return options{}, flag.ErrHelp
		case strings.HasPrefix(tok, "-"):
			return options{}, fmt.Errorf("选项必须写在模式之前：`migrate -seeds <目录> seed`，不是 `migrate seed -seeds <目录>`（看到 %q）", tok)
		}
		positional = append(positional, tok)
	}
	mode, err := resolveMode(positional, *flagMode)
	if err != nil {
		return options{}, err
	}
	switch mode {
	case modeUp, modeStatus, modeSeed:
	default:
		return options{}, fmt.Errorf("未知 mode %q（只支持 up / status / seed）", mode)
	}
	return options{mode: mode, schemaDir: *schemaDir, migrateDir: *migrateDir, seedDir: *seedDir}, nil
}

// alreadyReported 标记「flag 包自己已经把错误和用法打到 stderr 了」。
// 用它把「再打一遍」变成显式选择，而不是靠调用方记着别调 fail。
type alreadyReported struct{ err error }

func (e alreadyReported) Error() string { return e.err.Error() }

func (e alreadyReported) Unwrap() error { return e.err }

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}

// schema_migrations 是「已经跑到第几版」的唯一记录。
// checksum 存文件内容的 sha256：改了已合入的迁移，第二次运行时会在这里对不上。
func ensureLedger(ctx context.Context, conn *pgx.Conn) error {
	_, err := conn.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version    bigint      PRIMARY KEY,
			name       text        NOT NULL,
			checksum   text        NOT NULL,
			applied_at timestamptz NOT NULL DEFAULT now()
		)`)
	return err
}

type migration struct {
	version  int64
	name     string
	path     string
	checksum string
}

func listMigrations(dir string) ([]migration, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("读迁移目录失败: %w", err)
	}
	var out []migration
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".sql") {
			continue
		}
		// 文件名约定：NNNN_名称.sql
		parts := strings.SplitN(name, "_", 2)
		if len(parts) != 2 || len(parts[0]) != 4 {
			return nil, fmt.Errorf("迁移文件名必须是 NNNN_名称.sql：%s", name)
		}
		n, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("迁移文件名前缀不是数字：%s", name)
		}
		sum, err := fileChecksum(filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
		out = append(out, migration{version: n, name: name, path: filepath.Join(dir, name), checksum: sum})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].version < out[j].version })
	return out, nil
}

func applied(ctx context.Context, conn *pgx.Conn) (map[int64]string, error) {
	rows, err := conn.Query(ctx, `SELECT version, checksum FROM schema_migrations`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]string{}
	for rows.Next() {
		var v int64
		var sum string
		if err := rows.Scan(&v, &sum); err != nil {
			return nil, err
		}
		out[v] = sum
	}
	return out, rows.Err()
}

func status(ctx context.Context, conn *pgx.Conn, schemaDir, migrateDir string) {
	// status 是只读命令：库还没建过时不该顺手建表。
	exists, err := ledgerExists(ctx, conn)
	if err != nil {
		fail("查 schema_migrations 失败: %v", err)
	}
	done := map[int64]string{}
	if exists {
		if done, err = applied(ctx, conn); err != nil {
			fail("读 schema_migrations 失败: %v", err)
		}
	}
	migs, err := listMigrations(migrateDir)
	if err != nil {
		fail("%v", err)
	}

	if _, ok := done[baselineVersion]; ok {
		fmt.Printf("基线（%s）：已应用\n", schemaDir)
	} else {
		fmt.Printf("基线（%s）：**未应用**\n", schemaDir)
	}
	for _, m := range migs {
		switch sum, ok := done[m.version]; {
		case !ok:
			fmt.Printf("  %04d %-40s 待应用\n", m.version, m.name)
		case sum != m.checksum:
			fmt.Printf("  %04d %-40s **校验和不一致**（文件被改过）\n", m.version, m.name)
		default:
			fmt.Printf("  %04d %-40s 已应用\n", m.version, m.name)
		}
	}
}

func applyUp(ctx context.Context, conn *pgx.Conn, schemaDir, migrateDir string) {
	if err := ensureLedger(ctx, conn); err != nil {
		fail("建 schema_migrations 失败: %v", err)
	}
	// 同一时刻只允许一个迁移器在跑。没有这把锁时，两个 up 会把同一条迁移各跑一遍：
	// 第二个在 INSERT 版本行时才撞主键，而那时结构已经改过一次了。
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, advisoryLockKey); err != nil {
		fail("拿迁移锁失败: %v", err)
	}
	defer func() {
		_, _ = conn.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, advisoryLockKey)
	}()

	done, err := applied(ctx, conn)
	if err != nil {
		fail("读 schema_migrations 失败: %v", err)
	}

	// 第一步：冻结基线。只在全新的库上跑一次。
	if _, ok := done[baselineVersion]; !ok {
		base, err := listBaseline(schemaDir)
		if err != nil {
			fail("%v", err)
		}
		if len(base) == 0 {
			fail("基线目录 %s 里没有 .sql", schemaDir)
		}
		fmt.Printf("应用基线（%d 个文件）…\n", len(base))
		if err := applyBaseline(ctx, conn, schemaDir, base); err != nil {
			fail("基线失败: %v", err)
		}
	} else {
		fmt.Println("基线已应用，跳过")
	}

	// 第二步：增量迁移，按序、每条一个事务。
	migs, err := listMigrations(migrateDir)
	if err != nil {
		fail("%v", err)
	}
	pending := 0
	for _, m := range migs {
		if sum, ok := done[m.version]; ok {
			if sum != m.checksum {
				fail("%04d %s 的校验和与已应用的不一致——已合入的迁移不能改，请新增一条", m.version, m.name)
			}
			continue
		}
		pending++
		fmt.Printf("应用 %04d %s … ", m.version, m.name)
		if err := applyOne(ctx, conn, m); err != nil {
			fail("失败: %v", err)
		}
		fmt.Println("ok")
	}
	if pending == 0 {
		fmt.Println("没有待应用的迁移")
	}
}

// applyOne 把一条迁移和它的登记放在同一个事务里：
// 要么全成，要么全不成 —— 不会出现「改了结构但没登记」的半截状态。
func applyOne(ctx context.Context, conn *pgx.Conn, m migration) error {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())

	body, err := os.ReadFile(m.path)
	if err != nil {
		return err
	}
	// 无参数的 Exec 走简单协议，可以一次执行多条语句。
	if _, err := tx.Exec(ctx, string(body)); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO schema_migrations (version, name, checksum) VALUES ($1, $2, $3)`,
		m.version, m.name, m.checksum); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// applyBaseline 把冻结基线整体放进一个事务。
//
// 不逐文件自动提交的理由：10 个文件里第 5 个失败时，前 4 个已经提交、而 schema_migrations
// 里没有 version=0 —— 下次重跑会在已存在的表上 CREATE TABLE，库就卡在既跑不下去、
// 也回不去的状态。整体一个事务，失败即全部回滚，重跑仍然干净。
//
// 前提是基线里没有 CREATE INDEX CONCURRENTLY —— 那类语句不能进事务。当前基线没有。
func applyBaseline(ctx context.Context, conn *pgx.Conn, schemaDir string, base []migration) error {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())

	for _, m := range base {
		body, err := os.ReadFile(m.path)
		if err != nil {
			return fmt.Errorf("%s: %w", m.name, err)
		}
		// 无参数的 Exec 走简单协议，可以一次执行多条语句。
		if _, err := tx.Exec(ctx, string(body)); err != nil {
			return fmt.Errorf("%s: %w", m.name, err)
		}
	}
	sum, err := dirChecksum(schemaDir)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO schema_migrations (version, name, checksum) VALUES ($1, $2, $3)`,
		baselineVersion, "baseline(db/schema)", sum); err != nil {
		return fmt.Errorf("登记基线: %w", err)
	}
	return tx.Commit(ctx)
}

// applySeeds 把开发种子灌进去。**不进迁移链**：种子属于开发环境，不该被 migrate up
// 带进任何真实库。因为种子里全部用 ON CONFLICT DO UPDATE，重复跑是幂等的。
//
// 它不建 schema_migrations、也不检查迁移跑到第几版——种子只关心「表在不在」，
// 不在就让 SQL 自己报错（错误信息比这里自己编的清楚）。
func applySeeds(ctx context.Context, conn *pgx.Conn, seedDir string) {
	entries, err := os.ReadDir(seedDir)
	if err != nil {
		fail("读种子目录失败: %v", err)
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	if len(names) == 0 {
		fail("种子目录 %s 里没有 .sql", seedDir)
	}
	sort.Strings(names)

	for _, name := range names {
		path := filepath.Join(seedDir, name)
		body, err := os.ReadFile(path)
		if err != nil {
			fail("读 %s 失败: %v", name, err)
		}
		tx, err := conn.Begin(ctx)
		if err != nil {
			fail("%s: %v", name, err)
		}
		// 无参数的 Exec 走简单协议，可以一次执行多条语句。
		if _, err := tx.Exec(ctx, string(body)); err != nil {
			_ = tx.Rollback(context.Background())
			fail("%s 失败: %v", name, err)
		}
		if err := tx.Commit(ctx); err != nil {
			fail("%s 提交失败: %v", name, err)
		}
		fmt.Println("种子", name, "ok")
	}
	fmt.Println("种子应用完成（开发数据，勿用于生产）")
}

// ledgerExists 探测 schema_migrations 在不在，不建表。
func ledgerExists(ctx context.Context, conn *pgx.Conn) (bool, error) {
	var ok bool
	err := conn.QueryRow(ctx, `SELECT to_regclass('public.schema_migrations') IS NOT NULL`).Scan(&ok)
	return ok, err
}
