package httpapi

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/api"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/demo"
)

// maxMultipartMemory 是 multipart 解析时放在内存里的上限。
//
// 超出部分 net/http 会落到临时文件，所以设小一点没关系——真正的体积闸在
// MaxBytesReader 与 domain 层，这里只是「别把表单字段也读到内存里」。
const maxMultipartMemory = 8 << 20

// SubmitDemo 处理投稿。
//
// 与其它端点不同，这里的手写成分更多：oapi-codegen 不会为 multipart 生成绑定器，
// 表单字段与文件都得自己读。
func (h *Handler) SubmitDemo(w http.ResponseWriter, r *http.Request, params api.SubmitDemoParams) {
	if h.demos == nil {
		writeError(w, http.StatusServiceUnavailable, api.Unavailable, "服务暂时不可用")
		return
	}

	// 1) 先认人。**在读请求体之前**做鉴权：未登录的请求不该先吃掉 200 MiB 带宽。
	if h.identity == nil {
		writeError(w, http.StatusServiceUnavailable, api.Unavailable, "服务暂时不可用")
		return
	}
	account, err := h.identity.Me(r.Context(), readSessionCookie(r))
	if err != nil {
		h.writeIdentityError(w, err)
		return
	}

	// 2) 体积硬闸：Content-Length 可能缺失或撒谎，这里给请求体套一层硬上限。
	maxBytes := h.maxUploadBytes
	if maxBytes <= 0 {
		maxBytes = 200 << 20
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)

	if err := r.ParseMultipartForm(maxMultipartMemory); err != nil {
		// MaxBytesReader 触发时返回的是 *http.MaxBytesError。
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, api.PayloadTooLarge, "作品包超过体积上限")
			return
		}
		writeError(w, http.StatusBadRequest, api.InvalidParam, "请求格式不对（需要 multipart/form-data）")
		return
	}
	defer func() {
		//nolint:errcheck // 清理临时文件失败不该影响响应
		_ = r.MultipartForm.RemoveAll()
	}()

	title := strings.TrimSpace(r.FormValue("title"))
	description := strings.TrimSpace(r.FormValue("description"))
	demoType := strings.TrimSpace(r.FormValue("demoType"))
	// modelSlugs 是重复字段（多选），且**可以不出现**——空选择在浏览器侧不产生字段。
	// 用 MultipartForm.Value 而不是 FormValue：后者只返回第一个值，
	// 会把「选了三个模型」悄悄变成「只声明了一个」。
	modelIDs := formValues(r, "modelSlugs")
	classificationSlugs := formValues(r, "classificationSlugs")

	file, header, err := r.FormFile("package")
	if err != nil {
		writeError(w, http.StatusBadRequest, api.InvalidParam, "缺少作品文件 package")
		return
	}
	defer func() { _ = file.Close() }()

	// 3) 把上传包落到本机临时文件。domain 需要随机读、多次读，reader 做不到。
	//    边写边数：不信 Content-Length，也不信 multipart 头部声明的长度。
	pkgPath, pkgSize, err := spillToTemp(file, maxBytes)
	if err != nil {
		var tooLarge *spillTooLarge
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, api.PayloadTooLarge, "作品包超过体积上限")
			return
		}
		slog.Error("save upload failed", "err", err)
		writeError(w, http.StatusInternalServerError, api.Unavailable, "服务暂时不可用")
		return
	}
	defer func() { _ = os.Remove(pkgPath) }()

	// header.Filename 只用来取扩展名；不参与任何路径拼接。
	ctx, cancel := context.WithTimeout(r.Context(), demo.SubmitTimeout)
	defer cancel()

	var idemKey string
	if params.IdempotencyKey != nil {
		idemKey = *params.IdempotencyKey
	}

	res, err := h.demos.Submit(ctx, demo.SubmitInput{
		AuthorID:            account.UserID,
		Title:               title,
		Description:         description,
		DemoType:            demoType,
		PackagePath:         pkgPath,
		PackageName:         header.Filename,
		PackageSize:         pkgSize,
		ModelSlugs:          modelIDs,
		ClassificationSlugs: classificationSlugs,
		IdempotencyKey:      idemKey,
	})
	if err != nil {
		h.writeDemoError(w, err)
		return
	}

	// 201 与 200 的回执体一模一样，区别只在「这次是不是新建的」。
	// 脚本靠它判断要不要重新上传、要不要记一条新的提交日志——
	// 把重放也报成 201，等于告诉调用方「我刚才又建了一条重复作品」。
	status := http.StatusCreated
	if res.Replayed {
		status = http.StatusOK
	}
	writeJSON(w, status, api.SubmissionResult{
		Slug:            res.Slug,
		RevisionNo:      res.RevisionNo,
		ModerationState: api.SubmissionResultModerationState(res.ModerationState),
		LifecycleState:  api.LifecycleState(res.LifecycleState),
	})
}

// spillToTemp 把上传内容写进临时文件，返回路径与字节数。
//
// 用临时文件而不是内存：200 MiB 读进内存就是 200 MiB 常驻，几个并发就 OOM。
func spillToTemp(src io.Reader, limit int64) (string, int64, error) {
	f, err := os.CreateTemp("", "demo-upload-*")
	if err != nil {
		return "", 0, err
	}
	path := f.Name()

	// 多读一个字节：读到 1 说明超限，此时内容已经写出去 limit 个字节，直接放弃。
	n, copyErr := io.Copy(f, io.LimitReader(src, limit+1))
	closeErr := f.Close()
	if copyErr != nil {
		_ = os.Remove(path)
		return "", 0, copyErr
	}
	if closeErr != nil {
		_ = os.Remove(path)
		return "", 0, closeErr
	}
	if n > limit {
		_ = os.Remove(path)
		return "", 0, &spillTooLarge{limit: limit}
	}
	return path, n, nil
}

type spillTooLarge struct{ limit int64 }

func (e *spillTooLarge) Error() string { return "作品包超过体积上限" }

// writeDemoError 把领域错误映射成状态码。
func (h *Handler) writeDemoError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, demo.ErrInvalidInput):
		writeError(w, http.StatusBadRequest, api.InvalidParam, messageOf(err))
	case errors.Is(err, demo.ErrPackageTooLarge):
		writeError(w, http.StatusRequestEntityTooLarge, api.PayloadTooLarge, messageOf(err))
	case errors.Is(err, demo.ErrUnsupportedPackage):
		writeError(w, http.StatusUnsupportedMediaType, api.InvalidParam, messageOf(err))
	case errors.Is(err, demo.ErrBadPackage):
		writeError(w, http.StatusUnprocessableEntity, api.InvalidParam, messageOf(err))
	case errors.Is(err, demo.ErrNotFound):
		writeError(w, http.StatusNotFound, api.NotFound, messageOf(err))
	case errors.Is(err, demo.ErrNotEditable):
		// 409 而不是 403：作品的归属没问题，是**状态**不允许改。
		// 「被平台移除后作者不能自行改回」是一条只有管理员能走的边，
		// 说清是状态问题，作者才知道该去申诉而不是重试。
		writeError(w, http.StatusConflict, api.Conflict, messageOf(err))
	case errors.Is(err, demo.ErrLifecycleConflict):
		// 409：归属没问题，是作品当前状态不接受这个动作（已经下架、本来没删……）。
		writeError(w, http.StatusConflict, api.Conflict, messageOf(err))
	case errors.Is(err, demo.ErrRestoreWindowClosed):
		// 409 + 专用码：用户需要知道「不是权限问题，是来晚了」。
		writeError(w, http.StatusConflict, api.RestoreWindowClosed, messageOf(err))
	case errors.Is(err, demo.ErrDerivativePurged):
		// 与「窗口过了」分开：这是系统提前清掉了文件，用户无过错。
		writeError(w, http.StatusConflict, api.DerivativePurged, messageOf(err))
	case errors.Is(err, demo.ErrRevisionNotFound):
		// 404：这个作品没有这个版本号，或者那一版已经擦掉了。
		// 不区分两者——擦除过的版本对外就是不存在，区分会透出「这里曾经有过一版」。
		writeError(w, http.StatusNotFound, api.NotFound, messageOf(err))
	case errors.Is(err, demo.ErrTooManyPending):
		// 429 而不是 400：这不是「你写错了」，是「现在不行，等一会儿」。
		// 契约里 429 已经存在（登录限流用同一个码）。
		//
		// Retry-After 是**建议的复查间隔，不是承诺**：真正解锁的时刻是有人审完
		// 压着的那几条，服务端不知道那会是什么时候。没有这个头，脚本只能
		// 「立刻重试」（打成循环）或「直接放弃」；有了它至少能退避。
		// 登录限流（identity 里的 429）早就在带这个头，两处口径到这才一致。
		w.Header().Set("Retry-After", strconv.Itoa(int(demo.PendingRetryHint.Seconds())))
		writeError(w, http.StatusTooManyRequests, api.RateLimited, messageOf(err))
	default:
		slog.Error("submit demo failed", "err", err)
		writeError(w, http.StatusServiceUnavailable, api.Unavailable, "服务暂时不可用")
	}
}
