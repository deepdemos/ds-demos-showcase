// Package httpapi 实现 api/openapi.yaml 定义的端点。
//
// 分层：这个包只负责「HTTP ↔ 领域判定」，SQL 在 internal/store（sqlc 生成）。
// 路由注册不在这里，在 internal/httpapi/router.go——那里把实现塞进生成的路由表。
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/api"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/demo"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/identity"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/model"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/platform/mailer"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

const (
	// healthzTimeout 是一次健康检查的上限。PG 卡住时不能让 /healthz 跟着挂住。
	healthzTimeout = 2 * time.Second
	// queryTimeout 是业务查询的上限。请求已经被客户端取消了也没关系，context 会先断。
	queryTimeout = 5 * time.Second
	// registerTimeout 比 queryTimeout 宽：注册要跑一次 argon2id（64 MiB / 3 轮），
	// 在慢机器上一次性占掉几百毫秒到一秒，5 秒会误杀。
	registerTimeout = 15 * time.Second

	// 分页口径与治理域 §P5 一致：默认 50，上限 200。
	// maxSearchLen 与契约里 q 的 maxLength 保持一致。
	maxSearchLen = 100
	// maxFilterValues 与契约里四个筛选参数的 maxItems 保持一致。
	maxFilterValues = 50
	defaultPageSize = 50
	maxPageSize     = 200
)

// likeEscaper 把 LIKE 的通配符转成字面量。用户搜 `100%` 时想找的是带百分号的
// 作品，不是「100 后面随便什么」；不转义的话 `q=%` 会回到全部作品，而 matchedIn
// 还会把它们全标成「标题命中」——搜索框里那个字符根本不存在于标题。
//
// 与本文件配套的是 SQL 里的 `ESCAPE '\'`：反斜杠本身也要能被搜到，所以它先转。
var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// Pinger 是 /healthz 依赖的最小接口，便于单测替换。
type Pinger interface {
	Ping(ctx context.Context) error
}

// Handler 实现 api.ServerInterface。
type Handler struct {
	pinger   Pinger
	queries  *store.Queries    // nil 表示未配置数据库
	identity *identity.Service // nil 表示身份域未接线
	mailer   mailer.Mailer     // nil 表示不投递（验证码请求仍然返回 204）
	demos    *demo.Service     // nil 表示作品域未接线
	models   *model.Service    // nil 表示模型域未接线

	// maxUploadBytes 是上传包的体积硬上限，用来给请求体套 MaxBytesReader。
	maxUploadBytes int64
}

// New 组装处理器。queries 可以为 nil（未配置数据库），此时业务端点返回 503。
func New(pinger Pinger, queries *store.Queries, identitySvc *identity.Service, m mailer.Mailer, demos *demo.Service) *Handler {
	h := &Handler{pinger: pinger, queries: queries, identity: identitySvc, mailer: m, demos: demos}
	if demos != nil {
		h.maxUploadBytes = demos.MaxPackageBytes()
	}
	return h
}

// WithModels 注入模型域服务，返回自身以便链式组装。
//
// 用可选注入而不是扩 `New` 的签名：模型域目前只有一个端点用得上，
// 改签名会让 30 多个测试调用点跟着改，收益为零。
func (h *Handler) WithModels(s *model.Service) *Handler {
	h.models = s
	return h
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		slog.Error("write json failed", "err", err)
	}
}

// decodeJSON 读请求体并拒绝未知字段。
//
// 拒绝未知字段是刻意的：客户端把 `username` 写成 `userName` 时，静默忽略会建出一个
// 用户名为空的账号，而错误信息会在很后面才出现。这里直接 400。
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	// 传 w 而不是 nil：超限时 net/http 才会给这次请求打上「连接该关」的标记。
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	// 只允许一个 JSON 值：后面还有东西说明客户端发错了，静默忽略会让排查变难。
	// 正常的单值请求在这里读到的是 io.EOF，那是成功。
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("请求体只能包含一个 JSON 值")
	}
	return nil
}

func writeError(w http.ResponseWriter, status int, code api.ErrorCode, message string) {
	writeJSON(w, status, api.Error{Code: code, Message: message})
}

// unavailable 是「依赖不可用」的统一出口。
// 真实错误只进日志：连接串、主机名、驱动报错都可能出现在里面，而这个响应是公开的。
func unavailable(w http.ResponseWriter, err error) {
	slog.Error("database unavailable", "err", err)
	writeError(w, http.StatusServiceUnavailable, api.Unavailable, "核心资料暂不可访问")
}

// GetHealthz 对应 GET /healthz。
// 200 时返回 {"status":"ok"}；数据库不可用时 503，且不泄漏具体错误（SET-006）。
func (h *Handler) GetHealthz(w http.ResponseWriter, r *http.Request) {
	if h.pinger != nil {
		ctx, cancel := context.WithTimeout(r.Context(), healthzTimeout)
		defer cancel()
		if err := h.pinger.Ping(ctx); err != nil {
			unavailable(w, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, api.Healthz{Status: api.Ok})
}

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
	// sort 目前只有 latest 一个合法值，传别的也按 latest 处理：契约里是枚举，
	// 生成物已经替我们挡掉了非法值，这里不需要再判。

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

	rows, err := h.queries.ListPublishedDemos(ctx, store.ListPublishedDemosParams{
		Limit:     int32(pageSize),
		Offset:    int32((page - 1) * pageSize),
		Q:         q,
		Genre:     genre,
		Technique: technique,
		Subject:   subject,
		Model:     model,
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

// classificationsOf 读一件作品的分类（详情页用）。
//
// 与 modelsOf 的两点不同：①分类不随版本变，所以不判 viewerIsAuthor；
// ②出错只记日志并返回空数组——分类读不出来不该让整个详情页 503，
// 那会把「少一行标注」升级成「页面打不开」。
func (h *Handler) classificationsOf(ctx context.Context, demoID int64) []api.ClassificationRef {
	out := []api.ClassificationRef{}
	if demoID <= 0 {
		return out
	}
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
	rows, err := h.queries.ListDemoClassifications(ctx, demoID)
	if err != nil {
		slog.Error("读作品分类失败", "demo", demoID, "err", err)
		return out
	}
	for _, r := range rows {
		out = append(out, api.ClassificationRef{
			Dimension: api.ClassificationRefDimension(r.Dimension),
			Slug:      r.Slug,
			Label:     r.Label,
		})
	}
	return out
}

// filterSlugs 把请求里的筛选值整理成「去空、去重、限长」的一批 slug。
//
// 返回 false 表示某一维超过了契约声明的 50 个：那说明调用方在拼 URL 时失控了，
// 与其让它变成一条 50 个 OR 的 SQL（PG 能跑，但计划会烂），不如直接 400。
//
// 不做「slug 是否存在」的校验：筛选是查询，未收录的 slug 命中 0 行就够了。
// 报 400 会把「用户改 URL 试试看」变成一堆红字，而那正是浏览筛选器的正常用法。
func filterSlugs(raw *[]string) ([]string, bool) {
	if raw == nil {
		return []string{}, true
	}
	seen := make(map[string]struct{}, len(*raw))
	out := make([]string, 0, len(*raw))
	for _, v := range *raw {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if _, dup := seen[v]; dup {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	if len(out) > maxFilterValues {
		return nil, false
	}
	return out, true
}

// classificationsFor 一次取回整页作品的分类，按 demo_id 分组。
//
// 一件作品一次查询会在 pageSize=50 时变成 51 次往返；这条查询只跑一次。
// 出错只记日志并返回空分类：列表本身还有用，不该因为分类读不出来整页 503。
func (h *Handler) classificationsFor(ctx context.Context, rows []store.ListPublishedDemosRow) map[int64][]api.ClassificationRef {
	out := make(map[int64][]api.ClassificationRef, len(rows))
	if len(rows) == 0 {
		return out
	}

	ids := make([]int64, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}

	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
	cls, err := h.queries.ListClassificationsForDemos(ctx, ids)
	if err != nil {
		slog.Error("读作品分类失败", "count", len(ids), "err", err)
		return out
	}
	for _, c := range cls {
		out[c.DemoID] = append(out[c.DemoID], api.ClassificationRef{
			Dimension: api.ClassificationRefDimension(c.Dimension),
			Slug:      c.Slug,
			Label:     c.Label,
		})
	}
	// 每一行都要有非 nil 的数组：契约里是 required，null 会让前端多一条分支。
	for _, row := range rows {
		if _, ok := out[row.ID]; !ok {
			out[row.ID] = []api.ClassificationRef{}
		}
	}
	return out
}

// ListClassifications 对应 GET /api/v1/classifications。
//
// 一次返回全部三个维度：92 条分类的响应只有几 KB，分页反而是负担——
// 筛选器需要一次拿到全量才画得出来，分页会让它要么发三次请求、要么先画半张。
func (h *Handler) ListClassifications(w http.ResponseWriter, r *http.Request) {
	if h.queries == nil {
		unavailable(w, errors.New("database not configured"))
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), queryTimeout)
	defer cancel()

	dims, err := h.queries.ListDimensions(ctx)
	if err != nil {
		unavailable(w, err)
		return
	}
	items, err := h.queries.ListClassifications(ctx)
	if err != nil {
		unavailable(w, err)
		return
	}

	// slug 索引：parent_id 是 id，响应里要的是 slug。
	slugByID := make(map[int64]string, len(items))
	for _, it := range items {
		slugByID[it.ID] = it.Slug
	}

	out := api.ClassificationCatalog{
		Dimensions: make([]struct {
			Description *string                                      `json:"description,omitempty"`
			Dimension   api.ClassificationCatalogDimensionsDimension `json:"dimension"`
			Label       string                                       `json:"label"`
			Mode        api.ClassificationCatalogDimensionsMode      `json:"mode"`
		}, 0, len(dims)),
		Items: make([]api.Classification, 0, len(items)),
	}
	for _, d := range dims {
		desc := d.Description
		out.Dimensions = append(out.Dimensions, struct {
			Description *string                                      `json:"description,omitempty"`
			Dimension   api.ClassificationCatalogDimensionsDimension `json:"dimension"`
			Label       string                                       `json:"label"`
			Mode        api.ClassificationCatalogDimensionsMode      `json:"mode"`
		}{
			Description: &desc,
			Dimension:   api.ClassificationCatalogDimensionsDimension(d.Dimension),
			Label:       d.Label,
			Mode:        api.ClassificationCatalogDimensionsMode(d.Mode),
		})
	}

	for _, it := range items {
		item := api.Classification{
			Dimension:   api.ClassificationDimension(it.Dimension),
			Slug:        it.Slug,
			Label:       it.Label,
			DemoCount:   int(it.DemoCount),
			Description: &it.Description,
			Aliases:     &it.Aliases,
		}
		if it.ParentID.Valid {
			if parent, ok := slugByID[it.ParentID.Int64]; ok {
				item.ParentSlug = &parent
			}
		}
		out.Items = append(out.Items, item)
	}

	writeJSON(w, http.StatusOK, out)
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

// preferString：有值就用它，空串退回备用值。
//
// 单独写一个而不是 `if x != ""`：详情页有四处这类取舍，散着写迟早漏掉一处，
// 而漏掉的表现是「作者看到空标题」这种不像 bug 的显示问题。
func preferString(primary string, fallback pgtype.Text) *string {
	if strings.TrimSpace(primary) != "" {
		return textPtr(pgtype.Text{String: primary, Valid: true})
	}
	return textPtr(fallback)
}

// formValues 读一个可以重复出现的 multipart 字段。
//
// 为什么不用 `r.FormValue`：它只返回第一个值。「选了 deepseek-chat 与
// deepseek-reasoner 两个模型」在表单里就是两个同名字段，只用 FormValue 会把
// 第二个悄悄丢掉——作者以为声明了两个，详情页只显示一个。
//
// 空值（全空格）在这里就过滤掉：调用方传空串进来没有意义，
// 而 `[]string{""}` 会让下游的「解析 slug」多跑一次无用查询。
func formValues(r *http.Request, key string) []string {
	if r.MultipartForm == nil {
		return nil
	}
	raw := r.MultipartForm.Value[key]
	if len(raw) == 0 {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func textPtr(t pgtype.Text) *string {
	if !t.Valid {
		return nil
	}
	value := t.String
	return &value
}
