package demo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

// 渠道上架 / 撤下：给**已经对外**的作品追加或移除一个渠道。
//
// # 为什么与 Publish 分开
//
// `Publish` 做的是状态跃迁（`draft`/`archived` → `published`），只在第一次对外时成立；
// 它把「能不能对外」和「挂到哪个渠道」绑在同一次调用里。第二个渠道没有状态跃迁可做，
// 只剩「再挂一处」，所以需要一个只做后者的端点。
//
// 松点 26 记的就是这件事：`PublishDemo` 的 WHERE 里有 `lifecycle_state IN
// ('draft','archived')`，作品一旦变成 `published`，第二个渠道永远发不出去——
// 而契约里那句「换渠道请走上架端点」指向的端点当时并不存在。
var (
	// ErrChannelNotPublished 表示作品还没有对外版本，不能追加渠道。
	ErrChannelNotPublished = errors.New("demo: 作品还没有对外版本，请先发布")
	// ErrChannelNotFound 表示这个作品没有这个渠道行。
	ErrChannelNotFound = errors.New("demo: 这个作品没有这个渠道")
	// ErrLastChannel 表示这是最后一个渠道，撤掉会让对外版本没有出口。
	ErrLastChannel = errors.New("demo: 这是最后一个渠道，先把作品下架再撤")
)

// ChannelInput 是一次渠道上架请求。Channel 与 Visibility 必填，Lang 可空。
type ChannelInput struct {
	Slug       string
	Channel    string
	Visibility string
	Lang       string
}

// ChannelResult 是上架后的渠道状态。Created 区分「新建」与「改可见性」。
type ChannelResult struct {
	Slug       string
	Channel    string
	Visibility string
	Lang       string
	Created    bool
}

// AddChannel 给已对外的作品追加一个渠道（同渠道重复调用 = 改可见性）。
func (s *Service) AddChannel(ctx context.Context, actor Actor, in ChannelInput) (ChannelResult, error) {
	var out ChannelResult

	if in.Channel == "" {
		return out, fmt.Errorf("%w: 渠道不能为空", ErrInvalidInput)
	}
	switch in.Visibility {
	case "listed", "unlisted", "hidden":
	default:
		return out, fmt.Errorf("%w: %q", ErrInvalidVisibility, in.Visibility)
	}
	if in.Lang == "" {
		in.Lang = publishLangDefault
	}
	switch in.Lang {
	case "zh", "en":
	default:
		return out, fmt.Errorf("%w: lang 只能是 zh 或 en", ErrInvalidInput)
	}
	// 与 Publish 同一张表判定：渠道的语言硬要求不能因为换了个入口就失效。
	if req, ok := channelRequiredLang[in.Channel]; ok && in.Lang != req {
		return out, fmt.Errorf("%w: 渠道 %s 只收 %s 作品，当前是 %s",
			ErrInvalidInput, in.Channel, req, in.Lang)
	}
	if actor.Role == "" {
		return out, fmt.Errorf("%w: 缺少角色", ErrInvalidInput)
	}

	err := s.inTx(ctx, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)

		row, err := q.GetDemoForChannel(ctx, in.Slug)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNotFound
			}
			return fmt.Errorf("demo: 查作品失败: %w", err)
		}
		if row.DeletedAt.Valid || row.ErasedAt.Valid {
			return ErrNotFound
		}
		// 没有对外版本 = 从没发布过。这种作品要走上架（会顺带做状态跃迁），
		// 而不是在这里追加渠道——否则会得到「published 但对外一个字节也没有」。
		if !row.PublishedRevisionID.Valid {
			return ErrChannelNotPublished
		}
		if !approvedContent(row.ModerationState) {
			return ErrNotApproved
		}
		if err := activeChannel(ctx, tx, in.Channel); err != nil {
			return err
		}

		existing, err := q.ListDemoChannels(ctx, row.ID)
		if err != nil {
			return fmt.Errorf("demo: 查渠道行失败: %w", err)
		}
		created := true
		for _, c := range existing {
			if c.Channel == in.Channel {
				created = false
				break
			}
		}

		if err := q.UpsertDemoChannel(ctx, store.UpsertDemoChannelParams{
			DemoID:     row.ID,
			Channel:    in.Channel,
			Visibility: in.Visibility,
			Lang:       in.Lang,
			CuratedBy:  int8(actor.UserID),
		}); err != nil {
			return fmt.Errorf("demo: 落渠道行失败: %w", err)
		}

		after, err := json.Marshal(map[string]string{
			"channel":    in.Channel,
			"visibility": in.Visibility,
			"lang":       in.Lang,
		})
		if err != nil {
			return fmt.Errorf("demo: 序列化留痕失败: %w", err)
		}
		// 时间线记的是「对外发生了什么」：多挂一个渠道确实是读者可见的变化，
		// 所以与首次发布一样写 publish。改可见性也写，message 里带上前后的值。
		msg := "上架到 " + in.Channel
		if !created {
			msg = "改 " + in.Channel + " 的可见性为 " + in.Visibility
		}
		if err := q.InsertDemoTimeline(ctx, store.InsertDemoTimelineParams{
			DemoID:  row.ID,
			Kind:    "publish",
			Column3: msg,
			ActorID: int8(actor.UserID),
		}); err != nil {
			return fmt.Errorf("demo: 写时间线失败: %w", err)
		}
		// audit_actions 里 attach / detach 的语义就是「建立关联 / 解除关联」，
		// 渠道行正是一条关联；不新造 publish 键（那要迁移 + 改文档）。
		action := "attach"
		if !created {
			action = "update"
		}
		if err := q.InsertAuditLog(ctx, store.InsertAuditLogParams{
			ActorID:   int8(actor.AccountID),
			ActorRole: actor.Role,
			Action:    action,
			EntityID:  row.ID,
			After:     after,
		}); err != nil {
			return fmt.Errorf("demo: 写审计失败: %w", err)
		}

		out = ChannelResult{
			Slug:       in.Slug,
			Channel:    in.Channel,
			Visibility: in.Visibility,
			Lang:       in.Lang,
			Created:    created,
		}
		return nil
	})
	if err != nil {
		return ChannelResult{}, err
	}
	return out, nil
}

// RemoveChannel 从某个渠道撤下。作品可以仍有别的渠道——那正是多渠道的意义。
func (s *Service) RemoveChannel(ctx context.Context, actor Actor, slug, channel string) error {
	if channel == "" {
		return fmt.Errorf("%w: 渠道不能为空", ErrInvalidInput)
	}
	if actor.Role == "" {
		return fmt.Errorf("%w: 缺少角色", ErrInvalidInput)
	}

	return s.inTx(ctx, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)

		row, err := q.GetDemoForChannel(ctx, slug)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNotFound
			}
			return fmt.Errorf("demo: 查作品失败: %w", err)
		}
		if row.DeletedAt.Valid || row.ErasedAt.Valid {
			return ErrNotFound
		}

		existing, err := q.ListDemoChannels(ctx, row.ID)
		if err != nil {
			return fmt.Errorf("demo: 查渠道行失败: %w", err)
		}
		found := false
		for _, c := range existing {
			if c.Channel == channel {
				found = true
				break
			}
		}
		if !found {
			return ErrChannelNotFound
		}
		// 仍是发布态却把最后一个渠道撤掉，会留下「published 但哪儿都看不到」的作品：
		// 列表靠渠道行、详情靠生命周期，两个判据打架时没人说得清它到底在不在。
		// 想彻底撤下就走下架（archive），那条路径有墓碑与恢复。
		if publishedLifecycle(row.LifecycleState) && len(existing) == 1 {
			return ErrLastChannel
		}

		n, err := q.DeleteDemoChannel(ctx, store.DeleteDemoChannelParams{
			DemoID:  row.ID,
			Channel: channel,
		})
		if err != nil {
			return fmt.Errorf("demo: 删渠道行失败: %w", err)
		}
		if n != 1 {
			return ErrChannelNotFound
		}

		before, err := json.Marshal(map[string]string{"channel": channel})
		if err != nil {
			return fmt.Errorf("demo: 序列化留痕失败: %w", err)
		}
		if err := q.InsertDemoTimeline(ctx, store.InsertDemoTimelineParams{
			DemoID:  row.ID,
			Kind:    "archive",
			Column3: "从 " + channel + " 撤下",
			ActorID: int8(actor.UserID),
		}); err != nil {
			return fmt.Errorf("demo: 写时间线失败: %w", err)
		}
		if err := q.InsertAuditLog(ctx, store.InsertAuditLogParams{
			ActorID:   int8(actor.AccountID),
			ActorRole: actor.Role,
			Action:    "detach",
			EntityID:  row.ID,
			Before:    before,
		}); err != nil {
			return fmt.Errorf("demo: 写审计失败: %w", err)
		}
		return nil
	})
}

// activeChannel 判渠道已登记且启用。外键只保证「在 channels 里」，
// 已停用的渠道仍能在表里存在，所以这里再判一次 status。
func activeChannel(ctx context.Context, tx pgx.Tx, code string) error {
	var status string
	if err := tx.QueryRow(ctx, `SELECT status FROM channels WHERE code = $1`, code).Scan(&status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("%w: %q 未登记", ErrInvalidChannel, code)
		}
		return fmt.Errorf("demo: 查渠道失败: %w", err)
	}
	if status != "active" {
		return fmt.Errorf("%w: %q 已停用", ErrInvalidChannel, code)
	}
	return nil
}
