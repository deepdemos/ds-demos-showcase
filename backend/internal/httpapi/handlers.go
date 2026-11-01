// Package httpapi 实现 api/openapi.yaml 定义的三个端点。
//
// 分层：这个包只负责「HTTP ↔ 领域判定」，SQL 在 internal/store（sqlc 生成）。
// 路由注册不在这里，在 internal/platform/httpserver——那里把实现塞进生成的路由表。
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/api"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

const (
	// healthzTimeout 是一次健康检查的上限。PG 卡住时不能让 /healthz 跟着挂住。
	healthzTimeout = 2 * time.Second
	// queryTimeout 是业务查询的上限。请求已经被客户端取消了也没关系，context 会先断。
	queryTimeout = 5 * time.Second

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
	pinger  Pinger
	queries *store.Queries // nil 表示未配置数据库
}

// New 组装处理器。queries 可以为 nil（未配置数据库），此时业务端点返回 503。
func New(pinger Pinger, queries *store.Queries) *Handler {
	return &Handler{pinger: pinger, queries: queries}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		slog.Error("write json failed", "err", err)
	}
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
		LifecycleState: api.Published,
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
	case api.Draft, api.Pending, api.Rejected:
		return api.DemoDetail{}, false
	}

	tombstone := row.DeletedAt.Valid || state == api.Archived || state == api.Removed

	detail := api.DemoDetail{
		Slug:           row.Slug,
		CreatedAt:      row.CreatedAt.Time,
		UpdatedAt:      row.UpdatedAt.Time,
		LifecycleState: state,
	}
	if tombstone {
		// 墓碑态：标题与简介置空，保留 slug / 时间 / 状态，让页面能说「这里曾经有过一个作品」。
		return detail, true
	}
	detail.Title = textPtr(row.Title)
	detail.Description = textPtr(row.Description)
	detail.CoverUrl = textPtr(row.CoverUrl)
	return detail, true
}

func textPtr(t pgtype.Text) *string {
	if !t.Valid {
		return nil
	}
	value := t.String
	return &value
}
