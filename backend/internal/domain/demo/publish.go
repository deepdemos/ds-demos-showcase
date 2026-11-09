package demo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

// 发布：把 draft（或 archived）推到 published，并在同一事务里落渠道行。
//
// # 为什么发布与审核是两个端点、两个权限
//
// 审核（`demo.review`）判的是「内容合不合规」；发布（`demo.publish`）判的是
// 「要不要对外存在」。设计里这两件事正交（Demo 域 §6.1「审核通过 ≠ 已发布」）——
// 合规的作品可以继续存着不发，发了的也可以随时下架而不改变合规结论。
// 合成一个动作之后，「驳回」和「下架」就分不清了。
//
// # 为什么渠道行必须同事务
//
// 状态是 published 但没有渠道行，作品在列表里查不到（列表按 `demo_channels`
// 的 `listed` 关联）。反过来有渠道行但状态是 draft，直访 `/demos/{slug}` 会 404
// 而列表却能查到——两种半截状态都不可解释。所以 UPDATE 与 INSERT 一起成功或一起回滚。
const (
	// publishLangDefault 是没传 lang 时的默认值，与 demo_channels.lang 的默认一致。
	publishLangDefault = "zh"
)

// channelRequiredLang 是「这个渠道只收哪种语言」的唯一登记处。
//
// 为什么不查库：渠道的语言要求是产品约定，不是运营数据——`channels` 表里
// 没有这一列，为一个稳定的两行映射加一列 + 一次迁移不划算。等渠道多到
// 需要运营自己配的时候，再把它挪进 `channels.required_lang`。
var channelRequiredLang = map[string]string{
	"astra": "en",
}

var (
	// ErrNotApproved 表示内容还没通过审核，不能发布。
	ErrNotApproved = errors.New("demo: 作品还没通过审核")
	// ErrAlreadyPublished 表示作品已经是 published。
	ErrAlreadyPublished = errors.New("demo: 作品已经发布")
	// ErrPublishConflict 表示查到状态之后、改之前被别的写者抢先改了。
	// 单独一个哨兵是因为「不处于待审状态」（ErrNotPending）是审核链路的话术，
	// 在发布链路上答非所问——调用方看到「待审」会以为自己走错了端点。
	ErrPublishConflict = errors.New("demo: 作品状态刚刚被改过，请刷新后重试")
	// ErrRemovedCannotPublish 表示违规下架的作品不能直接发布。
	ErrRemovedCannotPublish = errors.New("demo: 违规下架的作品不能直接发布，请走申诉")
	// ErrInvalidChannel 表示渠道码值不可用（未登记 / 已停用）。
	ErrInvalidChannel = errors.New("demo: 渠道不可用")
	// ErrInvalidVisibility 表示可见性不在枚举内。
	ErrInvalidVisibility = errors.New("demo: 可见性取值非法")
)

// PublishInput 是一次发布请求。Channel 与 Visibility 必填，Lang 可空。
type PublishInput struct {
	Slug       string
	Channel    string
	Visibility string
	Lang       string
}

// PublishResult 是发布成功后的对外状态。
type PublishResult struct {
	Slug           string
	LifecycleState string
	Channel        string
	Visibility     string
}

// Publish 把作品推到 published 并落渠道行。
func (s *Service) Publish(ctx context.Context, actor Actor, in PublishInput) (PublishResult, error) {
	var out PublishResult

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
	// 渠道对语言有硬要求：Astra 橱窗只收英文作品（Demo 域 §3.5 / §7.4）。
	//
	// 以前这条只在发布表单里挡着（前端把语言锁成 en 并禁用下拉），后端不判——
	// 一道只在浏览器里存在的校验等于没有：curl 一次就能把中文作品发到英文橱窗，
	// 而那个橱窗的读者一个字也读不懂。
	if req, ok := channelRequiredLang[in.Channel]; ok && in.Lang != req {
		return out, fmt.Errorf("%w: 渠道 %s 只收 %s 作品，当前是 %s",
			ErrInvalidInput, in.Channel, req, in.Lang)
	}
	if actor.Role == "" {
		return out, fmt.Errorf("%w: 缺少角色", ErrInvalidInput)
	}

	err := s.inTx(ctx, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)

		row, err := q.GetDemoForPublish(ctx, in.Slug)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNotFound
			}
			return fmt.Errorf("demo: 查作品失败: %w", err)
		}
		if row.DeletedAt.Valid || row.ErasedAt.Valid {
			return ErrNotFound
		}
		// 状态判定顺序是有意的：先看「能不能发」，再看「是不是已经发了」。
		// 反过来会把「未审核」报成「已发布」，让人以为是重复调用。
		if row.LifecycleState == "removed" {
			return ErrRemovedCannotPublish
		}
		if publishedLifecycle(row.LifecycleState) {
			return ErrAlreadyPublished
		}
		if !approvedContent(row.ModerationState) {
			return ErrNotApproved
		}
		if err := activeChannel(ctx, tx, in.Channel); err != nil {
			return err
		}

		n, err := q.PublishDemo(ctx, in.Slug)
		if err != nil {
			return fmt.Errorf("demo: 发布失败: %w", err)
		}
		if n != 1 {
			// 走到这里说明上面刚查过的状态在事务里被别人改了。
			// 可能是并发发布，也可能是同时被审核退回。两种都回 409。
			return ErrPublishConflict
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

		detail, err := json.Marshal(map[string]string{
			"lifecycle_state": "published",
			"channel":         in.Channel,
			"visibility":      in.Visibility,
		})
		if err != nil {
			return fmt.Errorf("demo: 序列化留痕失败: %w", err)
		}
		if err := q.InsertDemoTimeline(ctx, store.InsertDemoTimelineParams{
			DemoID:  row.ID,
			Kind:    "publish",
			Column3: "发布到 " + in.Channel,
			ActorID: int8(actor.UserID),
		}); err != nil {
			return fmt.Errorf("demo: 写时间线失败: %w", err)
		}
		// 动作用 status_set（状态跃迁）而不是新造一个 publish：
		// audit_actions 是目录表，加一个键要迁移 + 改文档，
		// 而「draft → published」本来就是一次状态跃迁，语义对得上。
		before, err := json.Marshal(map[string]string{"lifecycle_state": row.LifecycleState})
		if err != nil {
			return fmt.Errorf("demo: 序列化留痕前态失败: %w", err)
		}
		if err := q.InsertAuditLog(ctx, store.InsertAuditLogParams{
			ActorID:   int8(actor.AccountID),
			ActorRole: actor.Role,
			Action:    "status_set",
			EntityID:  row.ID,
			Before:    before,
			After:     detail,
			Reason:    "",
		}); err != nil {
			return fmt.Errorf("demo: 写审计失败: %w", err)
		}

		out = PublishResult{
			Slug:           in.Slug,
			LifecycleState: "published",
			Channel:        in.Channel,
			Visibility:     in.Visibility,
		}
		return nil
	})
	if err != nil {
		return PublishResult{}, err
	}
	return out, nil
}

// 状态判定的唯一出处。
//
// 这两条在发布、渠道上架两条路径上都要用（还要在别处判「对外可见」），
// 所以收成函数：字面量只写一次，`check-single-source` 才不会把第 N 个入口
// 当成第 N 份实现。渠道的「已登记且启用」同理，见 channels.go 的 activeChannel。
func approvedContent(moderationState string) bool {
	return moderationState == "approved"
}

func publishedLifecycle(lifecycleState string) bool {
	return lifecycleState == "published"
}
