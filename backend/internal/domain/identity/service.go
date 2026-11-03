package identity

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/kernel/verification"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

type Service struct {
	pool         *pgxpool.Pool
	q            *store.Queries
	ipSecret     []byte
	verification *verification.Service
}

// New 组装身份域。ipSecret 用于把客户端 IP 变成不可逆的 ip_hash——
// 明文 IP 不落库（身份域 §11.3 / §6）。
//
// verification 是 kernel 里的通用验证码机制，第②层限流要用它。
func New(pool *pgxpool.Pool, ipSecret []byte, verif *verification.Service) *Service {
	return &Service{pool: pool, q: store.New(pool), ipSecret: ipSecret, verification: verif}
}

// hashIP 把客户端 IP 变成不可逆的 ip_hash。
//
// 用 HMAC 而不是裸 SHA-256：IPv4 只有 2^32 种取值，裸哈希可以枚举反推。
// 密钥只在进程内，不入库、不出现在日志里。
func (s *Service) hashIP(ip string) string {
	mac := hmac.New(sha256.New, s.ipSecret)
	mac.Write([]byte(ip))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
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
	// 会话表里的 status 是**签发时的快照**，封禁之后它不会自己变。
	// 所以判断必须读 accounts 的当前值——这条查询本来就 JOIN 了 accounts，
	// 用它就不用再引一条「吊销全部会话」的路径：被封的人手里的 cookie 立刻失效。
	if row.Status == "banned" {
		return Account{}, ErrAccountBanned
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
