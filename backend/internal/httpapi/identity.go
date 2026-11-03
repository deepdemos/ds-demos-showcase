package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/api"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/identity"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/kernel/verification"
)

// sessionCookie 是会话 cookie 的名字。`__Host-` 前缀由浏览器强制 `Secure` +
// `Path=/` + **禁止 `Domain`**；第三条是关键，不设 Domain 就只有当前 host 碰得到它
// （身份域 §11.1）。
const sessionCookie = "__Host-demo_session"

func readSessionCookie(r *http.Request) string {
	cookie, err := r.Cookie(sessionCookie)
	if err != nil {
		return ""
	}
	return cookie.Value
}

func toMe(a identity.Account) api.Me {
	return api.Me{
		Sub:         a.Sub,
		Username:    a.Username,
		DisplayName: a.DisplayName,
		Email:       openapi_types.Email(a.Email),
		Role:        a.Role,
		Status:      api.AccountStatus(a.Status),
		Level:       int(a.Level),
		CreatedAt:   a.CreatedAt,
	}
}

func (h *Handler) GetMe(w http.ResponseWriter, r *http.Request) {
	if h.identity == nil {
		writeError(w, http.StatusServiceUnavailable, api.Unavailable, "服务暂时不可用")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), queryTimeout)
	defer cancel()
	account, err := h.identity.Me(ctx, readSessionCookie(r))
	if err != nil {
		h.writeIdentityError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toMe(account))
}

// writeIdentityError 把领域错误映射成状态码。**401 不区分原因**：区分了等于
// 给攻击者一个免费的会话探测器。
func (h *Handler) writeIdentityError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, identity.ErrInvalidInput):
		writeError(w, http.StatusBadRequest, api.InvalidParam, messageOf(err))
	case errors.Is(err, identity.ErrEmailTaken), errors.Is(err, identity.ErrUsernameTaken):
		// 409 而不是 400：格式没问题，是**这个值已经被占了**。前端据此把错误标在对应输入框。
		writeError(w, http.StatusConflict, api.Conflict, messageOf(err))
	case errors.Is(err, identity.ErrVerificationRequired):
		writeError(w, http.StatusUnauthorized, api.VerificationRequired, "需要邮箱验证码")
	case errors.Is(err, identity.ErrUnauthorized):
		writeError(w, http.StatusUnauthorized, api.Unauthorized, "邮箱或密码不对")
	case errors.Is(err, identity.ErrRateLimited):
		var limited *identity.RateLimitError
		if errors.As(err, &limited) {
			w.Header().Set("Retry-After", strconv.Itoa(int(limited.RetryAfter.Seconds())))
		}
		writeError(w, http.StatusTooManyRequests, api.RateLimited, "尝试太频繁，请稍后再试")
	default:
		slog.Error("identity request failed", "err", err) // 原文可能带 SQL 或连接串，只进日志
		writeError(w, http.StatusServiceUnavailable, api.Unavailable, "服务暂时不可用")
	}
}

// messageOf 取出领域错误里可直接展示的原因：先剥包前缀（"identity: "），再取第一个
// 冒号之后的部分。不剥的话用户会看到「输入不合法: 密码长度必须是…」这种半英文句子。
func messageOf(err error) string {
	msg := strings.TrimPrefix(err.Error(), "identity: ")
	if _, rest, ok := strings.Cut(msg, ": "); ok {
		return rest
	}
	return msg
}

// setSessionCookie 下发会话。属性见 sessionCookie 的注释：`__Host-` 要求 Secure + Path=/ + 无 Domain。
func setSessionCookie(w http.ResponseWriter, sessionID string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    sessionID,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
}

// sessionMaxAgeSeconds 与 web_sessions.expires_at 的默认值同源：30 天。
// 两边必须一起改——cookie 活得比会话久，前端会以为自己还登着。
const sessionMaxAgeSeconds = 30 * 24 * 60 * 60

// PostAuthRegister 实现 POST /api/v1/auth/register。
func (h *Handler) PostAuthRegister(w http.ResponseWriter, r *http.Request) {
	if h.identity == nil {
		writeError(w, http.StatusServiceUnavailable, api.Unavailable, "服务暂时不可用")
		return
	}
	var body api.PostAuthRegisterJSONRequestBody
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, api.InvalidParam, "请求体不是合法的 JSON")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), registerTimeout)
	defer cancel()

	account, sessionID, err := h.identity.Register(ctx, identity.RegisterInput{
		Email:       string(body.Email),
		Password:    body.Password,
		Username:    body.Username,
		DisplayName: body.DisplayName,
	})
	if err != nil {
		h.writeIdentityError(w, err)
		return
	}
	setSessionCookie(w, sessionID, sessionMaxAgeSeconds)
	writeJSON(w, http.StatusCreated, toMe(account))
}

// PostAuthLogin 实现 POST /api/v1/auth/login。
func (h *Handler) PostAuthLogin(w http.ResponseWriter, r *http.Request) {
	if h.identity == nil {
		writeError(w, http.StatusServiceUnavailable, api.Unavailable, "服务暂时不可用")
		return
	}
	var body api.PostAuthLoginJSONRequestBody
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, api.InvalidParam, "请求体不是合法的 JSON")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), registerTimeout)
	defer cancel()

	in := identity.LoginInput{
		Identifier: body.Identifier,
		Password:   body.Password,
		IP:         clientIP(r),
	}
	if body.EmailCode != nil {
		in.EmailCode = *body.EmailCode
	}
	account, sessionID, err := h.identity.Login(ctx, in)
	if err != nil {
		h.writeIdentityError(w, err)
		return
	}
	setSessionCookie(w, sessionID, sessionMaxAgeSeconds)
	writeJSON(w, http.StatusOK, toMe(account))
}

// PostAuthLogout 实现 POST /api/v1/auth/logout。幂等，总是 204。
func (h *Handler) PostAuthLogout(w http.ResponseWriter, r *http.Request) {
	sessionID := readSessionCookie(r)
	if h.identity != nil {
		ctx, cancel := context.WithTimeout(r.Context(), queryTimeout)
		defer cancel()
		if err := h.identity.Logout(ctx, sessionID); err != nil {
			// 登出失败也不阻断：cookie 仍然要清掉，否则用户会卡在「退不出去」的状态。
			slog.Error("logout failed", "err", err)
		}
	}
	clearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

// clearSessionCookie 让浏览器立刻丢掉会话 cookie。
//
// 属性必须与下发时**逐字一致**（`__Host-` 前缀要求 Path=/ 且无 Domain），
// 否则浏览器不会认为是同一个 cookie，会留下一个还能用的旧值。
func clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
}

// clientIP 取客户端地址。
//
// **不读 X-Forwarded-For**：那是个可以由客户端随便写的头，读了就等于把限流
// 的键交给攻击者——他每次换一个假 IP 就能绕开所有阈值。等真正上了反代，
// 再按「只信已知代理的 XFF」显式配置，而不是现在默认信任。
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// PostAuthEmailCode 实现 POST /api/v1/auth/email-code。
//
// **无论标识符有没有对应账号都返回 204**：账号不存在时验证码照样生成（以保持
// 发送频率的判定与账号是否存在无关），但不投递。响应形状完全相同，所以这个端点
// 不能用来判断某个邮箱注册过没有。
func (h *Handler) PostAuthEmailCode(w http.ResponseWriter, r *http.Request) {
	if h.identity == nil {
		writeError(w, http.StatusServiceUnavailable, api.Unavailable, "服务暂时不可用")
		return
	}
	var body api.EmailCodeRequest
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, api.InvalidParam, "请求体格式不对")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), queryTimeout)
	defer cancel()

	delivery, err := h.identity.RequestEmailCode(ctx, body.Identifier)
	if err != nil {
		h.writeIdentityError(w, err)
		return
	}

	if delivery.To != "" && h.mailer != nil {
		// 异步投递，且**不能**用请求的 context：响应一返回它就被取消，信永远发不出去。
		go h.sendLoginCode(mailerDelivery{Code: delivery.Code, To: delivery.To})
	}
	w.WriteHeader(http.StatusNoContent)
}

// sendLoginCode 异步发一封登录验证码。
//
// 独立 goroutine + 自己的超时：同步等 SMTP 会在**响应时间**上泄露「这个账号存在」，
// 而 204 是立刻返回的。
func (h *Handler) sendLoginCode(d mailerDelivery) {
	ctx, cancel := context.WithTimeout(context.Background(), loginCodeTimeout)
	defer cancel()
	subject := "登录验证码"
	body := fmt.Sprintf("你的登录验证码是 %s，%d 分钟内有效，只能使用一次。\n\n如果这不是你本人操作，忽略这封邮件即可。\n",
		d.Code, int(verification.CodeTTL.Minutes()))
	if err := h.mailer.Send(ctx, d.To, subject, body); err != nil {
		slog.Warn("发送登录验证码失败", "err", err)
	}
}

// mailerDelivery 是发信需要的最小信息，避免把领域类型泄进传输层的签名里。
type mailerDelivery struct {
	Code string
	To   string
}

// loginCodeTimeout 比 queryTimeout 宽：SMTP 握手（TLS + AUTH）在慢网络下要几秒。
const loginCodeTimeout = 20 * time.Second

// PostAuthPasswordResetCode 实现 POST /api/v1/auth/password-reset/code。
//
// 与登录验证码同样「一律 204」：不存在的邮箱也走一遍生成与频率限制，只是不投递。
func (h *Handler) PostAuthPasswordResetCode(w http.ResponseWriter, r *http.Request) {
	if h.identity == nil {
		writeError(w, http.StatusServiceUnavailable, api.Unavailable, "服务暂时不可用")
		return
	}
	var body api.PasswordResetCodeRequest
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, api.InvalidParam, "请求体格式不对")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), queryTimeout)
	defer cancel()

	delivery, err := h.identity.RequestPasswordResetCode(ctx, string(body.Email))
	if err != nil {
		h.writeIdentityError(w, err)
		return
	}
	if delivery.To != "" && h.mailer != nil {
		go h.sendPasswordResetCode(mailerDelivery{Code: delivery.Code, To: delivery.To})
	}
	w.WriteHeader(http.StatusNoContent)
}

// PostAuthPasswordReset 实现 POST /api/v1/auth/password-reset。
func (h *Handler) PostAuthPasswordReset(w http.ResponseWriter, r *http.Request) {
	if h.identity == nil {
		writeError(w, http.StatusServiceUnavailable, api.Unavailable, "服务暂时不可用")
		return
	}
	var body api.PasswordResetRequest
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, api.InvalidParam, "请求体格式不对")
		return
	}

	// 改密码要跑一次 argon2id，与注册同样给宽一点的超时。
	ctx, cancel := context.WithTimeout(r.Context(), registerTimeout)
	defer cancel()

	if err := h.identity.ResetPassword(ctx, string(body.Email), body.Code, body.NewPassword); err != nil {
		h.writeIdentityError(w, err)
		return
	}
	// 旧会话已经全部失效，本地这个 cookie 也就不再有意义——顺手清掉，
	// 免得用户看到「已重置」却还挂着一个半死的会话。
	clearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

// sendPasswordResetCode 异步发一封重置验证码。
func (h *Handler) sendPasswordResetCode(d mailerDelivery) {
	ctx, cancel := context.WithTimeout(context.Background(), loginCodeTimeout)
	defer cancel()
	body := fmt.Sprintf("你正在重设密码，验证码是 %s，%d 分钟内有效，只能使用一次。\n\n重设成功后，其他设备上的登录会全部失效。\n\n如果这不是你本人操作，忽略这封邮件即可。\n",
		d.Code, int(verification.CodeTTL.Minutes()))
	if err := h.mailer.Send(ctx, d.To, "重设密码验证码", body); err != nil {
		slog.Warn("发送重置密码验证码失败", "err", err)
	}
}
