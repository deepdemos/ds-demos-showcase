package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/platform/config"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/platform/database"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/platform/httpserver"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg := config.Load()

	// pinger 是 nil 接口表示「未配置数据库」。注意不能把 *pgxpool.Pool 的 nil 值
	// 直接赋给接口——那会得到一个非 nil 的接口，里面装着 nil 指针。
	var pinger httpserver.Pinger
	if cfg.HasDatabase() {
		pool, err := database.Open(context.Background(), cfg.DatabaseURL)
		if err != nil {
			// URL 写错属于启动期错误，立刻失败比拖到第一次请求更好定位。
			logger.Error("database config invalid", "err", err)
			os.Exit(1)
		}
		defer pool.Close()
		pinger = pool
	} else {
		logger.Warn("DATABASE_URL 未配置：服务可启动，但依赖数据库的能力不可用")
	}

	server := &http.Server{
		Addr:              cfg.Addr(),
		Handler:           httpserver.NewRouter(pinger),
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
