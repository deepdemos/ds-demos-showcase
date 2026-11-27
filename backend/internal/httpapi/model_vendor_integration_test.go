package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/api"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/identity"
)

// 厂商实体（模型域 §3.4 第 6 步 / §4.7.3）：手动新建时「选已有厂商」或「就地建一家」。
//
// 这组用例钉的是「把厂商从自由文本变成实体」的四条凭据：
//
//  1. 挂上已有厂商：`models.vendor_id` 指向那一行，过渡期文本列在**同一事务**里双写成
//     同一个值（两边不一致就是自己造出了第二份事实源）；
//  2. 就地新建：只收名称、`source='manual'`、slug 按名字折；两家折出同一个 slug 回 409
//     并**要求改名**（不自动加后缀），名字逐字相同则复用那一家（同一个厂商建两次不该
//     出现两条行）；冲突回滚后不留下半截厂商；
//  3. 读路径取**实体名**：实体改名后模型页与目录跟着改名，历史行（只有文本列）退回文本；
//  4. 不选厂商也能建：`vendor_id` 与文本列都是 NULL、接口回空串（界面显示「厂商未标注」）。

// createModelBody 拼一次手动新建的请求体。
//
// 用 map + json.Marshal 而不是字符串拼接：#587 那个 helper 只认字符串字段，而这一票
// 引入了数字字段 `vendorId`——手工拼 JSON 会在「多一个字段」时静默漏掉它，用例就会
// 测一个自己都没发出去的请求。
func createModelBody(fields map[string]any) string {
	body, err := json.Marshal(fields)
	if err != nil {
		panic("拼请求体失败: " + err.Error())
	}
	return string(body)
}

// postCreateModel 走真路由发一次手动新建（路径参数与 JSON 绑定都过生成的路由表）。
func postCreateModel(t *testing.T, h *Handler, sid string, fields map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/models",
		strings.NewReader(createModelBody(fields)))
	req.Header.Set("Content-Type", "application/json")
	if sid != "" {
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: sid})
	}
	rec := httptest.NewRecorder()
	NewRouter(h, nil, nil).ServeHTTP(rec, req)
	return rec
}

// putVendor 直接往 `vendors` 里放一家厂商：用例要造的正是「已经有这一家」这个前提，
// 而「已有的那家从哪来」不是本票的被测路径（就地新建一次只能建一家）。
func putVendor(t *testing.T, pool *pgxpool.Pool, slug, name string) int64 {
	t.Helper()
	var id int64
	if err := pool.QueryRow(context.Background(),
		`INSERT INTO vendors (slug, name, source) VALUES ($1, $2, 'manual') RETURNING id`,
		slug, name).Scan(&id); err != nil {
		t.Fatalf("建厂商失败: %v", err)
	}
	t.Cleanup(func() { purgeVendor(t, pool, slug) })
	return id
}

// purgeVendor 按 slug 收干净。
//
// 先解挂再删：`models.vendor_id` 没有外键（基线里就没有），留着悬空指针会让读路径的
// COALESCE 悄悄退回文本列——后面的用例就分不清「读到的是实体还是文本」。
func purgeVendor(t *testing.T, pool *pgxpool.Pool, slug string) {
	t.Helper()
	ctx := context.Background()
	if _, err := pool.Exec(ctx,
		`UPDATE models SET vendor_id = NULL WHERE vendor_id IN (SELECT id FROM vendors WHERE slug = $1)`,
		slug); err != nil {
		t.Fatalf("解挂厂商失败: %v", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM vendors WHERE slug = $1`, slug); err != nil {
		t.Fatalf("清厂商失败: %v", err)
	}
}

// TestIntegrationManualModelCreateAttachesVendor 是本票的主验收：两条挂厂商的路都要通，
// 且都不该多造一条厂商行。
func TestIntegrationManualModelCreateAttachesVendor(t *testing.T) {
	h, idSvc, pool := newModelTestHandler(t)
	h.WithPool(pool)
	sid := mkAdmin(t, pool, idSvc, "mwv1")
	ctx := context.Background()

	// ① 下拉里没有 → 就地新建（请求里只有名称，没有 id）。
	vendorName := "IT 厂商 " + uniq()
	rec := postCreateModel(t, h, sid, map[string]any{
		"name":   "IT 厂商模型 " + uniq(),
		"vendor": vendorName,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("就地新建厂商时建模型状态码 = %d，想要 201；body=%s", rec.Code, rec.Body.String())
	}
	var created api.Model
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("解析模型失败: %v", err)
	}
	t.Cleanup(func() { purgeManualModel(t, pool, created.Slug) })
	if created.Vendor != vendorName {
		t.Fatalf("模型上的厂商 = %q，想要 %q", created.Vendor, vendorName)
	}

	var vendorID int64
	var vendorSlug, source, textVendor string
	// 一条 SQL 同时核三件事：模型挂上了实体（JOIN 命中）、实体来源是 manual、
	// 过渡期文本列与实体同值。
	err := pool.QueryRow(ctx, `
SELECT v.id, v.slug, v.source, COALESCE(m.vendor, '')
  FROM models m
  JOIN vendors v ON v.id = m.vendor_id
 WHERE m.slug = $1`, created.Slug).Scan(&vendorID, &vendorSlug, &source, &textVendor)
	if err != nil {
		t.Fatalf("模型没有挂上厂商实体（vendor_id 为空？）: %v", err)
	}
	t.Cleanup(func() { purgeVendor(t, pool, vendorSlug) })
	if source != "manual" {
		t.Fatalf("厂商来源 = %q，想要 manual：就地新建是人点下去的", source)
	}
	if textVendor != vendorName {
		t.Fatalf("文本列 = %q，实体名 = %q：双写两边必须一致", textVendor, vendorName)
	}

	// ② 下拉里有 → 选它。同一个厂商被第二次使用时不该多出一行。
	rec = postCreateModel(t, h, sid, map[string]any{
		"name":     "IT 选已有厂商 " + uniq(),
		"vendorId": vendorID,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("选已有厂商时建模型状态码 = %d，想要 201；body=%s", rec.Code, rec.Body.String())
	}
	var attached api.Model
	if err := json.Unmarshal(rec.Body.Bytes(), &attached); err != nil {
		t.Fatalf("解析模型失败: %v", err)
	}
	t.Cleanup(func() { purgeManualModel(t, pool, attached.Slug) })
	if attached.Vendor != vendorName {
		t.Fatalf("选已有厂商后模型上的厂商 = %q，想要 %q", attached.Vendor, vendorName)
	}

	var secondVendorID int64
	if err := pool.QueryRow(ctx, `SELECT vendor_id FROM models WHERE slug = $1`, attached.Slug).
		Scan(&secondVendorID); err != nil {
		t.Fatalf("读第二条模型的 vendor_id 失败: %v", err)
	}
	if secondVendorID != vendorID {
		t.Fatalf("第二条模型的 vendor_id = %d，想要 %d", secondVendorID, vendorID)
	}

	var vendorCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM vendors WHERE slug = $1`, vendorSlug).
		Scan(&vendorCount); err != nil {
		t.Fatalf("数厂商失败: %v", err)
	}
	if vendorCount != 1 {
		t.Fatalf("同一家厂商的行数 = %d，想要 1：选已有不该再建一条", vendorCount)
	}
}

// TestIntegrationManualModelCreateVendorSlugConflict 两个厂商名折出同一个 slug 时报错
// 并要求改名——不要自动加后缀。
//
// 与「同名模型撞车」的关键差别：撞的是厂商，不是模型。所以 409 **不能**带
// `existingSlug`（它指的是已有那条模型）：带了界面会把用户带到一条不相干的模型页。
func TestIntegrationManualModelCreateVendorSlugConflict(t *testing.T) {
	h, idSvc, pool := newModelTestHandler(t)
	h.WithPool(pool)
	sid := mkAdmin(t, pool, idSvc, "mwv2")
	ctx := context.Background()

	tag := uniq()
	existingName := "IT Clash " + tag
	vendorSlug := "it-clash-" + tag
	existing := putVendor(t, pool, vendorSlug, existingName)

	// 另一个写法（大小写不同）折出同一个 slug：这是「两家其实是同一家」的典型形态，
	// 但名字不同，所以要求人决定叫哪个 —— 自动加后缀会让厂商键永久变成 it-clash-x-2。
	conflicting := strings.ToLower(existingName)
	rec := postCreateModel(t, h, sid, map[string]any{
		"name":   "IT 撞厂商 " + uniq(),
		"vendor": conflicting,
	})
	if rec.Code != http.StatusConflict {
		t.Fatalf("厂商名撞 slug 状态码 = %d，想要 409；body=%s", rec.Code, rec.Body.String())
	}
	var e api.Error
	if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil {
		t.Fatalf("解析错误体失败: %v", err)
	}
	if e.Code != api.Conflict {
		t.Fatalf("错误码 = %q，想要 conflict", e.Code)
	}
	if e.ExistingSlug != nil {
		t.Fatalf("厂商撞 slug 的 409 带上了 existingSlug=%q：那个字段指的是模型", *e.ExistingSlug)
	}
	if !strings.Contains(e.Message, existingName) {
		t.Fatalf("409 文案要点名已有那家厂商（%q），读到的：%q", existingName, e.Message)
	}

	// 冲突不该留下半截：厂商还是一行，也没有留下一条挂不上的模型。
	var vendorCount, orphanModels int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM vendors WHERE slug = $1`, vendorSlug).
		Scan(&vendorCount); err != nil {
		t.Fatalf("数厂商失败: %v", err)
	}
	if vendorCount != 1 {
		t.Fatalf("冲突后该 slug 的厂商行数 = %d，想要 1（没有自动加后缀、没有多插一行）", vendorCount)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM models WHERE vendor_id = $1`, existing).
		Scan(&orphanModels); err != nil {
		t.Fatalf("数模型失败: %v", err)
	}
	if orphanModels != 0 {
		t.Fatalf("冲突请求留下了 %d 条模型，想要 0：整个事务该回滚", orphanModels)
	}

	// 名字逐字相同 → 就是同一家，直接挂上去（同一个厂商被两个人先后建不该互相报错）。
	rec = postCreateModel(t, h, sid, map[string]any{
		"name":   "IT 同名厂商 " + uniq(),
		"vendor": existingName,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("同名厂商建模型状态码 = %d，想要 201（复用已有那家）；body=%s", rec.Code, rec.Body.String())
	}
	var created api.Model
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("解析模型失败: %v", err)
	}
	t.Cleanup(func() { purgeManualModel(t, pool, created.Slug) })
	var attached int64
	if err := pool.QueryRow(ctx, `SELECT vendor_id FROM models WHERE slug = $1`, created.Slug).
		Scan(&attached); err != nil {
		t.Fatalf("读 vendor_id 失败: %v", err)
	}
	if attached != existing {
		t.Fatalf("vendor_id = %d，想要复用已有那家 %d", attached, existing)
	}
}

// TestIntegrationManualModelCreateWithoutVendor 厂商可空：不选也能建，两边都空才是
// 「厂商未标注」。空串会让读路径看起来像有值，界面就不该说「未标注」。
func TestIntegrationManualModelCreateWithoutVendor(t *testing.T) {
	h, idSvc, pool := newModelTestHandler(t)
	h.WithPool(pool)
	sid := mkAdmin(t, pool, idSvc, "mwv3")
	ctx := context.Background()

	rec := postCreateModel(t, h, sid, map[string]any{"name": "IT 无厂商 " + uniq()})
	if rec.Code != http.StatusCreated {
		t.Fatalf("不选厂商时状态码 = %d，想要 201；body=%s", rec.Code, rec.Body.String())
	}
	var created api.Model
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("解析模型失败: %v", err)
	}
	t.Cleanup(func() { purgeManualModel(t, pool, created.Slug) })
	if created.Vendor != "" {
		t.Fatalf("模型上的厂商 = %q，想要空串（未标注）", created.Vendor)
	}

	var vendorID *int64
	var textVendor *string
	if err := pool.QueryRow(ctx, `SELECT vendor_id, vendor FROM models WHERE slug = $1`, created.Slug).
		Scan(&vendorID, &textVendor); err != nil {
		t.Fatalf("读厂商列失败: %v", err)
	}
	if vendorID != nil || textVendor != nil {
		t.Fatalf("两列该都是 NULL（vendor_id=%v, vendor=%v）：空串会让「未标注」看起来像有值",
			vendorID, textVendor)
	}

	// 详情页读回来同样是空串——界面据此显示「厂商未标注」，不是「厂商未知」。
	rec = getWithSession(t, h, "/api/v1/models/"+created.Slug, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("详情状态码 = %d，想要 200；body=%s", rec.Code, rec.Body.String())
	}
	var profile api.ModelProfile
	if err := json.Unmarshal(rec.Body.Bytes(), &profile); err != nil {
		t.Fatalf("解析详情失败: %v", err)
	}
	if profile.Model.Vendor != "" {
		t.Fatalf("详情里的厂商 = %q，想要空串", profile.Model.Vendor)
	}
}

// TestIntegrationModelReadsVendorEntityName 读路径取厂商**实体**的名字。
//
// 这条用例的存在理由：双写让两边**现在**一致，所以「读文本列」在今天的库上也能过。
// 唯一的判别办法是把实体改名——只有真的读了 `vendors.name` 的那份实现才会跟着变。
// 同时钉住历史行的回退：只有文本列的行照样读得出来，否则存量模型会集体变成「未标注」。
func TestIntegrationModelReadsVendorEntityName(t *testing.T) {
	h, idSvc, pool := newModelTestHandler(t)
	h.WithPool(pool)
	sid := mkAdmin(t, pool, idSvc, "mwv4")
	ctx := context.Background()

	vendorName := "IT 实体名 " + uniq()
	rec := postCreateModel(t, h, sid, map[string]any{
		"name":   "IT 实体读路径 " + uniq(),
		"vendor": vendorName,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("建模型状态码 = %d，想要 201；body=%s", rec.Code, rec.Body.String())
	}
	var created api.Model
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("解析模型失败: %v", err)
	}
	t.Cleanup(func() { purgeManualModel(t, pool, created.Slug) })

	var vendorSlug string
	if err := pool.QueryRow(ctx, `SELECT slug FROM vendors WHERE name = $1`, vendorName).
		Scan(&vendorSlug); err != nil {
		t.Fatalf("查厂商 slug 失败: %v", err)
	}
	t.Cleanup(func() { purgeVendor(t, pool, vendorSlug) })

	// 改名（管理端还没有改名入口，直接改库）：读路径必须跟着实体走，不许留在旧文本上。
	renamed := vendorName + " 新"
	if _, err := pool.Exec(ctx, `UPDATE vendors SET name = $1 WHERE slug = $2`, renamed, vendorSlug); err != nil {
		t.Fatalf("改厂商名失败: %v", err)
	}

	rec = getWithSession(t, h, "/api/v1/models/"+created.Slug, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("详情状态码 = %d，想要 200；body=%s", rec.Code, rec.Body.String())
	}
	var profile api.ModelProfile
	if err := json.Unmarshal(rec.Body.Bytes(), &profile); err != nil {
		t.Fatalf("解析详情失败: %v", err)
	}
	if profile.Model.Vendor != renamed {
		t.Fatalf("详情里的厂商 = %q，想要实体名 %q：读路径读的还是文本列", profile.Model.Vendor, renamed)
	}

	// 目录与详情必须是同一个口径（同一份数据在两处长得不一样，是最容易漏的不一致）。
	rec = getWithSession(t, h, "/api/v1/models", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("目录状态码 = %d，想要 200", rec.Code)
	}
	var list api.ModelList
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("解析目录失败: %v", err)
	}
	found := false
	for _, m := range list.Items {
		if m.Slug != created.Slug {
			continue
		}
		found = true
		if m.Vendor != renamed {
			t.Fatalf("目录里的厂商 = %q，想要 %q（与详情同一个口径）", m.Vendor, renamed)
		}
	}
	if !found {
		t.Fatalf("新建的模型 %s 没有出现在目录里", created.Slug)
	}

	// 历史行：只有文本列、没有实体。回退到文本，否则存量模型集体变「未标注」。
	legacySlug := "it-legacy-" + uniq()
	legacyVendor := "it-legacy-vendor"
	if _, err := pool.Exec(ctx, `
INSERT INTO models (slug, name, vendor, status, resolution, description)
VALUES ($1, $2, $3, 'active', 'exact', '')`, legacySlug, "IT 历史行 "+uniq(), legacyVendor); err != nil {
		t.Fatalf("造历史行失败: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM models WHERE slug = $1`, legacySlug)
	})

	rec = getWithSession(t, h, "/api/v1/models/"+legacySlug, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("历史行详情状态码 = %d，想要 200；body=%s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &profile); err != nil {
		t.Fatalf("解析历史行详情失败: %v", err)
	}
	if profile.Model.Vendor != legacyVendor {
		t.Fatalf("历史行的厂商 = %q，想要文本列的 %q", profile.Model.Vendor, legacyVendor)
	}
}

// TestIntegrationListModelVendors 下拉的数据源：只给「选哪一个」需要的三列。
func TestIntegrationListModelVendors(t *testing.T) {
	h, idSvc, pool := newModelTestHandler(t)
	h.WithPool(pool)
	sid := mkAdmin(t, pool, idSvc, "mwv5")

	tag := uniq()
	vendorSlug := "it-dropdown-" + tag
	vendorName := "IT 下拉厂商 " + tag
	id := putVendor(t, pool, vendorSlug, vendorName)

	rec := getWithSession(t, h, "/api/v1/admin/models/vendors", sid)
	if rec.Code != http.StatusOK {
		t.Fatalf("厂商列表状态码 = %d，想要 200；body=%s", rec.Code, rec.Body.String())
	}
	var list api.ModelVendorList
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("解析厂商列表失败: %v", err)
	}

	found := false
	for _, v := range list.Items {
		if int64(v.Id) != id {
			continue
		}
		found = true
		if v.Slug != vendorSlug || v.Name != vendorName {
			t.Fatalf("厂商行 = {%d %q %q}，想要 {%d %q %q}", v.Id, v.Slug, v.Name, id, vendorSlug, vendorName)
		}
	}
	if !found {
		t.Fatalf("厂商列表里没有刚建的那家（id=%d）", id)
	}
}

// TestIntegrationListModelVendorsPermissions 下拉只服务「我要建模型」这条路，
// 所以鉴权与新建同一个键：没有 model.create 的人连候选厂商都不该看到。
func TestIntegrationListModelVendorsPermissions(t *testing.T) {
	h, idSvc, pool := newModelTestHandler(t)
	h.WithPool(pool)

	// 未登录是 401，不是 403：两者含义不同，混起来会让人以为登录坏了。
	rec := getWithSession(t, h, "/api/v1/admin/models/vendors", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("未登录读厂商列表状态码 = %d，想要 401；body=%s", rec.Code, rec.Body.String())
	}

	tag := uniq()
	acc, sid := registerVerified(t, pool, idSvc, identity.RegisterInput{
		Email:       "it-mwvp-" + tag + "@example.com",
		Password:    "importtest123",
		Username:    "it-mwvp-" + tag,
		DisplayName: "厂商列表权限测试",
	})
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE sub = $1`, acc.Sub)
		_, _ = pool.Exec(context.Background(), `DELETE FROM accounts WHERE sub = $1`, acc.Sub)
	})

	rec = getWithSession(t, h, "/api/v1/admin/models/vendors", sid)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("无权限读厂商列表状态码 = %d，想要 403；body=%s", rec.Code, rec.Body.String())
	}
	var e api.Error
	if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil {
		t.Fatalf("解析错误体失败: %v", err)
	}
	if e.Code != api.Forbidden {
		t.Fatalf("错误码 = %q，想要 forbidden", e.Code)
	}
}

// TestIntegrationManualModelCreateRejectsVendorInput 厂商输入的三种坏形状：
// 两个字段同时给（二选一）、指向不存在的厂商、厂商名超长。
//
// 超长这一条最值得钉：厂商实体先插进去了、模型那步才撞 `ck_models_vendor_len`——
// 事务没兜住就会留下一条没有任何模型指向的厂商行。
func TestIntegrationManualModelCreateRejectsVendorInput(t *testing.T) {
	h, idSvc, pool := newModelTestHandler(t)
	h.WithPool(pool)
	sid := mkAdmin(t, pool, idSvc, "mwv6")
	ctx := context.Background()

	vendorSlug := "it-ambiguous-" + uniq()
	id := putVendor(t, pool, vendorSlug, "IT 二选一 "+uniq())

	// 同时说「选这家」与「建一家叫这个的」：猜错哪一个都会写进共享目录。
	rec := postCreateModel(t, h, sid, map[string]any{
		"name":     "IT 厂商二选一 " + uniq(),
		"vendor":   "IT 另一家 " + uniq(),
		"vendorId": id,
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("两个厂商字段同时给状态码 = %d，想要 400；body=%s", rec.Code, rec.Body.String())
	}

	// 指向不存在的厂商：400（这是新建模型的请求体，形状错了不该走得更远）。
	rec = postCreateModel(t, h, sid, map[string]any{
		"name":     "IT 厂商不存在 " + uniq(),
		"vendorId": 99999999,
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("不存在的 vendorId 状态码 = %d，想要 400；body=%s", rec.Code, rec.Body.String())
	}

	// 厂商名超过 models.vendor 的 64 字符闸：400，且不留下来路不明的厂商行。
	longName := strings.Repeat("v", 65)
	rec = postCreateModel(t, h, sid, map[string]any{
		"name":   "IT 长厂商名 " + uniq(),
		"vendor": longName,
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("超长厂商名状态码 = %d，想要 400；body=%s", rec.Code, rec.Body.String())
	}
	var leftovers int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM vendors WHERE name = $1`, longName).
		Scan(&leftovers); err != nil {
		t.Fatalf("数厂商失败: %v", err)
	}
	if leftovers != 0 {
		t.Fatalf("超长厂商名留下了 %d 条厂商行，想要 0：整个事务该回滚", leftovers)
	}
}
