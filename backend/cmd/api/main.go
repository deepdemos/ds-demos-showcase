package main

import (
	"context"
	"crypto/rand"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/demo"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/identity"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/httpapi"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/kernel/verification"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/platform/config"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/platform/database"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/platform/mailer"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/platform/storage"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg := config.Load()

	// 两个变量都保持 nil，表示「未配置数据库」。注意不能把 *pgxpool.Pool 的 nil 值
	// 直接赋给接口——那会得到「非 nil 接口装着 nil 指针」，后面的 nil 判断全部失效。
	ipSecret := loadIPSecret(cfg)
	verificationSecret := loadVerificationSecret(cfg)

	// 邮件通道。没配 SMTP 时拿到的是 logMailer：它不假装发出去，只告警。
	mail, err := mailer.New(mailer.Config{
		Host:     cfg.SMTPHost,
		Port:     cfg.SMTPPort,
		User:     cfg.SMTPUser,
		Pass:     cfg.SMTPPass,
		From:     cfg.SMTPFrom,
		LogCodes: cfg.MailLogCodes,
		Logger:   logger,
	})
	if err != nil {
		// 配置了一半（有 host 没 from）属于启动期错误，立刻失败。
		logger.Error("mailer config invalid", "err", err)
		os.Exit(1)
	}

	// 作品文件存储。本地磁盘是真相源：OSS 推失败作品还能用，反过来不行。
	// 起不来就启动失败——没有存储就没有投稿，装作能用只会把错误推到用户面前。
	files, err := storage.NewLocal(cfg.StorageRoot)
	if err != nil {
		logger.Error("storage init failed", "root", cfg.StorageRoot, "err", err)
		os.Exit(1)
	}
	logger.Info("storage ready", "root", files.Root())

	var pinger httpapi.Pinger
	var queries *store.Queries
	var identitySvc *identity.Service
	var demoSvc *demo.Service

	if cfg.HasDatabase() {
		pool, err := database.Open(context.Background(), cfg.DatabaseURL)
		if err != nil {
			// URL 写错属于启动期错误，立刻失败比拖到第一次请求更好定位。
			logger.Error("database config invalid", "err", err)
			os.Exit(1)
		}
		defer pool.Close()
		pinger = pool
		queries = store.New(pool)
		identitySvc = identity.New(pool, ipSecret, verification.New(queries, verificationSecret))
		demoSvc = demo.New(
			pool, queries, files,
			storage.DefaultLimits(),
			cfg.UploadMaxPackageMiB<<20,
			cfg.UploadMaxPending,
			logger,
		)
	} else {
		logger.Warn("DATABASE_URL 未配置：/healthz 仍可用，业务端点返回 503")
	}

	server := &http.Server{
		Addr:              cfg.Addr(),
		Handler:           httpapi.NewRouter(httpapi.New(pinger, queries, identitySvc, mail, demoSvc), httpapi.NewPreview(queries, files, identitySvc)),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		logger.Info("api listening", "addr", cfg.Addr(), "database", cfg.HasDatabase())
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("api failed", "err", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		logger.Error("shutdown failed", "err", err)
	}
}

// loadIPSecret 取 IP 哈希密钥。没配就当场生成一把并告警。
//
// 为什么不直接启动失败：本地开发和 CI 里没人会去配这个变量，而它缺失只影响
// 「跨重启、跨实例的限流计数是否连续」，不影响登录正确性。但必须 WARN——
// 否则生产上漏配会表现成「限流时灵时不灵」，那是最难查的一类问题。
func loadIPSecret(cfg config.Config) []byte {
	if cfg.IPHashSecret != "" {
		return []byte(cfg.IPHashSecret)
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		panic("生成 IP 哈希密钥失败: " + err.Error())
	}
	slog.Warn("IP_HASH_SECRET 未配置：本进程临时生成一把。重启后历史 ip_hash 不再可比，多实例之间也互不可见")
	return secret
}

// loadVerificationSecret 与 loadIPSecret 同样的降级策略，但**必须与它分开**：
// 两个用途共用一把密钥，一处泄露会连累另一处。
func loadVerificationSecret(cfg config.Config) []byte {
	if cfg.VerificationSecret != "" {
		return []byte(cfg.VerificationSecret)
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		panic("生成验证码密钥失败: " + err.Error())
	}
	slog.Warn("VERIFICATION_SECRET 未配置：本进程临时生成一把。重启后已发出的验证码全部作废，多实例之间也互不认账")
	return secret
}
