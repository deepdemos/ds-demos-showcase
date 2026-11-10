package httpapi

import (
	"embed"
	"html/template"
	"net/http"
	"path"
	"strings"
)

// 站内三维预览器：把作者上传的 .glb 放进一个只读查看页里。
//
// # 为什么内置播放器，而不是让作者自己带页面
//
// .glb 是二进制资产，浏览器不会自己渲染它。若平台不给查看页，作者就得把模型塞进
// 一个自写的 HTML 里（那等于要求每个作者都会写 three.js），或者干脆不投。
//
// # 为什么把 model-viewer 打进二进制
//
// 从 CDN 引会带来三个问题：预览 iframe 的 CSP 要放行外部脚本源、离线/内网部署白屏、
// 以及「同一份作品今天能看明天不能看」取决于别人的服务。1 MB 的一次性体积换掉这三条，
// 值。资产随二进制走，部署不需要额外同步目录。
//
// # 为什么这个页面**不加 sandbox**
//
// previewCSP 里的 sandbox 是给「作者写的 HTML」用的：那份内容不完全可信，不能让它的
// 脚本拿到同源权限。这一页是我们自己生成的静态骨架，只引用同源资产、只读一个模型地址，
// 没有作者可控的脚本。给它加 sandbox 反而会挡住模块脚本加载（不透明源下的 CORS 失败），
// 于是换成一个更窄的策略：没有内联脚本、没有外部源，只允许读自己。
const glbViewerCSP = "default-src 'none'; script-src 'self'; style-src 'unsafe-inline'; " +
	"img-src 'self' data: blob:; connect-src 'self' blob: data:; worker-src blob:; " +
	"frame-ancestors *"

//go:embed previewassets/model-viewer.min.js
var previewAssets embed.FS

// modelViewerPath 是查看页引用的播放器地址。放在服务端而不是前端产物里：
// 预览页与它必须同源，而前端的静态目录在容器部署里由 nginx 托管。
const modelViewerPath = "/preview-assets/model-viewer.min.js"

var glbViewerTemplate = template.Must(template.New("glb").Parse(`<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>三维模型预览</title>
<style>
  html, body { margin: 0; height: 100%; background: #0f1115; }
  model-viewer { width: 100%; height: 100%; --poster-color: transparent; }
  .hint { position: fixed; left: 12px; bottom: 10px; color: #7c8798; font: 12px/1.5 system-ui, sans-serif; }
</style>
</head>
<body>
<model-viewer src="{{.Src}}" camera-controls touch-action="pan-y" auto-rotate
  auto-rotate-delay="1500" rotation-per-second="20deg" shadow-intensity="1"
  exposure="1" interaction-prompt="none" alt="{{.Alt}}"></model-viewer>
<div class="hint">拖动旋转 · 滚轮缩放 · 双击复位</div>
<script type="module" src="{{.Player}}"></script>
</body>
</html>
`))

// isGLB 判断这个相对路径是不是三维模型入口。
func isGLB(rel string) bool {
	return strings.EqualFold(path.Ext(rel), ".glb")
}

// serveGLBViewer 渲染查看页；真实字节走同一个地址加 `?raw=1`。
//
// 为什么不另开一个「原始文件」路径：预览地址已经带着版本与文件名，任何第二套路径都要
// 再解一次 slug/版本，也就多一处可以判错的地方。查一个 query 只花一行。
func (p *Preview) serveGLBViewer(w http.ResponseWriter, r *http.Request, rel string, csp string) {
	raw := *r.URL
	q := raw.Query()
	q.Set("raw", "1")
	raw.RawQuery = q.Encode()

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", csp)
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodHead {
		return
	}
	_ = glbViewerTemplate.Execute(w, struct {
		Src    string
		Player string
		Alt    string
	}{
		Src:    raw.RequestURI(),
		Player: modelViewerPath,
		Alt:    "三维模型：" + path.Base(rel),
	})
}

// servePreviewAssets 把内置播放器发出去。
//
// 长缓存 + immutable：文件名里没有摘要，但内容随二进制走——升级二进制时它的修改时间变了，
// 部署会先换进程再换连接，浏览器最多拿一天旧版本，而旧版本对新模型同样能跑。
func servePreviewAssets(w http.ResponseWriter, r *http.Request) {
	data, err := previewAssets.ReadFile("previewassets/model-viewer.min.js")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(data)
}
