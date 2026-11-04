// Package identity 是身份域的业务逻辑。
//
// 它不认识 HTTP：不 import net/http，也不 import 生成的 api 包。
// 这一条由 check-layering 的 L2 强制——业务规则不该被传输层的形状绑架。
package identity

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// 业务错误。传输层按它们映射状态码——domain 不认识 HTTP 状态码。
var (
	// ErrInvalidInput 是「用户填的不对」，带一句可直接展示的中文原因。
	ErrInvalidInput = errors.New("identity: 输入不合法")
	// ErrEmailTaken / ErrUsernameTaken 分开，是因为前端要把错误标在不同输入框上。
	ErrEmailTaken    = errors.New("identity: 邮箱已被使用")
	ErrUsernameTaken = errors.New("identity: 用户名已被使用")
	// ErrVerificationRequired 是「这次登录需要邮箱验证码」。
	// 它只由第②层限流触发，与账号是否存在无关。
	ErrVerificationRequired = errors.New("identity: 需要邮箱验证码")
	// ErrUnauthorized 是「没登录 / 会话失效」。**不区分原因**——区分了就是给攻击者送信息。
	ErrUnauthorized = errors.New("identity: 未登录")
	// ErrAccountBanned 是「账号被封禁了，不能进站」。与 ErrUnauthorized 分开：
	// 被封的人需要读到「你被封了」而不是「密码错误」，否则他会一直重试。
	// 它只可能在密码校验通过之后出现，所以不构成账号枚举。
	ErrAccountBanned = errors.New("identity: 账号已被封禁")
	// ErrVerificationSendFailed 是「账号建好了，但验证信没发出去」。
	// 它不代表注册失败——账号是真的，用户可以到验证页重发。传输层据此在 201 响应里
	// 带上 `verificationSent: false`，前端据此提示「信没发出去，去验证页重发」。
	ErrVerificationSendFailed = errors.New("identity: 验证信没发出去")
	// ErrEmailNotVerified 是「邮箱还没验证过，账号还没启用」。
	// 与 ErrUnauthorized 分开：这类用户密码是对的，报「密码不对」会让他一直重试；
	// 前端也要据此给出「去验证 / 重新发送」的入口，而不是一句「登录失败」。
	// 它只在密码校验通过之后出现，所以不构成账号枚举。
	ErrEmailNotVerified = errors.New("identity: 邮箱还没验证，请先完成验证")
	// ErrResetCodeInvalid 是「重置密码的验证码不对或已过期」。
	// 与 ErrUnauthorized 分开：重置页面要提示的是验证码，说成密码错只会让人更困惑。
	ErrResetCodeInvalid = errors.New("identity: 验证码不对或已过期")
)

// 用户名规则与 db/schema/10-identity.sql 的 ck 同口径：3–20、[A-Za-z0-9_-]、
// 按小写唯一（标准对象 §ID-2）。两边必须一致——只在一处收紧等于没有约束。
var usernamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{3,20}$`)

// emailPattern 只判「形状像不像一个地址」，不做 RFC 5322 全解析——能不能真收到信
// 只有发一封才知道。但它必须挡住 `a@b@c` 这类地址：那种地址永远收不到邮件，
// 验证码与重置码都到不了，账号会**永久不可恢复**。
var emailPattern = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)

// validateEmail 是注册、找回密码、发登录验证码共用的口径。
//
// 三处各写一份的话，最松的那一处就是实际生效的那一处。
func validateEmail(email string) error {
	// 空白单独报：用户看到「格式不对」通常不知道该删空格。
	if strings.ContainsAny(email, " \t\r\n") {
		return fmt.Errorf("%w: 邮箱不能包含空白字符", ErrInvalidInput)
	}
	if email == "" || len(email) > maxEmailLen || !emailPattern.MatchString(email) {
		return fmt.Errorf("%w: 邮箱格式不对", ErrInvalidInput)
	}
	return nil
}

const (
	minPasswordLen = 10
	maxPasswordLen = 128
	maxDisplayName = 50
	maxEmailLen    = 254
)

// Account 是 domain 眼中的「一个人」。它不认识 api.Me——那一层的形状属于传输层。
type Account struct {
	ID int64
	// UserID 是 demo 站投影表 users 的主键。业务侧（作品作者、创建人等）
	// 引用的是它，不是 ID——两者靠 sub 对齐，ID 只在本域内用。
	UserID      int64
	Sub         string
	Username    string
	DisplayName string
	Email       string
	Role        string
	Status      string
	Level       int32
	CreatedAt   time.Time
}

// RegisterInput 是注册需要的东西。
type RegisterInput struct {
	Email       string
	Password    string
	Username    string
	DisplayName string
}

// Validate 把「用户填错了」与「系统坏了」分开。
//
// 顺序有意：先校验格式再查重（查重在用到它的那层）。反过来的话，一个格式非法的邮箱
// 也会打到数据库，既浪费一次查询，也让「格式错」和「已占用」两种错误混在一起。
func (in RegisterInput) Validate() error {
	if err := validateEmail(strings.TrimSpace(in.Email)); err != nil {
		return err
	}
	// 密码长度按字节算——对中文密码来说这比按字符算更严格，是安全的一侧。
	if n := len(in.Password); n < minPasswordLen || n > maxPasswordLen {
		return fmt.Errorf("%w: 密码长度必须是 %d–%d 个字符", ErrInvalidInput, minPasswordLen, maxPasswordLen)
	}
	if !usernamePattern.MatchString(in.Username) {
		return fmt.Errorf("%w: 用户名只能是 3–20 个字母、数字、下划线或连字符", ErrInvalidInput)
	}
	if len([]rune(in.DisplayName)) > maxDisplayName {
		return fmt.Errorf("%w: 展示名最多 %d 个字符", ErrInvalidInput, maxDisplayName)
	}
	return nil
}
