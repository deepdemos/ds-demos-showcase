// Package mailer 负责把一封纯文本邮件交给 SMTP 服务商。
//
// 它**不认识验证码**：收件人、主题、正文都是调用方给的字符串。所以任何域都能用它。
// 反过来说，验证码的生成、有效期、一次性、发送频率都不在这里——那是业务，在 kernel。
package mailer

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
)

// ErrNotConfigured 表示「这台机器没配 SMTP，信没有真的发出」。
//
// 调用方据此区分两种失败：配置问题（这条）与投递问题（SMTP 报错）。
// 前者的处理是「告诉用户信没发出去，且他知道该找谁」；后者是「重试」。
var ErrNotConfigured = errors.New("mailer: 未配置 SMTP，邮件没有真的发出")

// Mailer 发一封纯文本邮件。
type Mailer interface {
	Send(ctx context.Context, to, subject, body string) error
}

// Config 是发信配置。全部来自环境变量，不进设置域——
// 它们是部署拓扑（用哪家 ESP、什么凭据），不是运营参数。
type Config struct {
	Host string
	Port string
	User string
	Pass string
	From string

	// LogCodes 只在「没配 SMTP」时生效：是否把正文写进日志。
	// 默认 false——验证码进日志在生产上是泄漏。开发要看码时显式打开。
	LogCodes bool

	// Logger 是日志出口，为空时用 slog.Default()。
	// 不做成包级可变变量：那会让测试之间互相污染，而且谁也说不清当前用的是哪个。
	Logger *slog.Logger

	// tlsConfig 只给测试注入自签证书用，生产走系统根证书。
	tlsConfig *tls.Config
}

const defaultPort = "587"

// New 按配置造一个 Mailer。
//
// 没配 Host 时返回 logMailer：**不发信，也不假装发了**——每次 Send 都会打一条
// WARN 说明「未配置 SMTP，邮件没有真的发出」。这样生产上漏配会表现成日志里
// 一眼可见的告警，而不是「用户说收不到验证码」。
func New(cfg Config) (Mailer, error) {
	if cfg.Host == "" {
		log := cfg.Logger
		if log == nil {
			log = slog.Default()
		}
		return &logMailer{logger: log, logCodes: cfg.LogCodes}, nil
	}
	if cfg.From == "" {
		// 从地址是收件方判断「谁发的」的唯一凭据，空着必然被判垃圾或直接拒收。
		return nil, fmt.Errorf("mailer: 配置了 SMTP_HOST 就必须配置 SMTP_FROM")
	}
	return newSMTP(cfg)
}

// port 取配置里的端口，空则 587。
func port(cfg Config) (int, error) {
	raw := cfg.Port
	if raw == "" {
		raw = defaultPort
	}
	p, err := strconv.Atoi(raw)
	if err != nil || p <= 0 || p > 65535 {
		return 0, fmt.Errorf("mailer: SMTP_PORT 不是合法端口: %q", cfg.Port)
	}
	return p, nil
}
