package demo

import (
	"path/filepath"
	"sort"
	"strings"
)

// # 投稿形态：一个事实，一个出处
//
// 一次投稿会落成什么形状（单文件还是 zip、入口文件叫什么），**只由这里的
// `singleFileKinds` 与 `stagedName` 决定**。别的任何地方都不许再推导一遍。
//
// 这条规矩是拿事故换来的：预览曾经在没有文件名时把入口写死成 index.html，
// 而单文件 SVG 的入口叫 index.svg —— 于是「投稿成功、预览页 404」。
// 写侧与读侧各推了一次同一个事实，推岔了。
//
// 现在的补偿手段有三层，缺一层这个洞就会再开：
//
//  1. 读侧不再推：预览从 `demo_revisions.entry_path` 取值（302 到带文件名的地址）；
//  2. 门禁：`check-single-source` 把入口文件名的字面量登记成一类判定，
//     除本文件之外出现即红；
//  3. 测试：`SubmitShapes()` 是形态矩阵用例的数据源——**加一种格式只改这里**，
//     矩阵会自动多一行，于是「新增格式的预览能不能打开」这件事不可能没人测。
//
// 新增格式时只改 `singleFileKinds`：投稿校验、入口文件名、落库的 `single_file`
// 与形态矩阵用例全部从它派生。

// singleFileKinds 是「单文件作品」接受的扩展名 → `demo_revisions.single_file` 取值。
//
// 键是小写扩展名（含点）。`html` 与 `htm` 落同一个 single_file 值：
// 对下游（预览、下载）它们就是一回事，区分只发生在暂存阶段。
var singleFileKinds = map[string]string{
	".html": "html",
	".htm":  "html",
	".svg":  "svg",
	".glb":  "glb",
	".png":  "png",
	".jpg":  "jpg",
	".jpeg": "jpg",
	".webp": "webp",
}

// zipEntry 是 zip 包的入口文件名：`submit` 只认根目录下的它（或 `stagedName`
// 归一后的同一名字）。压缩包不像单文件那样能「原件就是入口」，没有它就无从打开。
const zipEntry = "index.html"

// SubmitShape 描述一种**能被投稿的输入形态**，以及它落库之后的样子。
//
// 它有两个用途：测试的形态矩阵数据源，以及人读的「我们现在支持哪些投稿」清单。
type SubmitShape struct {
	// Kind 是人话里的形态名：`zip` / `html` / `htm` / `svg`。
	Kind string
	// FileName 是提交时用的文件名（改扩展名会改形态，所以它属于契约的一部分）。
	FileName string
	// SingleFile 是 `demo_revisions.single_file` 的取值；空串表示多文件（zip）。
	SingleFile string
	// EntryPath 是落库的 `demo_revisions.entry_path`。
	EntryPath string
}

// SubmitShapes 列出当前支持的**全部**投稿形态，按入口文件名与形态名排序。
//
// 顺序固定是为了让测试报错稳定：形态矩阵的失败信息里会带下标，
// 顺序一乱，两次跑出来的「第 3 行」指的就不是同一件事。
func SubmitShapes() []SubmitShape {
	out := []SubmitShape{{
		Kind:       "zip",
		FileName:   "bundle.zip",
		SingleFile: "",
		EntryPath:  zipEntry,
	}}

	exts := make([]string, 0, len(singleFileKinds))
	for ext := range singleFileKinds {
		exts = append(exts, ext)
	}
	sort.Strings(exts)

	for _, ext := range exts {
		out = append(out, SubmitShape{
			Kind:       strings.TrimPrefix(ext, "."),
			FileName:   "upload" + ext,
			SingleFile: singleFileKinds[ext],
			EntryPath:  stagedName("upload" + ext),
		})
	}
	return out
}

// singleFileKind 返回扩展名对应的 `single_file` 取值；ok 为 false 表示不接受。
func singleFileKind(ext string) (string, bool) {
	kind, ok := singleFileKinds[strings.ToLower(ext)]
	return kind, ok
}

// stagedName 是单文件作品在暂存目录里的文件名，也就是最终的 `entry_path`。
//
// `.htm` 归一成 `.html`：预览按扩展名给 Content-Type，两种写法归一到一个出口，
// MIME 判断就不会在两条路径上分叉。
func stagedName(fileName string) string {
	ext := strings.ToLower(filepath.Ext(fileName))
	if ext == ".htm" {
		ext = ".html"
	}
	return "index" + ext
}

// isRootIndex 判断 zip 里的某个相对路径是不是根目录的入口文件。
//
// 只有根目录的算入口：`dist/index.html` 是内容而不是入口——那样打开预览会直接
// 看到一份没有静态资源的页面。要发布构建产物，请把 `dist` 里的东西作为根目录重新打包。
func isRootIndex(rel string) bool {
	switch rel {
	case zipEntry, "index.htm":
		return true
	}
	return false
}

// 格式串是「类型允许什么」与「这份上传到底是什么」之间的对接词汇。
//
// 真相只在这个文件里：`demo_type_formats.format` 存的是这些字符串（那是数据），
// 词汇本身是代码。加一种形态要改这里；加「某个类型收哪些格式」只加一行数据。
const (
	// FormatURL 表示没有上传文件，作者给一个外部地址。
	FormatURL = "url"

	formatSingle = "single:"
	formatZip    = "zip:"
)

// FormatOfSingleFile 把 `demo_revisions.single_file` 的取值转成格式串。
func FormatOfSingleFile(kind string) string { return formatSingle + kind }

// ZipFormatOf 判断这个 zip 是哪一种包。
//
// Godot 导出的 Web 包一定带 `.pck`（4.x 另有 `.wasm`，3.x 是 `.asm.js`）。
// 只看扩展名、不看内容：判错形态的代价是作者拿到一句可读的提示，
// 而猜内容的代价是「上传成功、打开白屏」。
func ZipFormatOf(files []string) string {
	hasScript, hasPack := false, false
	for _, f := range files {
		switch strings.ToLower(filepath.Ext(f)) {
		case ".wasm", ".asm.js":
			hasScript = true
		case ".pck":
			hasPack = true
		}
	}
	if hasScript && hasPack {
		return formatZip + "godot"
	}
	return formatZip + "html"
}

// KnownFormats 列出代码认识的格式串。
//
// `demo_type_formats` 里出现别的值说明代码与数据漂了：集成测试会拿这份清单
// 与库里的行做差集，漂了就红。
func KnownFormats() []string {
	out := []string{FormatURL, ZipFormatOf([]string{"a.wasm", "a.pck"}), ZipFormatOf([]string{"index.html"})}
	kinds := make([]string, 0, len(singleFileKinds))
	seen := map[string]bool{}
	for _, kind := range singleFileKinds {
		if !seen[kind] {
			seen[kind] = true
			kinds = append(kinds, kind)
		}
	}
	sort.Strings(kinds)
	for _, kind := range kinds {
		out = append(out, FormatOfSingleFile(kind))
	}
	return out
}
