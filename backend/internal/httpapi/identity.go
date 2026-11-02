package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/api"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/identity"
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
	case errors.Is(err, identity.ErrUnauthorized):
		writeError(w, http.StatusUnauthorized, api.Unauthorized, "请先登录")
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
