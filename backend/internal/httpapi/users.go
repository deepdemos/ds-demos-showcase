package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/api"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

// 公开作者页的传输层（#425）。
//
// # 为什么这两个端点不需要登录
//
// 作者资料与公开作品本来就是访客可见的内容；要求登录才能看作者页，等于把
// 「按作者浏览」锁在账号后面，而需求（SOC-001 / DEMO-043）说的是公开资料。
//
// # 404 的三种情况为什么是同一句话
//
// 用户名不存在、账号不是 active、用户名格式不合法——对外都回「作者不存在」。
// 分开说会变成账号状态探测器：试一个用户名，就能知道它曾经注册过、现在被封了。
//
// # 为什么作品计数不在这个文件里重算
//
// 公开性判据只有一处（demo_public_demos 视图）。这里的 SQL 只 join 视图，
// 不抄「approved + published + 有 listed 渠道」那串条件。

const userPageSizeDefault = 12
const userPageSizeMax = 50

// normalizeUsername 做与 profiles.username CHECK 相同的形状校验。
//
// 契约里写了 pattern，但生成的绑定层不校验它；这里显式判一次，避免把
// 任意字符串送进 SQL。形状不合法不可能命中任何作者，按 404 处理。
func normalizeUsername(raw string) (string, bool) {
	username := strings.TrimSpace(raw)
	if len(username) < 3 || len(username) > 20 {
		return "", false
	}
	for _, r := range username {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '_' || r == '-':
		default:
			return "", false
		}
	}
	return username, true
}

func writeAuthorNotFound(w http.ResponseWriter) {
	writeError(w, http.StatusNotFound, api.NotFound, "作者不存在")
}

// GetUserProfile 对应 GET /api/v1/users/{username}。
func (h *Handler) GetUserProfile(w http.ResponseWriter, r *http.Request, username string) {
	if h.queries == nil {
		unavailable(w, errors.New("database not configured"))
		return
	}
	name, ok := normalizeUsername(username)
	if !ok {
		writeAuthorNotFound(w)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), queryTimeout)
	defer cancel()

	row, err := h.queries.GetPublicProfileByUsername(ctx, name)
	if errors.Is(err, pgx.ErrNoRows) {
		writeAuthorNotFound(w)
		return
	}
	if err != nil {
		unavailable(w, err)
		return
	}

	profile := api.PublicProfile{
		Username:    row.Username,
		DisplayName: row.DisplayName,
		Bio:         row.Bio,
		JoinedAt:    row.JoinedAt.Time,
		DemoCount:   int(row.DemoCount),
	}
	if row.AvatarUrl.Valid {
		avatar := row.AvatarUrl.String
		profile.AvatarUrl = &avatar
	}
	writeJSON(w, http.StatusOK, profile)
}

// ListUserDemos 对应 GET /api/v1/users/{username}/demos。
func (h *Handler) ListUserDemos(w http.ResponseWriter, r *http.Request, username string, params api.ListUserDemosParams) {
	if h.queries == nil {
		unavailable(w, errors.New("database not configured"))
		return
	}
	name, ok := normalizeUsername(username)
	if !ok {
		writeAuthorNotFound(w)
		return
	}

	page := 1
	if params.Page != nil {
		page = *params.Page
	}
	pageSize := userPageSizeDefault
	if params.PageSize != nil {
		pageSize = *params.PageSize
	}
	if page < 1 || pageSize < 1 || pageSize > userPageSizeMax {
		writeError(w, http.StatusBadRequest, api.InvalidParam, "page 必须 ≥ 1，pageSize 必须在 1..50 之间")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), queryTimeout)
	defer cancel()

	// 先确认作者可见：不存在的用户名回 404，而不是 200 + 空列表——后者会让
	// 「作者不存在」与「作者还没发过作品」看起来一样。
	if _, err := h.queries.GetPublicProfileByUsername(ctx, name); errors.Is(err, pgx.ErrNoRows) {
		writeAuthorNotFound(w)
		return
	} else if err != nil {
		unavailable(w, err)
		return
	}

	rows, err := h.queries.ListPublicDemosByAuthor(ctx, store.ListPublicDemosByAuthorParams{
		Username:  name,
		RowLimit:  int32(pageSize),
		RowOffset: int32((page - 1) * pageSize),
	})
	if err != nil {
		unavailable(w, err)
		return
	}
	total, err := h.queries.CountPublicDemosByAuthor(ctx, name)
	if err != nil {
		unavailable(w, err)
		return
	}

	items := make([]api.ProfileDemo, 0, len(rows))
	for _, row := range rows {
		item := api.ProfileDemo{
			Slug:      row.Slug,
			CreatedAt: row.CreatedAt.Time,
		}
		if row.Title.Valid {
			title := row.Title.String
			item.Title = &title
		}
		if row.CoverUrl.Valid {
			cover := row.CoverUrl.String
			item.CoverUrl = &cover
		}
		items = append(items, item)
	}
	writeJSON(w, http.StatusOK, api.ProfileDemoList{
		Items:    items,
		Total:    int(total),
		Page:     page,
		PageSize: pageSize,
	})
}
