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

// maxModelNameLen 与 `models.name` 的长度约束一致（1..128）。
const maxModelNameLen = 128

// ModelRef 是详情页要展示的一条模型声明。
//
// 只带三个字段：slug 稳定可引用、name 给人看、vendor 说明来源。
// 不带 id——id 是库内主键，不该出现在对外契约里（模型合并、重建主数据时它会变）。
type ModelRef struct {
	Slug   string
	Name   string
	Vendor string
}

// 模型声明挂在版本上（`demo_revision_models`，MODEL-007），所以下面两个函数
// 都在「版本已经建好」之后调，参数是 revision id 而不是 demo id。

// attachModels 把这些模型挂到刚建好的版本上。
func attachModels(ctx context.Context, q *store.Queries, revisionID int64, modelIDs []int64) error {
	for _, id := range modelIDs {
		if err := q.InsertRevisionModel(ctx, store.InsertRevisionModelParams{
			RevisionID: revisionID,
			ModelID:    id,
		}); err != nil {
			return fmt.Errorf("写模型声明失败: %w", err)
		}
	}
	return nil
}

// resolveModelRefs 把作者填的模型「指称」解析成模型 id，**解析不到就建一个候选项**。
//
// 指称可以是 slug、展示名或别名（大小写不敏感）。为什么允许三种：
// 作者在投稿框里打的是「DeepSeek V4 Flash」这种人话，不是 slug；只认 slug 等于要求他
// 先去目录里查一遍，而灰测模型本来就查不到。
//
// 解析不到时**不留 400、也不静默丢掉**，而是落一行 `status='candidate'`：
//   - 留 400 会把「上游还没收录」变成作者的错，而旧站的 142 种模型写法里有大量这种名字；
//   - 静默丢掉会让作者以为声明了、详情页却少一个，且他无从发现。
//
// 候选项与真模型的区别只是 `status`：归并时改的是指针（`merged_into_id`），作品不用动。
func resolveModelRefs(ctx context.Context, q *store.Queries, refs []string) ([]int64, error) {
	cleaned, err := cleanModelRefs(refs)
	if err != nil {
		return nil, err
	}
	if len(cleaned) == 0 {
		return nil, nil
	}

	rows, err := q.ResolveModelRefs(ctx, cleaned)
	if err != nil {
		return nil, fmt.Errorf("解析模型失败: %w", err)
	}
	ids := make([]int64, 0, len(cleaned))
	seen := make(map[int64]struct{}, len(cleaned))
	// 已经解析出来的先按输入顺序收好，保持详情页的声明顺序稳定。
	resolved := make(map[string]int64, len(rows))
	for _, r := range rows {
		resolved[strings.ToLower(r.Ref)] = r.ModelID
	}
	for _, ref := range cleaned {
		id, ok := resolved[strings.ToLower(ref)]
		if !ok {
			id, err = createCandidateModel(ctx, q, ref)
			if err != nil {
				return nil, err
			}
		}
		if _, dup := seen[id]; dup {
			// 两个不同写法指向同一个模型（比如 slug 与别名各写一次）：只挂一条。
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids, nil
}

// cleanModelRefs 去空白、去空项、按大小写不敏感去重，并先挡下超长输入。
//
// 去重放在这里而不是交给 SQL：`ResolveModelRefs` 只回「这个指称是谁」，回不了
// 「作者重复写了几次」，而重复的声明不该在详情页出现两遍。
func cleanModelRefs(refs []string) ([]string, error) {
	out := make([]string, 0, len(refs))
	seen := make(map[string]struct{}, len(refs))
	for _, raw := range refs {
		s := strings.TrimSpace(raw)
		if s == "" {
			continue
		}
		if len([]rune(s)) > maxModelNameLen {
			return nil, fmt.Errorf("%w: 模型名称最长 %d 个字：%s", ErrInvalidInput, maxModelNameLen, s)
		}
		key := strings.ToLower(s)
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, s)
	}
	return out, nil
}

// createCandidateModelAttempts 是「slug 被别人占了」时的总尝试次数。
//
// slug 是「先查再插」（uniqueModelSlug 查 ModelSlugTaken，再 INSERT）。两个**不同**的名字
// 折出同一个基座时（「Qwen 2.5」与「Qwen-2.5」都折成 qwen-2-5），两边都可能查到「没被
// 占用」，后插的那条撞上 models.slug 的唯一约束。这跟作者写错无关，换一个带随机后缀的
// slug 重来即可，所以给它几次机会，而不是把整次投稿判死。
const createCandidateModelAttempts = 3

// createCandidateModel 建一个候选项，并把作者这次的写法记成别名。
//
// 撞车有两种，形状都是 `pgx.ErrNoRows`：`CreateCandidateModel` 是
// `ON CONFLICT DO NOTHING`，冲突时**不报 23505** 而是返回 0 行，sqlc 的 `:one`
// 于是让 Scan 返回 ErrNoRows。两条路都不该让作者整次投稿失败——他什么都没做错：
//   - **同名**：别人刚建了同一个名字。按名字回查拿到现成的那条，把冲突当成成功；
//   - **同 slug 不同名**：名字确实不同（回查不到），撞的是机器生成的 slug。换一个
//     带随机后缀的 slug 重试（次数见 createCandidateModelAttempts）。
func createCandidateModel(ctx context.Context, q *store.Queries, ref string) (int64, error) {
	for attempt := 0; attempt < createCandidateModelAttempts; attempt++ {
		slug, err := uniqueModelSlug(ctx, q, ref)
		if err != nil {
			return 0, err
		}
		row, err := q.CreateCandidateModel(ctx, store.CreateCandidateModelParams{
			Slug: slug,
			Name: ref,
			// 厂商留空：作者填的是模型名，没有厂商信息。按模型域 §4.6.3 的规则，
			// 拿不到证据就不建厂商，避免造出「Bothub」这种假厂商。
			Vendor: pgtype.Text{},
		})
		switch {
		case err == nil:
			if err := q.InsertModelAlias(ctx, store.InsertModelAliasParams{Alias: ref, ModelID: row}); err != nil {
				return 0, fmt.Errorf("记模型别名失败: %w", err)
			}
			return row, nil
		case errors.Is(err, pgx.ErrNoRows), isUniqueViolation(err):
			// 两条路都归到这里，因为「写不进去」有两种形状：
			//   - `ON CONFLICT DO NOTHING` 命中 → 0 行 → ErrNoRows；
			//   - 未来若有人把查询改成会报错的形状（比如去掉 ON CONFLICT）→ 23505。
			// 名字是作者给的、slug 才是机器生成的，所以先按名字回查。
			existing, findErr := q.FindModelByName(ctx, ref)
			switch {
			case findErr == nil:
				return existing, nil
			case !errors.Is(findErr, pgx.ErrNoRows):
				return 0, fmt.Errorf("回查同名模型失败: %w", findErr)
			}
			// 名字也不在库里（否则上面就返回了）→ 撞的是 slug：下一轮 ModelSlugTaken
			// 会看见别人刚写进去的那一行，uniqueModelSlug 于是加随机后缀。
		default:
			return 0, fmt.Errorf("新建模型失败: %w", err)
		}
	}
	// 走到这里说明连续几次都撞上：不是作者的输入错（名字与 slug 都是合法的），
	// 所以不包 ErrInvalidInput，让它按服务端故障报出去。
	return 0, fmt.Errorf("新建模型失败：连续 %d 次撞上别人刚建的 slug（%s）", createCandidateModelAttempts, ref)
}

// uniqueModelSlug 从名字生成一个没被占用的 slug。
//
// slug 是公开 URL 的一部分、创建后不可变（模型域 §1），所以宁可在建之前多查一次，
// 也不要让它撞上唯一约束再回退。名字全是中文时 slugify 返回空，退回 `model-<随机>`。
func uniqueModelSlug(ctx context.Context, q *store.Queries, name string) (string, error) {
	base := slugify(name)
	if base == "" {
		base = "model"
	}
	for attempt := 0; attempt < 5; attempt++ {
		slug := base
		if attempt > 0 {
			suffix, err := randomSuffix(4)
			if err != nil {
				return "", fmt.Errorf("生成模型 slug 失败: %w", err)
			}
			slug = base + "-" + suffix
		}
		taken, err := q.ModelSlugTaken(ctx, slug)
		if err != nil {
			return "", fmt.Errorf("检查模型 slug 失败: %w", err)
		}
		if !taken {
			return slug, nil
		}
	}
	return "", fmt.Errorf("%w: 模型名称生成的标识已被占用：%s", ErrInvalidInput, base)
}
