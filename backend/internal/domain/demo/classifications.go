package demo

import (
	"context"
	"fmt"
	"strings"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

// ClassificationRef 是详情页/列表要展示的一条分类。
//
// 三个字段就够：dimension 决定分组、slug 稳定可引用、label 给人看。
// 不带 id——id 是库内主键，分类合并（`merged_into_id`）时它会变。
type ClassificationRef struct {
	Dimension string
	Slug      string
	Label     string
}

// classificationPick 是「解析完、还没写库」的一条分类。
type classificationPick struct {
	Dimension string
	ID        int64
}

// 分类挂在**作品**上（`demo_classifications` 没有 revision_id），模型挂在**版本**上
// （`demo_revision_models` 有）。这个不统一是刻意的：
//
//   - 改稿换掉技术实现，不该让「这是个游戏」这条判断重算；
//   - 而换模型声明确实会产生新版本（它决定「这一版用了什么模型」）。
//
// 所以下面两个函数都在 demo 行已经存在之后调，参数是 demo id。

// resolveClassificationSlugs 把前端传来的分类 slug 解析成带维度的 (dimension, id)。
//
// 与模型解析的三点不同：
//
//  1. **不按 status 过滤**：已 deprecated 的分类仍可被继续引用。删掉一个分类
//     不该让挂着它的老作品报错——那是「历史数据被后来的管理动作打断」。
//  2. 找不到的照样报 ErrInvalidInput，理由与模型相同：作者以为打了标签、
//     库里没有，而他自己看不见这件事。
//  3. `UNIQUE (dimension, slug)` 允许同一 slug 出现在多个维度（`subject` 里
//     可能有一个和 `genre` 同名的项）。真撞上要报错而不是替作者挑一个——
//     挑错了没人会发现。
func resolveClassificationSlugs(ctx context.Context, q *store.Queries, slugs []string) ([]classificationPick, error) {
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

	rows, err := q.GetClassificationsBySlugs(ctx, wanted)
	if err != nil {
		return nil, fmt.Errorf("解析分类失败: %w", err)
	}

	bySlug := make(map[string][]store.GetClassificationsBySlugsRow, len(rows))
	for _, r := range rows {
		bySlug[r.Slug] = append(bySlug[r.Slug], r)
	}

	out := make([]classificationPick, 0, len(wanted))
	missing := make([]string, 0)
	ambiguous := make([]string, 0)
	for _, s := range wanted {
		hits := bySlug[s]
		switch len(hits) {
		case 0:
			missing = append(missing, s)
		case 1:
			out = append(out, classificationPick{Dimension: hits[0].Dimension, ID: hits[0].ID})
		default:
			ambiguous = append(ambiguous, s)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("%w: 分类不存在：%s", ErrInvalidInput, strings.Join(missing, "、"))
	}
	if len(ambiguous) > 0 {
		return nil, fmt.Errorf("%w: 分类标识在多个维度下重复：%s", ErrInvalidInput, strings.Join(ambiguous, "、"))
	}
	return out, nil
}

// replaceClassifications 整体替换一件作品的分类。
//
// 「先删后插」而不是 diff：分类是个位数到十几个的集合，diff 省下的那点写入
// 换不来「两条路径行为一致」的确定性。这与 modelSlugs 的处理方式一致。
func replaceClassifications(ctx context.Context, q *store.Queries, demoID int64, picks []classificationPick) error {
	if err := q.DeleteDemoClassifications(ctx, demoID); err != nil {
		return fmt.Errorf("清空分类失败: %w", err)
	}
	for _, p := range picks {
		if err := q.InsertDemoClassification(ctx, store.InsertDemoClassificationParams{
			DemoID:           demoID,
			Dimension:        p.Dimension,
			ClassificationID: p.ID,
		}); err != nil {
			return fmt.Errorf("写分类失败: %w", err)
		}
	}
	return nil
}

// classificationsOf 读一件作品的分类（详情页用）。
func classificationsOf(ctx context.Context, q *store.Queries, demoID int64) ([]ClassificationRef, error) {
	rows, err := q.ListDemoClassifications(ctx, demoID)
	if err != nil {
		return nil, err
	}
	out := make([]ClassificationRef, 0, len(rows))
	for _, r := range rows {
		out = append(out, ClassificationRef{Dimension: r.Dimension, Slug: r.Slug, Label: r.Label})
	}
	return out, nil
}
