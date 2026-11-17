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

// checkpointFromReport 是端点上报顺手建的版本行的备注。
//
// 写来源不是为了好看：`model_checkpoints` 有两个可能的写入方（上报、将来的管理端），
// 出问题时第一句话就是「这行是谁建的」。
const checkpointFromReport = "来自端点上报"

// ModelRef 是详情页要展示的一条模型声明。
//
// 只带三个字段：slug 稳定可引用、name 给人看、vendor 说明来源。
// 不带 id——id 是库内主键，不该出现在对外契约里（模型合并、重建主数据时它会变）。
type ModelRef struct {
	Slug   string
	Name   string
	Vendor string
}

// modelDeclaration 是一条要挂到版本上的声明：模型 + 可选的版本。
//
// CheckpointID 为 0 表示「这一版没说用的是哪一版模型」——落库是 NULL，展示是「未固定」。
// 它与 `model_offerings.pinning = 'unknown'` 各自独立：声明记的是「这一版用哪一版」，
// 调用行记的是「最后一次上报说了什么」。改稿只带 modelSlugs 时，声明会从上一版继承版本，
// 而调用行不会被重写——两者本来就可以不同。
type modelDeclaration struct {
	ModelID      int64
	CheckpointID int64
}

// 模型声明挂在版本上（`demo_revision_models`，MODEL-007），所以下面几个函数
// 都在「版本已经建好」之后调，参数是 revision id 而不是 demo id。

// applyModelDeclarations 是投稿与改稿共用的那一步：解析指称、写声明、写端点上报。
//
// 顺序不能换：
//  1. 先把指称解析完（解析不到一律 400，且一次点名所有没认出来的）；
//  2. 再把版本 label 换成 id（不存在就建，建了就是这一版的第一次引用）；
//  3. 写 `demo_revision_models`（声明）；
//  4. 写 `model_offerings`（调用）。
//
// 全部在调用方的事务里跑：任何一步失败都等于这次投稿 / 改稿失败，不会出现
// 「作品建好了、模型声明丢了」或「声明写了、调用没记」。
//
// carryOver 是「上一版给这些模型记过的版本」，改稿时传，投稿时传 nil。
func applyModelDeclarations(
	ctx context.Context,
	q *store.Queries,
	revisionID int64,
	slugs []string,
	rawCalls []string,
	carryOver map[int64]int64,
) error {
	calls, err := parseModelCalls(rawCalls)
	if err != nil {
		return err
	}
	prepared, err := prepareModelCalls(calls)
	if err != nil {
		return err
	}
	slugRefs, err := cleanModelRefs(slugs)
	if err != nil {
		return err
	}

	// 指称合集：modelSlugs 的顺序在前，端点上报里没出现过的补在后面。
	// 顺序决定详情页的声明顺序，所以不能靠 map 迭代。
	refs := make([]string, 0, len(slugRefs)+len(prepared))
	seenRef := make(map[string]struct{}, len(slugRefs)+len(prepared))
	for _, r := range append(append([]string{}, slugRefs...), callRefs(prepared)...) {
		key := strings.ToLower(r)
		if _, dup := seenRef[key]; dup {
			continue
		}
		seenRef[key] = struct{}{}
		refs = append(refs, r)
	}

	resolved, err := resolveModelRefs(ctx, q, refs)
	if err != nil {
		return err
	}

	// 版本 label → id。同一个 (模型, label) 只查一次：声明与调用行共用同一批 id。
	cpIDs := make(map[string]int64, len(prepared))
	for _, c := range prepared {
		if c.Checkpoint == "" {
			continue
		}
		modelID := resolved[strings.ToLower(c.Ref)]
		key := checkpointKey(modelID, c.Checkpoint)
		if _, ok := cpIDs[key]; ok {
			continue
		}
		id, err := ensureCheckpoint(ctx, q, modelID, c.Checkpoint)
		if err != nil {
			return err
		}
		cpIDs[key] = id
	}

	// 声明：同一个模型只挂一条（两个写法指向同一个模型时按第一次出现算）。
	decls := make([]modelDeclaration, 0, len(refs))
	seenModel := make(map[int64]struct{}, len(refs))
	for _, ref := range refs {
		modelID := resolved[strings.ToLower(ref)]
		if _, dup := seenModel[modelID]; dup {
			continue
		}
		seenModel[modelID] = struct{}{}
		decls = append(decls, modelDeclaration{
			ModelID:      modelID,
			CheckpointID: declarationCheckpoint(modelID, prepared, resolved, cpIDs, carryOver),
		})
	}
	if err := attachModels(ctx, q, revisionID, decls); err != nil {
		return err
	}

	return writeEndpointOfferings(ctx, q, prepared, resolved, cpIDs)
}

// declarationCheckpoint 决定一条声明该带哪个版本。
//
// 优先级：这次上报里给了 label > 上一版记过的版本 > 未指定。
// 上报优先是 #418 D07 的口径（「上报优先，名字只作索引」）：名字里带不带 1017
// 不参与判断——那条路（A03 的后缀识别）还没做，做了也不该盖过显式上报。
func declarationCheckpoint(
	modelID int64,
	prepared []preparedCall,
	resolved map[string]int64,
	cpIDs map[string]int64,
	carryOver map[int64]int64,
) int64 {
	for _, c := range prepared {
		if c.Checkpoint == "" || resolved[strings.ToLower(c.Ref)] != modelID {
			continue
		}
		if id, ok := cpIDs[checkpointKey(modelID, c.Checkpoint)]; ok {
			return id
		}
	}
	return carryOver[modelID]
}

// callRefs 取出上报里的模型名，保持顺序。
func callRefs(prepared []preparedCall) []string {
	out := make([]string, 0, len(prepared))
	for _, c := range prepared {
		out = append(out, c.Ref)
	}
	return out
}

// revisionCheckpoints 读出某一版给每个模型记过的版本，改稿时当作默认值。
//
// 只回有版本的行：没记过的模型本来就没有可带的东西，缺省即「未指定」。
func revisionCheckpoints(ctx context.Context, q *store.Queries, revisionID int64) (map[int64]int64, error) {
	rows, err := q.ListRevisionModelCheckpoints(ctx, revisionID)
	if err != nil {
		return nil, fmt.Errorf("读上一版的模型版本失败: %w", err)
	}
	out := make(map[int64]int64, len(rows))
	for _, r := range rows {
		if r.CheckpointID.Valid {
			out[r.ModelID] = r.CheckpointID.Int64
		}
	}
	return out, nil
}

// checkpointKey 是 (模型, 版本) 的缓存键。
func checkpointKey(modelID int64, label string) string {
	return fmt.Sprintf("%d|%s", modelID, label)
}

// attachModels 把这些声明挂到刚建好的版本上。
func attachModels(ctx context.Context, q *store.Queries, revisionID int64, decls []modelDeclaration) error {
	for _, d := range decls {
		if err := q.InsertRevisionModel(ctx, store.InsertRevisionModelParams{
			RevisionID:   revisionID,
			ModelID:      d.ModelID,
			CheckpointID: checkpointArg(d.CheckpointID),
		}); err != nil {
			return fmt.Errorf("写模型声明失败: %w", err)
		}
	}
	return nil
}

// writeEndpointOfferings 把上报的调用写进 `model_offerings`（authority='endpoint'）。
//
// 只写带了端点的那些：没有端点就没有「出处」，写进去只会是一行无法解释的记录。
// 只报版本不报端点的上报仍然有用——它把版本记到了声明上。
func writeEndpointOfferings(
	ctx context.Context,
	q *store.Queries,
	prepared []preparedCall,
	resolved map[string]int64,
	cpIDs map[string]int64,
) error {
	for _, c := range prepared {
		if c.Endpoint == "" {
			continue
		}
		modelID := resolved[strings.ToLower(c.Ref)]
		var cpID int64
		if c.Checkpoint != "" {
			cpID = cpIDs[checkpointKey(modelID, c.Checkpoint)]
		}
		pinning := pinningUnknown
		if cpID != 0 {
			pinning = pinningExact
		}
		if err := q.UpsertEndpointOffering(ctx, store.UpsertEndpointOfferingParams{
			ModelID:      modelID,
			ProviderID:   c.Endpoint,
			ProviderName: c.ProviderName,
			ExternalID:   c.Ref,
			CheckpointID: checkpointArg(cpID),
			Pinning:      pinning,
		}); err != nil {
			return fmt.Errorf("写端点上报失败: %w", err)
		}
	}
	return nil
}

// ensureCheckpoint 拿到 (模型, label) 那一版的 id，没有就建一行。
//
// 「建了再说」是 4d 的 d3 选 A 的口径：版本表原先一个写入方都没有，
// 上报是最自然的第一处。label 仍受 1..64 与「同模型唯一」约束，notes 记来源。
func ensureCheckpoint(ctx context.Context, q *store.Queries, modelID int64, label string) (int64, error) {
	id, err := q.InsertModelCheckpoint(ctx, store.InsertModelCheckpointParams{
		ModelID: modelID,
		Label:   label,
		Notes:   checkpointFromReport,
	})
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return 0, fmt.Errorf("登记模型版本失败: %w", err)
	}
	// ON CONFLICT DO NOTHING 不返回行：这一版早就有了，回查拿 id。
	existing, err := q.FindModelCheckpoint(ctx, store.FindModelCheckpointParams{
		ModelID: modelID,
		Label:   label,
	})
	if err != nil {
		return 0, fmt.Errorf("回查模型版本失败: %w", err)
	}
	return existing, nil
}

// checkpointArg 把「0 表示未指定」翻译成可空列。
func checkpointArg(id int64) pgtype.Int8 {
	if id == 0 {
		return pgtype.Int8{}
	}
	return pgtype.Int8{Int64: id, Valid: true}
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
//
// 返回的键是**小写化后的指称**：调用方按自己手里的原样去查，大小写不敏感这件事
// 只在这一处实现。
func resolveModelRefs(ctx context.Context, q *store.Queries, refs []string) (map[string]int64, error) {
	if len(refs) == 0 {
		return map[string]int64{}, nil
	}
	rows, err := q.ResolveModelRefs(ctx, refs)
	if err != nil {
		return nil, fmt.Errorf("解析模型失败: %w", err)
	}
	resolved := make(map[string]int64, len(rows))
	for _, r := range rows {
		resolved[strings.ToLower(r.Ref)] = r.ModelID
	}
	var unknown []string
	for _, ref := range refs {
		if _, ok := resolved[strings.ToLower(ref)]; !ok {
			unknown = append(unknown, ref)
		}
	}
	if len(unknown) > 0 {
		// 一次把没认出来的都列出来：只报第一个会让作者改一次投一次。
		// 但回显要有上限：请求体里能塞多少个名字，响应体就会跟着长多少（自伤型放大）。
		return nil, fmt.Errorf("%w: 模型目录里没有：%s（请从目录里挑；灰测模型等收录后再声明）",
			ErrInvalidInput, summarizeUnknown(unknown))
	}
	return resolved, nil
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
