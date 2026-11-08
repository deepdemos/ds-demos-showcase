// Package httpapi 实现 api/openapi.yaml 定义的端点。
//
// 分层：这个包只负责「HTTP ↔ 领域判定」，SQL 在 internal/store（sqlc 生成）。
// 路由注册不在这里，在 internal/httpapi/router.go——那里把实现塞进生成的路由表。
package httpapi

// Package httpapi 实现 api/openapi.yaml 定义的端点。
//
// 分层：这个包只负责「HTTP ↔ 领域判定」，SQL 在 internal/store（sqlc 生成）。
// 路由注册不在这里，在 internal/httpapi/router.go——那里把实现塞进生成的路由表。

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

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
