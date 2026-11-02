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
// 中间件仍由我们自己加（RequestID / RealIP / Recoverer）；生成的只是路由与参数绑定。
func NewRouter(si api.ServerInterface) http.Handler {
	router := chi.NewRouter()
	router.Use(middleware.RequestID, middleware.RealIP, middleware.Recoverer)
	return api.HandlerFromMux(si, router)
}
