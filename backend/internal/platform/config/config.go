package config

import "os"

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
}

// Load 从环境变量读取配置。
func Load() Config {
	return Config{
		Port:        getenv("PORT", "8080"),
		DatabaseURL: os.Getenv("DATABASE_URL"),
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
