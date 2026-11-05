package demo

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

// 更新作品：追加一个新版本，等复审。
//
// # 为什么是「追加版本」而不是「就地改」
//
// `demo_revisions` 上有不可变触发器（`demo_revision_immutable`，迁移 0009）：
// 一旦创建只允许改 `deleted_at`。这不是阻碍，是设计——审核过的内容必须能原样追溯，
// 否则「他改完又改回去了」这种事没人能证明。
//
// # 为什么旧版本继续对外服务
//
// 迁移 0014 把「当前版本」（`current_revision_id`，作者视角）与「对外版本」
// （`published_revision_id`，访客视角）拆成了两条时间轴。更新只动前者并把审核态
// 打回 `pending`；对外版本要等管理员通过才切换（`SetPublishedRevision`）。
// 所以作者改稿期间，作品在作品库、详情页、预览、下载里都还是上一版——不会
// 因为「有人点了保存」就让线上作品消失半小时。
//
// # 为什么不复制文件
//
// 只改标题时新版本的清单是上一版的整表复制（`CopyRevisionFiles`），
// 每行都指向同一批 blob——零字节新增。换文件时只有真正变了的那几个文件会写新 blob。
var (
	// ErrNotEditable 表示作品当前状态不允许编辑（被平台移除）。
	ErrNotEditable = errors.New("demo: 作品当前状态不允许编辑")
)

// UpdateInput 是一次编辑请求。
//
// 与 SubmitInput 的差异只有一个：**上传包可选**。不传就表示「只改文字」。
// 不拆成两个端点（PATCH 元数据 + PUT 文件）是因为它们的校验、鉴权、复审、
// 留痕完全一样，拆开只会让两份代码慢慢长歪。
type UpdateInput struct {
	// AuthorID 是 users.id（与 demos.author_id 同一口径）。
	AuthorID    int64
	Slug        string
	Title       string
	Description string
	// DemoType 只在同时上传了新包时才允许变——类型是内容决定的，
	// 只改文字却把 web 改成 zip，会让下载与预览指着不同的事实。
	DemoType string
	// PackagePath 为空表示只改文字。
	PackagePath string
	PackageName string
	PackageSize int64
	// ModelSlugs 是这一版声明的模型（slug）。
	//
	// 语义是**整体替换**：请求里给了就以它为准（空列表 = 这一版不声明任何模型），
	// 不给也是空列表。不做「没传就沿用上一版」——multipart 里「没传」与「传了空」
	// 在浏览器侧无法区分（空选择不会产生字段），沿用会让作者永远删不掉一个声明。
	ModelSlugs []string
}

// UpdateResult 是编辑成功后的回执。
type UpdateResult struct {
	Slug            string
	RevisionNo      int
	ModerationState string
	LifecycleState  string
	// FilesChanged 表示这一版是否换了文件（false = 只改了文字）。
	FilesChanged bool
}

// Update 追加一个新版本。
//
// 顺序：校验 → 读现状 → 鉴权与状态闸 → （有包就暂存并传字节）→ 事务里写版本、
// 清单、当前版本指针、时间线。
//
// 字节在事务**之前**传：内容寻址的写入是幂等的，重复写不会出错；而把 200 MiB
// 塞进事务会占住连接、阻塞 vacuum，失败时回滚的代价也更高。代价是事务失败会留下
// 孤儿 blob——归 GC 管（松点 33）。
func (s *Service) Update(ctx context.Context, in UpdateInput) (UpdateResult, error) {
	var out UpdateResult

	if err := validateMeta(in.Title, in.Description); err != nil {
		return out, err
	}
	if s.maxPackageBytes > 0 && in.PackageSize > s.maxPackageBytes {
		return out, fmt.Errorf("%w（%d > %d 字节）", ErrPackageTooLarge, in.PackageSize, s.maxPackageBytes)
	}

	ctx, cancel := context.WithTimeout(ctx, submitTimeout)
	defer cancel()

	cur, err := s.q.GetDemoForEdit(ctx, in.Slug)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return out, ErrNotFound
		}
		return out, fmt.Errorf("demo: 查作品失败: %w", err)
	}
	// 擦除 / 软删的作品对外不存在，对作者也一样：他的行已经拿走了。
	if cur.DeletedAt.Valid || cur.ErasedAt.Valid {
		return out, ErrNotFound
	}
	// 不是作者就当作不存在，不返回 403——403 会说「这个 slug 是存在的」。
	if !cur.AuthorID.Valid || cur.AuthorID.Int64 != in.AuthorID {
		return out, ErrNotFound
	}
	// 被平台移除的作品不允许作者自行改回来：那是一条只有管理员能走的边。
	if cur.LifecycleState == "removed" {
		return out, ErrNotEditable
	}

	hasPackage := in.PackagePath != ""
	if !hasPackage && in.DemoType != "" && in.DemoType != cur.DemoType {
		return out, fmt.Errorf("%w: 换作品类型必须同时上传新的作品包", ErrInvalidInput)
	}
	// 没有当前版本就没法「追加」——正常数据不该出现（投稿必定建 r1），
	// 但要给一条能看懂的错，而不是让 NextRevisionNo 从 0 开始造一个 r1。
	if !cur.CurrentRevisionID.Valid || cur.CurrentRevNo <= 0 {
		return out, fmt.Errorf("%w: 作品还没有任何版本，不能更新", ErrInvalidInput)
	}

	// 这一版的内容元数据：有包就用新的，没包就继承当前版本。
	meta := revisionMeta{
		Title:        text(strings.TrimSpace(in.Title)),
		Description:  text(strings.TrimSpace(in.Description)),
		DemoType:     cur.DemoType,
		EntryPath:    text(cur.EntryPath),
		SingleFile:   text(cur.SingleFile),
		ManifestHash: text(cur.ManifestHash),
		FileCount:    pgtype.Int8{Int64: cur.FileCount, Valid: true},
		TotalBytes:   pgtype.Int8{Int64: cur.TotalBytes, Valid: true},
	}

	var files []blobFile
	if hasPackage {
		st, cleanup, err := s.stage(ctx, SubmitInput{
			Title:       in.Title,
			Description: in.Description,
			DemoType:    in.DemoType,
			PackagePath: in.PackagePath,
			PackageName: in.PackageName,
			PackageSize: in.PackageSize,
		})
		if err != nil {
			return out, err
		}
		defer cleanup()

		entries, err := manifestEntries(st)
		if err != nil {
			return out, err
		}
		files, err = s.uploadBlobs(ctx, entries)
		if err != nil {
			return out, err
		}

		meta.DemoType = in.DemoType
		if meta.DemoType == "" {
			meta.DemoType = "web"
		}
		meta.EntryPath = text(st.entryPath)
		meta.SingleFile = text(st.singleFile)
		meta.ManifestHash = text(st.manifest)
		meta.FileCount = pgtype.Int8{Int64: int64(len(st.files)), Valid: true}
		meta.TotalBytes = pgtype.Int8{Int64: st.totalBytes, Valid: true}
	}

	var revNo int
	err = s.inTx(ctx, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)

		next, err := q.NextRevisionNo(ctx, cur.ID)
		if err != nil {
			return fmt.Errorf("算版本号失败: %w", err)
		}
		rev, err := q.AppendRevision(ctx, store.AppendRevisionParams{
			DemoID:       cur.ID,
			RevNo:        next,
			Title:        meta.Title,
			Description:  meta.Description,
			DemoType:     meta.DemoType,
			EntryPath:    meta.EntryPath,
			SingleFile:   meta.SingleFile,
			ManifestHash: meta.ManifestHash,
			FileCount:    meta.FileCount,
			TotalBytes:   meta.TotalBytes,
			CreatedBy:    pgtype.Int8{Int64: in.AuthorID, Valid: true},
		})
		if err != nil {
			if isForeignKeyViolation(err) {
				return fmt.Errorf("%w: 作品类型不存在", ErrInvalidInput)
			}
			return fmt.Errorf("追加版本失败: %w", err)
		}
		revNo = int(rev.RevNo)

		if hasPackage {
			if err := s.writeManifestRows(ctx, q, rev.ID, files); err != nil {
				return err
			}
		} else {
			// 只改文字：新版本的清单是旧清单的整表复制，指向同一批 blob。
			if err := q.CopyRevisionFiles(ctx, store.CopyRevisionFilesParams{
				TargetRevisionID: rev.ID,
				SourceRevisionID: cur.CurrentRevisionID.Int64,
			}); err != nil {
				return fmt.Errorf("复制清单失败: %w", err)
			}
		}

		// 这一版的模型声明整体替换（语义见 UpdateInput.ModelSlugs 的注释）。
		// 新版本本来没有行，所以「替换」就是写入；不需要先删。
		modelIDs, err := resolveModelSlugs(ctx, q, in.ModelSlugs)
		if err != nil {
			return err
		}
		if err := attachModels(ctx, q, rev.ID, modelIDs); err != nil {
			return err
		}

		if err := q.SetCurrentRevisionForEdit(ctx, store.SetCurrentRevisionForEditParams{
			ID:                cur.ID,
			CurrentRevisionID: pgtype.Int8{Int64: rev.ID, Valid: true},
		}); err != nil {
			return fmt.Errorf("切换当前版本失败: %w", err)
		}

		message := fmt.Sprintf("作者提交第 %d 版，等待复审", revNo)
		if !hasPackage {
			message = fmt.Sprintf("作者更新了标题与简介（第 %d 版），等待复审", revNo)
		}
		if err := q.InsertDemoTimeline(ctx, store.InsertDemoTimelineParams{
			DemoID:  cur.ID,
			Kind:    "edit",
			Column3: message,
			ActorID: pgtype.Int8{Int64: in.AuthorID, Valid: true},
		}); err != nil {
			return fmt.Errorf("写时间线失败: %w", err)
		}
		return nil
	})
	if err != nil {
		return UpdateResult{}, err
	}

	return UpdateResult{
		Slug:            cur.Slug,
		RevisionNo:      revNo,
		ModerationState: "pending",
		LifecycleState:  cur.LifecycleState,
		FilesChanged:    hasPackage,
	}, nil
}

// revisionMeta 是新版本要落库的那几列。抽出来只为让「有包 / 没包」两条路
// 在事务外就把值定下来，事务里不再分支。
type revisionMeta struct {
	Title        pgtype.Text
	Description  pgtype.Text
	DemoType     string
	EntryPath    pgtype.Text
	SingleFile   pgtype.Text
	ManifestHash pgtype.Text
	FileCount    pgtype.Int8
	TotalBytes   pgtype.Int8
}

// validateMeta 检查标题与简介。与 SubmitInput.validate 共用同一套口径。
func validateMeta(title, description string) error {
	title = strings.TrimSpace(title)
	if title == "" {
		return fmt.Errorf("%w: 标题不能为空", ErrInvalidInput)
	}
	if len([]rune(title)) > maxTitleLen {
		return fmt.Errorf("%w: 标题最多 %d 个字符", ErrInvalidInput, maxTitleLen)
	}
	if len([]rune(description)) > maxDescriptionLen {
		return fmt.Errorf("%w: 描述最多 %d 个字符", ErrInvalidInput, maxDescriptionLen)
	}
	return nil
}
