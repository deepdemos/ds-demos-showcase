// Package demo 是作品域的核心逻辑：投稿、校验、落盘。
//
// 分层：domain 不碰 HTTP（不 import net/http），SQL 走 internal/store（sqlc 生成），
// 文件走 internal/platform/storage。传输层只负责把 HTTP 请求翻译成这里的输入。
package demo

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"unicode/utf8"
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
	//
	// 注意它**没有扩展名**（`os.CreateTemp` 生成的随机名），所以判断类型时
	// 必须看 PackageName，不能看它。踩过：只看 PackagePath 时
	// `filepath.Ext` 恒为空串，单文件 html/svg 投稿 100% 报「不支持的作品包类型」。
	PackagePath string
	// PackageName 是客户端带过来的原始文件名，**只用来取扩展名**，
	// 不参与任何路径拼接。
	PackageName string
	// PackageSize 是它的字节数（由调用方边写边数，不信客户端给的 Content-Length）。
	PackageSize int64
	// IdempotencyKey 是客户端给的「这次提交的编号」，可以不给。
	//
	// 给了就意味着：同一个作者的同一个 key 只会落一行。脚本传 200 MiB 传到一半
	// 连接断了、它自己重传一次，不该变成两条一模一样的作品等人工去删。
	//
	// 长度上限 128 与 `demos.idempotency_key` 的约束一致——**在领域层先挡**，
	// 别让它跑到库里变成一个 23514，那种错误对客户端毫无意义。
	IdempotencyKey string
	// ModelSlugs 是作者声明的模型（slug，不是 id）。空列表合法。
	//
	// 用 slug 而不是 id：id 是库内主键，前端拿到的模型清单来自
	// `GET /api/v1/models`，它给的就是 slug；把 id 放进请求体等于让客户端
	// 依赖一个会随主数据重建而变的数字。
	// ExternalURL 是外链作品的地址：给了它就不该再有上传文件。
	//
	// 两种输入形态互斥——外链作品没有字节可交付，上传了文件反而说明作者选错了类型。
	// 「只能给一个」这条判在 validate 里，因为它同时是契约（400）与领域不变量。
	ExternalURL string
	ModelSlugs  []string
	// ClassificationSlugs 是这件作品的内容分类（取 classifications.slug）。
	//
	// 与 ModelSlugs 的关键区别：分类挂在**作品**上，模型挂在**版本**上
	// （demo_classifications 没有 revision_id，demo_revision_models 有）。
	// 改稿换技术实现不该让「这是个游戏」重算，而换模型声明会产生新版本。
	// 所以分类只在投稿时写一次，之后所有版本共享。
	ClassificationSlugs []string
}

// SubmitResult 是投稿成功后的最小回执。
type SubmitResult struct {
	Slug            string
	RevisionNo      int
	ModerationState string
	LifecycleState  string
	// Replayed 为真表示这次请求命中了幂等键，返回的是**当初那条**，没有新建。
	//
	// 传输层据此回 200 而不是 201：客户端（尤其是脚本）应当能区分
	// 「我创建了它」与「它早就存在」——后者不该被当成一次新的上传成功。
	Replayed bool
}

// validate 检查元数据。文件层面的检查在 stagePackage 里。
// validateText 检查一段用户文本是不是合法 UTF-8。
//
// 不检查的后果是错的：非法字节会被原样送进 PG，PG 报 22021，
// 然后这条错误一路落到 writeDemoError 的 default，回给作者 503
// 「服务暂时不可用」——输入问题被说成了我们这边坏了（真栈复现过：
// Windows 上的 curl 传中文会按 GBK 发出去）。
// maxExternalURLLen 与 `demo_revisions.ck_rev_external_url_len` 对齐。
//
// 两处都挡：库约束是最后一道（绕过领域层也进不去），这里挡是为了给出
// 「外部地址最长 2000 个字符」而不是一句 23514。
const maxExternalURLLen = 2000

// validateExternalURL 只放行 http / https。
//
// 为什么不做更细的校验：地址由作者提供，平台只负责 302 过去；放行 javascript:
// 这类协议会把「点一个作品」变成执行脚本。判断 scheme 是这里唯一值得做的事。
func validateExternalURL(raw string) error {
	v := strings.TrimSpace(raw)
	if v == "" {
		return nil
	}
	if len([]rune(v)) > maxExternalURLLen {
		return fmt.Errorf("%w: 外部地址最长 %d 个字符", ErrInvalidInput, maxExternalURLLen)
	}
	u, err := url.Parse(v)
	if err != nil {
		return fmt.Errorf("%w: 外部地址不是合法的 URL", ErrInvalidInput)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("%w: 外部地址只能以 http:// 或 https:// 开头", ErrInvalidInput)
	}
	if u.Host == "" {
		return fmt.Errorf("%w: 外部地址缺少域名", ErrInvalidInput)
	}
	return nil
}

func validateText(field, v string) error {
	if !utf8.ValidString(v) {
		return fmt.Errorf("%w: %s 不是合法的 UTF-8 文本", ErrInvalidInput, field)
	}
	return nil
}

// validateMetaTexts 校验「元信息」这一类输入：标题、描述、作品类型、模型标识。
//
// 投稿与编辑共用一套口径——两边各写一份的结果是编辑链路上少了 UTF-8 校验，
// 同一类非法字节在 POST 上得到 400、在 PUT 上得到 503（第六轮 review 抓到的）。
func validateMetaTexts(title, description, demoType string, modelSlugs []string) error {
	for _, f := range []struct{ name, value string }{
		{"标题", title},
		{"描述", description},
		{"作品类型", demoType},
	} {
		if err := validateText(f.name, f.value); err != nil {
			return err
		}
	}
	for _, slug := range modelSlugs {
		if err := validateText("模型标识", slug); err != nil {
			return err
		}
	}
	return nil
}

func (in SubmitInput) validate() error {
	if err := validateText("幂等键", in.IdempotencyKey); err != nil {
		return err
	}
	if err := validateMetaTexts(in.Title, in.Description, in.DemoType, in.ModelSlugs); err != nil {
		return err
	}
	if err := validateText("外部地址", in.ExternalURL); err != nil {
		return err
	}
	if err := validateExternalURL(in.ExternalURL); err != nil {
		return err
	}
	// 文件与地址互斥：外链作品没有字节，传了文件说明类型选错了；反过来，
	// 要给地址却没给文件也不算上传——那会建出一条「有版本、没内容」的作品。
	hasFile := strings.TrimSpace(in.PackagePath) != ""
	hasURL := strings.TrimSpace(in.ExternalURL) != ""
	if hasFile == hasURL {
		return fmt.Errorf("%w: 要么上传作品文件，要么给一个外部地址（只能给一个）", ErrInvalidInput)
	}

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
	// 外链作品没有包：这条判据只对「有文件的那条路」成立。
	// 互斥关系在 validate 开头已经判过，这里再判一次只是防御——
	// 但不能再要求「必须有包」，否则外链投稿永远过不来。
	if strings.TrimSpace(in.ExternalURL) == "" && (in.PackagePath == "" || in.PackageSize <= 0) {
		return fmt.Errorf("%w: 作品包为空", ErrInvalidInput)
	}
	return nil
}

// slugify 把标题压成 URL 片段。
//
// 中文标题压不出东西是正常的——返回空串，由调用方退回随机 slug。
// 不引入拼音库：多一个依赖换来的只是「好看一点的 URL」，而 slug 一旦发出就不该再变，
// 生成规则越简单越不容易在以后被改。
// SlugifyForImport 把一个名字（可能是中文、可能带渠道标记）折成 slug 的基座。
//
// 中文折不出可读的 slug（一律归成分隔符），调用方拿到空串时应退回自己的兜底形态，
// 而不是硬造一个 `-` 开头的串。这里只是把既有的 slugify 暴露给导入侧复用，
// 保证「作者投稿建的 slug」与「管理员导入建的 slug」遵循同一套规则。
func SlugifyForImport(name string) string {
	return slugify(name)
}

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
