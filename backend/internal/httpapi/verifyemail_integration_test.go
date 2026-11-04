package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

// captureMailer 把信留在内存里，供用例读取验证码。
//
// 为什么需要它：验证信的唯一投递口是 SMTP，测试里没有真 SMTP。用「捕获 + 断言」
// 代替「假装发出去了」——后者会让「注册到底有没有发信」这件事永远没人测。
type captureMailer struct {
	mu   sync.Mutex
	sent []capturedMail
}

type capturedMail struct {
	To      string
	Subject string
	Body    string
}

func (m *captureMailer) Send(_ context.Context, to, subject, body string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sent = append(m.sent, capturedMail{To: to, Subject: subject, Body: body})
	return nil
}

// lastTo 取发给某个地址的最后一封。
func (m *captureMailer) lastTo(t *testing.T, to string) capturedMail {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := len(m.sent) - 1; i >= 0; i-- {
		if m.sent[i].To == to {
			return m.sent[i]
		}
	}
	t.Fatalf("没有发给 %q 的信，实际发了 %d 封", to, len(m.sent))
	return capturedMail{}
}

// TestIntegrationRegisterSendsVerificationMail 覆盖注册 → 收信 → 验码 → 能登录整条路。
//
// 这是这一轮的核心用例：它同时钉住「注册会发信」「信里真的有能用的码」
// 「验证之后状态变了」「验证之后登得进来」。少任何一条，用户就卡在中间。
func TestIntegrationRegisterSendsVerificationMail(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()
	for _, q := range []string{"DELETE FROM web_sessions", "DELETE FROM users", "DELETE FROM accounts"} {
		if _, err := pool.Exec(ctx, q); err != nil {
			t.Fatalf("%s 失败: %v", q, err)
		}
	}

	box := &captureMailer{}
	h := New(pool, store.New(pool), integrationIdentity(t, pool), box, nil)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register",
		strings.NewReader(`{"email":"it-mail@example.com","password":"correct-horse-battery","username":"it-mail","displayName":"收信测试"}`))
	req.Header.Set("Content-Type", "application/json")
	h.PostAuthRegister(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("注册应 201，实际 %d（body=%s）", rec.Code, rec.Body.String())
	}

	var accepted struct {
		Email            string `json:"email"`
		VerificationSent bool   `json:"verificationSent"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &accepted); err != nil {
		t.Fatalf("响应不合法: %v", err)
	}
	if !accepted.VerificationSent {
		t.Fatalf("有 mailer 时 verificationSent 应为 true")
	}

	mail := box.lastTo(t, "it-mail@example.com")
	if !strings.Contains(mail.Subject, "验证") {
		t.Fatalf("邮件主题不对: %q", mail.Subject)
	}
	code := extractCode(t, mail.Body)

	// 验证：204，且不下发会话。
	verifyRec := httptest.NewRecorder()
	verifyReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/verify-email",
		strings.NewReader(`{"email":"it-mail@example.com","code":"`+code+`"}`))
	verifyReq.Header.Set("Content-Type", "application/json")
	h.PostAuthVerifyEmail(verifyRec, verifyReq)
	if verifyRec.Code != http.StatusNoContent {
		t.Fatalf("验证应 204，实际 %d（body=%s）", verifyRec.Code, verifyRec.Body.String())
	}
	if cookies := verifyRec.Result().Cookies(); len(cookies) != 0 {
		t.Fatalf("验证不该下发会话，实际 %+v", cookies)
	}

	// 状态真的变了。
	var status string
	var verified bool
	if err := pool.QueryRow(ctx, `
		SELECT a.status, e.verified_at IS NOT NULL FROM accounts a
		JOIN account_emails e ON e.account_id = a.id
		WHERE lower(e.email) = 'it-mail@example.com'`).Scan(&status, &verified); err != nil {
		t.Fatalf("查账号失败: %v", err)
	}
	if status != "active" || !verified {
		t.Fatalf("验证后 status=%q verified=%v，想要 active / true", status, verified)
	}

	// 现在登得进来。
	loginRec := httptest.NewRecorder()
	loginReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login",
		strings.NewReader(`{"identifier":"it-mail@example.com","password":"correct-horse-battery"}`))
	loginReq.Header.Set("Content-Type", "application/json")
	h.PostAuthLogin(loginRec, loginReq)
	if loginRec.Code != http.StatusOK {
		t.Fatalf("验证后登录应 200，实际 %d（body=%s）", loginRec.Code, loginRec.Body.String())
	}
}

// TestIntegrationVerifyResendAlwaysNoContent 钉住「探不出账号是否存在」。
func TestIntegrationVerifyResendAlwaysNoContent(t *testing.T) {
	pool := integrationPool(t)
	box := &captureMailer{}
	h := New(pool, store.New(pool), integrationIdentity(t, pool), box, nil)

	resend := func(email string) int {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/verify-email/resend",
			strings.NewReader(`{"email":"`+email+`"}`))
		req.Header.Set("Content-Type", "application/json")
		h.PostAuthVerifyEmailResend(rec, req)
		return rec.Code
	}

	// 不存在的邮箱：也是 204，且没有信。
	if code := resend("nobody-at-all@example.com"); code != http.StatusNoContent {
		t.Fatalf("不存在的邮箱应 204，实际 %d", code)
	}
	box.mu.Lock()
	for _, m := range box.sent {
		if m.To == "nobody-at-all@example.com" {
			t.Fatalf("不存在的邮箱不该收到信")
		}
	}
	box.mu.Unlock()

	// 格式不对：400。
	if code := resend("not-an-email"); code != http.StatusBadRequest {
		t.Fatalf("格式不对应 400，实际 %d", code)
	}
}
