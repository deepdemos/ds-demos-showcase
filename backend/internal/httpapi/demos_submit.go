package httpapi

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
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
func (h *Handler) SubmitDemo(w http.ResponseWriter, r *http.Request) {
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
	_ = header

	ctx, cancel := context.WithTimeout(r.Context(), demo.SubmitTimeout)
	defer cancel()

	res, err := h.demos.Submit(ctx, demo.SubmitInput{
		AuthorID:    account.UserID,
		Title:       title,
		Description: description,
		DemoType:    demoType,
		PackagePath: pkgPath,
		PackageSize: pkgSize,
	})
	if err != nil {
		h.writeDemoError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, api.SubmissionResult{
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
	case errors.Is(err, demo.ErrTooManyPending):
		// 429 而不是 400：这不是「你写错了」，是「现在不行，等一会儿」。
		// 契约里 429 已经存在（登录限流用同一个码）。
		writeError(w, http.StatusTooManyRequests, api.RateLimited, messageOf(err))
	default:
		slog.Error("submit demo failed", "err", err)
		writeError(w, http.StatusServiceUnavailable, api.Unavailable, "服务暂时不可用")
	}
}
