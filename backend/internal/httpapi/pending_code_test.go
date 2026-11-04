package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// pendingCode 从库里取出某个邮箱当前的验证码明文。
//
// # 为什么能这么干
//
// 库里存的是**哈希**，取出来的只有明文是拿不到的——那正是安全设计。所以这里不是
// 「读库拿码」，而是「重发一条，再从返回的投递信息里拿」：测试直接调领域服务发，
// 不经过 HTTP，因此不占 IP 配额，也顺手绕开了「注册那条码 1 分钟内不能重发」。
//
// 换句话说：这条路只有测试能用，生产上拿到码的唯一途径是收邮件。
func pendingCode(t *testing.T, pool *pgxpool.Pool, email string) string {
	t.Helper()
	// 注册那一刻已经发过一条码，同一分钟内再要会被 kernel 按
	// 「(purpose, identifier) 每分钟 1 条」拦下。测试里显式清掉那一行等于
	// 把限流窗口清零——这是**测试专用的**操作，也正因为如此，生产上重发
	// 是真的要等一分钟的（那是设计，不是缺陷）。
	if _, err := pool.Exec(context.Background(),
		"DELETE FROM verification_codes WHERE purpose = 'verify_email'"); err != nil {
		t.Fatalf("清验证码失败: %v", err)
	}
	svc := integrationIdentity(t, pool)
	delivery, err := svc.RequestEmailVerification(context.Background(), email, "10.255.255.1")
	if err != nil {
		// 1 分钟内重发会被 kernel 拦。测试里换个「IP」不够——限流是
		// (purpose, identifier) 维度的，与 IP 无关。这种情况下必须**换一个邮箱**，
		// 所以要在这里说清楚，而不是回一个空串让上层红在别处。
		t.Fatalf("要验证码失败（同一分钟内重复要码会被限流，用例应换邮箱或等一分钟）: %v", err)
	}
	if delivery.Code == "" {
		t.Fatalf("没有拿到验证码")
	}
	return delivery.Code
}

// codePattern 从邮件正文里抠出 6 位验证码，给「读日志取码」那条路用。
var codePattern = regexp.MustCompile(`(\d{6})`)

func extractCode(t *testing.T, body string) string {
	t.Helper()
	m := codePattern.FindStringSubmatch(body)
	if len(m) < 2 {
		t.Fatalf("正文里找不到验证码: %q", body)
	}
	return m[1]
}

// verifyEmailForTest 把某个刚注册的账号验证掉，走的是真实的 HTTP 端点。
func verifyEmailForTest(t *testing.T, pool *pgxpool.Pool, h *Handler, email string) {
	t.Helper()
	code := pendingCode(t, pool, email)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/verify-email",
		strings.NewReader(`{"email":"`+email+`","code":"`+code+`"}`))
	req.Header.Set("Content-Type", "application/json")
	h.PostAuthVerifyEmail(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("验证 %s 应 204，实际 %d（body=%s）", email, rec.Code, rec.Body.String())
	}
}
