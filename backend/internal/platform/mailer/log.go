package mailer

import (
	"context"
	"log/slog"
)

// logMailer 是「没配 SMTP」时的实现：不发送，只记一条告警。
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
	return nil
}
