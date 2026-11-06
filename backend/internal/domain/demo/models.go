package demo

import (
	"context"
	"fmt"
	"strings"

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

// createCandidateModel 建一个候选项，并把作者这次的写法记成别名。
//
// 并发时两个请求可能同时建同名模型：`CreateCandidateModel` 的 `ON CONFLICT DO NOTHING`
// 不返回行，于是回查名字拿现成的那条——把并发冲突当成成功，而不是报错。
func createCandidateModel(ctx context.Context, q *store.Queries, ref string) (int64, error) {
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
	case isUniqueViolation(err):
		// 撞的是 `models.name`（同一个名字已存在，可能刚被并发的那次建好）。
		existing, findErr := q.FindModelByName(ctx, ref)
		if findErr != nil {
			return 0, fmt.Errorf("回查同名模型失败: %w", findErr)
		}
		return existing, nil
	default:
		return 0, fmt.Errorf("新建模型失败: %w", err)
	}
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
