package demo

import (
	"context"
	"fmt"
	"strings"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

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

// resolveModelSlugs 把前端传来的模型 slug 解析成 id。
//
// 三种情况都要分清，不能一律当成功：
//   - 空列表：合法。作品可以不声明模型（展示件未使用模型，或者作者没填）。
//   - 全部命中 active：返回 id 列表。
//   - 有任何一个不存在或不是 active：报 ErrInvalidInput。
//
// 最后一种**不能静默丢掉**：作者以为声明了 A、B 两个模型，库里只落了 A，
// 详情页就会少一个而他不知道。宁可让这次投稿失败，让他看得见原因。
//
// 这里没有「最多 N 个」的上限：模型的多少由主数据（active 的行数）与作品本身的
// 事实决定，人为封顶只是给真实作品添麻烦。重复提交同一个 slug 会被去重，
// 所以 `modelSlugs` 里出现重复也不会多落行。
func resolveModelSlugs(ctx context.Context, q *store.Queries, slugs []string) ([]int64, error) {
	wanted := make([]string, 0, len(slugs))
	seen := make(map[string]struct{}, len(slugs))
	for _, raw := range slugs {
		s := strings.TrimSpace(raw)
		if s == "" {
			continue
		}
		if _, dup := seen[s]; dup {
			continue
		}
		seen[s] = struct{}{}
		wanted = append(wanted, s)
	}
	if len(wanted) == 0 {
		return nil, nil
	}

	rows, err := q.ResolveModelSlugs(ctx, wanted)
	if err != nil {
		return nil, fmt.Errorf("解析模型失败: %w", err)
	}
	ids := make([]int64, 0, len(rows))
	found := make(map[string]struct{}, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ID)
		found[r.Slug] = struct{}{}
	}
	if len(found) != len(wanted) {
		missing := make([]string, 0, len(wanted)-len(found))
		for _, s := range wanted {
			if _, ok := found[s]; !ok {
				missing = append(missing, s)
			}
		}
		return nil, fmt.Errorf("%w: 模型不存在或未启用：%s", ErrInvalidInput, strings.Join(missing, "、"))
	}
	return ids, nil
}

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
