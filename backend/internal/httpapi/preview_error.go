package httpapi

import (
	"fmt"
	"net/http"
)

// 预览的失败分支为什么要自己写 HTML
//
// `/preview/{slug}/` 的响应体是**给人看的页面**（作品自己的 HTML），不是 JSON。
// 失败时用 `http.NotFound` 会得到一行纯文本 `404 page not found`，它出现在两个
// 用户可见的地方：详情页的 `<iframe>` 里，以及「在新标签页打开预览」时。
//
// 前端 iframe 是跨源的（sandbox 不给 allow-same-origin），**读不到 iframe 内部状态**，
// 所以兜底只能在服务端做。
//
// 三条硬约束：
//  1. 页面里**不插值任何请求内容**（slug、路径都不进页面）——纯静态文案，
//     注入面直接归零；
//  2. 状态码保持原语义：404 / 500 / 502 不为好看而合并，500 不改成 404；
//  3. 继续带预览那套 `Content-Security-Policy: sandbox …`，与正常预览一致。
const (
	previewErrorTitle = "这个作品现在打不开"
	previewErrorHint  = "可以回到作品库查看其它作品。"
	previewErrorCT    = "text/html; charset=utf-8"
	previewErrorLang  = "zh-CN"
	// 三段文案按状态分开：404 与 500 的「下一步」不一样，混成一句等于都没说。
	previewMissingDetail      = "作品不存在、还没有公开，或者内容已经被移除。"
	previewMissingEntryDetail = "这一版缺少入口文件，作者需要重新上传。"
	previewReadFailedDetail   = "读取作品文件失败，请稍后再试。"
	previewErrorHTMLFm        = `<!doctype html>
<html lang="` + previewErrorLang + `">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>%s</title>
<style>
  html, body { margin: 0; height: 100%%; }
  body {
    display: flex; align-items: center; justify-content: center;
    background: #f6f6f4; color: #1f2328;
    font-family: -apple-system, "Segoe UI", "Noto Sans SC", sans-serif;
    padding: 24px; box-sizing: border-box;
  }
  .card { max-width: 26rem; text-align: center; }
  h1 { font-size: 1.125rem; font-weight: 600; margin: 0 0 0.75rem; }
  p { font-size: 0.9375rem; line-height: 1.6; margin: 0 0 0.5rem; color: #444a51; }
  .hint { font-size: 0.8125rem; color: #6b7280; margin-top: 1.25rem; }
  @media (prefers-color-scheme: dark) {
    body { background: #16181c; color: #e6e8eb; }
    p { color: #b6bcc4; }
    .hint { color: #8b929c; }
  }
</style>
</head>
<body>
<div class="card">
  <h1>%s</h1>
  <p>%s</p>
  <p class="hint">%s</p>
</div>
</body>
</html>
`
)

// previewError 是预览失败的唯一出口。
//
// detail 是**固定文案之一**（见下），调用方不得把 slug、路径或驱动错误拼进去：
// 这条响应会渲染在别人的作品页 iframe 里，任何插值都是一个注入面。
func previewError(w http.ResponseWriter, r *http.Request, status int, detail string) {
	w.Header().Set("Content-Type", previewErrorCT)
	// 失败页不缓存：作品可能在下一秒被重新发布、或作者刚修好入口文件。
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// 与正常预览同一套 sandbox：失败页里也没有任何脚本，但口径要一致——
	// 两处不一致迟早会导致「某个分支下的行为不一样」。
	w.Header().Set("Content-Security-Policy", previewCSP)
	w.WriteHeader(status)
	// HEAD 不写 body：真实的 net/http 会替我们丢掉，但 handler 本身也不该把
	// 一千多字节的页面塞给它——测试用的 ResponseRecorder 不会丢，两边行为不一致
	// 时最先发现的往往是这种「只在测试里带 body」的差异。
	if r.Method == http.MethodHead {
		return
	}
	if _, err := fmt.Fprintf(w, previewErrorHTMLFm, previewErrorTitle, previewErrorTitle, detail, previewErrorHint); err != nil {
		// 响应头已发出，写失败只能放弃：这里没有可回的通道了。
		return
	}
}
