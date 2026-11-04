package identity

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/kernel/verification"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

// 邮箱验证：把 `accounts.status` 从 `pending` 推到 `active`，并给主邮箱盖上
// `verified_at`。
//
// # 为什么必须有这一步
//
// 没有它时「邮箱」只是一个用户随手敲进去的字符串：抢注别人的邮箱会让真正的主人在
// 注册时撞唯一索引，而更要命的是**找回密码**——重置码寄往一个不属于注册者的地址，
// 他永远收不到，账号变砖且没有人知道原因。
//
// # 为什么不在这里发信
//
// 域层不认识 SMTP（也不该认识）。`RequestEmailVerification` 只把验证码准备好，投递由
// 传输层做——与 `RequestEmailCode` 同一个形状。

// ErrVerifyCodeInvalid 是「验证码不对或已过期」。
//
// 与 ErrResetCodeInvalid 分开：这两条码的用途不同，报成同一句会让用户拿着验证邮件里的
// 码去重置密码，或者反过来。
var ErrVerifyCodeInvalid = errors.New("identity: 验证码不对或已过期")

// RequestEmailVerification 为某个账号准备一封「验证你的邮箱」的验证码。
//
// 传的是邮箱而不是账号 ID：验证页只知道用户填了什么邮箱，不知道 ID；
// 而且「这个邮箱有没有账号」本身不该由验证页的行为暴露出来。
//
// 账号不存在、已注销、或邮箱本来就验证过时，**照样走一遍生成与频率限制**，
// 但返回的 To 为空、调用方不发信。这样「204 / 429」与账号状态无关。
func (s *Service) RequestEmailVerification(ctx context.Context, email, ip string) (EmailCodeDelivery, error) {
	email = strings.TrimSpace(email)
	if err := validateEmail(email); err != nil {
		return EmailCodeDelivery{}, err
	}

	ipHash := s.hashIP(ip)
	if err := s.checkEmailCodeQuota(ctx, ipHash); err != nil {
		return EmailCodeDelivery{}, err
	}

	row, found, err := s.findForVerification(ctx, email)
	if err != nil {
		return EmailCodeDelivery{}, err
	}

	var accountID int64
	if found && row.Status != "deleted" && !row.VerifiedAt.Valid {
		accountID = row.ID
	}

	code, err := s.verification.Issue(ctx, verification.PurposeVerifyEmail, email, accountID)
	if err != nil {
		if errors.Is(err, verification.ErrRateLimited) {
			return EmailCodeDelivery{}, &RateLimitError{RetryAfter: verification.CodeTTL}
		}
		return EmailCodeDelivery{}, err
	}

	// 与发登录码共用一个 IP 配额：换个邮箱就是新额度的话，1000 个邮箱等于没有上限。
	s.recordEmailCodeSend(ctx, ipHash, s.verification.IdentifierHash(verification.PurposeVerifyEmail, email))

	if accountID == 0 {
		return EmailCodeDelivery{Code: code}, nil
	}
	return EmailCodeDelivery{Code: code, To: row.Email, Username: row.DisplayName}, nil
}

// VerifyEmail 消费验证码，把账号从 `pending` 转 `active`。
//
// # 幂等
//
// 已经验证过的账号再提交一次，直接返回成功、**不消费码**：用户可能刷新页面重提交，
// 也可能在两台设备上操作。回一句「验证码不对」只会让他以为自己的码坏了。
//
// # 一个事务
//
// `verified_at` 与 `status` 必须一起改：只改一个会留下「已验证但登不进来」或者
// 「能登录但邮箱没验证」的半状态，而这两个半状态都没有对应的修复路径。
func (s *Service) VerifyEmail(ctx context.Context, email, code string) error {
	email = strings.TrimSpace(email)
	if err := validateEmail(email); err != nil {
		return err
	}
	code = strings.TrimSpace(code)
	if code == "" {
		return fmt.Errorf("%w: 请填写验证码", ErrInvalidInput)
	}

	row, found, err := s.findForVerification(ctx, email)
	if err != nil {
		return err
	}
	if !found || row.Status == "deleted" {
		// 账号不存在与验证码错给同一个答案：区分了就能拿验证页探「这个邮箱注册过没有」。
		return ErrVerifyCodeInvalid
	}
	if row.VerifiedAt.Valid {
		return nil
	}

	if err := s.verification.Consume(ctx, verification.PurposeVerifyEmail, email, code); err != nil {
		if errors.Is(err, verification.ErrNotFound) ||
			errors.Is(err, verification.ErrMismatch) ||
			errors.Is(err, verification.ErrTooManyAttempts) {
			return ErrVerifyCodeInvalid
		}
		return err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("identity: 开事务失败: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := s.q.WithTx(tx)
	if err := q.MarkEmailVerified(ctx, store.MarkEmailVerifiedParams{
		AccountID: row.ID,
		Email:     row.Email,
	}); err != nil {
		return fmt.Errorf("identity: 标记邮箱已验证失败: %w", err)
	}
	if err := q.ActivatePendingAccount(ctx, row.ID); err != nil {
		return fmt.Errorf("identity: 启用账号失败: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("identity: 提交邮箱验证失败: %w", err)
	}
	return nil
}

// findForVerification 按邮箱取账号。
//
// 这里**不过滤 status**：`suspended` / `banned` 的账号也要能被找到，
// 否则它们会一直留在 `pending` 里（或者更糟：验证按钮点了没反应）。
// 允不允许登录是登录那一层的事，这里只负责「证明邮箱归属」。
func (s *Service) findForVerification(ctx context.Context, email string) (store.GetAccountForVerificationRow, bool, error) {
	row, err := s.q.GetAccountForVerification(ctx, email)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return store.GetAccountForVerificationRow{}, false, nil
		}
		return store.GetAccountForVerificationRow{}, false, fmt.Errorf("identity: 查账号失败: %w", err)
	}
	return row, true, nil
}
