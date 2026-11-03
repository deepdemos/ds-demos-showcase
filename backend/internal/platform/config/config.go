package config

import (
	"os"
	"strconv"
)

// Config 是进程级的启动配置。
//
// 这里只放「启动时决定、之后不变」的东西。可以在运行时改的运营参数走设置域
// （db/schema/15-settings.sql 的 setting_keys），不在这里——判据见
// docs/设置域设计.md §4.1「这 18 条是怎么来的」。
type Config struct {
	Port string
	// DatabaseURL 是 PostgreSQL 连接串。空字符串表示**未配置数据库**：
	// 服务仍然会起，/healthz 也仍然返回 200（当前阶段 API 还没有依赖库的能力）。
	DatabaseURL string
	// IPHashSecret 是给客户端 IP 做 HMAC 的密钥。明文 IP 不落库，但裸 SHA-256
	// 对 IPv4 只有 2^32 种取值，可以枚举反推，所以必须带密钥（身份域 §6）。
	//
	// 未配置时进程内随机生成一把并打 WARN：单实例开发够用，但重启后
	// 历史 ip_hash 就对不上了，多实例之间也互相看不见。
	IPHashSecret string

	// VerificationSecret 是给验证码做 HMAC 的密钥。6 位码只有 10^6 种取值，
	// 裸哈希可以枚举反推，必须带密钥。
	//
	// **不要与 IPHashSecret 混用**：两处用途不同，一把泄露不该连累另一处。
	// 未配置时同样进程内随机生成 + WARN，重启后已发的码全部作废。
	VerificationSecret string

	// 发信配置。它们是**部署拓扑**（用哪家邮件服务商、什么凭据），不是运营参数，
	// 所以不进设置域。全为空时程序仍能启动：邮件会由 logMailer 记一条告警而不发出。
	SMTPHost string
	SMTPPort string
	SMTPUser string
	SMTPPass string
	SMTPFrom string

	// MailLogCodes 只在没配 SMTP 时生效：是否把邮件正文（含验证码）写进日志。
	// 默认 false——验证码进日志在生产上是泄漏。
	MailLogCodes bool

	// StorageRoot 是作品文件的落盘根目录。默认 ./var/storage（相对工作目录）。
	//
	// 它是**部署拓扑**不是运营参数：换盘、换挂载点属于部署动作。所以不进设置域。
	StorageRoot string

	// UploadMaxPackageMiB 是单个上传包的体积上限（MiB）。
	//
	// 默认 200，与 `setting_keys` 里 `demo.upload_package_mib` 的默认值一致。
	// **目前还没接设置域**（设置域服务尚未实现），等它落地后改为运行时读；
	// 在那之前改这个值要发版——已登记在 docs/已知松点.md。
	UploadMaxPackageMiB int64
}

// Load 从环境变量读取配置。
func Load() Config {
	return Config{
		Port:         getenv("PORT", "8080"),
		DatabaseURL:  os.Getenv("DATABASE_URL"),
		IPHashSecret: os.Getenv("IP_HASH_SECRET"),

		VerificationSecret: os.Getenv("VERIFICATION_SECRET"),

		SMTPHost:            os.Getenv("SMTP_HOST"),
		SMTPPort:            os.Getenv("SMTP_PORT"),
		SMTPUser:            os.Getenv("SMTP_USER"),
		SMTPPass:            os.Getenv("SMTP_PASS"),
		SMTPFrom:            os.Getenv("SMTP_FROM"),
		MailLogCodes:        os.Getenv("MAIL_LOG_CODES") == "1",
		StorageRoot:         getenv("STORAGE_ROOT", "./var/storage"),
		UploadMaxPackageMiB: intenv("UPLOAD_MAX_PACKAGE_MIB", 200),
	}
}

// Addr 返回 http.Server 需要的监听地址。
func (c Config) Addr() string {
	return ":" + c.Port
}

// HasDatabase 报告是否配置了数据库连接串。
func (c Config) HasDatabase() bool {
	return c.DatabaseURL != ""
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

// intenv 读正整数配置。值写错时退回默认值而不是启动失败——
// 这类变量缺失或写错只影响「能不能上传大文件」，不该让整个 API 起不来。
func intenv(key string, fallback int64) int64 {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}
