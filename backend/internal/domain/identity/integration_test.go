package identity

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/kernel/verification"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

// 身份域的集成测试：连真库跑，验证「第②层限流 + 邮箱验证码」整条链路。
// 默认跳过（TEST_DATABASE_URL 未设置时）。
func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL 未设置，跳过集成测试（CI 里会设）")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("连接测试库失败: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func testService(t *testing.T, pool *pgxpool.Pool) *Service {
	t.Helper()
	q := store.New(pool)
	return New(pool, []byte("test-ip-secret"), verification.New(q, []byte("test-verification-secret")))
}

// mkUser 建一个唯一用户，返回邮箱、用户名、密码。测试库里每轮都清，所以直接建。
func mkUser(t *testing.T, s *Service, tag string) (email, username string) {
	t.Helper()
	email = fmt.Sprintf("it-%s@example.com", tag)
	username = fmt.Sprintf("it-%s", tag)
	_, _, err := s.Register(context.Background(), RegisterInput{
		Email:       email,
		Password:    "correct-horse-battery",
		Username:    username,
		DisplayName: "集成测试",
	})
	if err != nil {
		t.Fatalf("建用户失败: %v", err)
	}
	t.Cleanup(func() {
		// 删账号会级联删 profiles / emails / credentials / sessions；
		// login_attempts 与 verification_codes 没有指向 accounts 的外键，单独清。
		//
		// 这里必须真的删：不删的话第二次跑同一组用例就会撞 uq_profiles_username，
		// 而失败信息是「用户名已被使用」，看起来像业务 bug 而不是测试脏数据。
		_, _ = s.pool.Exec(context.Background(),
			"DELETE FROM accounts WHERE id IN (SELECT account_id FROM profiles WHERE lower(username) = lower($1))",
			username)
		_, _ = s.pool.Exec(context.Background(),
			"DELETE FROM login_attempts WHERE identifier_hash IS NOT NULL")
		_, _ = s.pool.Exec(context.Background(),
			"DELETE FROM verification_codes WHERE purpose LIKE 'login_challenge'")
	})
	return email, username
}

// 失败够 5 次之后，即使密码正确也必须带验证码。
func TestIntegrationSecondLayerRequiresEmailCode(t *testing.T) {
	pool := testPool(t)
	s := testService(t, pool)
	email, _ := mkUser(t, s, "layer2")
	const pw = "correct-horse-battery"
	const ip = "10.9.9.1"

	// 前 4 次密码错：仍是普通 401。
	for i := 0; i < 4; i++ {
		_, _, err := s.Login(context.Background(), LoginInput{Identifier: email, Password: "wrong", IP: ip})
		if !errors.Is(err, ErrUnauthorized) {
			t.Fatalf("第 %d 次应 ErrUnauthorized，实际 %v", i+1, err)
		}
	}
	// 第 5 次仍然 401（这一步把计数推过阈值）。
	_, _, _ = s.Login(context.Background(), LoginInput{Identifier: email, Password: "wrong", IP: ip})

	// 第 6 次密码正确，但没有验证码 → 必须要求验证码。
	_, _, err := s.Login(context.Background(), LoginInput{Identifier: email, Password: pw, IP: ip})
	if !errors.Is(err, ErrVerificationRequired) {
		t.Fatalf("应 ErrVerificationRequired，实际 %v", err)
	}

	// 申请验证码（这里直接调领域方法拿明文码，生产里走邮件投递）。
	delivery, err := s.RequestEmailCode(context.Background(), email, "203.0.113.9")
	if err != nil {
		t.Fatalf("RequestEmailCode: %v", err)
	}
	if delivery.Code == "" || delivery.To != email {
		t.Fatalf("投递信息不对: %+v", delivery)
	}

	// 带上正确的码就能登录。
	_, sessionID, err := s.Login(context.Background(), LoginInput{
		Identifier: email, Password: pw, EmailCode: delivery.Code, IP: ip,
	})
	if err != nil {
		t.Fatalf("带正确验证码应登录成功: %v", err)
	}
	if sessionID == "" {
		t.Fatal("没有下发会话")
	}
}

// 码错一次就换一个 401，且不告诉对方是哪一种错。
func TestIntegration验证码错就是验不过(t *testing.T) {
	pool := testPool(t)
	s := testService(t, pool)
	email, _ := mkUser(t, s, "badcode")
	const ip = "10.9.9.2"

	for i := 0; i < 5; i++ {
		_, _, _ = s.Login(context.Background(), LoginInput{Identifier: email, Password: "wrong", IP: ip})
	}
	if _, err := s.RequestEmailCode(context.Background(), email, "203.0.113.9"); err != nil {
		t.Fatalf("RequestEmailCode: %v", err)
	}

	_, _, err := s.Login(context.Background(), LoginInput{
		Identifier: email, Password: "correct-horse-battery", EmailCode: "000000", IP: ip,
	})
	if !errors.Is(err, ErrVerificationRequired) {
		t.Fatalf("码错应 ErrVerificationRequired，实际 %v", err)
	}
}

// 账号不存在时也返回同样的 204 语义（Code 有值但 To 为空）。
func TestIntegration不存在的账号也发码但不下发(t *testing.T) {
	pool := testPool(t)
	s := testService(t, pool)

	delivery, err := s.RequestEmailCode(context.Background(), "nobody-here@example.com", "203.0.113.9")
	if err != nil {
		t.Fatalf("不存在的账号不该报错: %v", err)
	}
	if delivery.Code == "" {
		t.Fatal("应当照样生成验证码（保持响应形状一致）")
	}
	if delivery.To != "" {
		t.Fatalf("不存在的账号不该有收件地址，实际 %q", delivery.To)
	}
}

// 重置密码：验证码换密码，并把重置之前签发的会话全部作废。
func TestIntegrationPasswordReset(t *testing.T) {
	pool := testPool(t)
	s := testService(t, pool)
	email, _ := mkUser(t, s, "reset")
	const ip = "10.9.9.3"

	// 先登录一次，拿到一个「旧会话」。
	_, oldSession, err := s.Login(context.Background(), LoginInput{
		Identifier: email, Password: "correct-horse-battery", IP: ip,
	})
	if err != nil {
		t.Fatalf("登录失败: %v", err)
	}
	if _, err := s.Me(context.Background(), oldSession); err != nil {
		t.Fatalf("旧会话应有效: %v", err)
	}

	delivery, err := s.RequestPasswordResetCode(context.Background(), email, "203.0.113.9")
	if err != nil {
		t.Fatalf("RequestPasswordResetCode: %v", err)
	}
	if delivery.Code == "" || delivery.To != email {
		t.Fatalf("投递信息不对: %+v", delivery)
	}

	const newPassword = "another-correct-horse"
	if err := s.ResetPassword(context.Background(), email, delivery.Code, newPassword); err != nil {
		t.Fatalf("重置失败: %v", err)
	}

	// 旧会话必须已经失效——只改哈希不踢会话等于没改。
	if _, err := s.Me(context.Background(), oldSession); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("重置后旧会话应失效，实际 %v", err)
	}

	// 旧密码登不上，新密码能登上。
	if _, _, err := s.Login(context.Background(), LoginInput{Identifier: email, Password: "correct-horse-battery", IP: ip}); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("旧密码应失效，实际 %v", err)
	}
	if _, _, err := s.Login(context.Background(), LoginInput{Identifier: email, Password: newPassword, IP: ip}); err != nil {
		t.Fatalf("新密码应能登录: %v", err)
	}

	// 同一个码不能用第二次。
	if err := s.ResetPassword(context.Background(), email, delivery.Code, newPassword); !errors.Is(err, ErrResetCodeInvalid) {
		t.Fatalf("验证码应一次性，实际 %v", err)
	}
}

// 登录验证码不能拿来重置密码（purpose 不同，码不同）。
func TestIntegrationCrossPurposeCodeRejected(t *testing.T) {
	pool := testPool(t)
	s := testService(t, pool)
	email, _ := mkUser(t, s, "xpurpose")

	loginCode, err := s.RequestEmailCode(context.Background(), email, "203.0.113.9")
	if err != nil {
		t.Fatalf("RequestEmailCode: %v", err)
	}
	if err := s.ResetPassword(context.Background(), email, loginCode.Code, "another-correct-horse"); !errors.Is(err, ErrResetCodeInvalid) {
		t.Fatalf("登录验证码不该能重置密码，实际 %v", err)
	}
}

// 封禁账号：密码对也不能进站，而且**已经签发**的会话立刻失效。
//
// web_sessions.status 是签发时的快照，封禁不会回头改它——所以放行与否必须读
// accounts 的当前值。只改登录判定是不够的：在线的人手里那条 cookie 仍然有效。
func TestIntegrationBannedAccount(t *testing.T) {
	pool := testPool(t)
	s := testService(t, pool)
	email, username := mkUser(t, s, "banned")

	_, sessionID, err := s.Login(context.Background(), LoginInput{
		Identifier: email, Password: "correct-horse-battery", IP: "203.0.113.11",
	})
	if err != nil {
		t.Fatalf("封禁前应能登录: %v", err)
	}
	if _, err := s.Me(context.Background(), sessionID); err != nil {
		t.Fatalf("封禁前会话应有效: %v", err)
	}

	if _, err := pool.Exec(context.Background(),
		"UPDATE accounts SET previous_status=status, status='banned' WHERE id IN (SELECT account_id FROM profiles WHERE lower(username)=lower($1))",
		username); err != nil {
		t.Fatalf("设置封禁失败: %v", err)
	}

	if _, err := s.Me(context.Background(), sessionID); !errors.Is(err, ErrAccountBanned) {
		t.Fatalf("封禁后旧会话应失效（ErrAccountBanned），实际 %v", err)
	}
	if _, _, err := s.Login(context.Background(), LoginInput{
		Identifier: email, Password: "correct-horse-battery", IP: "203.0.113.11",
	}); !errors.Is(err, ErrAccountBanned) {
		t.Fatalf("封禁账号登录应返回 ErrAccountBanned，实际 %v", err)
	}
}

// 发码有 IP 配额：换着邮箱发也不能无限发，否则一个 IP 就是一台邮件炮台。
//
// kernel 那层只按 (purpose, identifier) 限 1 次/分、5 次/时——每个邮箱都是新额度，
// 所以 IP 维度必须单独有一道。
func TestIntegrationEmailCodeIPQuota(t *testing.T) {
	pool := testPool(t)
	s := testService(t, pool)
	const ip = "203.0.113.42"
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM login_attempts")
		_, _ = pool.Exec(context.Background(), "DELETE FROM verification_codes")
	})

	for i := 0; i < emailCodeIPHourlyLimit; i++ {
		if _, err := s.RequestEmailCode(context.Background(),
			fmt.Sprintf("quota-%d@example.com", i), ip); err != nil {
			t.Fatalf("第 %d 次发码不该被拦: %v", i+1, err)
		}
	}
	if _, err := s.RequestEmailCode(context.Background(), "quota-over@example.com", ip); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("超过 IP 配额应被拦（ErrRateLimited），实际 %v", err)
	}
	// 配额按 IP 计，不是全站计：同一个 IP 打满不影响另一个 IP。
	if _, err := s.RequestEmailCode(context.Background(), "quota-other-ip@example.com", "203.0.113.43"); err != nil {
		t.Fatalf("换 IP 不该被拦: %v", err)
	}
}
