package httpserver

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// Pinger 是 /healthz 依赖的最小接口。
//
// 用接口而不是直接依赖 *pgxpool.Pool，有两个好处：
//   - 没配数据库时也能起服务（传 nil）；
//   - 单测里可以喂一个必定失败的实现，验证 503 分支。
type Pinger interface {
	Ping(ctx context.Context) error
}

// healthzTimeout 是一次健康检查的上限。PG 卡住时不能让 /healthz 跟着挂住——
// 运行检查本身超时，比它返回 503 更难排查。
const healthzTimeout = 2 * time.Second

// NewRouter 组装 HTTP 路由。db 可以为 nil（表示未配置数据库）。
func NewRouter(db Pinger) http.Handler {
	router := chi.NewRouter()
	router.Use(middleware.RequestID, middleware.RealIP, middleware.Recoverer)
	router.Get("/healthz", healthz(db))
	return router
}

// errorBody 与 api/openapi.yaml 的 Error schema 对齐。
type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		slog.ErrorContext(context.Background(), "write json failed", "err", err)
	}
}

// healthz 对应契约里的 GET /healthz。
//
// 契约要求：200 时返回 {"status":"ok"}；核心资料暂不可访问时返回非 200（503 + Error）。
// **不得携带任何配置值、账号信息或内部依赖细节**（SET-006）——所以 503 的 message 是
// 固定文案，真实错误只进日志，不进响应体。
func healthz(db Pinger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if db == nil {
			// 未配置数据库：当前阶段 API 还没有依赖库的能力，视为可用。
			// 等端点真正查库之后，这里要改成 503——那时「没有库」就是「用不了」。
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), healthzTimeout)
		defer cancel()

		if err := db.Ping(ctx); err != nil {
			// 具体错误只进日志：连接串、主机名、驱动报错都可能出现在里面。
			slog.ErrorContext(ctx, "healthz: database unreachable", "err", err)
			writeJSON(w, http.StatusServiceUnavailable, errorBody{
				Code:    "unavailable",
				Message: "核心资料暂不可访问",
			})
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}
