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
		// login_attempts 没有外键，单独清。
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
	delivery, err := s.RequestEmailCode(context.Background(), email)
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
	if _, err := s.RequestEmailCode(context.Background(), email); err != nil {
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

	delivery, err := s.RequestEmailCode(context.Background(), "nobody-here@example.com")
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
