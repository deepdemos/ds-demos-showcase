package main

import (
	"testing"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/platform/config"
)

// 验证码密钥缺失与 IP 密钥缺失不是一回事：后者只让限流计数不连续，
// 前者会让「A 实例发的码 B 实例永远验不过」——用户看到的是「验证码总是错」，
// 而重发几次都一样。所以配了数据库的运行形态下必须直接退出，而不是 WARN 后照跑。
func TestLoadVerificationSecretRefusesRandomWhenDatabaseConfigured(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("配了数据库却没配 VERIFICATION_SECRET 时应当直接失败，而不是进程内临时生成一把")
		}
	}()
	loadVerificationSecret(config.Config{DatabaseURL: "postgres://user:pass@10.1.2.3:5432/db"})
}

// 没配数据库的开发形态仍然允许临时生成：否则每个本地起的进程都要先配一个变量。
func TestLoadVerificationSecretAllowsRandomWithoutDatabase(t *testing.T) {
	got := loadVerificationSecret(config.Config{})
	if len(got) != 32 {
		t.Fatalf("未配数据库时应临时生成 32 字节密钥，实际 %d 字节", len(got))
	}
}

// 配了就一定用配的那把，且与 IP 密钥不共用。
func TestLoadVerificationSecretUsesConfiguredValue(t *testing.T) {
	got := loadVerificationSecret(config.Config{VerificationSecret: "explicit-secret"})
	if string(got) != "explicit-secret" {
		t.Fatalf("应使用配置值，实际 %q", string(got))
	}
}
