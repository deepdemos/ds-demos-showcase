// 迁移执行器：把 db/schema 基线 + db/migrations 增量应用到真库。
//
// 用法（在 backend/ 目录下）：
//
//	DATABASE_URL=postgres://... go run ./cmd/migrate status
//	DATABASE_URL=postgres://... go run ./cmd/migrate up
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
	var (
		mode       = flag.String("mode", "up", "up | status")
		schemaDir  = flag.String("schema", "../db/schema", "冻结基线目录")
		migrateDir = flag.String("migrations", "../db/migrations", "增量迁移目录")
	)
	flag.Parse()

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

	switch *mode {
	case "status":
		status(ctx, conn, *schemaDir, *migrateDir)
	case "up":
		applyUp(ctx, conn, *schemaDir, *migrateDir)
	default:
		fail("未知 mode %q（只支持 up / status）", *mode)
	}
}

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

// ledgerExists 探测 schema_migrations 在不在，不建表。
func ledgerExists(ctx context.Context, conn *pgx.Conn) (bool, error) {
	var ok bool
	err := conn.QueryRow(ctx, `SELECT to_regclass('public.schema_migrations') IS NOT NULL`).Scan(&ok)
	return ok, err
}
