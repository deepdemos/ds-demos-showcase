package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/api"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/demo"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/identity"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

// 模型域写路径的鉴权与「缺权限」出口。
//
// # 为什么收成一处
//
// 权限不是中间件（依赖挂在 Handler 上，路由表里没有鉴权环节），所以每个 handler 自己
// 判一次。判法逐字相同、只有键不同——那是同一段代码的四份副本，迟早出现「一处记着
// 未接线要先回 503，另一处忘了」。这里把「认人 → 查键 → 摊成 actor」收成一个函数。
//
// # 为什么 403 文案由键查表得出，而不是调用点传一句人话
//
// 文案必须跟着键走：**同一个键在任何端点都是同一句话**。让调用点各自传「没有 XX 权限」，
// 新增一个调用点时就会顺手编一句新的——#599 之后 model.govern 在确认 / 下架 / 上架与
// 合并 / 撤销合并上说过两种话（「没有模型治理权限」与「没有合并 / 撤销合并的权限」），
// 而操作者据此去找管理员要权限，说错了比不说更糟。表只有一张，句子只由键决定。
//
// # 审核链路（demo.review / proposal.review）为什么不并进来
//
// 那两个键的 403 **有意不点名**缺哪个键（见 writeReviewError 的注释），而且那个 mapper
// 还承担领域错误（作品不存在 / 状态不对 / 输入不合法）的映射。不同判断、不同出口，
// 并进来只会让两边的取舍互相拉扯。

const (
	// permModelImport 管「把上游目录里的条目搬进来」（导入 / 预览 diff / 同步镜像）。
	permModelImport = "model.import"
	// permModelCreate 管「认定这条模型存在」（手动新建，依据是管理员的判断）。
	permModelCreate = "model.create"
	// permModelGovern 管「本站为这条模型背书」（确认 / 下架 / 上架 / 合并 / 撤销合并 / 驳回候选）。
	permModelGovern = "model.govern"
)

// permissionAction 是权限键在 403 文案里的说法（「没有{它}权限」）。
//
// 写成表而不是由调用点传字符串：句子只由键决定，调用点就没有「顺手编一句」的机会。
var permissionAction = map[string]string{
	permModelImport: "模型导入",
	permModelCreate: "新建模型",
	permModelGovern: "模型治理",
}

// requirePermission 是模型域写路径唯一的鉴权点：认人 → 查键 → 摊成 actor。
//
// 键的字面量与 db/schema 的 permissions 种子行逐字一致（迁移 0043 / 0044 加的键也在这里）。
func (h *Handler) requirePermission(ctx context.Context, r *http.Request, key string) (demo.Actor, error) {
	if h.identity == nil || h.queries == nil {
		return demo.Actor{}, errNotWired
	}
	account, err := h.identity.Me(ctx, readSessionCookie(r))
	if err != nil {
		return demo.Actor{}, err
	}
	ok, err := h.queries.HasPermission(ctx, store.HasPermissionParams{
		Sub:           account.Sub,
		PermissionKey: key,
	})
	if err != nil {
		return demo.Actor{}, err
	}
	if !ok {
		return demo.Actor{}, errForbidden
	}
	return demo.Actor{
		AccountID: account.ID,
		UserID:    account.UserID,
		Role:      account.Role,
	}, nil
}

// writePermissionError 把 requirePermission 的错误摊成响应。
//
// 401 / 403 / 503 三档与 writeReviewError 同一口径：未登录是「你还没说你是谁」，
// 没权限是「我知道你是谁，但你不行」，依赖没接线是「服务坏了」。混成一个值会让前端
// 不知道该跳登录页还是该提示无权，用户会在两个页面之间来回弹。
func (h *Handler) writePermissionError(w http.ResponseWriter, err error, key string) {
	switch {
	case errors.Is(err, identity.ErrUnauthorized):
		writeError(w, http.StatusUnauthorized, api.Unauthorized, "请先登录")
	case errors.Is(err, errForbidden):
		writeError(w, http.StatusForbidden, api.Forbidden, forbiddenMessage(key))
	case errors.Is(err, errNotWired):
		writeError(w, http.StatusServiceUnavailable, api.Unavailable, "服务暂时不可用")
	default:
		unavailable(w, err)
	}
}

// forbiddenMessage 是「缺哪把键」的 403 文案：同一个键永远同一句话。
func forbiddenMessage(key string) string {
	action := permissionAction[key]
	if action == "" {
		// 表里没有的键（新增键时忘了登记）：直接把键名念出来。
		// 编一句猜的中文会让「我该申请哪个键」这件事彻底失传，而这句话正是操作者
		// 找管理员时唯一的线索。
		return "没有 " + key + " 权限"
	}
	return "没有" + action + "权限"
}
