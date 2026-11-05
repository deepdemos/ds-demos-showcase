package demo

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

// 作品生命周期：下架 / 重新上架 / 软删 / 恢复。
//
// # 两层，不是四个状态
//
// `lifecycle_state`（draft/published/archived/removed）是**可见性层**，
// `deleted_at` 是**删除标记层**。两层是笛卡尔积：`archived + 软删` 合法，
// `published + 软删` 也合法。所以软删**不改** `lifecycle_state`，
// 恢复也只清 `deleted_at`——作品自动回到删除前的档位，不需要「原状态」列
// （生命周期 §4.3 把这条写成了专门的警告：不要往 lifecycle_state 里加 deleted）。
//
// # 为什么不写时间线
//
// 时间线记的是「这个作品对外发生过什么」（Demo 域 §3.11）。下架后对外是墓碑、
// 软删后对外根本不存在。把这两件事写进时间线只有两种下场：读者以为它们对外可见，
// 或者为了自洽给墓碑开一个「曾经被删过」的漏口。所以下架与删除都不进时间线，
// 它们的痕迹在 `updated_at` 与治理域的 `audit_log` 里。
//
// # 恢复窗口为什么读队列行
//
// 窗口在**删除那一刻定型**：`purge_after = 删除时刻 + 当时配置的窗口`。
// 恢复判定只看这一行的 `purge_after > now()`，不用「当前配置」重算——
// 否则管理员把窗口从 30 天调到 45 天，系统就承诺了一件它在第 30 天做不到的事
// （生命周期 §4.2）。

var (
	// ErrLifecycleConflict 表示当前状态不允许这个动作（已被下架、本来没删……）。
	ErrLifecycleConflict = errors.New("demo: 作品当前状态不允许这个操作")
	// ErrRestoreWindowClosed 表示恢复窗口已过。
	ErrRestoreWindowClosed = errors.New("demo: 恢复窗口已过，作品不能再恢复")
	// ErrDerivativePurged 表示窗口内、但文件已被回收。
	ErrDerivativePurged = errors.New("demo: 文件已被回收，作品不能再恢复")
)

// restoreWindowSettingKey 是恢复窗口的参数键。它登记在 `setting_keys` 里
// （db/schema/15-settings.sql），默认 30 天。
const restoreWindowSettingKey = "lifecycle.restore_window_days"

// LifecycleInput 是一次上下架 / 删除请求。
//
// ActorID 是 users.id（与 demos.author_id 同一口径）。管理员代操作也走这条路：
// 传的是管理员的 id，权限判定在 HTTP 层做（与编辑、回滚同一分成）。
type LifecycleInput struct {
	ActorID int64
	Slug    string
	// Reason 只在软删时使用，进 `demos.delete_reason`。
	Reason string
}

// LifecycleResult 是动作成功后的回执。
type LifecycleResult struct {
	Slug           string
	LifecycleState string
	// PurgeAfter 只在软删时有值：作品的最晚恢复时刻。
	PurgeAfter time.Time
}

// Archive 把作品下架（published → archived）。
//
// 下架之后：列表不再出现、详情给墓碑页（200，只有标题与「作者已下架」）、
// 预览与下载 404。**渠道行一个字节不动**——下架不是「删渠道」，
// 重新上架时渠道还在原来的可见性上。
func (s *Service) Archive(ctx context.Context, in LifecycleInput) (LifecycleResult, error) {
	return s.transition(ctx, in, "published", "archived", "只有已发布的作品能下架")
}

// Unarchive 把作品重新上架（archived → published）。
//
// 不留「上架通知」这类动作：上架只改变可见性，对外内容还是原来那一版。
func (s *Service) Unarchive(ctx context.Context, in LifecycleInput) (LifecycleResult, error) {
	return s.transition(ctx, in, "archived", "published", "只有已下架的作品能重新上架")
}

// transition 是所有生命周期跃迁的唯一实现：从 from 到 to，别的什么都不做。
//
// 「跃迁合不合法」不在这里判——它由 SQL 的 WHERE lifecycle_state = @from_state 表达，
// 0 行受影响就是不合法的跃迁。这样做的理由：把合法跃迁表写进 Go 的 if 里，
// 就是同一张状态机在 SQL 与 Go 各存一份，加一种状态时两边要同时改。
func (s *Service) transition(
	ctx context.Context, in LifecycleInput, from, to, conflict string,
) (LifecycleResult, error) {
	row, err := s.lifecycleRow(ctx, in)
	if err != nil {
		return LifecycleResult{}, err
	}
	affected, err := s.q.TransitionDemoLifecycle(ctx, store.TransitionDemoLifecycleParams{
		DemoID:    row.ID,
		FromState: from,
		ToState:   to,
	})
	if err != nil {
		return LifecycleResult{}, fmt.Errorf("demo: 生命周期跃迁失败: %w", err)
	}
	if affected == 0 {
		return LifecycleResult{}, fmt.Errorf("%w: %s，当前是 %s", ErrLifecycleConflict, conflict, row.LifecycleState)
	}
	return LifecycleResult{Slug: row.Slug, LifecycleState: to}, nil
}

// SoftDelete 软删作品。
//
// 写 `deleted_at` 与一条 GC 队列行；`purge_after` 在这里算一次，之后不再重算。
// 队列行的 `object_key` 是作品目录前缀 `demos/{slug}/`：
// 新数据的内容已经内容寻址、不再有这个目录，所以这行**主要是给恢复窗口定时刻**；
// 对旧数据（`demos/{slug}/r{n}/` 还在的作品）它顺便真能把目录清掉。
// 共享的 blob 不在这里删——一份字节可能被其他作品引用，归 blob GC。
func (s *Service) SoftDelete(ctx context.Context, in LifecycleInput) (LifecycleResult, error) {
	row, err := s.lifecycleRow(ctx, in)
	if err != nil {
		return LifecycleResult{}, err
	}
	if row.DeletedAt.Valid {
		return LifecycleResult{}, fmt.Errorf("%w: 作品已经删除了", ErrLifecycleConflict)
	}

	window, err := s.restoreWindow(ctx)
	if err != nil {
		return LifecycleResult{}, err
	}
	purgeAfter := time.Now().Add(window)

	reason := strings.TrimSpace(in.Reason)
	if len([]rune(reason)) > 200 {
		return LifecycleResult{}, fmt.Errorf("%w: 删除理由最多 200 字", ErrInvalidInput)
	}

	err = s.inTx(ctx, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		if err := q.SoftDeleteDemo(ctx, store.SoftDeleteDemoParams{ID: row.ID, DeleteReason: reason}); err != nil {
			return fmt.Errorf("写删除标记失败: %w", err)
		}
		return q.EnqueueDemoGc(ctx, store.EnqueueDemoGcParams{
			ObjectKey:  demoDirKey(row.Slug),
			EntityID:   row.ID,
			PurgeAfter: pgTimestamptz(purgeAfter),
		})
	})
	if err != nil {
		return LifecycleResult{}, fmt.Errorf("demo: 删除失败: %w", err)
	}
	return LifecycleResult{Slug: row.Slug, LifecycleState: row.LifecycleState, PurgeAfter: purgeAfter}, nil
}

// RestoreDeleted 恢复一件软删的作品。
//
// 四道判定，顺序与生命周期 §4.2 那张表逐行一致：
// 已擦除 → 不存在；本来没删 → 冲突；窗口外 → 窗口关闭；文件已被回收 → 派生已清；
// 都过 → 清空 deleted_at 并删掉队列行。
func (s *Service) RestoreDeleted(ctx context.Context, in LifecycleInput) (LifecycleResult, error) {
	row, err := s.lifecycleRow(ctx, in)
	if err != nil {
		return LifecycleResult{}, err
	}
	if !row.DeletedAt.Valid {
		return LifecycleResult{}, fmt.Errorf("%w: 作品没有被删除", ErrLifecycleConflict)
	}

	gc, err := s.q.GetDemoGcWindow(ctx, demoDirKey(row.Slug))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// 行还在但队列行不见了：说明删除发生在入队逻辑之前（历史数据），
			// 没有窗口可判，按最保守的处理——不给恢复，别承诺做不到的事。
			return LifecycleResult{}, fmt.Errorf("%w: 这件作品没有恢复窗口记录", ErrRestoreWindowClosed)
		}
		return LifecycleResult{}, fmt.Errorf("读恢复窗口失败: %w", err)
	}
	if gc.PurgedAt.Valid || gc.AbandonedAt.Valid {
		return LifecycleResult{}, fmt.Errorf("%w: 删除时的文件已经被回收", ErrDerivativePurged)
	}
	if !gc.PurgeAfter.Valid || !gc.PurgeAfter.Time.After(time.Now()) {
		return LifecycleResult{}, fmt.Errorf("%w: 删除已超过可恢复期限", ErrRestoreWindowClosed)
	}

	err = s.inTx(ctx, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		if err := q.RestoreDeletedDemo(ctx, row.ID); err != nil {
			return fmt.Errorf("清删除标记失败: %w", err)
		}
		return q.DeleteDemoGcRow(ctx, demoDirKey(row.Slug))
	})
	if err != nil {
		return LifecycleResult{}, fmt.Errorf("demo: 恢复失败: %w", err)
	}
	return LifecycleResult{Slug: row.Slug, LifecycleState: row.LifecycleState}, nil
}

// lifecycleRow 读现状并做归属与墓碑判定。
//
// **非作者一律 ErrNotFound**，与编辑、回滚同一口径：403 会承认这个 slug 存在过。
// 已擦除的作品对外不存在，对作者也不给——擦除是不可恢复的动作，
// 留一个「你的作品已被擦除」的页面只会让人去找客服（生命周期 §4.1）。
func (s *Service) lifecycleRow(ctx context.Context, in LifecycleInput) (store.GetDemoForLifecycleRow, error) {
	row, err := s.q.GetDemoForLifecycle(ctx, in.Slug)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return store.GetDemoForLifecycleRow{}, fmt.Errorf("%w: 作品不存在", ErrNotFound)
		}
		return store.GetDemoForLifecycleRow{}, fmt.Errorf("读作品失败: %w", err)
	}
	if row.ErasedAt.Valid {
		return store.GetDemoForLifecycleRow{}, fmt.Errorf("%w: 作品不存在", ErrNotFound)
	}
	if !row.AuthorID.Valid || row.AuthorID.Int64 != in.ActorID {
		return store.GetDemoForLifecycleRow{}, fmt.Errorf("%w: 作品不存在", ErrNotFound)
	}
	if row.LifecycleState == "removed" {
		// 违规下架是平台处置，作者不能自己恢复；上架与删除都不该从这里走通。
		return store.GetDemoForLifecycleRow{}, fmt.Errorf("%w: 作品已被平台移除，不能自行操作", ErrNotEditable)
	}
	return row, nil
}

// restoreWindow 读设置域的恢复窗口。
//
// 读不到或解析不出都返回错误而不是退回默认值：静默用默认值会让「配置被改坏」
// 表现成「恢复窗口莫名其妙变回 30 天」，而管理员以为自己设的是 7 天。
func (s *Service) restoreWindow(ctx context.Context) (time.Duration, error) {
	raw, err := s.q.GetSiteSettingNumber(ctx, restoreWindowSettingKey)
	if err != nil {
		return 0, fmt.Errorf("读恢复窗口失败（参数 %s）: %w", restoreWindowSettingKey, err)
	}
	days, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil {
		return 0, fmt.Errorf("恢复窗口参数不是数字（%q）: %w", raw, err)
	}
	if days < 0 {
		return 0, fmt.Errorf("恢复窗口不能是负数（%v）", days)
	}
	return time.Duration(days * 24 * float64(time.Hour)), nil
}

// demoDirKey 是作品目录的 GC 键。
//
// 用前缀而不是裸 slug：GC 要删的是一整个前缀，而 slug 在旧数据里也常被直接
// 当目录名用，两者拼起来才没有歧义。
func demoDirKey(slug string) string {
	return "demos/" + slug + "/"
}

// pgTimestamptz 只在本文件用得到，就地定义，不去动别人的文件。

func pgTimestamptz(v time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: v, Valid: true}
}
