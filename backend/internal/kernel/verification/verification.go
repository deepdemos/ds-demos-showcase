// Package verification 是通用的验证码机制：生成、哈希、存储、有效期、一次性、发送频率。
//
// 它**不认识任何具体业务**：不 import domain，不 import net/http。
// purpose 是调用方给的字符串，所以登录挑战、注册验证、改邮箱可以共用同一套。
//
// 放在 kernel 而不是某个域里：它一旦落在 domain/identity，别的域就会因为
// 「域之间不得互相 import」而用不了（docs/后端分层约定.md L3 / L6）。
package verification

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

// Purpose 是一次验证码的用途。取值是自由字符串，但同一个字符串才算同一个场景。
type Purpose string

const (
	// PurposeLoginChallenge：登录时「账号维度失败太多次」之后要求证明邮箱所有权。
	PurposeLoginChallenge Purpose = "login_challenge"
)

// 阈值。它们是安全下限，登记在 docs/运行手册-可变常量.md §3。
const (
	CodeDigits  = 6
	CodeTTL     = 5 * time.Minute
	MaxAttempts = 5
	perMinute   = 1
	perHour     = 5
)

// 错误。调用方按它们决定「让不让人继续」与「文案怎么写」。
var (
	// ErrNotFound：没有有效的码（没发过、已用过、已过期）。
	ErrNotFound = errors.New("verification: 没有有效的验证码")
	// ErrMismatch：码不对。
	ErrMismatch = errors.New("verification: 验证码不正确")
	// ErrTooManyAttempts：同一条码错太多次，已作废。
	ErrTooManyAttempts = errors.New("verification: 验证码错误次数过多")
	// ErrRateLimited：发送太频繁。
	ErrRateLimited = errors.New("verification: 发送太频繁")
)

// Service 是验证码机制的入口。
type Service struct {
	q      *store.Queries
	secret []byte
}

func New(q *store.Queries, secret []byte) *Service {
	return &Service{q: q, secret: secret}
}

// Issue 生成一个验证码并落库，把**明文码**返回给调用方去投递。
//
// 为什么要返回明文而不是「顺手帮你发」：投递方式是调用方的事（邮件、短信、站内信），
// 文案也是。机制只管生成与校验，渠道由业务选。这样 kernel 连 mailer 都不依赖。
func (s *Service) Issue(ctx context.Context, purpose Purpose, identifier string, accountID int64) (string, error) {
	key := s.identifierKey(purpose, identifier)

	counts, err := s.q.CountRecentVerificationCodes(ctx, store.CountRecentVerificationCodesParams{
		Purpose:        string(purpose),
		IdentifierHash: key,
	})
	if err != nil {
		return "", fmt.Errorf("verification: 读发送频率失败: %w", err)
	}
	if counts.LastMinute >= perMinute || counts.LastHour >= perHour {
		return "", ErrRateLimited
	}

	code, err := newCode()
	if err != nil {
		return "", err
	}

	var acct pgtype.Int8
	if accountID > 0 {
		acct = pgtype.Int8{Int64: accountID, Valid: true}
	}
	if _, err := s.q.CreateVerificationCode(ctx, store.CreateVerificationCodeParams{
		Purpose:        string(purpose),
		IdentifierHash: key,
		CodeHash:       s.hash(s.delimited(purpose, identifier, code)),
		AccountID:      acct,
		TtlSeconds:     int32(CodeTTL / time.Second),
	}); err != nil {
		return "", fmt.Errorf("verification: 写验证码失败: %w", err)
	}
	return code, nil
}

// Consume 校验并消耗一个验证码。
//
// 抢占用 `UPDATE … WHERE consumed_at IS NULL` 完成：两个并发请求即使都拿着正确的码，
// 也只有一个能把 consumed_at 从 NULL 改掉。
func (s *Service) Consume(ctx context.Context, purpose Purpose, identifier, code string) error {
	key := s.identifierKey(purpose, identifier)

	row, err := s.q.LatestVerificationCode(ctx, store.LatestVerificationCodeParams{
		Purpose:        string(purpose),
		IdentifierHash: key,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("verification: 读验证码失败: %w", err)
	}

	want := s.hash(s.delimited(purpose, identifier, code))
	if !hmac.Equal([]byte(want), []byte(row.CodeHash)) {
		attempts, bumpErr := s.q.BumpVerificationAttempts(ctx, row.ID)
		if bumpErr != nil {
			return fmt.Errorf("verification: 记录错误次数失败: %w", bumpErr)
		}
		if attempts >= MaxAttempts {
			// 错够次数就作废，逼对方重新发码——把在线爆破限制在 5 次。
			_ = s.q.InvalidateVerificationCode(ctx, row.ID)
			return ErrTooManyAttempts
		}
		return ErrMismatch
	}

	won, err := s.q.ConsumeVerificationCode(ctx, row.ID)
	if err != nil {
		return fmt.Errorf("verification: 消耗验证码失败: %w", err)
	}
	if won != 1 {
		// 输给了并发请求：说明这条码已经被用掉了。
		return ErrNotFound
	}
	return nil
}

// Purge 清掉一天前的记录。调用时机由调用方决定（成功登录时顺手清一次即可）。
func (s *Service) Purge(ctx context.Context) error {
	if err := s.q.PurgeVerificationCodes(ctx); err != nil {
		return fmt.Errorf("verification: 清理验证码失败: %w", err)
	}
	return nil
}

// identifierKey 把「用途 + 归一化标识符」变成一个不可逆的键。
//
// 带上 purpose：同一个邮箱在「登录挑战」与「改邮箱确认」下是两条独立的记录，
// 互不影响计数，也不能拿一处发的码去另一处用。
func (s *Service) identifierKey(purpose Purpose, identifier string) string {
	return s.hash("key:" + string(purpose) + "|" + normalize(identifier))
}

// delimited 是参与哈希的完整上下文。把 purpose 与 identifier 一起放进去，
// 意味着**同一条码换一个用途就验不过**——防止跨场景重放。
func (s *Service) delimited(purpose Purpose, identifier, code string) string {
	return "code:" + string(purpose) + "|" + normalize(identifier) + "|" + code
}

func (s *Service) hash(parts string) string {
	mac := hmac.New(sha256.New, s.secret)
	mac.Write([]byte(parts))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func normalize(identifier string) string {
	return strings.ToLower(strings.TrimSpace(identifier))
}

// newCode 生成 CodeDigits 位十进制码。
//
// 用 crypto/rand 而不是 math/rand：验证码是凭据，可预测的伪随机等于没有。
func newCode() (string, error) {
	max := big.NewInt(1)
	for i := 0; i < CodeDigits; i++ {
		max.Mul(max, big.NewInt(10))
	}
	n, err := rand.Int(rand.Reader, max)
	if err != nil {
		return "", fmt.Errorf("verification: 生成验证码失败: %w", err)
	}
	// 左侧补零：000123 也是合法码，不能因为去掉前导零把空间缩小十分之一。
	return fmt.Sprintf("%0*s", CodeDigits, n.String()), nil
}
