package mailer

import (
	"context"
	"fmt"
	"time"

	"github.com/wneessen/go-mail"
)

// smtpMailer 用 go-mail 发信。
//
// 为什么不用标准库 net/smtp：它已冻结，且不内置两件与安全直接相关的事——
// 隐式 TLS（465）与「服务端不支持 STARTTLS 时必须失败」。go-mail 把这两件
// 变成一句声明（TLSMandatory / WithSSLPort），而不是几十行我写的判断。
type smtpMailer struct {
	client *mail.Client
	from   string
}

func newSMTP(cfg Config) (*smtpMailer, error) {
	p, err := port(cfg)
	if err != nil {
		return nil, err
	}

	opts := []mail.Option{
		mail.WithPort(p),
		// 15 秒：验证码邮件的体量发不出去就是发不出去，等更久只会拖住 goroutine。
		mail.WithTimeout(15 * time.Second),
		// TLS 强制。少了这条，遇到不支持 STARTTLS 的服务端会退化成明文，
		// 凭据和验证码就一起裸奔了。
		mail.WithTLSPolicy(mail.TLSMandatory),
	}
	// 465 是隐式 TLS（先握手再说话），与 587 的 STARTTLS 语义不同，必须分开。
	if p == 465 {
		opts = append(opts, mail.WithSSL())
	}
	if cfg.User != "" {
		opts = append(opts,
			mail.WithSMTPAuth(mail.SMTPAuthPlain),
			mail.WithUsername(cfg.User),
			mail.WithPassword(cfg.Pass),
		)
	}
	if cfg.tlsConfig != nil {
		opts = append(opts, mail.WithTLSConfig(cfg.tlsConfig))
	}

	client, err := mail.NewClient(cfg.Host, opts...)
	if err != nil {
		return nil, fmt.Errorf("mailer: 初始化 SMTP 客户端失败: %w", err)
	}
	return &smtpMailer{client: client, from: cfg.From}, nil
}

func (m *smtpMailer) Send(ctx context.Context, to, subject, body string) error {
	msg := mail.NewMsg()
	if err := msg.From(m.from); err != nil {
		return fmt.Errorf("mailer: 发件地址不合法: %w", err)
	}
	if err := msg.To(to); err != nil {
		return fmt.Errorf("mailer: 收件地址不合法: %w", err)
	}
	msg.Subject(subject)
	msg.SetBodyString(mail.TypeTextPlain, body)

	// 每次发送单独建连：验证码邮件频率极低（用户输错 5 次才会触发一次），
	// 复用的收益抵不上连接状态管理的复杂度与出错面。
	if err := m.client.DialAndSendWithContext(ctx, msg); err != nil {
		return fmt.Errorf("mailer: 发送失败: %w", err)
	}
	return nil
}
