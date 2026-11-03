package identity

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/kernel/verification"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

// EmailCodeDelivery 是「验证码要投递到哪」。
//
// Code 为空表示不必发信；其他字段只在 Code 非空时有意义。
type EmailCodeDelivery struct {
	// Code 是明文验证码，交给调用方投递。
	Code string
	// To 是收件地址（账号的主邮箱）。
	To string
	// Username 用于信件称呼。
	Username string
}

// RequestEmailCode 为一次登录挑战准备验证码。
//
// **无论标识符有没有对应账号，都会走一遍 kernel 的生成与频率限制**，只是没有账号时
// 返回的 To 为空、调用方不发信。这样做是为了让「204 / 429」这两种响应与账号是否
// 存在完全无关——否则光是发送频率的差异就能把注册过的邮箱探出来。
func (s *Service) RequestEmailCode(ctx context.Context, identifier, ip string) (EmailCodeDelivery, error) {
	identifier = strings.TrimSpace(identifier)
	if identifier == "" {
		return EmailCodeDelivery{}, fmt.Errorf("%w: 请填写邮箱或用户名", ErrInvalidInput)
	}

	ipHash := s.hashIP(ip)
	if err := s.checkEmailCodeQuota(ctx, ipHash); err != nil {
		return EmailCodeDelivery{}, err
	}

	row, found, err := s.findForLogin(ctx, identifier)
	if err != nil {
		return EmailCodeDelivery{}, err
	}

	var accountID int64
	if found && row.Status != "deleted" {
		accountID = row.ID
	}

	code, err := s.verification.Issue(ctx, verification.PurposeLoginChallenge, identifier, accountID)
	if err != nil {
		if errors.Is(err, verification.ErrRateLimited) {
			return EmailCodeDelivery{}, &RateLimitError{RetryAfter: verification.CodeTTL}
		}
		return EmailCodeDelivery{}, err
	}

	// 无论最后有没有投递，都记一条「这个 IP 要过一封」：配额要能约束
	// 「换着邮箱发」，就必须在发出去的那一刻计数，而不是等投递成功。
	//
	// 记录里带上标识符的哈希：出问题时能看出「是谁在刷」，而不是只有一堆 IP。
	s.recordEmailCodeSend(ctx, ipHash, s.verification.IdentifierHash(verification.PurposeLoginChallenge, identifier))

	if accountID == 0 {
		// 账号不存在（或已注销）：码已经生成，但不投递。调用方照样回 204。
		return EmailCodeDelivery{Code: code}, nil
	}
	return EmailCodeDelivery{Code: code, To: row.Email, Username: row.DisplayName}, nil
}

// emailCodeIPHourlyLimit 是每个 IP 每小时最多要多少封验证码（运行手册 §3）。
//
// kernel 那层只按 (purpose, identifier) 限 1 次/分、5 次/时——换个邮箱就是新额度，
// 1000 个邮箱等于没有上限。所以 IP 维度必须单独有一道。
const emailCodeIPHourlyLimit = 20

// checkEmailCodeQuota 判这个 IP 还能不能要码。
//
// 读失败**不拦**：配额是加固，不是正确性前提；卡在这里等于全站发不出验证码，
// 而发不出码的用户会永久卡在登录第②层。
func (s *Service) checkEmailCodeQuota(ctx context.Context, ipHash string) error {
	n, err := s.q.CountEmailCodeSends(ctx, ipHash)
	if err != nil {
		slog.Warn("读发码配额失败，本次不拦", "err", err)
		return nil
	}
	if n >= emailCodeIPHourlyLimit {
		return &RateLimitError{RetryAfter: time.Hour}
	}
	return nil
}

// recordEmailCodeSend 记一条发码事件。写失败不影响发码——和登录一样，限流是加固。
func (s *Service) recordEmailCodeSend(ctx context.Context, ipHash, identifierHash string) {
	ident := pgtype.Text{String: identifierHash, Valid: identifierHash != ""}
	if err := s.q.RecordLoginAttempt(ctx, store.RecordLoginAttemptParams{
		IpHash:         ipHash,
		IdentifierHash: ident,
		Succeeded:      true,
		Kind:           kindEmailCode,
	}); err != nil {
		slog.Warn("记录发码事件失败", "err", err)
	}
}
