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
	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/model"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/httpapi"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/kernel/verification"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/platform/catalog"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/platform/config"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/platform/database"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/platform/mailer"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/platform/storage"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	// 让全仓直调 slog.* 的地方也走这个 handler（否则出现两种日志格式）。
	slog.SetDefault(logger)
	cfg := config.Load()

	// 两个变量都保持 nil，表示「未配置数据库」。注意不能把 *pgxpool.Pool 的 nil 值
	// 直接赋给接口——那会得到「非 nil 接口装着 nil 指针」，后面的 nil 判断全部失效。
	ipSecret := loadIPSecret(cfg)
	verificationSecret := loadVerificationSecret(cfg)

	// 邮件通道。没配 SMTP 时拿到的是 logMailer：不发送、打一条告警，
	// 并且 Send 返回 ErrNotConfigured——调用方据此告诉用户「信没发出去」。
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
	var modelSvc *model.Service

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
		modelSvc = model.New(pool, queries, catalog.NewFetcher(cfg.ModelCatalogURL, nil), logger)
	} else {
		logger.Warn("DATABASE_URL 未配置：/healthz 仍可用，业务端点返回 503")
	}

	server := &http.Server{
		Addr: cfg.Addr(),
		Handler: httpapi.NewRouter(
			httpapi.New(pinger, queries, identitySvc, mail, demoSvc).WithModels(modelSvc),
			httpapi.NewPreview(queries, files, identitySvc),
			httpapi.NewDownload(queries, files),
		),
		// 头部读完就该有下文：慢读客户端拿着半截请求不吃不喝，
		// 一个连接就是一个 goroutine 加一个文件句柄。
		ReadHeaderTimeout: 5 * time.Second,

		// 读体的上限给 5 分钟而不是 30 秒：`POST /demos` 是 multipart，
		// 契约允许 200 MiB。30 秒会把「网慢一点的大包投稿」直接掐死，
		// 而那正是投稿功能存在的意义。头部仍然只有 5 秒。
		ReadTimeout: 5 * time.Minute,

		// keep-alive 连接的空闲回收。不设的话连接永不释放。
		IdleTimeout: 120 * time.Second,

		// WriteTimeout 刻意不设：它是「整个响应写完」的上限，
		// 而 `/download/{slug}` 可以传几百 MB——慢网下几分钟很正常，
		// 设一个数就等于给下载功能加了个随机的失败点。
		// 代价是「读响应很慢」的客户端能占住一个连接；已知松点 41 记了这件事。
	}

	if modelSvc != nil {
		go scheduleCatalogSync(modelSvc, logger)
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
// 全仓有三十余处直调 slog.Error/Warn（download.go、identity.go、preview.go…）。
// 不 SetDefault 的话它们走标准库的默认文本 handler，而启动路径用 logger.Info 走的是
// JSON——同一次 500 在日志里出现两种格式，且文本那半边丢掉结构化字段。
// 所以 logger 一建好就把默认 handler 换成它。

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
	// 与 IP 密钥不同的地方就在这里：IP 密钥缺失只影响限流计数是否连续，
	// 验证码密钥缺失会让**用户功能**坏掉——A 实例发的码 B 实例永远验不过，
	// 而用户看到的是「验证码错误，请重试」，重发几次都一样。
	//
	// 所以只要配了数据库（也就是它不可能是本地随手起的进程），就没有「临时生成」这个选项。
	if cfg.HasDatabase() {
		panic("VERIFICATION_SECRET 未配置：配了数据库的运行形态下不允许进程内临时生成密钥，" +
			"否则多实例发出的验证码互不认账，用户表现为「验证码总是错」")
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		panic("生成验证码密钥失败: " + err.Error())
	}
	slog.Warn("VERIFICATION_SECRET 未配置：本进程临时生成一把（仅限未配数据库的开发形态）。重启后已发出的验证码全部作废")
	return secret
}

// scheduleCatalogSync 每隔几分钟问一次「到点了没」，而不是按间隔 sleep。
//
// 为什么问而不是睡：间隔是设置域的值，管理员随时可能改（24 小时 → 1 小时）。
// 按启动时读到的值 sleep 一整轮的话，改完要等下一次进程重启才生效。
//
// 为什么不直接按秒级 tick：那一轮里是不是同步由 `SyncIfDue` 判断（读设置、比
// last_success_at、抢 advisory lock），这里只是个闹钟。5 分钟一次对「最快 1 小时
// 同步一次」的场景绰绰有余，代价只是每分钟一条空 SELECT。
func scheduleCatalogSync(svc *model.Service, log *slog.Logger) {
	const tick = 5 * time.Minute
	timer := time.NewTicker(tick)
	defer timer.Stop()

	// 起服先看一次：重启频繁的环境里（滚动发布），等 5 分钟意味着可能永远轮不到。
	catalogTick(svc, log)
	for range timer.C {
		catalogTick(svc, log)
	}
}

func catalogTick(svc *model.Service, log *slog.Logger) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	err := svc.SyncIfDue(ctx)
	switch {
	case err == nil:
		return
	case errors.Is(err, model.ErrSyncDisabled):
		// 开关没开。静默返回：这是正常状态，不是错误。
		return
	case errors.Is(err, model.ErrSyncBusy):
		log.Info("模型目录同步被另一个实例占用，跳过这一轮")
	default:
		// 失败已经写进 model_source_sync 了，这里只留一条日志便于排查。
		log.Warn("定时同步模型目录失败", "err", err)
	}
}
