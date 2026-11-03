package identity

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/kernel/verification"
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
func (s *Service) RequestEmailCode(ctx context.Context, identifier string) (EmailCodeDelivery, error) {
	identifier = strings.TrimSpace(identifier)
	if identifier == "" {
		return EmailCodeDelivery{}, fmt.Errorf("%w: 请填写邮箱或用户名", ErrInvalidInput)
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

	if accountID == 0 {
		// 账号不存在（或已注销）：码已经生成，但不投递。调用方照样回 204。
		return EmailCodeDelivery{Code: code}, nil
	}
	return EmailCodeDelivery{Code: code, To: row.Email, Username: row.DisplayName}, nil
}

// lookupPrimaryEmail 目前不需要单独查询：findForLogin 已经把主邮箱带回来了。
var _ = pgx.ErrNoRows
