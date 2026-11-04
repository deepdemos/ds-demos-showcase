package identity

import (
	"context"
	"errors"
	"fmt"
	"strconv"
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

// ErrAccountNotActive 是「这个账号处于封禁或已注销状态，不能提权」。
//
// 这条判断不是洁癖：提权的输入是一个邮箱，而邮箱是会写错的。封禁账号往往正是
// 出过事的那个人——一个手滑的邮箱不该把他重新变成管理员。
var ErrAccountNotActive = errors.New("identity: 账号状态不允许提权")

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
	if row.Status == "banned" || row.Status == "deleted" {
		return GrantResult{}, fmt.Errorf("%w: 账号当前是 %s，先解封再提权", ErrAccountNotActive, row.Status)
	}
	// 早期版本建出来的账号可能停在 `pending`（那时注册就写 active，但手工 SQL 或将来
	// 的政策变化都可能留下 pending 的管理员）。提权的语义就是「这个人被信任了」，
	// 顺手把它启用——否则提完权限还是登不进来，命令等于没生效。
	activated := false
	if row.Status == "pending" {
		if err := s.activate(ctx, row.ID, email); err != nil {
			return GrantResult{}, err
		}
		acct.Status = "active"
		activated = true
	}
	if row.Role == role {
		return GrantResult{Account: acct, Role: role, Changed: activated}, nil
	}
	if err := s.setRole(ctx, row.ID, role, row.Role); err != nil {
		return GrantResult{}, err
	}
	acct.Role = role
	return GrantResult{Account: acct, Role: role, Changed: true}, nil
}

// createThenPromote 处理「账号还没建」的那条路：走 Register，再提权、再激活。
//
// 走 createAccount 而不是自己拼 INSERT：账号建在五张表上（accounts / users / profiles /
// account_emails / credentials），少写一张就是一个坏账号。
//
// 不走 Register：那条路会发一封「验证你的邮箱」的信，而运维命令建出来的账号不需要
// 也不应该收到它——这里的人在机器上。之前复用 Register 的写法传了 nil 的 verification，
// 在 CI 上直接段错误（`verification.(*Service).identifierKey` 取 nil 的 key）。
//
// **必须显式激活**：`Register` 现在写的是 `pending`（等邮箱验证），而运维命令建出来的
// 账号没有「验证邮件」这条路——它的前提就是「人能接触到机器、拿到数据库」。
// 不激活的话，第一个管理员建完登不进来，而这个命令存在的意义正是解决这一点。
//
// 顺带把邮箱标成已验证：它的所有者不是通过验证邮件证明的，而是通过能跑这条命令证明的。
func (s *Service) createThenPromote(ctx context.Context, in GrantInput, email, role string) (GrantResult, error) {
	if strings.TrimSpace(in.Username) == "" || in.Password == "" {
		return GrantResult{}, fmt.Errorf("%w: 请同时提供 -username 与 -password（或用 ADMIN_GRANT_PASSWORD 传口令）", ErrMissingForCreate)
	}
	acct, err := s.createAccount(ctx, RegisterInput{
		Email:       email,
		Password:    in.Password,
		Username:    strings.TrimSpace(in.Username),
		DisplayName: strings.TrimSpace(in.DisplayName),
	})
	if err != nil {
		return GrantResult{}, err
	}
	if err := s.activate(ctx, acct.ID, email); err != nil {
		return GrantResult{}, fmt.Errorf("identity: 账号已建好（#%d）但启用失败，重跑本命令可补上: %w", acct.ID, err)
	}
	acct.Status = "active"
	// createThenPromote 建出来的账号角色一律是 user（createAccount 写死的），
	// 所以这里的 fromRole 是确定的。
	if err := s.setRole(ctx, acct.ID, role, "user"); err != nil {
		// 账号已经建好了，提权没成功。如实说清楚，别让人以为整个操作回滚了——
		// 重跑一次这个命令就能补上提权那一步。
		return GrantResult{}, fmt.Errorf("identity: 账号已建好（#%d）但提权失败，重跑本命令可补上: %w", acct.ID, err)
	}
	acct.Role = role
	return GrantResult{Account: acct, Role: role, Created: true, Changed: true}, nil
}

// promoteExisting 里也要能补激活：早期版本或手工 SQL 建出来的管理员可能是 `pending`。
func (s *Service) activate(ctx context.Context, id int64, email string) error {
	if err := s.q.MarkEmailVerified(ctx, store.MarkEmailVerifiedParams{AccountID: id, Email: email}); err != nil {
		return fmt.Errorf("identity: 标记邮箱已验证失败: %w", err)
	}
	if err := s.q.ActivatePendingAccount(ctx, id); err != nil {
		return fmt.Errorf("identity: 启用账号失败: %w", err)
	}
	return nil
}

// setRole 改角色并留痕，两件事在同一个事务里。
//
// # 为什么要写审计
//
// 这个命令把某个邮箱变成 admin，而 admin 能改别人作品、能发布、能看审核队列……
// 换句话说它能改「谁能拆掉整个站」的名单。审计表的用途正是回答「这个人的权限是谁、
// 什么时候、凭什么给的」——不写这一行，一次提权在库里就只留下 `accounts.role`
// 的变化结果，没有任何经过。治理域 §5.2 要求所有权限变更入 `audit_log`。
//
// # 为什么 before/after 只存角色
//
// 与现有的审核审计同一口径：只存关键字段的快照，不存整行（内容不进展）。
// 这里的关键字段就是 `role`。
//
// # 为什么在事务里
//
// 分开写会出现「角色改了、审计没写」——而审计缺失恰恰是最需要审计的那次操作。
func (s *Service) setRole(ctx context.Context, id int64, role, fromRole string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("identity: 开事务失败: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := s.q.WithTx(tx)
	if err := q.SetAccountRole(ctx, store.SetAccountRoleParams{ID: id, Role: role}); err != nil {
		return fmt.Errorf("identity: 改角色失败: %w", err)
	}
	if err := q.InsertRoleSetAudit(ctx, store.InsertRoleSetAuditParams{
		EntityID: id,
		Before:   roleSnapshot(fromRole),
		After:    roleSnapshot(role),
		Reason:   grantAuditReason,
	}); err != nil {
		return fmt.Errorf("identity: 写提权审计失败: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("identity: 提交事务失败: %w", err)
	}
	return nil
}

// grantAuditReason 是写进 audit_log.reason 的那句话。审计里一句人话比一串机器可读的
// 结构化字段更有用：翻审计的人首先想知道「这是谁在干什么」，而不是「这属于哪一类」。
const grantAuditReason = "运维命令 cmd/admin-grant 提权（运行者在机器上，无会话身份）"

// roleSnapshot 造 before/after 的 JSON 快照。fromRole 为空表示「提权前不知道原角色」，
// 那种情况只写 after，不编一个假的 before。
func roleSnapshot(role string) []byte {
	if role == "" {
		return nil
	}
	return []byte(`{"role":` + strconv.Quote(role) + `}`)
}
