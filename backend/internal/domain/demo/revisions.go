package demo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

// 版本历史与回滚（设计 §7.3）。
//
// # 回滚为什么是「追加」而不是「切换」
//
// 把 `current_revision_id` 指回旧版本是最省事的写法，但那样会造出两种状态：
// 「当前版本是 r2，而 r2 的内容其实是 r1 的」——之后任何一次审核都把 r2 的旧内容当成新内容，
// 时间线里也说不清「是谁、什么时候把它变回去的」。
//
// 所以回滚复用的还是「追加一个新版本」这条路：新版本 `source='rollback'`、清单整表复制
// 目标版本（同一批 blob，零字节写入），`current_revision_id` 指向新版本，审核态回到 pending。
// 好处是同一条审核与发布链路原样适用——没有第二条通往线上的路。

// ErrRevisionNotFound 表示这个作品没有这个版本号（或版本已被擦除）。
var ErrRevisionNotFound = errors.New("demo: 版本不存在")

// Revision 是版本列表里的一行。对外只暴露「作者需要知道的」，
// 不把 manifest_hash / entry_path 这类内部寻址细节透出去。
type Revision struct {
	RevNo       int
	Title       string
	DemoType    string
	Source      string
	FileCount   int64
	TotalBytes  int64
	CreatedAt   time.Time
	Deleted     bool
	Erased      bool
	IsCurrent   bool
	IsPublished bool
}

// ListRevisions 列出某个作品的全部版本（新的在前）。
//
// 授权不在这里做：调用方已经确认「这个人能看这件作品」。
// 领域层去判断权限就得知道 `demo.review` 这类权限键，而那是传输层的事。
func (s *Service) ListRevisions(ctx context.Context, slug string) ([]Revision, error) {
	if s.q == nil {
		return nil, errors.New("demo: 数据库未配置")
	}
	rows, err := s.q.ListDemoRevisions(ctx, slug)
	if err != nil {
		return nil, fmt.Errorf("demo: 列版本失败: %w", err)
	}
	out := make([]Revision, 0, len(rows))
	for _, r := range rows {
		out = append(out, Revision{
			RevNo:       int(r.RevNo),
			Title:       r.Title,
			DemoType:    r.DemoType,
			Source:      r.Source,
			FileCount:   r.FileCount,
			TotalBytes:  r.TotalBytes,
			CreatedAt:   r.CreatedAt.Time,
			Deleted:     r.Deleted,
			Erased:      r.Erased,
			IsCurrent:   r.IsCurrent,
			IsPublished: r.IsPublished,
		})
	}
	return out, nil
}

// RestoreResult 是回滚的回执。
//
// 带 `NewRevNo` 而不是「回滚成功」：作者需要知道自己的作品现在是第几版，
// 而且这个数字会立刻出现在版本列表的第一行。
type RestoreResult struct {
	Slug            string
	RestoredFromNo  int
	NewRevNo        int
	ModerationState string
}

// Restore 把作品回滚到第 revNo 版：追加一个新版本，内容取自 revNo。
//
// 拒绝的几种情况都用「哪个错」表达，不用状态码：
//   - 作品不存在 / 不是作者 / 已删已擦 → ErrNotFound（与编辑同口径：403 会承认 slug 存在）
//   - 被平台移除 → ErrNotEditable（只有管理员能改回来）
//   - 目标版本已擦除 / 已删 → ErrRevisionNotFound（内容列已经空了，回滚过去等于空版本）
//   - 目标版本就是当前版本 → ErrInvalidInput（回滚到「现在」没有意义，只会白造一个版本）
func (s *Service) Restore(ctx context.Context, slug string, revNo int, actorUserID int64) (RestoreResult, error) {
	var out RestoreResult

	if s.q == nil || s.pool == nil {
		return out, errors.New("demo: 数据库未配置")
	}
	if revNo < 1 {
		return out, fmt.Errorf("%w: 版本号必须大于 0", ErrInvalidInput)
	}

	ctx, cancel := context.WithTimeout(ctx, submitTimeout)
	defer cancel()

	row, err := s.q.GetDemoForRestore(ctx, store.GetDemoForRestoreParams{Slug: slug, RevNo: int32(revNo)})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return out, ErrRevisionNotFound
		}
		return out, fmt.Errorf("demo: 查作品版本失败: %w", err)
	}

	if row.DeletedAt.Valid || row.ErasedAt.Valid {
		return out, ErrNotFound
	}
	if !row.AuthorID.Valid || row.AuthorID.Int64 != actorUserID {
		return out, ErrNotFound
	}
	if row.LifecycleState == "removed" {
		return out, ErrNotEditable
	}
	if row.RevisionErasedAt.Valid || row.RevisionDeletedAt.Valid {
		return out, ErrRevisionNotFound
	}
	// 当前版本就是目标版本时不该造新版本：内容会完全一样，还得再审一次。
	if row.CurrentRevisionID.Valid && row.CurrentRevisionID.Int64 == row.RevisionID {
		return out, fmt.Errorf("%w: 第 %d 版就是当前版本", ErrInvalidInput, revNo)
	}

	var newRevNo int
	err = s.inTx(ctx, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)

		next, err := q.NextRevisionNo(ctx, row.DemoID)
		if err != nil {
			return fmt.Errorf("取新版本号失败: %w", err)
		}

		rev, err := q.AppendRollbackRevision(ctx, store.AppendRollbackRevisionParams{
			DemoID:       row.DemoID,
			RevNo:        next,
			Title:        text(row.Title),
			Description:  text(row.Description),
			DemoType:     row.DemoType,
			EntryPath:    row.EntryPath,
			SingleFile:   row.SingleFile,
			ManifestHash: row.ManifestHash,
			FileCount:    pgtype.Int8{Int64: row.FileCount, Valid: true},
			TotalBytes:   pgtype.Int8{Int64: row.TotalBytes, Valid: true},
			CreatedBy:    pgtype.Int8{Int64: actorUserID, Valid: true},
		})
		if err != nil {
			return fmt.Errorf("追加回滚版本失败: %w", err)
		}
		newRevNo = int(rev.RevNo)

		// 清单整表复制：指向同一批 blob，一个字节都不写。
		// 源版本没有清单行（PR #248 之前的老数据）时这里复制出空清单，
		// 读路径会回退到 `demos/{slug}/r{n}/`，所以仍然能工作。
		if err := q.CopyRevisionFiles(ctx, store.CopyRevisionFilesParams{
			TargetRevisionID: rev.ID,
			SourceRevisionID: row.RevisionID,
		}); err != nil {
			return fmt.Errorf("复制清单失败: %w", err)
		}

		if err := q.SetCurrentRevisionForEdit(ctx, store.SetCurrentRevisionForEditParams{
			ID:                row.DemoID,
			CurrentRevisionID: pgtype.Int8{Int64: rev.ID, Valid: true},
		}); err != nil {
			return fmt.Errorf("切换当前版本失败: %w", err)
		}

		if err := q.InsertDemoTimeline(ctx, store.InsertDemoTimelineParams{
			DemoID:  row.DemoID,
			Kind:    "rollback",
			Column3: fmt.Sprintf("作者回滚到第 %d 版，产生第 %d 版等待复审", revNo, newRevNo),
			ActorID: pgtype.Int8{Int64: actorUserID, Valid: true},
		}); err != nil {
			return fmt.Errorf("写时间线失败: %w", err)
		}
		return nil
	})
	if err != nil {
		return out, err
	}

	return RestoreResult{
		Slug:            slug,
		RestoredFromNo:  revNo,
		NewRevNo:        newRevNo,
		ModerationState: "pending",
	}, nil
}
