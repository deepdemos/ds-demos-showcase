package httpapi

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/identity"
)

// registerVerified 走一遍真实的注册 + 邮箱验证，然后**手工**建一条会话。
//
// 为什么不接着调 `Login`：登录那一层有 IP 与标识符维度的失败计数，几个测试轮着跑
// 会互相吃掉额度，红起来的时候看不出是被限流还是真坏了。这里的目的只是「拿到一个
// 能用的会话」，登录语义本身有它自己的用例（`login_test.go`）。
//
// 为什么不让它停在 `Register`：注册现在写的是 `pending`、不发会话，未验证的账号
// 连投稿都不该能过——那正是这一轮要保证的事，测试里不能用「提权绕过」把它抹掉。
func registerVerified(t *testing.T, pool *pgxpool.Pool, svc *identity.Service, in identity.RegisterInput) (identity.Account, string) {
	t.Helper()
	ctx := context.Background()

	// 先清掉同名/同邮箱的残留。
	//
	// 上一轮如果在中途崩了（`t.Fatalf` 之后 Cleanup 仍会跑，但 `go test -run` 中途被杀就不会），
	// 残留账号会让这一轮红在「用户名已被使用」——看起来像注册坏了，实际是测试没打扫干净。
	for _, q := range []string{
		`SELECT sub FROM accounts WHERE id IN (SELECT account_id FROM profiles WHERE username = $1)`,
		`SELECT sub FROM accounts WHERE id IN (SELECT account_id FROM account_emails WHERE lower(email) = lower($1))`,
	} {
		arg := in.Username
		if strings.Contains(q, "account_emails") {
			arg = in.Email
		}
		rows, err := pool.Query(ctx, q, arg)
		if err != nil {
			t.Fatalf("查残留账号失败: %v", err)
		}
		var subs []string
		for rows.Next() {
			var sub string
			if err := rows.Scan(&sub); err != nil {
				rows.Close()
				t.Fatalf("读残留账号失败: %v", err)
			}
			subs = append(subs, sub)
		}
		rows.Close()
		for _, sub := range subs {
			cleanupAuthor(t, pool, sub)
		}
	}

	// 把该邮箱的验证码限流窗口清零。
	//
	// kernel 按 `(purpose, identifier)` 限「每分钟 1 条」，而 `verification_codes` 没有指向
	// `accounts` 的外键——清库清不掉它。不清的话同一邮箱在这一分钟内跑第二次会红在
	// `verification: 发送太频繁`，看起来像功能坏了，实际是测试自己撞了配额。
	if _, err := pool.Exec(ctx, `DELETE FROM verification_codes WHERE purpose = 'verify_email'`); err != nil {
		t.Fatalf("清验证码失败: %v", err)
	}

	acc, delivery, err := svc.Register(ctx, in)
	if err != nil {
		t.Fatalf("注册失败: %v", err)
	}
	if delivery.Code == "" {
		t.Fatalf("注册没有拿到验证码，无法继续")
	}
	if err := svc.VerifyEmail(ctx, in.Email, delivery.Code); err != nil {
		t.Fatalf("验证邮箱失败: %v", err)
	}

	sessionID := "it-session-" + in.Username
	if _, err := pool.Exec(ctx, `
INSERT INTO web_sessions (id, account_sub, username, display_name, role, status, verified_at)
VALUES ($1, $2, $3, $4, 'user', 'active', now())
ON CONFLICT (id) DO UPDATE SET account_sub = EXCLUDED.account_sub,
                               username = EXCLUDED.username,
                               display_name = EXCLUDED.display_name`,
		sessionID, acc.Sub, acc.Username, acc.DisplayName); err != nil {
		t.Fatalf("建会话失败: %v", err)
	}
	return acc, sessionID
}

// cleanupAuthor 删掉某个账号建过的全部数据。
//
// 顺序是硬性的：`demos.author_id` 与 `demo_revisions.created_by` 都是
// `ON DELETE SET NULL`，直接删 `users` 会先触发一次 `UPDATE demo_revisions SET created_by = NULL`，
// 而那张表有「一旦创建即不可变」的触发器 —— 于是删账号报的是
// `demo_revisions 一旦创建即不可变，只允许改 deleted_at`，看起来像是别处的 bug。
// 所以必须**先把作品整条删掉**（`demo_revisions` 对 `demos` 是 ON DELETE CASCADE）。
//
// 之前两个测试写的是 `DELETE FROM demos WHERE author_sub = $1` —— 表里根本没有这一列，
// 错误被 `_, _ =` 吞掉，作品一直留着，于是「删账号」这一步偶发地在别的测试里炸。
func cleanupAuthor(t *testing.T, pool *pgxpool.Pool, sub string) {
	t.Helper()
	ctx := context.Background()
	for _, q := range []string{
		`DELETE FROM demos WHERE author_id = (SELECT id FROM users WHERE sub = $1)`,
		`DELETE FROM demo_channels WHERE curated_by = (SELECT id FROM users WHERE sub = $1)`,
		`DELETE FROM demo_ratings WHERE user_id = (SELECT id FROM users WHERE sub = $1)`,
		`DELETE FROM users WHERE sub = $1`,
		`DELETE FROM accounts WHERE sub = $1`,
	} {
		if _, err := pool.Exec(ctx, q, sub); err != nil {
			t.Fatalf("清理账号 %s 失败（%s）: %v", sub, q, err)
		}
	}
}
