package demo

import (
	"context"
	"fmt"
	"strings"

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

// resolveModelRefs 把作者填的模型「指称」解析成模型 id；解析不到就是 400。
//
// 指称可以是 slug、展示名或别名（大小写不敏感）——作者在投稿框里打的是
// 「DeepSeek V4 Flash」这种人话，不是 slug。但这**只是同义词匹配**，不是「目录之外
// 也能声明」：三种写法都必须在模型目录里对得上。
//
// 2026-11-06 曾有一版「解析不到就建 `status='candidate'` 的候选项」（#296），
// 2026-11-17 撤掉（#465）：需求里没有「作者可以造模型名」这一条
// （MODEL-001~021 管的是声明与核验口径），而它把模型表变成了任何登录用户都能写的表——
// 脏名字只能靠人工归并消化，归并端点至今没有。要收录灰测模型，走「申请收录 →
// 审核通过才建」，不该由投稿顺手建。
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
	var unknown []string
	for _, ref := range cleaned {
		id, ok := resolved[strings.ToLower(ref)]
		if !ok {
			unknown = append(unknown, ref)
			continue
		}
		if _, dup := seen[id]; dup {
			// 两个不同写法指向同一个模型（比如 slug 与别名各写一次）：只挂一条。
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	if len(unknown) > 0 {
		// 一次把没认出来的都列出来：只报第一个会让作者改一次投一次。
		// 但回显要有上限：请求体里能塞多少个名字，响应体就会跟着长多少（自伤型放大）。
		return nil, fmt.Errorf("%w: 模型目录里没有：%s（请从目录里挑；灰测模型等收录后再声明）",
			ErrInvalidInput, summarizeUnknown(unknown))
	}
	return ids, nil
}

// maxUnknownInMessage 是错误信息里最多点名几个没认出来的指称。
//
// 不设上限的话，请求体里塞多少个名字，400 的正文就会跟着长多少——这是自伤型的
// 放大（调用方自己付带宽），但也没必要把整份输入原样回显。超出部分只报个数，
// 作者仍然知道「还有哪些没认出来」这件事。
const maxUnknownInMessage = 20

// summarizeUnknown 把没认出来的指称拼成一句话，最多点名前 maxUnknownInMessage 个。
func summarizeUnknown(unknown []string) string {
	if len(unknown) <= maxUnknownInMessage {
		return strings.Join(unknown, "、")
	}
	return fmt.Sprintf("%s 等 %d 个", strings.Join(unknown[:maxUnknownInMessage], "、"), len(unknown))
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
