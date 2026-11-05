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
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/api"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/demo"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/identity"
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
	defaultPageSize = 50
	maxPageSize     = 200
)

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
	// q 与 sort 目前只有一个合法值，暂时不参与过滤：
	// q 需要全文检索（pg_trgm 已装，但检索口径没定），sort 只有 latest。
	// 这里不假装支持——传了也只按默认行为返回，等口径定了再实现。

	ctx, cancel := context.WithTimeout(r.Context(), queryTimeout)
	defer cancel()

	total, err := h.queries.CountPublishedDemos(ctx)
	if err != nil {
		unavailable(w, err)
		return
	}

	rows, err := h.queries.ListPublishedDemos(ctx, store.ListPublishedDemosParams{
		Limit:  int32(pageSize),
		Offset: int32((page - 1) * pageSize),
	})
	if err != nil {
		unavailable(w, err)
		return
	}

	items := make([]api.DemoSummary, 0, len(rows))
	for _, row := range rows {
		items = append(items, summaryOf(row))
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

	detail, ok := detailOf(row)
	if !ok {
		writeError(w, http.StatusNotFound, api.NotFound, "作品不存在")
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

// ---- 纯函数：从数据行到响应体。抽出来是为了能单测，不用起数据库。----

func summaryOf(row store.ListPublishedDemosRow) api.DemoSummary {
	// 列表查询本身就带了 lifecycle_state = 'published' 的过滤，
	// 所以取出来的行一定是 published——不需要再从结果里读这个字段。
	return api.DemoSummary{
		Slug:           row.Slug,
		Title:          textPtr(row.Title),
		CoverUrl:       textPtr(row.CoverUrl),
		CreatedAt:      row.CreatedAt.Time,
		LifecycleState: api.LifecycleStatePublished,
	}
}

// detailOf 决定「对外可见吗」与「是不是墓碑」，false 表示 404：
// 已擦除 / 草稿待审驳回 → 404；归档、移除、软删 → 墓碑；published → 完整内容。
func detailOf(row store.GetDemoBySlugRow) (api.DemoDetail, bool) {
	state := api.LifecycleState(row.LifecycleState)

	// 已擦除：行还在（逻辑封存），但对外不存在。
	if row.ErasedAt.Valid {
		return api.DemoDetail{}, false
	}
	// 从未公开过。
	switch state {
	case api.LifecycleStateDraft, api.LifecycleStatePending, api.LifecycleStateRejected:
		return api.DemoDetail{}, false
	}

	tombstone := row.DeletedAt.Valid || state == api.LifecycleStateArchived || state == api.LifecycleStateRemoved

	// 墓碑只承载「这里曾经有过一个作品」，不承载内容，所以不看审核态。
	//
	// 完整内容必须有**对外版本**（迁移 0014）——原来是「审核态必须是 approved」。
	// 换成指针判定的理由：审核态从此描述「当前版本」，作者改稿后它是 pending，
	// 而对外的内容仍是上一版、照常可见。继续判审核态会让作者一提交更新，
	// 自己的作品就从作品库里消失。
	if !tombstone && !row.HasPublishedRevision {
		return api.DemoDetail{}, false
	}
	// 渠道对外可见：`hidden` 的作品对外不存在。
	//
	// 这条以前漏了，导致「列表里看不见、详情页却完整返回 200」——一个作品被设成
	// hidden 之后，知道 slug 的人仍然能读到标题、简介、封面，而 iframe 预览已经 404，
	// 页面变成「有正文、无内容」的半截状态。三处入口（列表 / 详情 / 预览 / 下载）
	// 必须同一口径：列表 `listed`（unlisted 不上列表）；其余三处 `listed|unlisted`。
	if !tombstone && !row.HasVisibleChannel {
		return api.DemoDetail{}, false
	}

	detail := api.DemoDetail{
		Slug:           row.Slug,
		CreatedAt:      row.CreatedAt.Time,
		UpdatedAt:      row.UpdatedAt.Time,
		LifecycleState: state,
	}
	if tombstone {
		// 墓碑态：标题与简介置空，保留 slug / 时间 / 状态，让页面能说「这里曾经有过一个作品」。
		// demoType 留空、downloadable 为 false——墓碑不承载内容，也没有可下载的文件。
		return detail, true
	}
	detail.Title = textPtr(row.Title)
	detail.Description = textPtr(row.Description)
	detail.CoverUrl = textPtr(row.CoverUrl)
	detail.DemoType = row.DemoType
	// downloadable 的口径与 httpapi/download.go 必须一致，否则按钮点了 404：
	// 有文件可给（不是 link）+ 渠道对外可见（listed 或 unlisted）。
	// 这里不判存储里文件是否存在——那要多一次 Stat 往返，
	// 而「文件被外力删掉」属于事故，不该由详情页替它兜底（下载会明确 404）。
	detail.Downloadable = row.DemoType != "" && row.DemoType != "link" && row.HasVisibleChannel && row.HasPublishedRevision
	return detail, true
}

func textPtr(t pgtype.Text) *string {
	if !t.Valid {
		return nil
	}
	value := t.String
	return &value
}
