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

	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/identity"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/httpapi"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/platform/config"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/platform/database"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg := config.Load()

	// 两个变量都保持 nil，表示「未配置数据库」。注意不能把 *pgxpool.Pool 的 nil 值
	// 直接赋给接口——那会得到「非 nil 接口装着 nil 指针」，后面的 nil 判断全部失效。
	ipSecret := loadIPSecret(cfg)

	var pinger httpapi.Pinger
	var queries *store.Queries
	var identitySvc *identity.Service

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
		identitySvc = identity.New(pool, ipSecret)
	} else {
		logger.Warn("DATABASE_URL 未配置：/healthz 仍可用，业务端点返回 503")
	}

	server := &http.Server{
		Addr:              cfg.Addr(),
		Handler:           httpapi.NewRouter(httpapi.New(pinger, queries, identitySvc)),
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
