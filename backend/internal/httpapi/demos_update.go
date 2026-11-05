package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/api"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/demo"
)

// UpdateDemo 处理「更新作品」。
//
// 与投稿共用同一套 multipart 手写解析（oapi-codegen 不为 multipart 生成绑定器），
// 差别只有一处：`package` 是**可选**的，不传表示只改标题与简介。
func (h *Handler) UpdateDemo(w http.ResponseWriter, r *http.Request, slug string) {
	if h.demos == nil || h.identity == nil {
		writeError(w, http.StatusServiceUnavailable, api.Unavailable, "服务暂时不可用")
		return
	}

	// 先认人再读请求体：未登录的请求不该先吃掉 200 MiB 带宽。
	account, err := h.identity.Me(r.Context(), readSessionCookie(r))
	if err != nil {
		h.writeIdentityError(w, err)
		return
	}

	maxBytes := h.maxUploadBytes
	if maxBytes <= 0 {
		maxBytes = 200 << 20
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)

	if err := r.ParseMultipartForm(maxMultipartMemory); err != nil {
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

	in := demo.UpdateInput{
		AuthorID:    account.UserID,
		Slug:        slug,
		Title:       strings.TrimSpace(r.FormValue("title")),
		Description: strings.TrimSpace(r.FormValue("description")),
		DemoType:    strings.TrimSpace(r.FormValue("demoType")),
	}

	// package 可选：http.ErrMissingFile 表示这次只改文字。
	file, header, err := r.FormFile("package")
	switch {
	case err == nil:
		defer func() { _ = file.Close() }()
		pkgPath, pkgSize, spillErr := spillToTemp(file, maxBytes)
		if spillErr != nil {
			var tooLarge *spillTooLarge
			if errors.As(spillErr, &tooLarge) {
				writeError(w, http.StatusRequestEntityTooLarge, api.PayloadTooLarge, "作品包超过体积上限")
				return
			}
			slog.Error("save upload failed", "err", spillErr)
			writeError(w, http.StatusInternalServerError, api.Unavailable, "服务暂时不可用")
			return
		}
		defer func() { _ = os.Remove(pkgPath) }()
		in.PackagePath = pkgPath
		in.PackageName = header.Filename
		in.PackageSize = pkgSize
	case errors.Is(err, http.ErrMissingFile):
		// 只改文字。
	default:
		// 文件字段存在但读不出来（超限、multipart 坏了）。
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, api.PayloadTooLarge, "作品包超过体积上限")
			return
		}
		writeError(w, http.StatusBadRequest, api.InvalidParam, "作品文件读取失败")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), demo.SubmitTimeout)
	defer cancel()

	res, err := h.demos.Update(ctx, in)
	if err != nil {
		h.writeDemoError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, api.SubmissionResult{
		Slug:            res.Slug,
		RevisionNo:      res.RevisionNo,
		ModerationState: api.SubmissionResultModerationState(res.ModerationState),
		LifecycleState:  api.LifecycleState(res.LifecycleState),
	})
}
