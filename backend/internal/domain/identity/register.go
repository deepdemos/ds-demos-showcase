package identity

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/kernel/verification"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

// Register 建一个新账号，并准备好一封「验证你的邮箱」的验证码。
//
// # 为什么不再发会话
//
// 设计 §8.1 的原文是「`pending` 不能登录任何下游（IdP 自己也不发 token）」。
// 发一个「受限会话」听起来更顺，但 `web_sessions` 没有「受限」这个维度——
// 要做得先造一层状态机，而这一层以后再也拆不掉。所以注册成功只到「信发出去了」为止，
// 用户验证完再登录一次。
//
// # 为什么返回投递信息而不是自己发信
//
// 域层不认识 SMTP。返回的 `EmailCodeDelivery` 交给传输层投递——与 `RequestEmailCode`
// 同一个形状。投递失败**不回滚注册**：SMTP 抖动不该让人重填表单，更不该把账号
// 留在半死不活的状态。用户可以到验证页点「重新发送」。
func (s *Service) Register(ctx context.Context, in RegisterInput) (Account, EmailCodeDelivery, error) {
	acct, err := s.createAccount(ctx, in)
	if err != nil {
		return Account{}, EmailCodeDelivery{}, err
	}

	// 验证码在事务**之后**发：事务里发信会有「信已发出、事务回滚」的窗口，
	// 用户拿到一个查不到账号的码。代价是这一步失败时账号已经建好了——那正是
	// 「投递失败不回滚注册」的语义，用户重发即可。
	//
	// `verification` 为 nil 的调用方（`cmd/admin-grant`）不走这条路径——
	// 它调 createAccount 而不是 Register，因为运维命令的前提是「人能碰机器」，
	// 不需要也不应该发一封验证信。
	code, err := s.verification.Issue(ctx, verification.PurposeVerifyEmail, acct.Email, acct.ID)
	if err != nil {
		// 发不出码不是注册失败：账号建好了，用户可以到验证页重发。
		// 但仍然要把错误带出去——调用方要能告诉用户「信没发出去」。
		return acct, EmailCodeDelivery{}, fmt.Errorf("%w: %w", ErrVerificationSendFailed, err)
	}

	return acct, EmailCodeDelivery{Code: code, To: acct.Email, Username: acct.DisplayName}, nil
}

// createAccount 只做「把账号建出来」这一件事：五张表一个事务，状态是 pending。
//
// 单独拆出来是因为有两个调用方，它们的后半段不同：
//   - 注册：建完发验证码（Register）
//   - 运维命令：建完直接激活，不发信（grant.go 的 createThenPromote）
//
// 让运维命令复用 Register 再想办法压掉发信，是最省事的写法，也是这个函数存在之前
// 的写法——结果 `cmd/admin-grant` 传了 nil 的 verification，在 CI 上直接段错误。
// 拆开之后「建号」与「通知用户」是两件可以被独立调用的事。
func (s *Service) createAccount(ctx context.Context, in RegisterInput) (Account, error) {
	if err := in.Validate(); err != nil {
		return Account{}, err
	}
	in.Email = strings.TrimSpace(in.Email)
	in.Username = strings.TrimSpace(in.Username)
	in.DisplayName = strings.TrimSpace(in.DisplayName)

	hash, err := HashPassword(in.Password)
	if err != nil {
		return Account{}, err
	}
	sub, err := NewSub()
	if err != nil {
		return Account{}, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Account{}, fmt.Errorf("identity: 开事务失败: %w", err)
	}
	// 提交成功后 Rollback 返回 ErrTxClosed，忽略即可。
	defer func() { _ = tx.Rollback(ctx) }()

	q := s.q.WithTx(tx)

	row, err := q.CreateAccount(ctx, sub)
	if err != nil {
		return Account{}, mapRegisterError(err)
	}
	// demo 站的投影行：业务外键指向它的 id。与 accounts 同一事务，
	// 不存在「有账号没有投影行」的中间态。
	user, err := q.CreateUser(ctx, sub)
	if err != nil {
		return Account{}, mapRegisterError(err)
	}
	if err := q.CreateProfile(ctx, store.CreateProfileParams{
		AccountID:   row.ID,
		Username:    in.Username,
		DisplayName: in.DisplayName,
	}); err != nil {
		return Account{}, mapRegisterError(err)
	}
	if err := q.CreatePrimaryEmail(ctx, store.CreatePrimaryEmailParams{
		AccountID: row.ID,
		Email:     in.Email,
	}); err != nil {
		return Account{}, mapRegisterError(err)
	}
	if err := q.CreatePasswordCredential(ctx, store.CreatePasswordCredentialParams{
		AccountID:  row.ID,
		SecretHash: hash,
	}); err != nil {
		return Account{}, mapRegisterError(err)
	}

	if err := tx.Commit(ctx); err != nil {
		return Account{}, fmt.Errorf("identity: 提交注册失败: %w", err)
	}

	return Account{
		ID:          row.ID,
		UserID:      user.ID,
		Sub:         sub,
		Username:    in.Username,
		DisplayName: in.DisplayName,
		Email:       in.Email,
		Role:        "user",
		Status:      "pending",
		Level:       0,
		CreatedAt:   row.CreatedAt.Time,
	}, nil
}

// mapRegisterError 把唯一约束冲突翻译成「哪个输入框错了」。
//
// 靠约束名而不是先 SELECT 再 INSERT：先查后插有 TOCTOU 窗口（两个并发注册都能通过检查），
// 而唯一索引是数据库自己保证的。代价是要认约束名——régua 与 db/schema 的索引名绑在一起，
// 所以这里写死名字，改索引必须同时改这里（已登记 docs/已知松点.md 第 1 条同类问题）。
func mapRegisterError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		switch pgErr.ConstraintName {
		case "uq_profiles_username":
			return ErrUsernameTaken
		case "uq_account_emails_email":
			return ErrEmailTaken
		}
	}
	return fmt.Errorf("identity: 写账号失败: %w", err)
}
