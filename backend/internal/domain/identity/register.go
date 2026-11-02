package identity

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

// Register 建一个新账号并直接给它一个会话（注册完就是登录态）。
//
// 返回的第二个值是会话 ID，由传输层塞进 cookie——**domain 不认识 cookie**。
func (s *Service) Register(ctx context.Context, in RegisterInput) (Account, string, error) {
	if err := in.Validate(); err != nil {
		return Account{}, "", err
	}
	in.Email = strings.TrimSpace(in.Email)
	in.Username = strings.TrimSpace(in.Username)
	in.DisplayName = strings.TrimSpace(in.DisplayName)

	hash, err := HashPassword(in.Password)
	if err != nil {
		return Account{}, "", err
	}
	sub, err := NewSub()
	if err != nil {
		return Account{}, "", err
	}
	sessionID, err := newSessionID()
	if err != nil {
		return Account{}, "", err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Account{}, "", fmt.Errorf("identity: 开事务失败: %w", err)
	}
	// 提交成功后 Rollback 返回 ErrTxClosed，忽略即可。
	defer func() { _ = tx.Rollback(ctx) }()

	q := s.q.WithTx(tx)

	row, err := q.CreateAccount(ctx, sub)
	if err != nil {
		return Account{}, "", mapRegisterError(err)
	}
	if err := q.CreateProfile(ctx, store.CreateProfileParams{
		AccountID:   row.ID,
		Username:    in.Username,
		DisplayName: in.DisplayName,
	}); err != nil {
		return Account{}, "", mapRegisterError(err)
	}
	if err := q.CreatePrimaryEmail(ctx, store.CreatePrimaryEmailParams{
		AccountID: row.ID,
		Email:     in.Email,
	}); err != nil {
		return Account{}, "", mapRegisterError(err)
	}
	if err := q.CreatePasswordCredential(ctx, store.CreatePasswordCredentialParams{
		AccountID:  row.ID,
		SecretHash: hash,
	}); err != nil {
		return Account{}, "", mapRegisterError(err)
	}
	if err := q.CreateWebSession(ctx, store.CreateWebSessionParams{
		ID:          sessionID,
		AccountSub:  sub,
		Username:    in.Username,
		DisplayName: in.DisplayName,
		Role:        "user",
		Status:      "active",
	}); err != nil {
		return Account{}, "", fmt.Errorf("identity: 建会话失败: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return Account{}, "", fmt.Errorf("identity: 提交注册失败: %w", err)
	}

	return Account{
		Sub:         sub,
		Username:    in.Username,
		DisplayName: in.DisplayName,
		Email:       in.Email,
		Role:        "user",
		Status:      "active",
		Level:       0,
		CreatedAt:   row.CreatedAt.Time,
	}, sessionID, nil
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
