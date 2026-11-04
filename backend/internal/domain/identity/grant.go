package identity

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

// ErrUnknownRole 是「这个角色名没在 roles 表里登记」。
//
// 单独一个错误而不是 ErrInvalidInput：调用方是运维命令，它要能区分「你拼错了角色名」
// 与「邮箱格式不对」，这两件事的修法完全不同。
var ErrUnknownRole = errors.New("identity: 角色未登记")

// ErrMissingForCreate 是「账号不存在，而建号需要的信息没给全」。
var ErrMissingForCreate = errors.New("identity: 账号不存在且缺少建号信息")

// GrantInput 是「把某个邮箱对应的账号提到某个角色」的输入。
//
// 为什么提权与建号合在一处：生产上第一个管理员没有任何已存在的管理员可以点按钮给他授权，
// 唯一可行的入口是运维命令；而那个时刻账号往往还没建。分两个命令会让「先建号再提权」
// 中间多一次人工步骤，也就是多一个「建了号却没提权」的中间态。
type GrantInput struct {
	Email string
	Role  string
	// 下面三个只在账号不存在时用。已存在的账号**不看它们**——
	// 提权不应该顺手把别人的口令改掉。
	Username    string
	DisplayName string
	Password    string
}

// GrantResult 是执行结果。Changed 与 Created 分开，是因为命令要能回答
// 「我这次到底改了什么」——运维命令最怕的是「跑了但不知道动没动」。
type GrantResult struct {
	Account Account
	Role    string
	Created bool
	Changed bool
}

// GrantRole 把邮箱对应的账号提到 role；账号不存在时先按注册流程建号再提权。
//
// 幂等：目标角色已经是当前角色时什么都不写，返回 Changed=false。第一次跑失败（比如库断了）
// 之后重跑不会变成两个操作——它会看到账号已存在，只补提权那一步。
func (s *Service) GrantRole(ctx context.Context, in GrantInput) (GrantResult, error) {
	email := strings.TrimSpace(in.Email)
	if err := validateEmail(email); err != nil {
		return GrantResult{}, err
	}
	role := strings.TrimSpace(in.Role)
	if role == "" {
		role = "admin"
	}

	ok, err := s.q.RoleExists(ctx, role)
	if err != nil {
		return GrantResult{}, fmt.Errorf("identity: 查角色失败: %w", err)
	}
	if !ok {
		return GrantResult{}, fmt.Errorf("%w: %q（已登记的角色见 roles 表）", ErrUnknownRole, role)
	}

	row, err := s.q.GetAccountByEmail(ctx, email)
	switch {
	case err == nil:
		return s.promoteExisting(ctx, row, role, email)
	case errors.Is(err, pgx.ErrNoRows):
		return s.createThenPromote(ctx, in, email, role)
	default:
		return GrantResult{}, fmt.Errorf("identity: 查账号失败: %w", err)
	}
}

// promoteExisting 处理「账号已经在」的那条路。
//
// 邮箱要单独传：GetAccountByEmail 的返回行里没有 email 列（账号可以有多个邮箱，
// 拿哪一个都不算「这个人的邮箱」），而命令的回执要能打出「改的是谁」。
func (s *Service) promoteExisting(ctx context.Context, row store.GetAccountByEmailRow, role, email string) (GrantResult, error) {
	acct := Account{
		ID:          row.ID,
		Sub:         row.Sub,
		Username:    row.Username,
		DisplayName: row.DisplayName,
		Email:       email,
		Role:        row.Role,
		Status:      row.Status,
	}
	if row.Role == role {
		return GrantResult{Account: acct, Role: role}, nil
	}
	if err := s.setRole(ctx, row.ID, role); err != nil {
		return GrantResult{}, err
	}
	acct.Role = role
	return GrantResult{Account: acct, Role: role, Changed: true}, nil
}

// createThenPromote 处理「账号还没建」的那条路：走 Register，再用同一条更新路径提权。
//
// 复用 Register 而不是自己拼 INSERT：账号建在四张表上（accounts / users / profiles /
// account_emails / password_credentials），少写一张就是一个坏账号。代价是它会顺手签发一条
// 会话——没人拿得到这个会话 ID，它只是库里一行无主记录，不影响正确性。
func (s *Service) createThenPromote(ctx context.Context, in GrantInput, email, role string) (GrantResult, error) {
	if strings.TrimSpace(in.Username) == "" || in.Password == "" {
		return GrantResult{}, fmt.Errorf("%w: 请同时提供 -username 与 -password（或用 ADMIN_GRANT_PASSWORD 传口令）", ErrMissingForCreate)
	}
	acct, _, err := s.Register(ctx, RegisterInput{
		Email:       email,
		Password:    in.Password,
		Username:    strings.TrimSpace(in.Username),
		DisplayName: strings.TrimSpace(in.DisplayName),
	})
	if err != nil {
		return GrantResult{}, err
	}
	if err := s.setRole(ctx, acct.ID, role); err != nil {
		// 账号已经建好了，提权没成功。如实说清楚，别让人以为整个操作回滚了——
		// 重跑一次这个命令就能补上提权那一步。
		return GrantResult{}, fmt.Errorf("identity: 账号已建好（#%d）但提权失败，重跑本命令可补上: %w", acct.ID, err)
	}
	acct.Role = role
	return GrantResult{Account: acct, Role: role, Created: true, Changed: true}, nil
}

func (s *Service) setRole(ctx context.Context, id int64, role string) error {
	if err := s.q.SetAccountRole(ctx, store.SetAccountRoleParams{ID: id, Role: role}); err != nil {
		return fmt.Errorf("identity: 改角色失败: %w", err)
	}
	return nil
}
