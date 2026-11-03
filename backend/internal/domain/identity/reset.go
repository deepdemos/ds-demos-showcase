package identity

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/kernel/verification"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

// ResetCodeDelivery 是「重置密码的验证码要发到哪」。
type ResetCodeDelivery struct {
	Code     string
	To       string
	Username string
}

// RequestPasswordResetCode 给找回密码发一个验证码。
//
// **只认邮箱，不认用户名**：用户名是公开的，用它发起重置等于给攻击者一个
// 「知道昵称就能骚扰到人」的入口。
//
// 与登录验证码一样，账号不存在时也照样走一遍生成与频率限制（保持响应形状无关），
// 只是 To 为空、不投递。
func (s *Service) RequestPasswordResetCode(ctx context.Context, email string) (ResetCodeDelivery, error) {
	email = strings.TrimSpace(email)
	if email == "" || !strings.Contains(email, "@") {
		return ResetCodeDelivery{}, fmt.Errorf("%w: 邮箱格式不对", ErrInvalidInput)
	}

	row, err := s.q.GetPasswordResetTarget(ctx, email)
	found := true
	if errors.Is(err, pgx.ErrNoRows) {
		found = false
	} else if err != nil {
		return ResetCodeDelivery{}, fmt.Errorf("identity: 查重置目标失败: %w", err)
	}

	var accountID int64
	if found && row.Status != "deleted" {
		accountID = row.ID
	}

	code, err := s.verification.Issue(ctx, verification.PurposeResetPassword, email, accountID)
	if err != nil {
		if errors.Is(err, verification.ErrRateLimited) {
			return ResetCodeDelivery{}, &RateLimitError{RetryAfter: verification.CodeTTL}
		}
		return ResetCodeDelivery{}, err
	}

	if accountID == 0 {
		return ResetCodeDelivery{Code: code}, nil
	}
	return ResetCodeDelivery{Code: code, To: row.Email, Username: row.DisplayName}, nil
}

// ResetPassword 用验证码换一个新密码，并把旧会话全部作废。
//
// 顺序：先消费验证码，再改哈希，最后吊销会话——三步在**同一个事务**里。
// 分开做的后果是「密码换了但旧会话还有效」，那等于没换。
func (s *Service) ResetPassword(ctx context.Context, email, code, newPassword string) error {
	email = strings.TrimSpace(email)
	if email == "" || !strings.Contains(email, "@") {
		return fmt.Errorf("%w: 邮箱格式不对", ErrInvalidInput)
	}
	if n := len(newPassword); n < minPasswordLen || n > maxPasswordLen {
		return fmt.Errorf("%w: 密码长度必须是 %d–%d 个字符", ErrInvalidInput, minPasswordLen, maxPasswordLen)
	}
	if strings.TrimSpace(code) == "" {
		return fmt.Errorf("%w: 请填写验证码", ErrInvalidInput)
	}

	// 验证码先消费：失败就直接返回，不进事务。
	// 这里不看账号是否存在——不存在的邮箱本来就没有可消费的码，会返回 ErrNotFound。
	if err := s.verification.Consume(ctx, verification.PurposeResetPassword, email, code); err != nil {
		return ErrUnauthorized
	}

	row, err := s.q.GetPasswordResetTarget(ctx, email)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrUnauthorized
	}
	if err != nil {
		return fmt.Errorf("identity: 查重置目标失败: %w", err)
	}
	if row.Status == "deleted" {
		return ErrUnauthorized
	}

	hash, err := HashPassword(newPassword)
	if err != nil {
		return err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("identity: 开启事务失败: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)

	if err := q.UpdatePasswordHash(ctx, store.UpdatePasswordHashParams{AccountID: row.ID, SecretHash: hash}); err != nil {
		return fmt.Errorf("identity: 更新密码失败: %w", err)
	}
	// 用库里的 now() 而不是 Go 的时间：会话表与凭证表的时钟必须同源。
	var rotatedAt pgtype.Timestamptz
	if err := tx.QueryRow(ctx, "SELECT rotated_at FROM credentials WHERE account_id = $1 AND kind = 'password'", row.ID).Scan(&rotatedAt); err != nil {
		return fmt.Errorf("identity: 读轮换时刻失败: %w", err)
	}
	n, err := q.RevokeSessionsBefore(ctx, store.RevokeSessionsBeforeParams{AccountSub: row.Sub, CreatedAt: rotatedAt})
	if err != nil {
		return fmt.Errorf("identity: 吊销历史会话失败: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("identity: 提交事务失败: %w", err)
	}
	slog.Info("密码已重置", "account_sub", row.Sub, "revoked_sessions", n)
	return nil
}
