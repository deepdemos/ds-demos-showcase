package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/api"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/identity"
)

// 管理端模型列表（#596）测试：
//
//  1. 鉴权边界：未登录 401、无模型权限 403、持 model.import / model.create 任一键可 200；
//  2. 全状态返回：active / candidate / unverified / deprecated 全部列出，包含厂商实体、
//     合并归宿与已下架（deprecated 且 mergedIntoSlug 为空）；
//  3. 档案对象原样返回：无档案为 nil（missing）、缺字段保留空值（partial）、齐全为完整；
//  4. 按 status 筛选有效，非法 status 返回 400（不冒充空列表 200）。

func getAdminModels(t *testing.T, h *Handler, query, sid string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/models"+query, nil)
	if sid != "" {
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: sid})
	}
	rec := httptest.NewRecorder()
	NewRouter(h, nil, nil).ServeHTTP(rec, req)
	return rec
}

func TestParseAdminModelStatus(t *testing.T) {
	if got, ok := parseAdminModelStatus(nil); !ok || got.Valid {
		t.Fatalf("nil status = (%+v, %v)，想要 (invalid, true)", got, ok)
	}
	empty := ""
	if got, ok := parseAdminModelStatus(&empty); !ok || got.Valid {
		t.Fatalf("空串 status = (%+v, %v)，想要 (invalid, true)", got, ok)
	}
	for _, valid := range []string{
		string(api.ModelStatusCandidate),
		string(api.ModelStatusActive),
		string(api.ModelStatusUnverified),
		string(api.ModelStatusDeprecated),
	} {
		v := valid
		got, ok := parseAdminModelStatus(&v)
		if !ok || !got.Valid || got.String != valid {
			t.Fatalf("合法 status %q = (%+v, %v)，想要有效匹配", valid, got, ok)
		}
	}
	for _, bad := range []string{"bogus", "ACTIVE", "delisted"} {
		v := bad
		if _, ok := parseAdminModelStatus(&v); ok {
			t.Fatalf("非法 status %q 应当被拒", bad)
		}
	}
}

func TestListAdminModelsNotWired(t *testing.T) {
	h := New(nil, nil, nil, nil, nil)
	rec := httptest.NewRecorder()
	h.ListAdminModels(rec, httptest.NewRequest(http.MethodGet, "/api/v1/admin/models", nil), api.ListAdminModelsParams{})
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("未接线状态码 = %d，想要 503", rec.Code)
	}
}

func TestIntegrationListAdminModelsPermissions(t *testing.T) {
	h, idSvc, pool := newModelTestHandler(t)
	ctx := context.Background()

	// 未登录 401。
	rec := getAdminModels(t, h, "", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("未登录状态码 = %d，想要 401；body=%s", rec.Code, rec.Body.String())
	}

	// 普通登录账号无模型管理权限 → 403。
	tag := uniq()
	plain, plainSID := registerVerified(t, pool, idSvc, identity.RegisterInput{
		Email:       "it-aml-plain-" + tag + "@example.com",
		Password:    "admintest123",
		Username:    "it-aml-" + tag[:8],
		DisplayName: "普通用户",
	})
	t.Cleanup(func() {
		if _, err := pool.Exec(ctx, `DELETE FROM users WHERE sub = $1`, plain.Sub); err != nil {
			t.Errorf("清理普通用户 users 失败: %v", err)
		}
		if _, err := pool.Exec(ctx, `DELETE FROM accounts WHERE sub = $1`, plain.Sub); err != nil {
			t.Errorf("清理普通用户 accounts 失败: %v", err)
		}
	})
	rec = getAdminModels(t, h, "", plainSID)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("无权限状态码 = %d，想要 403；body=%s", rec.Code, rec.Body.String())
	}
	var errBody api.Error
	if err := json.Unmarshal(rec.Body.Bytes(), &errBody); err != nil {
		t.Fatalf("解析错误响应失败: %v", err)
	}
	if errBody.Code != api.Forbidden {
		t.Fatalf("错误码 = %q，想要 forbidden", errBody.Code)
	}

	// 只持 model.create 的自定义角色也应能看列表（不绑死 model.import）。
	roleKey := "it-mc-" + tag[:8]
	if _, err := pool.Exec(ctx, `INSERT INTO roles (key, name, is_system, description) VALUES ($1, '仅建模型', false, '测试')`, roleKey); err != nil {
		t.Fatalf("建角色失败: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO role_permissions (role_key, permission_key) VALUES ($1, $2)`, roleKey, permModelCreate); err != nil {
		t.Fatalf("赋权失败: %v", err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(ctx, `DELETE FROM role_permissions WHERE role_key = $1`, roleKey); err != nil {
			t.Errorf("清理角色权限失败: %v", err)
		}
		if _, err := pool.Exec(ctx, `DELETE FROM roles WHERE key = $1`, roleKey); err != nil {
			t.Errorf("清理角色失败: %v", err)
		}
	})
	if _, err := pool.Exec(ctx, `UPDATE accounts SET role = $1 WHERE sub = $2`, roleKey, plain.Sub); err != nil {
		t.Fatalf("切角色失败: %v", err)
	}
	rec = getAdminModels(t, h, "", plainSID)
	if rec.Code != http.StatusOK {
		t.Fatalf("仅持 model.create 的角色状态码 = %d，想要 200；body=%s", rec.Code, rec.Body.String())
	}
}

func TestIntegrationListAdminModelsAllStatusesAndSpecs(t *testing.T) {
	h, idSvc, pool := newModelTestHandler(t)
	sid := mkAdmin(t, pool, idSvc, "aml")
	ctx := context.Background()

	tag := uniq()
	vendorSlug := "it-aml-vendor-" + tag
	vendorName := "IT 厂商 " + tag
	activeSlug := "it-aml-active-" + tag
	candSlug := "it-aml-cand-" + tag
	unverSlug := "it-aml-unver-" + tag
	mergedSlug := "it-aml-merged-" + tag
	delistedSlug := "it-aml-delisted-" + tag
	allSlugs := []string{activeSlug, candSlug, unverSlug, mergedSlug, delistedSlug}

	t.Cleanup(func() {
		if _, err := pool.Exec(ctx, `UPDATE models SET merged_into_id = NULL WHERE slug = ANY($1)`, allSlugs); err != nil {
			t.Errorf("清理 merged_into_id 失败: %v", err)
		}
		if _, err := pool.Exec(ctx, `DELETE FROM model_specs WHERE model_id IN (SELECT id FROM models WHERE slug = ANY($1))`, allSlugs); err != nil {
			t.Errorf("清理 model_specs 失败: %v", err)
		}
		if _, err := pool.Exec(ctx, `DELETE FROM models WHERE slug = ANY($1)`, allSlugs); err != nil {
			t.Errorf("清理 models 失败: %v", err)
		}
		if _, err := pool.Exec(ctx, `DELETE FROM vendors WHERE slug = $1`, vendorSlug); err != nil {
			t.Errorf("清理 vendors 失败: %v", err)
		}
	})

	var vendorID int64
	if err := pool.QueryRow(ctx, `INSERT INTO vendors (slug, name, source) VALUES ($1, $2, 'manual') RETURNING id`,
		vendorSlug, vendorName).Scan(&vendorID); err != nil {
		t.Fatalf("建厂商失败: %v", err)
	}

	var activeID, unverID int64
	if err := pool.QueryRow(ctx,
		`INSERT INTO models (slug, name, vendor, vendor_id, status, resolution) VALUES ($1, $2, $3, $4, 'active', 'exact') RETURNING id`,
		activeSlug, "IT Active "+tag, vendorName, vendorID).Scan(&activeID); err != nil {
		t.Fatalf("建 active 模型失败: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO model_specs (model_id, context_window, modalities_in, modalities_out, param_kind, source) VALUES ($1, 128000, 'text,image', 'text', 'undisclosed', 'manual')`,
		activeID); err != nil {
		t.Fatalf("写完整档案失败: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO models (slug, name, vendor, status, resolution) VALUES ($1, $2, NULL, 'candidate', 'exact')`,
		candSlug, "IT Candidate "+tag); err != nil {
		t.Fatalf("建 candidate 模型失败: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO models (slug, name, vendor, status, resolution) VALUES ($1, $2, 'LegacyVendor', 'unverified', 'exact') RETURNING id`,
		unverSlug, "IT Unverified "+tag).Scan(&unverID); err != nil {
		t.Fatalf("建 unverified 模型失败: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO model_specs (model_id, context_window, modalities_in, modalities_out, param_kind, source) VALUES ($1, 64000, '', 'text', 'undisclosed', 'manual')`,
		unverID); err != nil {
		t.Fatalf("写部分档案失败: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO models (slug, name, vendor, status, resolution, merged_into_id) VALUES ($1, $2, NULL, 'deprecated', 'exact', $3)`,
		mergedSlug, "IT Merged "+tag, activeID); err != nil {
		t.Fatalf("建 merged 模型失败: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO models (slug, name, vendor, status, resolution, merged_into_id) VALUES ($1, $2, NULL, 'deprecated', 'exact', NULL)`,
		delistedSlug, "IT Delisted "+tag); err != nil {
		t.Fatalf("建 delisted 模型失败: %v", err)
	}

	// 1. 不带筛选：返回全部状态，且每行字段与档案形状准确。
	rec := getAdminModels(t, h, "", sid)
	if rec.Code != http.StatusOK {
		t.Fatalf("全部列表状态码 = %d，想要 200；body=%s", rec.Code, rec.Body.String())
	}
	var all api.AdminModelList
	if err := json.Unmarshal(rec.Body.Bytes(), &all); err != nil {
		t.Fatalf("解析列表失败: %v", err)
	}
	bySlug := map[string]api.AdminModelRow{}
	for _, row := range all.Items {
		bySlug[row.Model.Slug] = row
	}
	for _, slug := range allSlugs {
		if _, ok := bySlug[slug]; !ok {
			t.Fatalf("全部列表缺少模型 %q", slug)
		}
	}

	active := bySlug[activeSlug]
	if active.Model.Vendor != vendorName || active.Model.Status != api.ModelStatusActive {
		t.Fatalf("active 行模型字段不对：%+v", active.Model)
	}
	if active.Specs == nil || active.Specs.ContextWindow == nil || *active.Specs.ContextWindow != 128000 ||
		active.Specs.ModalitiesIn == nil || *active.Specs.ModalitiesIn != "text,image" {
		t.Fatalf("active 完整档案不对：%+v", active.Specs)
	}

	cand := bySlug[candSlug]
	if cand.Model.Vendor != "" || cand.Model.Status != api.ModelStatusCandidate || cand.Specs != nil {
		t.Fatalf("candidate 缺档案行不对：model=%+v specs=%+v", cand.Model, cand.Specs)
	}

	unver := bySlug[unverSlug]
	if unver.Model.Status != api.ModelStatusUnverified || unver.Specs == nil ||
		unver.Specs.ContextWindow == nil || *unver.Specs.ContextWindow != 64000 || unver.Specs.ModalitiesIn != nil {
		t.Fatalf("unverified 部分档案行不对：model=%+v specs=%+v", unver.Model, unver.Specs)
	}

	merged := bySlug[mergedSlug]
	if merged.Model.Status != api.ModelStatusDeprecated || merged.Model.MergedIntoSlug != activeSlug {
		t.Fatalf("merged 行不对：%+v", merged.Model)
	}

	delisted := bySlug[delistedSlug]
	if delisted.Model.Status != api.ModelStatusDeprecated || delisted.Model.MergedIntoSlug != "" {
		t.Fatalf("delisted 行不对（已下架应为 deprecated 且无归宿）：%+v", delisted.Model)
	}

	// 2. 按 status=candidate 筛选：只包含 candidate。
	rec = getAdminModels(t, h, "?status=candidate", sid)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=candidate 状态码 = %d，想要 200", rec.Code)
	}
	var cands api.AdminModelList
	if err := json.Unmarshal(rec.Body.Bytes(), &cands); err != nil {
		t.Fatalf("解析候选列表失败: %v", err)
	}
	foundCand := false
	for _, row := range cands.Items {
		if row.Model.Status != api.ModelStatusCandidate {
			t.Fatalf("按 candidate 筛时混入了非候选行：%+v", row.Model)
		}
		if row.Model.Slug == candSlug {
			foundCand = true
		}
	}
	if !foundCand {
		t.Fatalf("按 candidate 筛时缺少 %q", candSlug)
	}

	// 3. 非法 status 返回 400，不静默回空列表。
	rec = getAdminModels(t, h, "?status=bogus", sid)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法 status 状态码 = %d，想要 400；body=%s", rec.Code, rec.Body.String())
	}
	var errBody api.Error
	if err := json.Unmarshal(rec.Body.Bytes(), &errBody); err != nil {
		t.Fatalf("解析错误体失败: %v", err)
	}
	if errBody.Code != api.InvalidParam {
		t.Fatalf("错误码 = %q，想要 invalid_param", errBody.Code)
	}
}
