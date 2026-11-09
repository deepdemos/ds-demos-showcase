package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/api"
)

// NewRouter 把「实现」塞进契约生成的路由表。
//
// 地址表不是人手写的：api.HandlerFromMux 依据 api/openapi.yaml 注册路径与参数解析。
// 契约里加一个端点，这里编译就会要求实现补一个方法——这是「先改契约再改实现」的可执行形式。
//
// 中间件仍由我们自己加（RequestID / Recoverer）；生成的只是路由与参数绑定。
//
// **不要加 middleware.RealIP**：它会把客户端自报的 `X-Forwarded-For` 无条件写进
// `RemoteAddr`，而限流把 `RemoteAddr` 当作 IP 键——加了等于让攻击者自己选键。
// 实测：真实地址 203.0.113.7 + 伪造 XFF 1.2.3.4 时，clientIP() 返回 1.2.3.4。
//
// 上反向代理后要按白名单显式解析 XFF（见 httpapi.clientIP 的注释与运行手册 §3）。
// preview 与 download 都可以为 nil（未接线存储），此时对应路径统一 404。
func NewRouter(si api.ServerInterface, preview *Preview, download *Download) http.Handler {
	router := chi.NewRouter()
	router.Use(middleware.RequestID, middleware.Recoverer)

	// 预览托管不是契约端点（见 httpapi/preview.go 顶部注释），直接挂在 chi 上。
	// 契约路径都在 /api/v1 与 /healthz 下，与 /preview 不冲突。
	// 用 Handle 而不是逐方法注册：`ServeHTTP` 自己判方法并回 405 兜底页
	// （405 的正文同样是渲染在 iframe 里的页面，不该是 `http.Error` 的一行纯文本）。
	// 逐方法注册的话 chi 会先一步拦下 POST/PUT，那个分支就永远走不到。
	router.Handle("/preview/{slug}/*", previewOrNotFound(preview))
	router.Handle("/preview/{slug}", previewOrNotFound(preview))

	// 下载同样不是契约端点（响应体是 zip / html 字节，不是 JSON），理由见 download.go 顶部。
	// 两个处理器各占一个路径段，不冲突。
	router.Method("GET", "/download/{slug}", downloadOrNotFound(download))
	router.Method("HEAD", "/download/{slug}", downloadOrNotFound(download))

	return api.HandlerFromMux(si, router)
}

func downloadOrNotFound(d *Download) http.Handler {
	if d != nil {
		return d
	}
	return http.NotFoundHandler()
}

func previewOrNotFound(p *Preview) http.Handler {
	if p != nil {
		return p
	}
	return http.NotFoundHandler()
}
