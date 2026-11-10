package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/api"
)

// ListDemoTypes 给出投稿可选的类型，连同每个类型允许的上传形态。
//
// 为什么由服务端给、而不是前端写死：在此之前投稿页把两个类型硬编码在页面里，
// 而库里其实有四行——多出来的两种没人知道，也就没人能选。类型是主数据，
// 界面只是它的一个视图。
//
// 空 `formats` 是有意义的：`link` 与 `image` 还没有上传路径，它们的清单就是空的。
// 前端看到空数组应当把这一项标成不可选，而不是当成「任意都收」。
func (h *Handler) ListDemoTypes(w http.ResponseWriter, r *http.Request) {
	if h.queries == nil {
		unavailable(w, errors.New("database not configured"))
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), queryTimeout)
	defer cancel()

	rows, err := h.queries.ListDemoTypes(ctx)
	if err != nil {
		unavailable(w, err)
		return
	}

	items := make([]api.DemoType, 0, len(rows))
	for _, row := range rows {
		formats := row.Formats
		if formats == nil {
			// nil 会被编成 null，而契约说它是数组。空清单与「没有这个字段」
			// 在界面上的处理完全不同，不能混。
			formats = []string{}
		}
		items = append(items, api.DemoType{Code: row.Code, Name: row.Name, Formats: formats})
	}
	writeJSON(w, http.StatusOK, api.DemoTypeList{Items: items})
}