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
	// 先把同名残留删掉：清理是注册在 `t.Cleanup` 里的，而 `t.Cleanup` 只在清理**注册之后**
	// 失败时才跑。用例在注册前就失败（上一轮的脏数据、限流窗口），残留就会一直躺着，
	// 下一轮撞 `uq_profiles_username` 报「用户名已被使用」——看起来像业务 bug，
	// 实际是测试自己在给自己下绊子。做成幂等：每次建号前先清同名的。
	_, _ = s.pool.Exec(context.Background(),
		"DELETE FROM accounts WHERE id IN (SELECT account_id FROM profiles WHERE lower(username) = lower($1))",
		username)
	_, _ = s.pool.Exec(context.Background(),
		"DELETE FROM account_emails WHERE lower(email) = lower($1)", email)

	// mkUser 是给「测登录/限流/封禁」那些用例用的，它们不关心验证信。
	// 但 Register 每建一个号就发一条验证码，而 kernel 的限流是按 (purpose, identifier)
	// ——同一个邮箱一分钟只能发一条。不同用例的邮箱不同，所以本不该撞；
	// 撞上说明上一轮的残留（测试名变了/被中断）还在窗口里。
	// 所以建号前先把这个邮箱的窗口清干净，让「建用户」这一层不因为脏数据而随机红。
	// 限流键是 `identifierKey(purpose, email)`，与账号行无关；而 `verification_codes`
	// 没有指向 accounts 的外键——上一轮清账号时不会带走码。所以这里按**用途整体清**，
	// 不是按账号清：残留的码会让下一轮「每分钟 1 条」直接命中。
	// 本包内用例串行，各建各的账号，清掉不影响任何还没消费的码（它们都在建号之后才发）。
	_, _ = s.pool.Exec(context.Background(),
		"DELETE FROM verification_codes WHERE purpose = 'verify_email'")

	_, delivery, err := s.Register(context.Background(), RegisterInput{
		Email:       email,
		Password:    "correct-horse-battery",
		Username:    username,
		DisplayName: "集成测试",
	})
	if err != nil {
		t.Fatalf("建用户失败: %v", err)
	}
	// 注册现在只到 `pending`：这些用例要测的是**登录**那一层，所以先把邮箱验证掉，
	// 否则第一次登录就会被 ErrEmailNotVerified 拦下来，看起来像限流坏了。
	//
	// 用注册时那条码，**不再发一条**：kernel 对 (purpose, identifier) 限「每分钟 1 条」，
	// 立刻重发会让整组用例红在「尝试太频繁」上，看起来像限流坏了。
	if err := s.VerifyEmail(context.Background(), email, delivery.Code); err != nil {
		t.Fatalf("建用户时验证邮箱失败: %v", err)
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

// 注册只到 `pending`：不能登录，也不能拿会话。
func TestIntegrationRegisterLeavesAccountPending(t *testing.T) {
	pool := testPool(t)
	s := testService(t, pool)
	email, username, _ := mkPendingUser(t, s, "pending")

	var status string
	var verified bool
	if err := pool.QueryRow(context.Background(), `
SELECT a.status, e.verified_at IS NOT NULL
FROM accounts a JOIN account_emails e ON e.account_id = a.id
WHERE lower(e.email) = lower($1)`, email).Scan(&status, &verified); err != nil {
		t.Fatalf("查账号状态失败: %v", err)
	}
	if status != "pending" {
		t.Fatalf("注册后状态 = %q，想要 pending", status)
	}
	if verified {
		t.Fatalf("注册后 verified_at 不该有值")
	}

	// 登录被拦，且给的是「去验证」而不是「密码不对」。
	_, _, err := s.Login(context.Background(), LoginInput{Identifier: email, Password: "correct-horse-battery", IP: "10.9.9.11"})
	if !errors.Is(err, ErrEmailNotVerified) {
		t.Fatalf("pending 账号登录 = %v，想要 ErrEmailNotVerified", err)
	}
	_ = username
}

// 验证码验过之后：状态转 active、会话能正常发出来。
func TestIntegrationVerifyEmailActivates(t *testing.T) {
	pool := testPool(t)
	s := testService(t, pool)
	email, _, code := mkPendingUser(t, s, "verify")

	if err := s.VerifyEmail(context.Background(), email, code); err != nil {
		t.Fatalf("验证失败: %v", err)
	}

	var status string
	if err := pool.QueryRow(context.Background(), `
SELECT a.status FROM accounts a JOIN account_emails e ON e.account_id = a.id
WHERE lower(e.email) = lower($1)`, email).Scan(&status); err != nil {
		t.Fatalf("查状态失败: %v", err)
	}
	if status != "active" {
		t.Fatalf("验证后状态 = %q，想要 active", status)
	}

	// 登录现在能过。
	if _, _, err := s.Login(context.Background(), LoginInput{Identifier: email, Password: "correct-horse-battery", IP: "10.9.9.13"}); err != nil {
		t.Fatalf("验证后登录失败: %v", err)
	}
}

// 验证码错、已用过、或邮箱不存在，给同一个答案。
func TestIntegrationVerifyEmailRejectsBadCode(t *testing.T) {
	pool := testPool(t)
	s := testService(t, pool)
	email, _, code := mkPendingUser(t, s, "badcode")

	if err := s.VerifyEmail(context.Background(), email, "000000"); !errors.Is(err, ErrVerifyCodeInvalid) {
		t.Fatalf("错码 = %v，想要 ErrVerifyCodeInvalid", err)
	}
	if err := s.VerifyEmail(context.Background(), "nobody-here@example.com", "123456"); !errors.Is(err, ErrVerifyCodeInvalid) {
		t.Fatalf("不存在的邮箱 = %v，想要 ErrVerifyCodeInvalid（不能泄露账号是否存在）", err)
	}

	if err := s.VerifyEmail(context.Background(), email, code); err != nil {
		t.Fatalf("首次验证失败: %v", err)
	}
	// 已经验证过的账号：幂等成功，不消费码也不报错。
	if err := s.VerifyEmail(context.Background(), email, code); err != nil {
		t.Fatalf("重复验证 = %v，想要 nil（幂等）", err)
	}
}

// mkPendingUser 建一个**不验证**的用户，给「注册后是 pending」的用例用。
func mkPendingUser(t *testing.T, s *Service, tag string) (email, username, code string) {
	t.Helper()
	email = fmt.Sprintf("it-%s@example.com", tag)
	username = fmt.Sprintf("it-%s", tag)
	_, delivery, err := s.Register(context.Background(), RegisterInput{
		Email:       email,
		Password:    "correct-horse-battery",
		Username:    username,
		DisplayName: "集成测试",
	})
	if err != nil {
		t.Fatalf("建用户失败: %v", err)
	}
	t.Cleanup(func() {
		_, _ = s.pool.Exec(context.Background(),
			"DELETE FROM accounts WHERE id IN (SELECT account_id FROM profiles WHERE lower(username) = lower($1))",
			username)
		_, _ = s.pool.Exec(context.Background(), "DELETE FROM login_attempts WHERE identifier_hash IS NOT NULL")
		_, _ = s.pool.Exec(context.Background(), "DELETE FROM verification_codes WHERE purpose LIKE 'verify_email'")
	})
	return email, username, delivery.Code
}

// 重发验证码在同一分钟内被限流：这是 kernel 那层按 (purpose, identifier) 的口径，
// 也是「注册时那条码还没用到就别急着再发」的物理保证。
func TestIntegrationVerifyResendIsRateLimited(t *testing.T) {
	pool := testPool(t)
	s := testService(t, pool)
	email, _, _ := mkPendingUser(t, s, "resend")

	_, err := s.RequestEmailVerification(context.Background(), email, "10.9.9.15")
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("立刻重发 = %v，想要 ErrRateLimited", err)
	}

	// 不存在的邮箱：照样生成（保住「响应与账号存在无关」），但不投递。
	delivery, err := s.RequestEmailVerification(context.Background(), "nobody-resend@example.com", "10.9.9.16")
	if err != nil {
		t.Fatalf("给不存在的邮箱要码 = %v，想要 nil", err)
	}
	if delivery.To != "" {
		t.Fatalf("不存在的邮箱不该投递，实际 To = %q", delivery.To)
	}
	if delivery.Code == "" {
		t.Fatalf("不存在的邮箱也要生成码（否则响应能探出账号是否存在）")
	}
}
