package identity

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/kernel/verification"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

// dummyHash 是一串合法的 argon2id PHC 编码（明文是一段随机串，谁也登不上）。
//
// 它只用来**把时间抹平**：标识符不存在时也要跑一次同样代价的校验，否则
// 「查得到但密码错」和「查不到这个人」的响应时间差就能被用来枚举注册邮箱。
// 参数与 HashPassword 一致（m=64MiB,t=3,p=2），所以耗时同量级。
const dummyHash = "$argon2id$v=19$m=65536,t=3,p=2$YWJjZGVmZ2hpamtsbW5vcA$7n0m3Hk1wCq4rXyZ8sT2uV5aB9cD0eF1gH2iJ3kL4mM"

// LoginInput 是登录需要的东西。Identifier 允许填邮箱或用户名。
type LoginInput struct {
	Identifier string
	Password   string
	// EmailCode 是第②层限流要求的邮箱验证码。只有在同一标识符失败次数
	// 达到阈值时才有意义；平时传了也会被忽略。
	EmailCode string
	// IP 是客户端 IP 原文。它只在本域里被 hash 成 ip_hash，不落库、不进日志。
	IP string
}

// 限流阈值（身份域 §11.3）。这些是安全下限，不是运营旋钮——
// 登记在 docs/运行手册-可变常量.md §3。
const (
	failWindow       = 10 * time.Minute
	accountIPLimit   = 10  // 同账号 + 同 IP，10 次失败 → 锁
	ipBurstLimit     = 50  // 同 IP，10 次失败窗口内 50 次 → 延迟
	ipHourlyLimit    = 200 // 同 IP，1 小时 200 次失败 → 封
	accountFailLimit = 5   // 账号维度：留作验证码门槛，见下方说明
	ipThrottleDelay  = 2 * time.Second
	lockDuration     = 15 * time.Minute
)

// 登录尝试表的两种事件。同表不同 kind：登录失败算失败计数，发验证码不算——
// 混在一起的话，正常用户连要两次码就会触发第①层的锁定。
const (
	kindLogin     = "login"
	kindEmailCode = "email_code"
)

// Login 校验凭据并发一个会话。
//
// 失败一律返回 ErrUnauthorized——不区分「没这个人」与「密码错」。区分了就等于
// 提供一个免费的账号枚举接口，而且那种区分对正常用户毫无帮助。
func (s *Service) Login(ctx context.Context, in LoginInput) (Account, string, error) {
	identifier := strings.TrimSpace(in.Identifier)
	if identifier == "" || in.Password == "" {
		return Account{}, "", fmt.Errorf("%w: 请填写账号与密码", ErrInvalidInput)
	}

	row, found, err := s.findForLogin(ctx, identifier)
	if err != nil {
		return Account{}, "", err
	}

	ipHash := s.hashIP(in.IP)
	var accountID pgtype.Int8
	if found {
		accountID = pgtype.Int8{Int64: row.ID, Valid: true}
	}
	limits, err := s.checkRateLimits(ctx, ipHash, accountID)
	if err != nil {
		return Account{}, "", err
	}

	// 第②层（身份域 §11.3）：同一标识符 10 分钟内失败 5 次 → 要求邮箱验证码。
	//
	// **判定只看标识符，不看账号是否存在**，而且先于密码校验——否则「存在的账号
	// 返回 verification_required、不存在的返回 401」就成了免费的枚举接口。
	// 代价是密码输错时也要求验证码，这个取舍是刻意的。
	identifierHash := s.identifierHash(identifier)
	if s.requireEmailCode(ctx, identifierHash) {
		if strings.TrimSpace(in.EmailCode) == "" {
			// 没带码也算一次失败：服务端已经问了问题，不回答就是没通过。
			// 不记的话，被要求验证码之后反而可以无限重试而不留痕。
			limits.record(ctx, s, ipHash, accountID, identifierHash, false)
			return Account{}, "", ErrVerificationRequired
		}
		if err := s.verification.Consume(ctx, verification.PurposeLoginChallenge, identifier, in.EmailCode); err != nil {
			// 码不对 / 过期 / 试太多次，一律按凭据不对处理，不告诉对方是哪一种。
			limits.record(ctx, s, ipHash, accountID, identifierHash, false)
			return Account{}, "", ErrVerificationRequired
		}
	}

	if !found {
		// 走一遍同样的校验代价，再统一报 401。
		_, _ = VerifyPassword(in.Password, dummyHash)
		limits.record(ctx, s, ipHash, accountID, identifierHash, false)
		return Account{}, "", ErrUnauthorized
	}

	ok, err := VerifyPassword(in.Password, row.SecretHash)
	if err != nil {
		// 哈希本身坏了是数据问题，不是用户输错；对外仍然是 401，避免泄露出
		// 「这个账号的凭证存储有问题」这类可用于定向攻击的信号。
		return Account{}, "", ErrUnauthorized
	}
	if !ok {
		limits.record(ctx, s, ipHash, accountID, identifierHash, false)
		return Account{}, "", ErrUnauthorized
	}
	// 已注销的账号不能登录；被封禁的也不能——但两者对外要给不同的答案：
	// deleted 装作查无此人（401），banned 明说「账号已被封禁」（403），
	// 否则被封的人只会一直重试密码。
	//
	// `pending` 从 2026-11 起也不放行：注册不再发会话，未验证的账号根本不该有
	// 任何进入机会。它给的是 403 + `email_not_verified`，前端据此把人送到验证页——
	// 报「密码不对」会让这类用户一直重试。
	//
	// `suspended` 仍然放行：临时暂停是内容层面的处置，不是身份问题。
	//
	// 这几个判断都在**密码校验通过之后**，所以 403 只发给本来就知道密码的人，
	// 不构成账号枚举。
	if row.Status == "deleted" || row.Status == "banned" || row.Status == "pending" {
		limits.record(ctx, s, ipHash, accountID, identifierHash, false)
		switch row.Status {
		case "banned":
			return Account{}, "", ErrAccountBanned
		case "pending":
			return Account{}, "", ErrEmailNotVerified
		default:
			return Account{}, "", ErrUnauthorized
		}
	}

	sessionID, err := newSessionID()
	if err != nil {
		return Account{}, "", err
	}
	// 成功：记一条 succeeded=true（账号维度的「清零」靠 `created_at > 最后一次成功`
	// 表达，IP 维度不受影响），顺手清掉一小时前的旧行。
	limits.record(ctx, s, ipHash, accountID, identifierHash, true)
	if err := s.q.PurgeLoginAttempts(ctx); err != nil {
		slog.Warn("清理登录尝试记录失败", "err", err)
	}

	if err := s.q.CreateWebSession(ctx, store.CreateWebSessionParams{
		ID:          sessionID,
		AccountSub:  row.Sub,
		Username:    row.Username,
		DisplayName: row.DisplayName,
		Role:        row.Role,
		Status:      row.Status,
	}); err != nil {
		return Account{}, "", fmt.Errorf("identity: 建会话失败: %w", err)
	}

	return Account{
		Sub:         row.Sub,
		Username:    row.Username,
		DisplayName: row.DisplayName,
		Email:       row.Email,
		Role:        row.Role,
		Status:      row.Status,
		Level:       int32(row.Level),
		CreatedAt:   row.CreatedAt.Time,
	}, sessionID, nil
}

// Logout 吊销会话。**幂等**：会话不存在、已吊销、ID 是伪造的，都当成功。
//
// 不返回错误是刻意的：登出失败对用户没有可执行的动作，而「这个会话不存在」
// 又是一个可以拿来做探测的信号。
func (s *Service) Logout(ctx context.Context, sessionID string) error {
	if sessionID == "" {
		return nil
	}
	if err := s.q.RevokeWebSession(ctx, sessionID); err != nil {
		return fmt.Errorf("identity: 吊销会话失败: %w", err)
	}
	return nil
}

// findForLogin 先按邮箱找，找不到再按用户名找。
//
// 分成两条查询而不是一条 `WHERE email = $1 OR username = $1`：某人的用户名
// 完全可能等于另一个人的邮箱，一条 OR 查询会命中两个人里先被扫到的那个。
func (s *Service) findForLogin(ctx context.Context, identifier string) (store.GetLoginByEmailRow, bool, error) {
	byEmail, err := s.q.GetLoginByEmail(ctx, identifier)
	if err == nil {
		return byEmail, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return store.GetLoginByEmailRow{}, false, fmt.Errorf("identity: 查登录账号失败: %w", err)
	}
	byName, err := s.q.GetLoginByUsername(ctx, identifier)
	if errors.Is(err, pgx.ErrNoRows) {
		return store.GetLoginByEmailRow{}, false, nil
	}
	if err != nil {
		return store.GetLoginByEmailRow{}, false, fmt.Errorf("identity: 查登录账号失败: %w", err)
	}
	return store.GetLoginByEmailRow(byName), true, nil
}

// RateLimitError 带「建议等待多久」，传输层据此写 Retry-After。
type RateLimitError struct {
	RetryAfter time.Duration
}

func (e *RateLimitError) Error() string { return "identity: 尝试太频繁" }

// Unwrap 让 errors.Is(err, ErrRateLimited) 也能成立——调用方不用关心具体是哪一层触发的。
func (e *RateLimitError) Unwrap() error { return ErrRateLimited }

// ErrRateLimited 是「试太快了」。
var ErrRateLimited = errors.New("identity: 尝试太频繁")

// identifierHash 是第②层计数用的键。取不到（verification 未装配）时返回空串，
// 那条尝试记录就不参与第②层——降级为「只有第①③层」，不会因为依赖缺失而放行。
func (s *Service) identifierHash(identifier string) string {
	if s.verification == nil {
		return ""
	}
	return s.verification.IdentifierHash(verification.PurposeLoginChallenge, identifier)
}

// requireEmailCode 判断这个标识符是否已经失败够次数、需要验证码。
//
// 读失败时**不放行**：第②层是加固，读不到计数时宁可要求验证码（用户能自己解），
// 也不要静默跳过。但它也不能把登录整个打死——所以只有能确认「计数超阈值」才拦，
// 读出错时按「不拦」处理并记日志，因为卡在这里等于账号被一条 SQL 锁死。
func (s *Service) requireEmailCode(ctx context.Context, identifierHash string) bool {
	if s.verification == nil || identifierHash == "" {
		return false
	}
	row, err := s.q.CountIdentifierFailures(ctx, identifierHash)
	if err != nil {
		slog.Warn("读标识符失败计数失败，本次不做第②层判定", "err", err)
		return false
	}
	return row >= accountFailLimit
}

// loginLimits 是一次登录判定出来的三个计数。
//
// 曾经还有一个 account（账号维度、不分 IP）。它算了但没人读——第②层走的是
// CountIdentifierFailures（按标识符，不是按账号），所以那个字段是死的，已删。
type loginLimits struct {
	accountIP int32
	ip        int32
	ipHour    int32
}

// checkRateLimits 按 §11.3 的三层判定。
//
// 顺序有意：先判能不能进（硬封），再谈延迟。反过来的话，被硬封的请求
// 还要白等 2 秒才收到 429。
func (s *Service) checkRateLimits(ctx context.Context, ipHash string, accountID pgtype.Int8) (loginLimits, error) {
	row, err := s.q.CountLoginFailures(ctx, store.CountLoginFailuresParams{
		AccountID: accountID,
		IpHash:    ipHash,
	})
	if err != nil {
		return loginLimits{}, fmt.Errorf("identity: 读限流计数失败: %w", err)
	}
	l := loginLimits{
		accountIP: row.AccountIpFails,
		ip:        row.IpFails,
		ipHour:    row.IpFailsHour,
	}

	// 第 3 层（上界）：一小时 200 次失败 → 封一小时。
	if l.ipHour >= ipHourlyLimit {
		return l, &RateLimitError{RetryAfter: time.Hour}
	}
	// 第 1 层：同账号 + 同 IP 10 次 → 锁 15 分钟。
	if l.accountIP >= accountIPLimit {
		return l, &RateLimitError{RetryAfter: lockDuration}
	}
	// 第 3 层（下界）：同 IP 短时间 50 次 → 每个请求多花 2 秒。
	//
	// 这里**不 sleep**：用 select 等，让 context 取消能立刻退出。占着 goroutine
	// 睡 2 秒，一波攻击就能把连接池和 goroutine 一起耗干。
	if l.ip >= ipBurstLimit {
		timer := time.NewTimer(ipThrottleDelay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return l, ctx.Err()
		case <-timer.C:
		}
	}
	return l, nil
}

// record 写一条尝试记录。写失败不影响登录本身——限流是加固，不是正确性前提。
func (l loginLimits) record(ctx context.Context, s *Service, ipHash string, accountID pgtype.Int8, identifierHash string, succeeded bool) {
	ident := pgtype.Text{String: identifierHash, Valid: identifierHash != ""}
	if err := s.q.RecordLoginAttempt(ctx, store.RecordLoginAttemptParams{
		IpHash:         ipHash,
		AccountID:      accountID,
		IdentifierHash: ident,
		Succeeded:      succeeded,
		Kind:           kindLogin,
	}); err != nil {
		slog.Warn("记录登录尝试失败", "err", err)
	}
}
