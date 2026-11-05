package demo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

// 审核裁决。
//
// # 为什么只有 pending 能裁决
//
// `approved → 其它` 会被 `trg_demos_guard` 拦住（还存在 listed 渠道时不允许撤下）。
// 与其让调用方撞一条看不懂的数据库异常，不如在这里明确回 409，并指向「下架」。
// 已经 rejected 的作品同理：想再审一次，先说清是作者改了重投还是管理员改判，
// 那是另一个动作，不是重复调用同一件事。
//
// # 为什么裁决与留痕在同一个事务里
//
// 「状态改了但没人知道是谁改的」比「没改」更难查。demo_timeline 与 audit_log
// 任何一处写失败，整体回滚。
const (
	// reviewReasonMax 与 demo_timeline.message 的 CHECK（<= 500）和
	// audit_log.reason 的 CHECK 一致。在这里先挡一次，报错才说得清是哪一列。
	reviewReasonMax = 500
)

var (
	// ErrNotFound 表示作品不存在，或已被软删/擦除。
	ErrNotFound = errors.New("demo: 作品不存在")
	// ErrNotPending 表示作品不处于待审状态，不能再次裁决。
	ErrNotPending = errors.New("demo: 作品不处于待审状态")
)

// Actor 是「谁在操作」。两处留痕的主键不同，所以两个都要：
// audit_log.actor_id 指 accounts.id，demo_timeline.actor_id 指 users.id。
//
// 只由传输层从会话构造，不接受请求体自报——否则审计可以伪造（治理域 §5.2）。
//
// 它原来叫 ReviewActor，只服务审核裁决；生命周期的四个动作也要写审计之后，
// 这个名字就成了错的（下架不是「裁决」），所以按用途改成 Actor。
type Actor struct {
	AccountID int64
	UserID    int64
	Role      string
}

// ReviewInput 是一次裁决请求。
type ReviewInput struct {
	Slug    string
	Approve bool
	Reason  string
}

// ReviewResult 是裁决后的状态。
type ReviewResult struct {
	Slug            string
	ModerationState string
}

// Review 把作品从 pending 推到 approved / rejected，并写两处留痕。
func (s *Service) Review(ctx context.Context, actor Actor, in ReviewInput) (ReviewResult, error) {
	if len(in.Reason) > reviewReasonMax {
		return ReviewResult{}, fmt.Errorf("%w: 说明最多 %d 个字符", ErrInvalidInput, reviewReasonMax)
	}
	if actor.Role == "" {
		// 没有角色就没法写 actor_role，而那列是 NOT NULL。宁可不写，也不猜一个值。
		return ReviewResult{}, fmt.Errorf("%w: 缺少调用者角色", ErrInvalidInput)
	}

	next := "rejected"
	timelineKind := "reject"
	if in.Approve {
		next = "approved"
		timelineKind = "approve"
	}

	var out ReviewResult
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)

		cur, err := q.GetDemoForReview(ctx, in.Slug)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNotFound
			}
			return fmt.Errorf("查作品失败: %w", err)
		}
		// 擦除/软删的作品对外不存在，对审核员也一样。
		if cur.DeletedAt.Valid || cur.ErasedAt.Valid {
			return ErrNotFound
		}
		if cur.ModerationState != "pending" {
			return ErrNotPending
		}

		row, err := q.ReviewDemo(ctx, store.ReviewDemoParams{
			Slug:            in.Slug,
			ModerationState: next,
		})
		if err != nil {
			// 走到这里说明上面读到 pending 之后有人抢先改了它。
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNotPending
			}
			return fmt.Errorf("更新审核状态失败: %w", err)
		}

		// 通过 = 把对外版本切到当前版本（迁移 0014）。
		//
		// 这一步是「审核」与「对外」之间的唯一连接点：审核态只描述当前版本，
		// 对外内容由 published_revision_id 决定。漏了它，新版本审过了却永远不生效，
		// 表现为「审核通过但作品还是旧的」——比报错难查得多。
		if in.Approve {
			if err := q.SetPublishedRevision(ctx, in.Slug); err != nil {
				return fmt.Errorf("切换对外版本失败: %w", err)
			}
		}

		if err := q.InsertDemoTimeline(ctx, store.InsertDemoTimelineParams{
			DemoID:  row.ID,
			Kind:    timelineKind,
			Column3: in.Reason,
			ActorID: int8(actor.UserID),
		}); err != nil {
			return fmt.Errorf("写时间线失败: %w", err)
		}

		before, _ := json.Marshal(map[string]string{"moderation_state": cur.ModerationState})
		after, _ := json.Marshal(map[string]string{"moderation_state": row.ModerationState})
		if err := q.InsertAuditLog(ctx, store.InsertAuditLogParams{
			ActorID:   int8(actor.AccountID),
			ActorRole: actor.Role,
			Action:    "review",
			EntityID:  row.ID,
			Before:    before,
			After:     after,
			Reason:    in.Reason,
		}); err != nil {
			return fmt.Errorf("写审计失败: %w", err)
		}

		out = ReviewResult{Slug: row.Slug, ModerationState: row.ModerationState}
		return nil
	})
	if err != nil {
		return ReviewResult{}, err
	}
	return out, nil
}

// PendingReview 是队列里的一项。
type PendingReview struct {
	Slug         string
	Title        string
	Description  string
	AuthorName   string
	CurrentRevNo int
	SubmittedAt  time.Time
}

// ListPendingReviews 返回待审队列。
func (s *Service) ListPendingReviews(ctx context.Context, limit int32) ([]PendingReview, error) {
	rows, err := s.q.ListPendingDemoReviews(ctx, limit)
	if err != nil {
		return nil, fmt.Errorf("查待审队列失败: %w", err)
	}
	out := make([]PendingReview, 0, len(rows))
	for _, r := range rows {
		out = append(out, PendingReview{
			Slug:         r.Slug,
			Title:        r.Title.String,
			Description:  r.Description.String,
			AuthorName:   r.AuthorName,
			CurrentRevNo: int(r.CurrentRevNo),
			SubmittedAt:  r.CreatedAt.Time,
		})
	}
	return out, nil
}
