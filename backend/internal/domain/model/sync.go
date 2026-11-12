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

	// payload 没变就连解析都不做：整包 11 MB，摊成 9000 行再逐行比一遍，最后一条都没写，
	// 纯属白跑。按条更新之后这一步不再是「防 id 漂移」的必需品，省下的是解析与一趟写。
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

	written, err := s.write(ctx, entries, hash)
	if err != nil {
		s.recordFailure(ctx, err)
		return Result{}, err
	}
	// 一条日志说清三件事：这次上游给了多少条、真正写了多少条（其余内容没变）、指纹。
	// 「items 与 written 不相等」是正常的，一天里通常只有几条会动。
	s.log.Info("模型目录已同步", "source", catalog.Source, "items", len(entries),
		"written", written, "hash", hash[:12])
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

// write 在一个事务里「按条更新 + 删掉下架的 + 记账成功」。
//
// 三件事必须同一个事务：只删不写会让目录缺条目，写了不删会让下架模型永远留在
// 列表里，两件都做了不记账会让下次同步以为没成功。
//
// 为什么不是「先清空再灌」：镜像是快照，但快照不等于每次换一批 id。整表重写会让
// 9000 行每天都换 id、全是死元组，而且任何记住过某个 catalog id 的人第二天都会
// 拿到别人的行。按条更新之后，只有内容真的变了的行才会被写一次。
//
// 返回被真正写过的行数：其余行内容没变，闸门把它们挡在了 UPDATE 之外。
func (s *Service) write(ctx context.Context, entries []catalog.Entry, hash string) (int64, error) {
	payload, keys, err := catalogPayload(entries)
	if err != nil {
		return 0, err
	}
	now := time.Now()
	var written int64
	err = s.inTx(ctx, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		// 先写后删。同一事务里两种顺序结果一样（keys 与 payload 出自同一批 entries，
		// 先删也删不到这次要写的行），选这个顺序只是因为「先补齐、再清走」是读起来
		// 更省事的那一种：不用先想「删的时候会不会把即将写入的行也删掉」。
		n, err := q.UpsertCatalogItems(ctx, store.UpsertCatalogItemsParams{
			Source:    catalog.Source,
			FetchedAt: pgtype.Timestamptz{Time: now, Valid: true},
			Payload:   payload,
		})
		if err != nil {
			return fmt.Errorf("写入镜像失败: %w", err)
		}
		written = n
		if err := q.DeleteCatalogItemsNotIn(ctx, store.DeleteCatalogItemsNotInParams{
			Source: catalog.Source,
			Keys:   keys,
		}); err != nil {
			return fmt.Errorf("清走下架条目失败: %w", err)
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
	return written, err
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

// text 把空串变成 SQL NULL。
//
// 用到它的地方是 `payload_hash`：空指纹与「没有指纹」必须是同一件事，
// 否则「payload 没变过」的判断会拿一个空串去比。
func text(v string) pgtype.Text {
	return pgtype.Text{String: v, Valid: v != ""}
}
