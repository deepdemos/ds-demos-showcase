package identity

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

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
}

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
	if !found {
		// 走一遍同样的校验代价，再统一报 401。
		_, _ = VerifyPassword(in.Password, dummyHash)
		return Account{}, "", ErrUnauthorized
	}

	ok, err := VerifyPassword(in.Password, row.SecretHash)
	if err != nil {
		// 哈希本身坏了是数据问题，不是用户输错；对外仍然是 401，避免泄露出
		// 「这个账号的凭证存储有问题」这类可用于定向攻击的信号。
		return Account{}, "", ErrUnauthorized
	}
	if !ok {
		return Account{}, "", ErrUnauthorized
	}
	// 已注销的账号不能登录。其余状态（pending / suspended / banned）一律放行，
	// 让用户能读到自己的状态——前端要显示「你被封了」而不是「密码错误」。
	if row.Status == "deleted" {
		return Account{}, "", ErrUnauthorized
	}

	sessionID, err := newSessionID()
	if err != nil {
		return Account{}, "", err
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
