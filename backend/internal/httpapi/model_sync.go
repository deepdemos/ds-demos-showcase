package httpapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/api"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/model"
)

// syncTimeout 是手动同步的上限：拉包 60 秒 + 写入几秒，留一倍余量。
const syncTimeout = 2 * time.Minute

// SyncModelCatalog 立即同步一次上游目录镜像。
//
// 与定时同步共用 `model.Service.Sync`：两条触发路径必须有同一套保护（抢锁、
// payload 未变不重写、失败保留旧镜像）。在这里再写一份「先删后插」，
// 两处一定会慢慢长歪。
func (h *Handler) SyncModelCatalog(w http.ResponseWriter, r *http.Request) {
	if h.queries == nil || h.models == nil {
		writeError(w, http.StatusServiceUnavailable, api.Unavailable, "服务暂时不可用")
		return
	}
	if _, err := h.modelImporter(r.Context(), r); err != nil {
		h.writeModelError(w, err)
		return
	}

	// 同步要拉 6 MB 再写 9000 行，5 秒的查询上限不够；但也不能没有上限，
	// 否则上游挂住时这个 handler 会一直占着一个连接。
	ctx, cancel := context.WithTimeout(r.Context(), syncTimeout)
	defer cancel()

	res, err := h.models.Sync(ctx)
	if err != nil {
		switch {
		case errors.Is(err, model.ErrSyncBusy):
			// 409 而不是 503：这不是「服务坏了」，是「你要的事已经有人在做了」。
			// 排队等也没有意义——等到的下一轮已经过时。
			writeError(w, http.StatusConflict, api.Conflict, "另一个同步正在进行，稍后再试")
		default:
			// 502 而不是 500：失败几乎都来自上游（不可达、返回非 JSON、字段形状变了），
			// 而本地镜像保持原样。用 500 会让运维先怀疑自己的库。
			writeError(w, http.StatusBadGateway, api.Unavailable, "同步上游目录失败："+err.Error())
		}
		return
	}

	out := api.ModelSyncResult{
		Source:   catalogSource,
		Status:   model.StatusOK,
		Items:    res.Items,
		Skipped:  res.Skipped,
		SyncedAt: res.At,
	}
	writeJSON(w, http.StatusOK, out)
}

// catalogSource 与 `catalog.Source` 一致。
//
// 单独抄一份常量是为了让 httpapi 不必 import platform/catalog 只为拿一个字符串；
// `scripts/check-single-source.mjs` 不管这种常量，所以两边取值由测试核对
// （`TestCatalogSourceMatchesPlatform`）。
const catalogSource = "models.dev"
