package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/api"
)

// ListModels 返回投稿表单要用的模型主数据。
//
// 只出 `status = active` 的行：`candidate` / `unverified` 还没被确认，
// 不该被作者当事实声明；`deprecated` 是已退役，新作品不该再选。
//
// 不分页：模型主数据是受控的少量行（外部目录导入的一部分），不是用户内容。
// 到几百行的规模时它会先需要一个管理界面，那时再谈分页。
func (h *Handler) ListModels(w http.ResponseWriter, r *http.Request) {
	if h.queries == nil {
		unavailable(w, errors.New("database not configured"))
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), queryTimeout)
	defer cancel()

	rows, err := h.queries.ListActiveModels(ctx)
	if err != nil {
		unavailable(w, err)
		return
	}

	items := make([]api.Model, 0, len(rows))
	for _, m := range rows {
		items = append(items, api.Model{
			Slug:   m.Slug,
			Name:   m.Name,
			Vendor: m.Vendor,
		})
	}
	writeJSON(w, http.StatusOK, api.ModelList{Items: items})
}
