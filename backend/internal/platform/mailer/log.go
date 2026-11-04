package mailer

import (
	"context"
	"log/slog"
)

// logMailer 是「没配 SMTP」时的实现：不发送，只记一条告警，**并且返回错误**。
//
// # 为什么必须返回错误
//
// 这个类型以前 `return nil`——文件注释写着「不假装发出去」，返回值却告诉调用方
// 「发成功了」。后果在注册链路上是可见的：`verificationSent` 恒为 true，
// 用户被告知「验证信已发出」，然后去等一封永远不会来的邮件。
//
// 返回值是调用方唯一能看到的信号，注释不是。
type logMailer struct {
	logger   *slog.Logger
	logCodes bool
}

func (m *logMailer) Send(_ context.Context, to, subject, body string) error {
	attrs := []any{"to", to, "subject", subject}
	if m.logCodes {
		attrs = append(attrs, "body", body)
	}
	// 措辞要让人一眼看懂「这不是成功，是没配」。写在 WARN 级别里。
	m.logger.Warn("未配置 SMTP，邮件没有真的发出（设 MAIL_LOG_CODES=1 可把正文打进日志）", attrs...)
	return ErrNotConfigured
}
