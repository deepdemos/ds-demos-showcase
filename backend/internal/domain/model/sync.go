// Package model 是模型域的领域服务。
//
// 目前只有目录同步一件事。它没有放在 httpapi 里，因为「同步」同时被两条路触发：
// 管理员的手动端点，以及 `cmd/api` 里的定时循环。逻辑只能有一份。
package model

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/platform/catalog"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

// syncLockKey 是目录同步的 advisory lock key。
//
// 值本身不重要，重要的是全仓唯一。取一个固定常量而不是随机数：多实例必须抢**同一把**
// 锁，随机数等于没锁。
const syncLockKey int64 = 7_312_004_411

// 同步状态值，与 `model_source_sync.status` 的取值一致。
const (
	StatusOK     = "ok"
	StatusStale  = "stale"
	StatusFailed = "failed"
)

// 设置键（设置域 `setting_keys`，默认值也在那里）。
const (
	settingSyncEnabled  = "modelsdev.sync_enabled"
	settingSyncInterval = "modelsdev.sync_interval_hours"
)

// ErrSyncBusy 表示另一处正在同步（或另一个实例抢到了锁）。
//
// 单独一个哨兵：调用方要区分「这次没同步，因为有人在同步」与「同步失败了」——
// 前者不是错误，回 409 或直接跳过都行；后者要让人看到原因。
var ErrSyncBusy = errors.New("model: 另一个同步正在进行")

// ErrSyncDisabled 表示定时同步被设置关掉了（手动触发不受影响）。
var ErrSyncDisabled = errors.New("model: 定时同步未开启")

// Result 是一次同步的结果。
type Result struct {
	// Items 是这次写进镜像的条目数。
	Items int64
	// Hash 是 payload 指纹。
	Hash string
	// Skipped 表示 payload 与上一次相同，没有重写镜像。
	Skipped bool
	// At 是完成时间。
	At time.Time
}

// Service 是模型域的领域服务。
type Service struct {
	pool    *pgxpool.Pool
	q       *store.Queries
	fetcher *catalog.Fetcher
	log     *slog.Logger
}

// New 组装服务。
func New(pool *pgxpool.Pool, q *store.Queries, fetcher *catalog.Fetcher, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{pool: pool, q: q, fetcher: fetcher, log: log}
}

// Sync 立即同步一次。
//
// # 为什么要抢锁
//
// 定时同步在多实例部署下会同时到点；手动端点也可能被连点两次。没有锁的话，
// 两个调用各自拉 6 MB、各自清表、各自灌 9000 行——后完成的那次覆盖前一次的记账，
// 而且中间有一段时间镜像是空的（一个事务内读者看不到，但两次提交之间能）。
//
// # 为什么失败时保留旧镜像
//
// 镜像的价值是「有一条能搜到的目录」。上游抽风一次就把本地清空，比不更新更糟。
func (s *Service) Sync(ctx context.Context) (Result, error) {
	release, err := s.lock(ctx)
	if err != nil {
		return Result{}, err
	}
	defer release()

	raw, err := s.fetcher.Fetch(ctx)
	if err != nil {
		s.recordFailure(ctx, err)
		return Result{}, err
	}
	hash := catalog.Hash(raw)

	// payload 没变就不重写：一天一次、9000 行，没变化时白删白灌一遍没有任何收益，
	// 却会让每个 catalog 行的 id 漂移一次、留下 9000 个死元组。
	if last, err := s.q.GetModelSourceSync(ctx, catalog.Source); err == nil {
		if last.PayloadHash.Valid && last.PayloadHash.String == hash && last.Status == StatusOK {
			return Result{Items: last.ItemCount.Int64, Hash: hash, Skipped: true, At: time.Now()}, nil
		}
	}

	entries, err := catalog.Parse(raw)
	if err != nil {
		s.recordFailure(ctx, err)
		return Result{}, err
	}

	if err := s.write(ctx, entries, hash); err != nil {
		s.recordFailure(ctx, err)
		return Result{}, err
	}
	s.log.Info("模型目录已同步", "source", catalog.Source, "items", len(entries), "hash", hash[:12])
	return Result{Items: int64(len(entries)), Hash: hash, At: time.Now()}, nil
}

// lock 抢 advisory lock。抢不到返回 ErrSyncBusy。
func (s *Service) lock(ctx context.Context) (func(), error) {
	acquired, err := s.q.TryModelSyncLock(ctx, syncLockKey)
	if err != nil {
		return nil, fmt.Errorf("抢同步锁失败: %w", err)
	}
	if !acquired {
		return nil, ErrSyncBusy
	}
	return func() {
		// 用独立的 context：请求被取消时也要把锁还回去，否则这把锁会跟着
		// 连接活到进程结束。
		releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if err := s.q.ReleaseModelSyncLock(releaseCtx, syncLockKey); err != nil {
			s.log.Error("释放同步锁失败", "err", err)
		}
	}, nil
}

// write 在一个事务里「清空 + 灌入 + 记账成功」。
//
// 三件事必须同一个事务：只清不灌会让目录空着，灌了不记账会让下次同步以为没成功。
func (s *Service) write(ctx context.Context, entries []catalog.Entry, hash string) error {
	now := time.Now()
	return s.inTx(ctx, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		if err := q.DeleteCatalogItemsBySource(ctx, catalog.Source); err != nil {
			return fmt.Errorf("清空镜像失败: %w", err)
		}
		rows := make([]store.InsertCatalogItemParams, 0, len(entries))
		for _, e := range entries {
			rows = append(rows, store.InsertCatalogItemParams{
				Source:                   catalog.Source,
				ProviderID:               e.ProviderID,
				ProviderName:             e.ProviderName,
				ExternalID:               e.ExternalID,
				Name:                     e.Name,
				CanonicalID:              text(e.CanonicalID),
				VendorSlug:               text(e.VendorSlug),
				Family:                   text(e.Family),
				Description:              e.Description,
				ModalitiesIn:             e.ModalitiesIn,
				ModalitiesOut:            e.ModalitiesOut,
				ContextWindow:            int4(e.ContextWindow),
				MaxOutput:                int4(e.MaxOutput),
				CostIn:                   float8(e.CostIn),
				CostOut:                  float8(e.CostOut),
				ReleaseDate:              date(e.ReleaseDate),
				KnowledgeCutoff:          text(e.Knowledge),
				OpenWeights:              int4(e.OpenWeights),
				ModelType:                text(e.ModelType),
				UpstreamStatus:           text(e.UpstreamState),
				SupportsToolCall:         int4(e.SupportsToolCall),
				SupportsReasoning:        int4(e.SupportsReasoning),
				SupportsStructuredOutput: int4(e.SupportsStructuredOutput),
				SupportsAttachment:       int4(e.SupportsAttachment),
				RawJson:                  e.RawJSON,
				FetchedAt:                pgtype.Timestamptz{Time: now, Valid: true},
			})
		}
		if _, err := q.InsertCatalogItem(ctx, rows); err != nil {
			return fmt.Errorf("写入镜像失败: %w", err)
		}
		if err := q.UpsertModelSourceSync(ctx, store.UpsertModelSourceSyncParams{
			Source:        catalog.Source,
			LastSuccessAt: pgtype.Timestamptz{Time: now, Valid: true},
			PayloadHash:   text(hash),
			ItemCount:     pgtype.Int8{Int64: int64(len(entries)), Valid: true},
			Status:        StatusOK,
			Error:         "",
		}); err != nil {
			return fmt.Errorf("记录同步状态失败: %w", err)
		}
		return nil
	})
}

// recordFailure 把失败写进 `model_source_sync`：状态 `failed` + 原因。
//
// `last_success_at` / `item_count` / `payload_hash` 保持不变——镜像还是上一次成功那份，
// 那几个值就该停在上一次。
func (s *Service) recordFailure(ctx context.Context, cause error) {
	// 用脱离取消的 context：请求超时正是最常见的失败原因，那时原 context 已经废了，
	// 用它写日志等于把失败原因也一起丢掉。
	recordCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()

	msg := cause.Error()
	if len(msg) > 2000 {
		msg = msg[:2000]
	}
	if err := s.q.UpsertModelSourceSync(recordCtx, store.UpsertModelSourceSyncParams{
		Source:      catalog.Source,
		Status:      StatusFailed,
		Error:       msg,
		PayloadHash: pgtype.Text{},
	}); err != nil {
		s.log.Error("记录同步失败状态失败", "err", err)
		return
	}
	s.log.Warn("模型目录同步失败", "source", catalog.Source, "err", cause)
}

// retryGap 是「上次失败之后多久再试」。
//
// 比定时器的 5 分钟略长：上游长时间不可达时，别变成每跳一次就发一个请求。
const retryGap = 10 * time.Minute

// SyncIfDue 是定时循环的入口：读了设置才知道要不要跑、多久跑一次。
//
// 每次循环都重新读设置，而不是启动时读一次缓存：管理员在界面上打开开关之后
// 不应该还要重启进程。
func (s *Service) SyncIfDue(ctx context.Context) error {
	enabled, err := s.q.GetSiteSettingBoolean(ctx, settingSyncEnabled)
	if err != nil {
		return fmt.Errorf("读同步开关失败: %w", err)
	}
	if !enabled {
		return ErrSyncDisabled
	}

	raw, err := s.q.GetSiteSettingNumber(ctx, settingSyncInterval)
	if err != nil {
		return fmt.Errorf("读同步间隔失败: %w", err)
	}
	hours, err := parseHours(raw)
	if err != nil {
		return err
	}
	interval := time.Duration(hours * float64(time.Hour))

	last, err := s.q.GetModelSourceSync(ctx, catalog.Source)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("读同步状态失败: %w", err)
	}
	if err == nil && last.LastSuccessAt.Valid && last.Status != StatusFailed {
		if time.Since(last.LastSuccessAt.Time) < interval {
			return nil // 没到点
		}
	}
	// 上一次失败了就按 retryGap 重试，而不是等满一个 interval。
	//
	// 只用 last_success_at 判断「到点没有」的话，一次网络抖动会让镜像停在旧版本
	// 直到下一个 interval（默认 24 小时）——恰恰是最需要自动同步的时候它不动了。
	// 也不能没有间隔：定时器 5 分钟一跳，上游长时间不可达就变成每 5 分钟打一次。
	if err == nil && last.Status == StatusFailed && last.LastAttemptAt.Valid {
		if time.Since(last.LastAttemptAt.Time) < retryGap {
			return nil
		}
	}
	_, err = s.Sync(ctx)
	if errors.Is(err, ErrSyncBusy) {
		// 别的实例正在同步。这不是错误，下一次循环再看。
		return nil
	}
	return err
}

// parseHours 解析设置里的间隔（浮点，允许 0.5 小时）。
func parseHours(raw string) (float64, error) {
	var hours float64
	if _, err := fmt.Sscanf(strings.TrimSpace(raw), "%g", &hours); err != nil {
		return 0, fmt.Errorf("同步间隔不是数字: %q", raw)
	}
	if hours <= 0 {
		return 0, fmt.Errorf("同步间隔必须大于 0，当前 %q", raw)
	}
	return hours, nil
}

func (s *Service) inTx(ctx context.Context, fn func(tx pgx.Tx) error) error {
	if s.pool == nil {
		return errors.New("model: 数据库未配置")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func text(v string) pgtype.Text {
	return pgtype.Text{String: v, Valid: v != ""}
}

func int4(v *int32) pgtype.Int4 {
	if v == nil {
		return pgtype.Int4{}
	}
	return pgtype.Int4{Int32: *v, Valid: true}
}

func float8(v *float64) pgtype.Float8 {
	if v == nil {
		return pgtype.Float8{}
	}
	return pgtype.Float8{Float64: *v, Valid: true}
}

func date(v string) pgtype.Date {
	if v == "" {
		return pgtype.Date{}
	}
	t, err := time.Parse("2006-01-02", v)
	if err != nil {
		return pgtype.Date{}
	}
	return pgtype.Date{Time: t, Valid: true}
}
