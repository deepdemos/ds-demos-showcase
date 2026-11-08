// Package httpapi 实现 api/openapi.yaml 定义的端点。
//
// 分层：这个包只负责「HTTP ↔ 领域判定」，SQL 在 internal/store（sqlc 生成）。
// 路由注册不在这里，在 internal/httpapi/router.go——那里把实现塞进生成的路由表。
package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/api"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
	"github.com/jackc/pgx/v5"
)

// ListDemos 对应 GET /api/v1/demos。
func (h *Handler) ListDemos(w http.ResponseWriter, r *http.Request, params api.ListDemosParams) {
	if h.queries == nil {
		unavailable(w, errors.New("database not configured"))
		return
	}

	page := 1
	pageSize := defaultPageSize
	if params.Page != nil {
		page = *params.Page
	}
	if params.PageSize != nil {
		pageSize = *params.PageSize
	}
	// 契约里 pageSize 有 maximum: 200，超上限应当是 400 而不是静默截断——
	// 静默截断会让调用方以为拿到了全部。
	if pageSize < 1 || pageSize > maxPageSize || page < 1 {
		writeError(w, http.StatusBadRequest, api.InvalidParam, "分页参数超出允许范围")
		return
	}
	// 关键词：空与全空格等同没传（用户清空搜索框时不该得到 400）；
	// 超过契约声明的 100 字报 400，而不是静默截断——静默截断会让用户
	// 以为搜的是全部，搜不到时还会怀疑「明明有这几个字」。
	q := strings.TrimSpace("")
	if params.Q != nil {
		q = strings.TrimSpace(*params.Q)
	}
	if len([]rune(q)) > maxSearchLen {
		writeError(w, http.StatusBadRequest, api.InvalidParam, "搜索词太长，最多 100 个字")
		return
	}
	// 长度按**用户输入**判定（上面那行），转义放在判定之后：转义会改变字符数，
	// 不该让「搜一串百分号」比「搜同样多的汉字」更容易被拒。
	q = likeEscaper.Replace(q)
	// 排序：契约里是枚举，生成物已经替我们挡掉非法值，这里只做「有没有传」的默认。
	// `seed` 只在 random 下有意义，但一律往下传（SQL 里按 sort 决定用不用它），
	// 免得两个地方各判一次、迟早判得不一致。
	sortBy := "latest"
	if params.Sort != nil {
		// 三种取值是契约里的枚举，但生成的绑定只把字符串塞进来、不校验取值。
		// 静默回落到 latest 会让「传错参数」看起来像「排序没生效」，所以直接 400。
		switch *params.Sort {
		case api.Latest, api.Hot, api.Random:
			sortBy = string(*params.Sort)
		default:
			writeError(w, http.StatusBadRequest, api.InvalidParam, "排序取值非法（latest / hot / random）")
			return
		}
	}
	seed := int64(0)
	if params.Seed != nil {
		seed = int64(*params.Seed)
	}
	if seed < 0 || seed > math.MaxInt32 {
		writeError(w, http.StatusBadRequest, api.InvalidParam, "seed 超出范围（0 到 2147483647）")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), queryTimeout)
	defer cancel()

	// 四个筛选：维度内 OR、维度间 AND（见 demos.sql 的注释）。
	// 空的与全空格的 slug 直接丢掉——`?genre=` 是「没选」而不是「选了一个空值」。
	genre, ok := filterSlugs(params.Genre)
	if !ok {
		writeError(w, http.StatusBadRequest, api.InvalidParam, "筛选条件太多，每个维度最多 50 个")
		return
	}
	technique, ok := filterSlugs(params.Technique)
	if !ok {
		writeError(w, http.StatusBadRequest, api.InvalidParam, "筛选条件太多，每个维度最多 50 个")
		return
	}
	subject, ok := filterSlugs(params.Subject)
	if !ok {
		writeError(w, http.StatusBadRequest, api.InvalidParam, "筛选条件太多，每个维度最多 50 个")
		return
	}
	model, ok := filterSlugs(params.Model)
	if !ok {
		writeError(w, http.StatusBadRequest, api.InvalidParam, "筛选条件太多，每个维度最多 50 个")
		return
	}

	filters := store.CountPublishedDemosParams{
		Q: q, Genre: genre, Technique: technique, Subject: subject, Model: model,
	}
	total, err := h.queries.CountPublishedDemos(ctx, filters)
	if err != nil {
		unavailable(w, err)
		return
	}

	// seed 传成文本：SQL 里要拼进 md5(slug:seed)，用 text 省一次 cast。
	// 它是 int64 转的十进制串，形状固定，不会被注入。
	rows, err := h.queries.ListPublishedDemos(ctx, store.ListPublishedDemosParams{
		Q:         q,
		Genre:     genre,
		Technique: technique,
		Subject:   subject,
		Model:     model,
		Sort:      sortBy,
		Seed:      strconv.FormatInt(seed, 10),
		RowOffset: int32((page - 1) * pageSize),
		RowLimit:  int32(pageSize),
	})
	if err != nil {
		unavailable(w, err)
		return
	}

	// 整页的分类一次取回：一件作品一次查询会在 50 条时变成 51 次往返。
	classes := h.classificationsFor(ctx, rows)

	items := make([]api.DemoSummary, 0, len(rows))
	for _, row := range rows {
		items = append(items, summaryOf(row, classes[row.ID]))
	}

	writeJSON(w, http.StatusOK, api.DemoList{
		Items:    items,
		Page:     page,
		PageSize: pageSize,
		Total:    int(total),
	})
}

// GetDemo 对应 GET /api/v1/demos/{slug}。三种结果见 detailOf 的注释。
func (h *Handler) GetDemo(w http.ResponseWriter, r *http.Request, slug string) {
	if h.queries == nil {
		unavailable(w, errors.New("database not configured"))
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), queryTimeout)
	defer cancel()

	row, err := h.queries.GetDemoBySlug(ctx, slug)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, api.NotFound, "作品不存在")
			return
		}
		unavailable(w, err)
		return
	}

	// 作者视图：作者要能看到自己还没过审的稿件，否则「投稿完就再也打不开自己的作品」。
	// 认人失败一律按访客处理——这里不是鉴权点，不该因为没登录就报错。
	viewerIsAuthor := false
	if h.identity != nil && row.AuthorSub != "" {
		if account, err := h.identity.Me(ctx, readSessionCookie(r)); err == nil && account.Sub == row.AuthorSub {
			viewerIsAuthor = true
		}
	}

	detail, ok := detailOf(row, viewerIsAuthor)
	if !ok {
		writeError(w, http.StatusNotFound, api.NotFound, "作品不存在")
		return
	}
	// 模型声明按「版本」取：访客看对外版本，作者看当前版本——与标题、简介同一口径。
	// 墓碑态与已删除态 detailOf 已经把内容清空了，这里也不需要再取。
	detail.Models = h.modelsOf(ctx, row, viewerIsAuthor, detail)
	// 分类是作品级的，与版本无关：墓碑态也照样给它——「这是一款游戏」这件事
	// 不会因为下架而不再是事实，而墓碑页正需要一点上下文。
	detail.Classifications = h.classificationsOf(ctx, row.ID)
	// 评分是读出来的附加信息：读失败按 0 分 0 票，不让详情跟着 503
	// （与浏览计数同一原则：它是详情页的一部分，不是详情本身）。
	// 墓碑态照样给——「这件作品当初 4.5 分」是事实，不因为下架而不再成立。
	if h.demos != nil {
		_, viewerKey := h.raterOf(ctx, r)
		detail.Rating = h.ratingOfDetail(ctx, slug, viewerKey)
	} else {
		detail.Rating = api.DemoRating{}
	}
	writeJSON(w, http.StatusOK, detail)
}

// ---- 纯函数：从数据行到响应体。抽出来是为了能单测，不用起数据库。----

func summaryOf(row store.ListPublishedDemosRow, classes []api.ClassificationRef) api.DemoSummary {
	// 列表查询本身就带了 lifecycle_state = 'published' 的过滤，
	// 所以取出来的行一定是 published——不需要再从结果里读这个字段。
	return api.DemoSummary{
		Slug:           row.Slug,
		Title:          textPtr(row.Title),
		CoverUrl:       textPtr(row.CoverUrl),
		CreatedAt:      row.CreatedAt.Time,
		LifecycleState: api.LifecycleStatePublished,
		// 只在有搜索词时有意义；没搜索词时 SQL 给的也是 title（见 demos.sql 的注释）。
		MatchedIn: api.DemoSummaryMatchedIn(row.MatchedIn),
		// 空数组而不是 nil：契约里这是 required 字段，null 会让前端多一条分支，
		// 而「没打分类」与「加载失败」在界面上本来就该长得不一样。
		Classifications: classes,
	}
}

// detailOf 决定「对外可见吗」与「是不是墓碑」，false 表示 404：
// 已擦除 / 草稿待审驳回 → 404（**作者本人除外**）；归档、移除、软删 → 墓碑；
// published → 完整内容。
//
// viewerIsAuthor 改变三件事：能不能看到未公开的稿件、看到哪一版的文字、
// 以及 canEdit。它不改「墓碑」与「渠道可见性」的判定——作者也看不见被移除的内容。
// modelsOf 取这一版声明的模型。
//
// 三种情况返回空数组而不是省略字段：
//   - 墓碑态 / 已删除态：那两种状态没有内容，也就没有「用了什么模型」
//   - 没有版本可读（异常数据）：空数组比 500 更诚实
//
// 查询失败不把整个详情打成 503：模型声明是**附加信息**，缺了它详情页仍然可用。
// 这是刻意的取舍——为了一块可选的展示，让整个页面挂掉不划算。
func (h *Handler) modelsOf(ctx context.Context, row store.GetDemoBySlugRow, viewerIsAuthor bool, detail api.DemoDetail) []api.Model {
	out := []api.Model{}
	// 墓碑态：detailOf 只填了标题与 lifecycleActions，类型是空串。
	// 用「有没有类型」判不可靠（异常数据也可能没有），所以直接看重放出来的字段。
	if detail.LifecycleState == api.LifecycleStateArchived || detail.LifecycleState == api.LifecycleStateRemoved {
		return out
	}
	if row.DeletedAt.Valid {
		return out
	}

	revID := row.PublishedRevisionID
	if viewerIsAuthor && row.CurrentRevisionID.Valid {
		revID = row.CurrentRevisionID
	}
	if !revID.Valid || revID.Int64 <= 0 {
		return out
	}

	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
	rows, err := h.queries.ListRevisionModels(ctx, revID.Int64)
	if err != nil {
		slog.Error("读模型声明失败", "slug", row.Slug, "err", err)
		return out
	}
	aliasRows, err := h.queries.ListRevisionModelAliases(ctx, revID.Int64)
	if err != nil {
		// 别名是锦上添花：读不到不该让整条详情变成 503，但得留下痕迹。
		slog.Error("读模型别名失败", "slug", row.Slug, "err", err)
	}
	aliases := map[int64][]string{}
	for _, a := range aliasRows {
		aliases[a.ModelID] = append(aliases[a.ModelID], a.Alias)
	}
	for _, m := range rows {
		out = append(out, modelOf(store.ListModelsRow{
			ID:             m.ID,
			Slug:           m.Slug,
			Name:           m.Name,
			Vendor:         m.Vendor,
			Status:         m.Status,
			Resolution:     m.Resolution,
			MergedIntoSlug: m.MergedIntoSlug,
			Source:         m.Source,
			Description:    m.Description,
			CreatedAt:      m.CreatedAt,
			UpdatedAt:      m.UpdatedAt,
		}, aliases[m.ID]))
	}
	return out
}

func detailOf(row store.GetDemoBySlugRow, viewerIsAuthor bool) (api.DemoDetail, bool) {
	state := api.LifecycleState(row.LifecycleState)

	// 已擦除：行还在（逻辑封存），但对外不存在。
	if row.ErasedAt.Valid {
		return api.DemoDetail{}, false
	}
	// 从未公开过。作者本人要看得见——否则他投完稿就再也打不开自己的作品，
	// 「更新作品」也就无从谈起（更新页要先把当前内容读出来）。
	switch state {
	case api.LifecycleStateDraft, api.LifecycleStatePending, api.LifecycleStateRejected:
		if !viewerIsAuthor {
			return api.DemoDetail{}, false
		}
	}

	// 软删对外不存在——**连墓碑也不给**。
	//
	// 生命周期 §4.1 把这条定死了：`deleted` / `erased` 对外就是不存在，
	// 给墓碑等于承认「这里曾经有过东西」，而这条档位恰恰是最需要「当作没发生」的。
	// 墓碑只属于 `archived` / `removed` 这两种「曾经公开过」的档位。
	//
	// 作者本人仍然看得到（否则他恢复不了自己刚删的东西），但看到的是
	// 「已删除 · 可恢复」，不是墓碑文案。
	if row.DeletedAt.Valid && !viewerIsAuthor {
		return api.DemoDetail{}, false
	}

	tombstone := state == api.LifecycleStateArchived || state == api.LifecycleStateRemoved

	// 墓碑只承载「这里曾经有过一个作品」，不承载内容，所以不看审核态。
	//
	// 完整内容必须有**对外版本**（迁移 0014）——原来是「审核态必须是 approved」。
	// 换成指针判定的理由：审核态从此描述「当前版本」，作者改稿后它是 pending，
	// 而对外的内容仍是上一版、照常可见。继续判审核态会让作者一提交更新，
	// 自己的作品就从作品库里消失。
	// 作者能看自己的草稿（它本来就没有对外版本），访客不行。
	if !tombstone && !row.HasPublishedRevision && !viewerIsAuthor {
		return api.DemoDetail{}, false
	}
	// 渠道对外可见：`hidden` 的作品对外不存在。
	//
	// 这条以前漏了，导致「列表里看不见、详情页却完整返回 200」——一个作品被设成
	// hidden 之后，知道 slug 的人仍然能读到标题、简介、封面，而 iframe 预览已经 404，
	// 页面变成「有正文、无内容」的半截状态。三处入口（列表 / 详情 / 预览 / 下载）
	// 必须同一口径：列表 `listed`（unlisted 不上列表）；其余三处 `listed|unlisted`。
	// 作者能看自己还没上架的作品（刚投稿就属于这种），访客不行。
	if !tombstone && !row.HasVisibleChannel && !viewerIsAuthor {
		return api.DemoDetail{}, false
	}

	// 审核态：作者看真实值，访客恒为 approved。
	//
	// 访客能看到这条详情，凭据是「有对外版本」——那版内容本来就是审过的。
	// 回一个 pending 只会让人以为「这东西还没审完就上线了」，而那恰恰是
	// 版本分离想让大家都别误会的事（复审期间审核态是 pending，对外内容不变）。
	moderationState := api.DemoDetailModerationStateApproved
	if viewerIsAuthor {
		moderationState = api.DemoDetailModerationState(row.ModerationState)
	}

	detail := api.DemoDetail{
		Slug:            row.Slug,
		CreatedAt:       row.CreatedAt.Time,
		UpdatedAt:       row.UpdatedAt.Time,
		LifecycleState:  state,
		ModerationState: moderationState,
		Author: api.DemoAuthor{
			Username:    row.AuthorUsername,
			DisplayName: row.AuthorDisplayName,
		},
		Models: []api.Model{},
	}
	// 发布时间：没有对外可见渠道时为 null（草稿、从未发布过）。
	// 墓碑与已删除的作品仍然给——「它曾经公开过」这个事实不该被抹掉。
	if row.PublishedAt.Valid {
		t := row.PublishedAt.Time
		detail.PublishedAt = &t
	}
	if tombstone {
		// 墓碑态：**只留标题**，简介 / 封面 / 类型 / 可下载一律不给。
		//
		// 标题要留的理由（`docs/实体域数据结构设计.md` §5.0 的墓碑行明确列了「slug / 标题 /
		// 下架时间 / 说明」）：分享链接落到这里时，用户得看得出来「哪个作品没了」。
		// 一个只有「作者已下架」四个字的页面，等于让人怀疑自己点错了链接。
		// 反过来，简介与封面是内容，墓碑不承载内容——所以它们才是空的那部分。
		detail.Title = textPtr(row.Title)
		detail.LifecycleActions = lifecycleActionsOf(row, viewerIsAuthor)
		return detail, true
	}
	if row.DeletedAt.Valid {
		// 作者看自己删掉的作品：内容也不给，只给「还能不能恢复」。
		// 给内容会让「删除」变成「隐藏」，而这件作品对外确实已经不存在了。
		detail.LifecycleActions = lifecycleActionsOf(row, viewerIsAuthor)
		return detail, true
	}
	// 作者看**当前版本**的文字，访客看**对外版本**的。
	// 改稿期间这两个本来就不同：作者要看自己刚写的那版，才能接着改；
	// 访客要看你审过的那版，否则「新版本还在复审」等于攻略给出了未审内容。
	//
	// current_* 是 COALESCE 出来的空串——没有当前版本时（异常数据）
	// 退回 demos 上的列，不要给作者一个空标题。
	if viewerIsAuthor {
		detail.Title = preferString(row.CurrentTitle, row.Title)
		detail.Description = preferString(row.CurrentDescription, row.Description)
		detail.CoverUrl = textPtr(row.CoverUrl)
		if row.CurrentDemoType != "" {
			detail.DemoType = row.CurrentDemoType
		} else {
			detail.DemoType = row.DemoType
		}
	} else {
		detail.Title = textPtr(row.Title)
		detail.Description = textPtr(row.Description)
		detail.CoverUrl = textPtr(row.CoverUrl)
		detail.DemoType = row.DemoType
	}
	// 作者能不能接着改：作者本人 + 非墓碑 + 没被平台移除。
	detail.CanEdit = viewerIsAuthor && !tombstone && state != api.LifecycleStateRemoved
	// 与 canEdit 分开：canEdit 还要求「不是墓碑、没被移除」，
	// 而「这是不是你的作品」只取决于会话与作者。作品动态按它决定要不要请求。
	isAuthor := viewerIsAuthor
	detail.IsAuthor = &isAuthor
	// downloadable 的口径与 httpapi/download.go 必须一致，否则按钮点了 404：
	// 有文件可给（不是 link）+ 渠道对外可见（listed 或 unlisted）。
	// 这里不判存储里文件是否存在——那要多一次 Stat 往返，
	// 而「文件被外力删掉」属于事故，不该由详情页替它兜底（下载会明确 404）。
	detail.Downloadable = row.DemoType != "" && row.DemoType != "link" && row.HasVisibleChannel && row.HasPublishedRevision
	detail.LifecycleActions = lifecycleActionsOf(row, viewerIsAuthor)
	// 业务统计：墓碑态不展示计数（那是在替一件已下架的作品做宣传）。
	// 未发布的作品也不展示——它的浏览量没有意义，反而会让作者以为「有人在看」。
	if state == api.LifecycleStatePublished && row.HasPublishedRevision {
		detail.ViewCount = row.ViewCount
		detail.DownloadCount = row.DownloadCount
	}
	return detail, true
}

// lifecycleActionsOf 算出「这个观众此刻能对作品做哪些生命周期动作」。
//
// 放在服务端算，不让前端按 lifecycleState 自己推：能做什么取决于三个变量的组合
// （是不是作者、有没有被删、是不是被平台移除），推到前端就是第二份事实源，
// 而推错的表现是「按钮点了 409」。四个布尔全给，前端不需要再判任何状态。
func lifecycleActionsOf(row store.GetDemoBySlugRow, viewerIsAuthor bool) api.DemoLifecycleActions {
	if !viewerIsAuthor || row.ErasedAt.Valid {
		return api.DemoLifecycleActions{}
	}
	removed := row.LifecycleState == string(api.LifecycleStateRemoved)
	if row.DeletedAt.Valid {
		// 删除后作者只能恢复；平台移除的作品连恢复都不给。
		return api.DemoLifecycleActions{Restore: !removed, Delete: false}
	}
	return api.DemoLifecycleActions{
		Archive:   row.LifecycleState == string(api.LifecycleStatePublished),
		Unarchive: row.LifecycleState == string(api.LifecycleStateArchived),
		Delete:    !removed,
	}
}
