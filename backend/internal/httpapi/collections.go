package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/api"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

// 收藏域传输层（#438 / COL-001 – COL-014）。
//
// 权限与可见性的三种落态（设计见 docs/收藏域设计.md §8）：
//   - 私有夹读不到 / 不存在 → 404（不承认存在）
//   - 猜别人的夹 id 写 → 403
//   - 默认夹改名 / 删除 / 转公开 → 422
//
// 上限不写死：collection.max_per_account / collection.max_items 从设置域读，
// 键缺行时兜默认值（那是数据事故，不该让收藏功能整条不可用）。

const (
	collectionMaxPerAccountFallback = 20
	collectionMaxItemsFallback      = 500
	collectionNameMax               = 50
	collectionDescriptionMax        = 500
)

func (h *Handler) collectionLimits(ctx context.Context) (maxCollections, maxItems int64) {
	read := func(key string, fallback int64) int64 {
		raw, err := h.queries.GetSiteSettingNumber(ctx, key)
		if err != nil {
			return fallback
		}
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || value <= 0 {
			return fallback
		}
		return value
	}
	return read("collection.max_per_account", collectionMaxPerAccountFallback),
		read("collection.max_items", collectionMaxItemsFallback)
}

func pgtypeText(value string) pgtype.Text {
	return pgtype.Text{String: value, Valid: true}
}

func collectionView(id int64, name, description string, isDefault bool, visibility string, createdAt, updatedAt time.Time, itemCount int) api.Collection {
	return api.Collection{
		Id:          int(id),
		Name:        name,
		Description: description,
		IsDefault:   isDefault,
		Visibility:  api.CollectionVisibility(visibility),
		ItemCount:   itemCount,
		CreatedAt:   createdAt,
		UpdatedAt:   updatedAt,
	}
}

func (h *Handler) collectionItemCount(ctx context.Context, id int64) int {
	count, err := h.queries.GetCollectionItemCount(ctx, id)
	if err != nil {
		// 计数是附加信息：读失败按 0 返回，不让详情整页 503。
		slog.Warn("读收藏夹条目数失败", "collection", id, "err", err)
		return 0
	}
	return int(count)
}

// getCollectionOwned 取夹并判「存在 / 主人」，不限制默认夹。
//
// 加/移作品对默认夹是合法的（COL-001 的「收藏」按钮就是往默认夹里加），
// 只有改名/删除/转公开才需要「非默认夹」那道闸。
func (h *Handler) getCollectionOwned(w http.ResponseWriter, ctx context.Context, id, accountID int64) (store.Collection, bool) {
	row, err := h.queries.GetCollectionByID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, api.NotFound, "收藏夹不存在")
		return store.Collection{}, false
	}
	if err != nil {
		unavailable(w, err)
		return store.Collection{}, false
	}
	if row.AccountID != accountID {
		// 猜 id 的他人：设计（COL-012）明确要求写请求 403。
		writeError(w, http.StatusForbidden, api.Forbidden, "没有权限修改这个收藏夹")
		return store.Collection{}, false
	}
	return row, true
}

// getCollectionForWrite 在 getCollectionOwned 之上再加「默认夹不可改删」。
func (h *Handler) getCollectionForWrite(w http.ResponseWriter, ctx context.Context, id, accountID int64) (store.Collection, bool) {
	row, ok := h.getCollectionOwned(w, ctx, id, accountID)
	if !ok {
		return store.Collection{}, false
	}
	if row.IsDefault {
		writeError(w, http.StatusUnprocessableEntity, api.InvalidParam, "默认收藏夹不能改名、删除或转公开")
		return store.Collection{}, false
	}
	return row, true
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// ListCollections 对应 GET /api/v1/collections。
func (h *Handler) ListCollections(w http.ResponseWriter, r *http.Request) {
	if h.queries == nil {
		unavailable(w, errors.New("database not configured"))
		return
	}
	account, ctx, cancel, ok := h.requireAccount(w, r)
	if !ok {
		return
	}
	defer cancel()

	// 惰性创建默认夹：第一次打开收藏页也要看到它。
	if _, err := h.queries.EnsureDefaultCollection(ctx, account.ID); err != nil {
		unavailable(w, err)
		return
	}
	rows, err := h.queries.ListCollectionsByAccount(ctx, account.ID)
	if err != nil {
		unavailable(w, err)
		return
	}

	items := make([]api.Collection, 0, len(rows))
	for _, row := range rows {
		items = append(items, collectionView(row.ID, row.Name, row.Description, row.IsDefault,
			row.Visibility, row.CreatedAt.Time, row.UpdatedAt.Time, int(row.ItemCount)))
	}
	writeJSON(w, http.StatusOK, api.CollectionList{Items: items})
}

// CreateCollection 对应 POST /api/v1/collections。
func (h *Handler) CreateCollection(w http.ResponseWriter, r *http.Request) {
	if h.queries == nil {
		unavailable(w, errors.New("database not configured"))
		return
	}
	account, ctx, cancel, ok := h.requireAccount(w, r)
	if !ok {
		return
	}
	defer cancel()

	var body api.CollectionCreate
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, api.InvalidParam, "请求体不是合法的 JSON")
		return
	}
	name := strings.TrimSpace(body.Name)
	if name == "" || len([]rune(name)) > collectionNameMax {
		writeError(w, http.StatusUnprocessableEntity, api.InvalidParam, "名称需要 1–50 个字符")
		return
	}
	description := ""
	if body.Description != nil {
		description = *body.Description
	}
	if len([]rune(description)) > collectionDescriptionMax {
		writeError(w, http.StatusUnprocessableEntity, api.InvalidParam, "说明最多 500 个字符")
		return
	}

	maxCollections, _ := h.collectionLimits(ctx)
	row, err := h.queries.CreateCollection(ctx, store.CreateCollectionParams{
		AccountID:      account.ID,
		Name:           name,
		Description:    description,
		MaxCollections: maxCollections,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusUnprocessableEntity, api.InvalidParam,
			"收藏夹数量已达上限（"+strconv.FormatInt(maxCollections, 10)+" 个）")
		return
	}
	if isUniqueViolation(err) {
		writeError(w, http.StatusUnprocessableEntity, api.InvalidParam, "已经有同名收藏夹")
		return
	}
	if err != nil {
		unavailable(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, collectionView(row.ID, row.Name, row.Description, row.IsDefault,
		row.Visibility, row.CreatedAt.Time, row.UpdatedAt.Time, 0))
}

// GetCollection 对应 GET /api/v1/collections/{id}。
func (h *Handler) GetCollection(w http.ResponseWriter, r *http.Request, id int) {
	if h.queries == nil {
		unavailable(w, errors.New("database not configured"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), queryTimeout)
	defer cancel()

	row, err := h.queries.GetCollectionByID(ctx, int64(id))
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, api.NotFound, "收藏夹不存在")
		return
	}
	if err != nil {
		unavailable(w, err)
		return
	}

	// 私有夹：只有主人可读，其他人连「存在」都不承认。
	if row.Visibility != "public" {
		viewerID := int64(0)
		if h.identity != nil {
			if account, err := h.identity.Me(ctx, readSessionCookie(r)); err == nil {
				viewerID = account.ID
			}
		}
		if viewerID != row.AccountID {
			writeError(w, http.StatusNotFound, api.NotFound, "收藏夹不存在")
			return
		}
	}

	rows, err := h.queries.ListCollectionItems(ctx, row.ID)
	if err != nil {
		unavailable(w, err)
		return
	}
	items := make([]api.CollectionItem, 0, len(rows))
	for _, item := range rows {
		out := api.CollectionItem{
			Slug:       item.Slug,
			AddedAt:    item.AddedAt.Time,
			Accessible: item.Accessible,
		}
		if item.Title.Valid {
			title := item.Title.String
			out.Title = &title
		}
		if item.CoverUrl.Valid {
			cover := item.CoverUrl.String
			out.CoverUrl = &cover
		}
		items = append(items, out)
	}
	writeJSON(w, http.StatusOK, api.CollectionDetail{
		Collection: collectionView(row.ID, row.Name, row.Description, row.IsDefault, row.Visibility,
			row.CreatedAt.Time, row.UpdatedAt.Time, h.collectionItemCount(ctx, row.ID)),
		Items: items,
	})
}

// UpdateCollection 对应 PATCH /api/v1/collections/{id}。
func (h *Handler) UpdateCollection(w http.ResponseWriter, r *http.Request, id int) {
	if h.queries == nil {
		unavailable(w, errors.New("database not configured"))
		return
	}
	account, ctx, cancel, ok := h.requireAccount(w, r)
	if !ok {
		return
	}
	defer cancel()

	var body api.CollectionUpdate
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, api.InvalidParam, "请求体不是合法的 JSON")
		return
	}
	if body.Name == nil && body.Description == nil && body.Visibility == nil {
		writeError(w, http.StatusBadRequest, api.InvalidParam, "至少传一个字段")
		return
	}

	params := store.UpdateCollectionParams{
		ID:        int64(id),
		AccountID: account.ID,
	}
	if body.Name != nil {
		name := strings.TrimSpace(*body.Name)
		if name == "" || len([]rune(name)) > collectionNameMax {
			writeError(w, http.StatusUnprocessableEntity, api.InvalidParam, "名称需要 1–50 个字符")
			return
		}
		params.Name = pgtypeText(name)
	}
	if body.Description != nil {
		if len([]rune(*body.Description)) > collectionDescriptionMax {
			writeError(w, http.StatusUnprocessableEntity, api.InvalidParam, "说明最多 500 个字符")
			return
		}
		params.Description = pgtypeText(*body.Description)
	}
	if body.Visibility != nil {
		switch string(*body.Visibility) {
		case "private", "public":
			params.Visibility = pgtypeText(string(*body.Visibility))
		default:
			writeError(w, http.StatusBadRequest, api.InvalidParam, "visibility 只能是 private 或 public")
			return
		}
	}

	if _, ok := h.getCollectionForWrite(w, ctx, int64(id), account.ID); !ok {
		return
	}
	row, err := h.queries.UpdateCollection(ctx, params)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusUnprocessableEntity, api.InvalidParam, "默认收藏夹不能修改")
		return
	}
	if isUniqueViolation(err) {
		writeError(w, http.StatusUnprocessableEntity, api.InvalidParam, "已经有同名收藏夹")
		return
	}
	if err != nil {
		unavailable(w, err)
		return
	}
	writeJSON(w, http.StatusOK, collectionView(row.ID, row.Name, row.Description, row.IsDefault,
		row.Visibility, row.CreatedAt.Time, row.UpdatedAt.Time, h.collectionItemCount(ctx, row.ID)))
}

// DeleteCollection 对应 DELETE /api/v1/collections/{id}。
func (h *Handler) DeleteCollection(w http.ResponseWriter, r *http.Request, id int) {
	if h.queries == nil {
		unavailable(w, errors.New("database not configured"))
		return
	}
	account, ctx, cancel, ok := h.requireAccount(w, r)
	if !ok {
		return
	}
	defer cancel()

	if _, ok := h.getCollectionForWrite(w, ctx, int64(id), account.ID); !ok {
		return
	}
	rows, err := h.queries.DeleteCollection(ctx, store.DeleteCollectionParams{ID: int64(id), AccountID: account.ID})
	if err != nil {
		unavailable(w, err)
		return
	}
	if rows == 0 {
		// 帮手上一次读到的行在并发里被删了；不是默认夹（那个已在 helper 里挡掉）。
		writeError(w, http.StatusNotFound, api.NotFound, "收藏夹不存在")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// AddCollectionItem 对应 POST /api/v1/collections/{id}/items。
func (h *Handler) AddCollectionItem(w http.ResponseWriter, r *http.Request, id int) {
	if h.queries == nil {
		unavailable(w, errors.New("database not configured"))
		return
	}
	account, ctx, cancel, ok := h.requireAccount(w, r)
	if !ok {
		return
	}
	defer cancel()

	var body api.CollectionItemInput
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, api.InvalidParam, "请求体不是合法的 JSON")
		return
	}
	slug := strings.TrimSpace(body.Slug)
	if slug == "" {
		writeError(w, http.StatusBadRequest, api.InvalidParam, "缺少 slug")
		return
	}

	if _, ok := h.getCollectionOwned(w, ctx, int64(id), account.ID); !ok {
		return
	}
	demoID, err := h.queries.GetPublicDemoIDBySlug(ctx, slug)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusUnprocessableEntity, api.InvalidParam, "只能收藏当前公开的作品")
		return
	}
	if err != nil {
		unavailable(w, err)
		return
	}

	_, maxItems := h.collectionLimits(ctx)
	count, err := h.queries.GetCollectionItemCount(ctx, int64(id))
	if err != nil {
		unavailable(w, err)
		return
	}
	if count >= maxItems {
		writeError(w, http.StatusUnprocessableEntity, api.InvalidParam,
			"这个收藏夹已满（"+strconv.FormatInt(maxItems, 10)+" 件）")
		return
	}
	if _, err := h.queries.AddCollectionItem(ctx, store.AddCollectionItemParams{
		CollectionID: int64(id),
		DemoID:       demoID,
		MaxItems:     maxItems,
	}); err != nil {
		unavailable(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// RemoveCollectionItem 对应 DELETE /api/v1/collections/{id}/items/{slug}。幂等。
func (h *Handler) RemoveCollectionItem(w http.ResponseWriter, r *http.Request, id int, slug string) {
	if h.queries == nil {
		unavailable(w, errors.New("database not configured"))
		return
	}
	account, ctx, cancel, ok := h.requireAccount(w, r)
	if !ok {
		return
	}
	defer cancel()

	if _, ok := h.getCollectionOwned(w, ctx, int64(id), account.ID); !ok {
		return
	}
	if _, err := h.queries.RemoveCollectionItem(ctx, store.RemoveCollectionItemParams{
		CollectionID: int64(id),
		AccountID:    account.ID,
		Slug:         slug,
	}); err != nil {
		unavailable(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GetCollectionStatus 对应 GET /api/v1/me/collections/status。
func (h *Handler) GetCollectionStatus(w http.ResponseWriter, r *http.Request, params api.GetCollectionStatusParams) {
	if h.queries == nil {
		unavailable(w, errors.New("database not configured"))
		return
	}
	account, ctx, cancel, ok := h.requireAccount(w, r)
	if !ok {
		return
	}
	defer cancel()

	slug := strings.TrimSpace(params.Slug)
	if slug == "" {
		writeError(w, http.StatusBadRequest, api.InvalidParam, "缺少 slug")
		return
	}
	rows, err := h.queries.CollectionStatusForSlug(ctx, store.CollectionStatusForSlugParams{
		AccountID: account.ID,
		Slug:      slug,
	})
	if err != nil {
		unavailable(w, err)
		return
	}

	inDefault := false
	ids := make([]int, 0, len(rows))
	for _, row := range rows {
		if row.IsDefault {
			inDefault = true
			continue
		}
		ids = append(ids, int(row.ID))
	}
	writeJSON(w, http.StatusOK, api.CollectionStatus{
		Slug:          slug,
		InDefault:     inDefault,
		CollectionIds: ids,
	})
}
