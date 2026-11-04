package httpapi

import (
	"context"
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
VALUES ($1, $2, $3, $4, 'user', 'active', now())`,
		sessionID, acc.Sub, acc.Username, acc.DisplayName); err != nil {
		t.Fatalf("建会话失败: %v", err)
	}
	return acc, sessionID
}
