package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/api"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/model"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

// 厂商实体（模型域 §3.4 第 6 步 / §4.7.3）。
//
// # 为什么厂商要实体化，而不是继续用一个文本框
//
// 自由文本让同一家厂商出现 `DeepSeek` / `deepseek` / `深度求索` 三种写法，
// 而「官方 provider 判定」（`provider.vendor_id == model.vendor_id`）这类判断
// 靠字符串比对必然出错。所以厂商是一张表，模型挂 `vendor_id`。
//
// # 为什么过渡期还要双写文本列
//
// `models.vendor` 已经有历史行（导入建的那些只写了文本）。读路径因此是
// `COALESCE(vendors.name, models.vendor, '')` —— 实体在就取实体，不在就退回文本。
// 写入方必须**两边同值**：文本列是展示缓存，缓存与实体不一致时它就不是缓存了，
// 而是第二份事实源（登记在 docs/已知松点.md「厂商文本列的双写过渡」）。
//
// # 什么是「就地新建」
//
// 下拉里没有这家时，只填一个名称就把它建出来（`source='manual'`，slug 按名字折）。
// 这一版没有厂商的改名 / 合并 / 删除入口：那些动作要有治理面板，
// 而厂商现在的用处只有「这条模型属于谁」。

var (
	// errVendorAmbiguous：`vendorId` 与 `vendor` 同时给。二选一——同时说「选这家」
	// 和「建一家叫这个的」，猜错哪一个都会写进共享目录。
	errVendorAmbiguous = errors.New("vendorId 与 vendor 只能给一个")
	// errVendorNotFound：`vendorId` 指向不存在的厂商。回 400 而不是 404：
	// 这是「新建模型」的请求体，形状错了不该走得更远。
	errVendorNotFound = errors.New("厂商不存在")
)

// vendorSlugConflict 是「两家厂商名折出同一个 slug」。
//
// 与 manualModelConflict 分开：撞的是**厂商**，不是模型。所以 409 的响应体不带
// `existingSlug`——那个字段指的是已有那条模型，带上会把界面带到一条不相干的模型页。
type vendorSlugConflict struct {
	slug         string
	existingName string
}

func (e *vendorSlugConflict) Error() string {
	return "厂商 slug 已被占用：" + e.slug
}

// writeVendorConflict 写 409：要求改名，不自动加后缀。
//
// 自动后缀（`deepseek-2`）会造出一个永久难看的厂商键，而撞车多半说明「这就是同一家，
// 直接选它就行」——所以文案里给出两条出路：改名，或者选已有那家。
func writeVendorConflict(w http.ResponseWriter, conflict *vendorSlugConflict) {
	message := "厂商名折出的 slug「" + conflict.slug + "」已经被「" + conflict.existingName +
		"」占用，请换个厂商名，或直接选那一家"
	if conflict.existingName == "" {
		// 并发建同一家：另一个请求刚插进去。此时查不到名字（事务已中止回读），
		// 但出路仍然一样。
		message = "厂商名折出的 slug「" + conflict.slug + "」刚被别人占用，刷新下拉选那一家，或换个厂商名"
	}
	writeJSON(w, http.StatusConflict, api.Error{Code: api.Conflict, Message: message})
}

// modelVendorRef 是「这条模型挂哪家厂商」的判定结果。
type modelVendorRef struct {
	ID   pgtype.Int8
	Name string
}

// column 是写进过渡期文本列的值：没挂厂商时是 NULL，不是空串。
//
// 空串会让读路径看起来像「有值但是空的」——界面就分不出「未标注」与「填了个空白」。
func (v modelVendorRef) column() pgtype.Text {
	if v.ID.Valid && v.Name != "" {
		return pgtype.Text{String: v.Name, Valid: true}
	}
	return pgtype.Text{}
}

// resolveModelVendor 决定这条模型挂哪家厂商，判定只在这里写一遍。
//
// 三条路：
//
//  1. `vendorId`：选下拉里已有的一家（取不到 → 400）；
//  2. `vendor`：就地新建。名字折出的 slug 已被**另一家**占用 → 409 要求改名；
//     名字逐字相同 → 复用那一家（同一个厂商被两个人先后建不该互相报错）；
//  3. 两个都不给：厂商未标注（`vendor_id` 与文本列都留 NULL）。
//
// 必须在调用方的**事务内**执行：新建的厂商行要与模型行同生共死，否则会出现
// 「厂商建出来了但模型撞名回滚」这种没有人指向的孤儿厂商。
func (h *Handler) resolveModelVendor(
	ctx context.Context,
	q *store.Queries,
	body api.ModelManualCreate,
) (modelVendorRef, error) {
	hasID := body.VendorId != nil && *body.VendorId > 0
	name := strings.TrimSpace(derefString(body.Vendor))

	switch {
	case hasID && name != "":
		return modelVendorRef{}, errVendorAmbiguous
	case hasID:
		vendor, err := q.GetVendorByID(ctx, int64(*body.VendorId))
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return modelVendorRef{}, errVendorNotFound
			}
			return modelVendorRef{}, err
		}
		return modelVendorRef{ID: pgtype.Int8{Int64: vendor.ID, Valid: true}, Name: vendor.Name}, nil
	case name == "":
		return modelVendorRef{}, nil
	}

	// slug 由名称折出——与模型 slug 同一个纯函数。折不出 ASCII（纯中文名）时退名字
	// 摘要，与模型那条规则同构：同一个名字永远得到同一个值，不掺随机也不掺时间。
	slug := model.Slug(name, "")
	existing, err := q.FindVendorBySlug(ctx, slug)
	switch {
	case err == nil:
		if existing.Name == name {
			return modelVendorRef{ID: pgtype.Int8{Int64: existing.ID, Valid: true}, Name: existing.Name}, nil
		}
		return modelVendorRef{}, &vendorSlugConflict{slug: slug, existingName: existing.Name}
	case !errors.Is(err, pgx.ErrNoRows):
		return modelVendorRef{}, err
	}

	created, err := q.InsertVendor(ctx, store.InsertVendorParams{Slug: slug, Name: name})
	if err != nil {
		// 竞态：另一个管理员刚建了折出同一 slug 的厂商。事务已中止，回读拿不到名字；
		// 从这里返回会让整个事务回滚（不留下半截模型）。
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" && strings.Contains(pgErr.ConstraintName, "vendors_slug_key") {
			return modelVendorRef{}, &vendorSlugConflict{slug: slug}
		}
		return modelVendorRef{}, err
	}
	return modelVendorRef{ID: pgtype.Int8{Int64: created.ID, Valid: true}, Name: created.Name}, nil
}

// ListModelVendors 对应 GET /api/v1/admin/models/vendors。
//
// 只服务手动新建的下拉，所以鉴权与新建同一个键（`model.create`）：没有新建权限的人
// 拿这份列表也没有可做的事。三列够用——id 是提交键、name 是显示名、slug 是稳定键。
func (h *Handler) ListModelVendors(w http.ResponseWriter, r *http.Request) {
	if h.queries == nil {
		unavailable(w, errors.New("database not configured"))
		return
	}
	if _, err := h.requirePermission(r.Context(), r, permModelCreate); err != nil {
		h.writePermissionError(w, err, permModelCreate)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), queryTimeout)
	defer cancel()

	rows, err := h.queries.ListVendors(ctx)
	if err != nil {
		unavailable(w, err)
		return
	}
	items := make([]api.ModelVendor, 0, len(rows))
	for _, v := range rows {
		items = append(items, api.ModelVendor{Id: int(v.ID), Slug: v.Slug, Name: v.Name})
	}
	// 一家都没有时回空数组而不是 null：调用方拿到 null 会走「读不到」分支，
	// 而空下拉是一个正常状态（第一家公司就是在这里建出来的）。
	writeJSON(w, http.StatusOK, api.ModelVendorList{Items: items})
}
