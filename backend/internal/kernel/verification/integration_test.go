package verification

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

// 集成测试：连真库跑一遍真实的 SQL。
//
// 默认跳过——没设 TEST_DATABASE_URL 时不跑。CI 里由 postgres service 提供库，
// 并且已经跑过迁移（含 0002_verification_codes.sql）。
func pool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL 未设置，跳过集成测试（CI 里会设）")
	}
	p, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("连接测试库失败: %v", err)
	}
	t.Cleanup(p.Close)
	return p
}

func newTestService(t *testing.T) *Service {
	t.Helper()
	p := pool(t)
	if _, err := p.Exec(context.Background(), "DELETE FROM verification_codes"); err != nil {
		t.Fatalf("清空 verification_codes 失败: %v", err)
	}
	return New(store.New(p), []byte("test-secret"))
}

func TestIntegration_发出再校验(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()

	code, err := s.Issue(ctx, PurposeLoginChallenge, "user@example.com", 0)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if len(code) != CodeDigits {
		t.Fatalf("码长度不对: %q", code)
	}

	if err := s.Consume(ctx, PurposeLoginChallenge, "USER@example.com", code); err != nil {
		t.Fatalf("大小写不同也应验得过: %v", err)
	}
	// 一次性：同一个码不能再用。
	if err := s.Consume(ctx, PurposeLoginChallenge, "user@example.com", code); !errors.Is(err, ErrNotFound) {
		t.Fatalf("二次使用应 ErrNotFound，实际 %v", err)
	}
}

func TestIntegration_码错会累计次数并作废(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()

	code, err := s.Issue(ctx, PurposeLoginChallenge, "user@example.com", 0)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	wrong := bump(code)

	for i := 1; i < MaxAttempts; i++ {
		if err := s.Consume(ctx, PurposeLoginChallenge, "user@example.com", wrong); !errors.Is(err, ErrMismatch) {
			t.Fatalf("第 %d 次错码应 ErrMismatch，实际 %v", i, err)
		}
	}
	// 第 MaxAttempts 次错码：直接作废，错误类型升级。
	if err := s.Consume(ctx, PurposeLoginChallenge, "user@example.com", wrong); !errors.Is(err, ErrTooManyAttempts) {
		t.Fatalf("错够次数应 ErrTooManyAttempts，实际 %v", err)
	}
	// 作废之后，**即使拿正确的码**也不能用了。
	if err := s.Consume(ctx, PurposeLoginChallenge, "user@example.com", code); !errors.Is(err, ErrNotFound) {
		t.Fatalf("作废后正确码也应失效，实际 %v", err)
	}
}

func TestIntegration_发送频率限制(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()

	if _, err := s.Issue(ctx, PurposeLoginChallenge, "rate@example.com", 0); err != nil {
		t.Fatalf("第一次 Issue 应成功: %v", err)
	}
	// 1 次/分钟：紧接着再发必须被拒。
	if _, err := s.Issue(ctx, PurposeLoginChallenge, "rate@example.com", 0); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("一分钟内第二次应 ErrRateLimited，实际 %v", err)
	}
	// 换个标识符不受影响。
	if _, err := s.Issue(ctx, PurposeLoginChallenge, "other@example.com", 0); err != nil {
		t.Fatalf("别的标识符不该被牵连: %v", err)
	}
}

func TestIntegration_跨用途不能重放(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()

	code, err := s.Issue(ctx, PurposeLoginChallenge, "user@example.com", 0)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	// 同一个码拿去当「改邮箱」的验证码：必须验不过。
	if err := s.Consume(ctx, "email_change", "user@example.com", code); !errors.Is(err, ErrNotFound) {
		t.Fatalf("跨用途必须验不过，实际 %v", err)
	}
	// 原来的用途仍然有效（失败的那次没有消耗掉它）。
	if err := s.Consume(ctx, PurposeLoginChallenge, "user@example.com", code); err != nil {
		t.Fatalf("原用途应仍然有效: %v", err)
	}
}

// bump 把码的最后一位改掉，得到一个「肯定不对」的等长码。
func bump(code string) string {
	b := []byte(code)
	if b[len(b)-1] == '9' {
		b[len(b)-1] = '0'
	} else {
		b[len(b)-1]++
	}
	return string(b)
}
