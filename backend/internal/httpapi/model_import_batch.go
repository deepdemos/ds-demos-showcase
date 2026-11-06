package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/api"
)

// maxBatchImportItems 是一次批量导入的条数上限。
//
// 50 不是一个技术常数，是一个**人**的常数：一次点五十条已经超出逐条审阅的可能，
// 再多就变成「全选然后祈祷」。上限与契约里的 `maxItems: 50` 必须一致——
// 契约先拒掉超限请求，这里只是二次校验（别让绕过契約的调用方打进来）。
const maxBatchImportItems = 50

// ApplyModelImportBatch 是 apply 的批量版：每条一个事务，逐条返回结果。
//
// # 为什么不做整批回滚
//
// 第一次导入就是几十条起步。整批回滚意味着第 17 条撞「已被认领」时前 16 条白做，
// 管理员得重新勾一遍；而「逐条结果」让他只重试 `failed` 那几条。
// 代价写清楚：这一批不是原子的，`results` 里 `failed` 的那些确实没落库。
//
// # 为什么形态错误整批 400
//
// 批内重复、未知字段名、某条空勾选——这些是**请求形态**问题，不是某条数据的运气
// 问题。让它们逐条失败会得到「17 条成功、1 条因为拼错了字段名失败」这种半成品，
// 而管理员真正需要的是「请求写错了，改完再发」。
func (h *Handler) ApplyModelImportBatch(w http.ResponseWriter, r *http.Request) {
	if h.queries == nil || h.demos == nil {
		writeError(w, http.StatusServiceUnavailable, api.Unavailable, "服务暂时不可用")
		return
	}
	actor, err := h.modelImporter(r.Context(), r)
	if err != nil {
		h.writeModelError(w, err)
		return
	}

	var body api.ModelImportBatchRequest
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, api.InvalidParam, "请求体不是合法的 JSON")
		return
	}
	if len(body.Items) == 0 {
		writeError(w, http.StatusBadRequest, api.InvalidParam, "items 不能为空")
		return
	}
	if len(body.Items) > maxBatchImportItems {
		writeError(w, http.StatusBadRequest, api.InvalidParam,
			fmt.Sprintf("一次最多导入 %d 条，这次是 %d 条", maxBatchImportItems, len(body.Items)))
		return
	}

	// 先把所有条目的形态校验完，再动手写——避免「前 3 条已落库，第 4 条字段名拼错」。
	fields := make([][]string, len(body.Items))
	refs := make([]catalogRef, len(body.Items))
	seen := make(map[string]int, len(body.Items))
	for i, item := range body.Items {
		key := item.ProviderId + "\x00" + item.ExternalId
		if prev, dup := seen[key]; dup {
			writeError(w, http.StatusBadRequest, api.InvalidParam,
				fmt.Sprintf("第 %d 条与第 %d 条是同一个上游条目（%s / %s）",
					prev+1, i+1, item.ProviderId, item.ExternalId))
			return
		}
		seen[key] = i

		cleaned, err := normalizeSpecFields(item.Fields)
		if err != nil {
			h.writeModelError(w, err)
			return
		}
		fields[i] = cleaned

		ref := catalogRef{
			Source:     sourceOrDefault(item.Source),
			ProviderID: item.ProviderId,
			ExternalID: item.ExternalId,
		}
		if item.CanonicalId != nil {
			ref.CanonicalID = *item.CanonicalId
		}
		if _, err := ref.params(); err != nil {
			writeError(w, http.StatusBadRequest, api.InvalidParam,
				fmt.Sprintf("第 %d 条：%s", i+1, err.Error()))
			return
		}
		refs[i] = ref
	}

	ctx, cancel := context.WithTimeout(r.Context(), batchImportTimeout)
	defer cancel()

	results := make([]api.ModelImportBatchResultItem, 0, len(body.Items))
	for i, item := range body.Items {
		out := api.ModelImportBatchResultItem{
			ProviderId: item.ProviderId,
			ExternalId: item.ExternalId,
		}
		model, created, err := h.applyImport(ctx, actor, refs[i], item, fields[i])
		switch {
		case err != nil:
			// 逐条失败不回滚已成功的：这正是批量导入的语义。
			out.Status = "failed"
			msg := modelErrorText(err)
			out.Error = &msg
		case created:
			out.Status = "created"
			out.Model = &model
		default:
			out.Status = "completed"
			out.Model = &model
		}
		results = append(results, out)
	}

	writeJSON(w, http.StatusOK, api.ModelImportBatchResult{Results: results})
}

// batchImportTimeout 是该请求的总预算：50 条各自一个事务，每条几毫秒，
// 两分钟足够跑完；再长就该让网关先超时了（那时调用方拿到的是网关错误，
// 分不清是「没跑」还是「跑了一半」，比 504 更难查）。
const batchImportTimeout = 2 * time.Minute
