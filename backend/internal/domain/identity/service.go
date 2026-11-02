package identity

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

type Service struct {
	pool *pgxpool.Pool
	q    *store.Queries
}

func New(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool, q: store.New(pool)}
}

// Me 用会话 ID 换回当前账号。查不到就是没登录——「不存在 / 过期 / 已吊销」
// **故意不区分**：区分了等于告诉攻击者「这个 ID 曾经存在过」。
func (s *Service) Me(ctx context.Context, sessionID string) (Account, error) {
	if sessionID == "" {
		return Account{}, ErrUnauthorized
	}
	row, err := s.q.GetMeBySession(ctx, sessionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Account{}, ErrUnauthorized
	}
	if err != nil {
		return Account{}, fmt.Errorf("identity: 查会话失败: %w", err)
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
	}, nil
}
