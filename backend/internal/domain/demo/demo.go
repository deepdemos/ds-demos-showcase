// Package demo 是作品域的核心逻辑：投稿、校验、落盘。
//
// 分层：domain 不碰 HTTP（不 import net/http），SQL 走 internal/store（sqlc 生成），
// 文件走 internal/platform/storage。传输层只负责把 HTTP 请求翻译成这里的输入。
package demo

import (
	"errors"
	"fmt"
	"strings"
)

// 领域错误。传输层据此映射状态码，错误文字只给人看。
var (
	// ErrInvalidInput 提交内容本身不合格（字段缺失、超长、类型未知）。
	ErrInvalidInput = errors.New("demo: 提交内容不合格")
	// ErrPackageTooLarge 包体积超过上限。
	ErrPackageTooLarge = errors.New("demo: 作品包超过体积上限")
	// ErrUnsupportedPackage 既不是 zip 也不是 html/svg。
	ErrUnsupportedPackage = errors.New("demo: 不支持的作品包类型")
	// ErrBadPackage 包内容不合格（缺 index.html、压缩炸弹、成员过多等）。
	ErrBadPackage = errors.New("demo: 作品包内容不合格")
	// ErrTooManyPending 待审作品太多，暂时不能再投。
	ErrTooManyPending = errors.New("demo: 待审作品太多")
)

const (
	// maxTitleLen 与 db/schema/30-demo.sql 的 ck_demos_title_len 一致。
	maxTitleLen = 200
	// maxDescriptionLen 是接口层口径，不落库约束——描述是长文本，库层不设上限。
	maxDescriptionLen = 5000
	// maxSlugLen 与 ck_demos_slug_len 一致。
	maxSlugLen = 128
	// slugBaseLimit 是「从标题生成」那一段的最大长度，留出后缀余量。
	slugBaseLimit = 60
)

// SubmitInput 是一次投稿的全部输入。
//
// 不在这里传 io.Reader：调用方（传输层）已经把文件落到临时文件，这里只拿路径与大小——
// 解压与校验需要随机读、多次读，reader 做不到。
type SubmitInput struct {
	AuthorID    int64
	Title       string
	Description string
	DemoType    string
	// PackagePath 是原始上传包在本机的临时路径。
	PackagePath string
	// PackageSize 是它的字节数（由调用方边写边数，不信客户端给的 Content-Length）。
	PackageSize int64
}

// SubmitResult 是投稿成功后的最小回执。
type SubmitResult struct {
	Slug            string
	RevisionNo      int
	ModerationState string
	LifecycleState  string
}

// validate 检查元数据。文件层面的检查在 stagePackage 里。
func (in SubmitInput) validate() error {
	title := strings.TrimSpace(in.Title)
	if title == "" {
		return fmt.Errorf("%w: 标题不能为空", ErrInvalidInput)
	}
	if len([]rune(title)) > maxTitleLen {
		return fmt.Errorf("%w: 标题最多 %d 个字符", ErrInvalidInput, maxTitleLen)
	}
	if len([]rune(in.Description)) > maxDescriptionLen {
		return fmt.Errorf("%w: 描述最多 %d 个字符", ErrInvalidInput, maxDescriptionLen)
	}
	// demoType 的**取值**合法性由 demo_types 外键兜底（fk_demo_revisions_demo_type75），
	// 这里只挡格式。外键违反是 23503，submit.go 会把它翻成 ErrInvalidInput ——
	// 不翻的话它会落到 writeDemoError 的 default 分支变成 503，
	// 而契约写的是 400「类型未知」。
	if in.DemoType == "" {
		return fmt.Errorf("%w: 缺少作品类型", ErrInvalidInput)
	}
	if len(in.DemoType) > 32 {
		return fmt.Errorf("%w: 作品类型名过长", ErrInvalidInput)
	}
	for _, r := range in.DemoType {
		if !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9') && r != '_' {
			return fmt.Errorf("%w: 作品类型只能用小写字母、数字和下划线", ErrInvalidInput)
		}
	}
	if in.AuthorID == 0 {
		return fmt.Errorf("%w: 缺少作者", ErrInvalidInput)
	}
	if in.PackagePath == "" || in.PackageSize <= 0 {
		return fmt.Errorf("%w: 作品包为空", ErrInvalidInput)
	}
	return nil
}

// slugify 把标题压成 URL 片段。
//
// 中文标题压不出东西是正常的——返回空串，由调用方退回随机 slug。
// 不引入拼音库：多一个依赖换来的只是「好看一点的 URL」，而 slug 一旦发出就不该再变，
// 生成规则越简单越不容易在以后被改。
func slugify(title string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(strings.TrimSpace(title)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		default:
			// 其余一律当分隔符：空格、标点、中文都归到这里。
			// 丢掉它们会把 "a1!b2" 变成 "a1b2"（两个词粘一起），加个连字符更诚实。
			if b.Len() > 0 && !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if len(out) > slugBaseLimit {
		out = strings.Trim(out[:slugBaseLimit], "-")
	}
	return out
}
