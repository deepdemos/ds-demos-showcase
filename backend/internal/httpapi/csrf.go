// 跨站请求伪造（CSRF）的防线。
//
// # 为什么用它、为什么不是自己写 token
//
// 这个项目只有一个客户端，就是同域的前端；会话走 `__Host-demo_session` cookie。
// 这样的形状正好落在浏览器已经能替我们判定的那一档上：现代浏览器对每个请求都发
// `Sec-Fetch-Site`，跨站的 POST 一眼可辨；老浏览器不退后，标准库还会比对
// `Origin` 与 `Host`（`http.CrossOriginProtection` 的文档原话）。
//
// OWASP 的 CSRF 备忘单把这套排在 token 之前：token（synchronizer / double-submit）
// 是给「要兼容老浏览器」或「有跨站调用方」的场景准备的，代价是每张表单多发一个随机数、
// 每个端点都得记得校验，漏一个就等于没有。这里没有跨站调用方（将来有自动化调用会走
// 显式的凭据方案，见松点 45），所以先用不需要在每个端点上写对的方案。
//
// # 为什么先只记不拦
//
// OWASP 明确要求先观察再开拦：漏掉一个合法的跨站流程（比如以后接的 OAuth 回调、
// 论坛 SSO 的 POST 回跳）会让用户看到 403 而完全不知道为什么。所以三种模式：
//
//	off     —— 完全不看（只用于排查「到底是它拦的」这种情况）
//	log     —— 判、记日志、放行（默认值，先在真实流量上看一段时间）
//	enforce —— 判、记日志、拦下并回 403
//
// 模式来自 `CSRF_MODE` 环境变量。默认 log 不是保守，是**观察期**：没在真实流量上
// 见过一轮之前就开拦，等于赌自己记得全。
package httpapi

import (
	"log/slog"
	"net/http"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/api"
)

// CSRFMode 是跨站检查的三种档位。
type CSRFMode string

const (
	// CSRFOff 完全不检查。只用于定位问题。
	CSRFOff CSRFMode = "off"
	// CSRFLog 判定但不拦，只记日志。默认档位，观察期用。
	CSRFLog CSRFMode = "log"
	// CSRFEnforce 判定并拦下跨站的非安全请求。
	CSRFEnforce CSRFMode = "enforce"
)

// ParseCSRFMode 把环境变量的字面值解析成档位。
//
// 空串按默认档位处理（观察期）；无法识别的值报错而不是静默降级——
// 拼错的 `CSRF_MODE=enforcee` 如果被当成默认值，运维会以为自己在拦，其实没有。
func ParseCSRFMode(raw string) (CSRFMode, error) {
	switch CSRFMode(raw) {
	case "":
		return CSRFLog, nil
	case CSRFOff, CSRFLog, CSRFEnforce:
		return CSRFMode(raw), nil
	default:
		return "", &unknownCSRFModeError{raw: raw}
	}
}

type unknownCSRFModeError struct{ raw string }

func (e *unknownCSRFModeError) Error() string {
	return `CSRF_MODE 只能是 off / log / enforce，收到的是 "` + e.raw + `"`
}

// newCSRFMiddleware 造一层跨站检查中间件。
//
// 判定交给标准库（`http.CrossOriginProtection`），这里只做「记什么、拦不拦」。
// 安全方法（GET/HEAD/OPTIONS）标准库自己会放过——这也是本站所有读端点都是 GET 的原因之一。
func newCSRFMiddleware(mode CSRFMode, logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if mode == CSRFOff || mode == "" {
			return next
		}
		cop := http.NewCrossOriginProtection()
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if err := cop.Check(r); err != nil {
				// 记的是「判定依据」而不是 err 文本：标准库的错误只有一句话，
				// 排查时真正要看的是这个请求自称从哪来。
				logger.WarnContext(r.Context(), "跨站请求检查未通过",
					"method", r.Method,
					"path", r.URL.Path,
					"sec_fetch_site", r.Header.Get("Sec-Fetch-Site"),
					"origin", r.Header.Get("Origin"),
					"host", r.Host,
					"mode", string(mode),
				)
				if mode == CSRFEnforce {
					// 403 与 identity 的「登录了但没权限」共用错误码：这里对客户端
					// 不区分「你没登录」与「这是跨站请求」，否则等于告诉攻击者
					// 哪一种尝试更接近成功。
					writeError(w, http.StatusForbidden, api.Forbidden, "跨站请求被拒绝")
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// WithCSRF 给整棵路由套上跨站检查。放在 main 里而不是 NewRouter 的参数里，
// 是因为 NewRouter 的签名被几十个测试直接调用，加参数会把噪声扩散到所有用例；
// 而「套不套、怎么套」是启动装配的事，测试里想测就自己包一层。
func WithCSRF(next http.Handler, mode CSRFMode, logger *slog.Logger) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return newCSRFMiddleware(mode, logger)(next)
}
