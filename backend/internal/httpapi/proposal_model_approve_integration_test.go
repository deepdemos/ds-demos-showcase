package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/api"
)

// 批准 new_model 收录提案（#593）：
//
//  1. 手填提案批准 → 建出 status='candidate' / resolution='exact' / source='manual' 的模型，
//     提案置 approved 且记录 result_kind='model' / result_id，同时写入 model:create 与 proposal:review 审计；
//  2. 带上游三元组的提案批准 → 建出 candidate 模型并当场写 model_offerings 认领，模型来源派生为 models.dev；
//  3. 三元组已被 active 或 deprecated（含合并归宿）模型认领时 → 409 并说明归属，提案保持 pending、不建模型；
//  4. 模型名 / slug 与已有模型冲突 → 409 并复用手动新建同一文案；
//  5. 兜底名 / 目录缺失 / 非法来源 / 空名 / NUL 字符 → 422 且不建模型；
//  6. 批量批准支持部分成功（合法条成功，认领冲突条逐条报归属原因）。

func TestParseNewModelProposalPayload(t *testing.T) {
	got, fail := parseNewModelProposalPayload([]byte(`{"name":"  DeepSeek V4 Preview  ","vendor":"DeepSeek","provider_id":"deepseek","external_id":"deepseek-v4-preview"}`))
	if fail != nil {
		t.Fatalf("合法 payload 不应失败: %+v", fail)
	}
	if got.Name != "DeepSeek V4 Preview" || got.Vendor != "DeepSeek" ||
		!got.HasUpstream || got.Source != "models.dev" ||
		got.ProviderID != "deepseek" || got.ExternalID != "deepseek-v4-preview" {
		t.Fatalf("解析结果不对: %+v", got)
	}

	manual, fail := parseNewModelProposalPayload([]byte(`{"name":"自研私有模型"}`))
	if fail != nil || manual.HasUpstream || manual.Name != "自研私有模型" {
		t.Fatalf("纯手填 payload 解析不对: got=%+v fail=%+v", manual, fail)
	}

	badCases := []struct {
		name string
		raw  string
	}{
		{"空名称", `{"name":"  "}`},
		{"名称非字符串", `{"name":123}`},
		{"兜底名 unspecified", `{"name":"unspecified"}`},
		{"兜底名 vendor-unknown", `{"name":"openai-unknown"}`},
		{"名称含 NUL", `{"name":"foo\u0000bar"}`},
		{"半截三元组缺 external_id", `{"name":"M","provider_id":"deepseek"}`},
		{"只给 source 缺三元组", `{"name":"M","source":"models.dev"}`},
		{"不支持的上游来源", `{"name":"M","source":"endpoint","provider_id":"https://x","external_id":"m"}`},
	}
	for _, tc := range badCases {
		t.Run(tc.name, func(t *testing.T) {
			if _, fail := parseNewModelProposalPayload([]byte(tc.raw)); fail == nil || fail.status != http.StatusUnprocessableEntity {
				t.Fatalf("%s: fail = %+v，想要 422", tc.name, fail)
			}
		})
	}
}

func TestIntegrationApproveNewModelProposalManual(t *testing.T) {
	h, _, idSvc := newIdempotencyHandler(t)
	pool := integrationPool(t)
	adminSID := mkAdmin(t, pool, idSvc, "pm-man")
	ctx := context.Background()

	tag := uniq()
	modelName := "IT Proposal Manual " + tag
	var proposalID int64
	if err := pool.QueryRow(ctx, `
INSERT INTO proposals (kind, payload, source)
VALUES ('new_model', jsonb_build_object('name', $1::text, 'vendor', '自研实验室'), 'user')
RETURNING id`, modelName).Scan(&proposalID); err != nil {
		t.Fatalf("插入手填模型提案失败: %v", err)
	}
	var createdModelID int64
	t.Cleanup(func() {
		if createdModelID != 0 {
			if _, err := pool.Exec(ctx, `DELETE FROM audit_log WHERE entity_type = 'model' AND entity_id = $1`, createdModelID); err != nil {
				t.Errorf("清理模型审计失败: %v", err)
			}
			if _, err := pool.Exec(ctx, `DELETE FROM models WHERE id = $1`, createdModelID); err != nil {
				t.Errorf("清理候选模型失败: %v", err)
			}
		}
		if _, err := pool.Exec(ctx, `DELETE FROM audit_log WHERE entity_type = 'proposal' AND entity_id = $1`, proposalID); err != nil {
			t.Errorf("清理提案审计失败: %v", err)
		}
		if _, err := pool.Exec(ctx, `DELETE FROM proposals WHERE id = $1`, proposalID); err != nil {
			t.Errorf("清理提案失败: %v", err)
		}
	})

	rec := httptest.NewRecorder()
	h.ReviewAdminProposal(rec, proposalReq(http.MethodPost, "/api/v1/admin/proposals/x/review", adminSID, `{"decision":"approve"}`), int(proposalID))
	if rec.Code != http.StatusOK {
		t.Fatalf("批准手填模型提案 = %d，想要 200；body=%s", rec.Code, rec.Body.String())
	}

	var status, resultKind string
	if err := pool.QueryRow(ctx, `
SELECT status, COALESCE(result_kind, ''), COALESCE(result_id, 0)
FROM proposals WHERE id = $1`, proposalID).Scan(&status, &resultKind, &createdModelID); err != nil {
		t.Fatalf("读提案结果失败: %v", err)
	}
	if status != "approved" || resultKind != "model" || createdModelID == 0 {
		t.Fatalf("提案回填不对：status=%q resultKind=%q resultID=%d", status, resultKind, createdModelID)
	}

	// 验证产出的模型是 candidate（批准 ≠ 可用）、exact、manual 来源。
	var slug, name, vendor, modelStatus, resolution string
	if err := pool.QueryRow(ctx, `
SELECT slug, name, COALESCE(vendor, ''), status, resolution
FROM models WHERE id = $1`, createdModelID).Scan(&slug, &name, &vendor, &modelStatus, &resolution); err != nil {
		t.Fatalf("读产出模型失败: %v", err)
	}
	if name != modelName || vendor != "自研实验室" || modelStatus != "candidate" || resolution != "exact" {
		t.Fatalf("产出模型字段不对：slug=%q name=%q vendor=%q status=%q resolution=%q", slug, name, vendor, modelStatus, resolution)
	}

	// 通过公开模型详情确认派生 source='manual' 且状态是 candidate。
	getRec := httptest.NewRecorder()
	h.GetModel(getRec, httptest.NewRequest(http.MethodGet, "/api/v1/models/"+slug, nil), slug)
	if getRec.Code != http.StatusOK {
		t.Fatalf("读产出模型详情 = %d，想要 200；body=%s", getRec.Code, getRec.Body.String())
	}
	var profile api.ModelProfile
	if err := json.Unmarshal(getRec.Body.Bytes(), &profile); err != nil {
		t.Fatalf("解析模型详情失败: %v", err)
	}
	if profile.Model.Status != api.ModelStatusCandidate || profile.Model.Source != api.Manual {
		t.Fatalf("详情状态/来源不对：status=%q source=%q", profile.Model.Status, profile.Model.Source)
	}

	// 核对两条审计记录都在（model:create 与 proposal:review）。
	var modelAudits, propAudits int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE entity_type='model' AND entity_id=$1 AND action='create'`, createdModelID).Scan(&modelAudits); err != nil {
		t.Fatalf("查模型审计失败: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE entity_type='proposal' AND entity_id=$1 AND action='review'`, proposalID).Scan(&propAudits); err != nil {
		t.Fatalf("查提案审计失败: %v", err)
	}
	if modelAudits != 1 || propAudits != 1 {
		t.Fatalf("审计条数不对：model:create=%d proposal:review=%d，都想要 1", modelAudits, propAudits)
	}

	// 重复批准同一提案 → 409。
	again := httptest.NewRecorder()
	h.ReviewAdminProposal(again, proposalReq(http.MethodPost, "/api/v1/admin/proposals/x/review", adminSID, `{"decision":"approve"}`), int(proposalID))
	if again.Code != http.StatusConflict {
		t.Fatalf("重复批准 = %d，想要 409", again.Code)
	}
}

func TestIntegrationApproveNewModelProposalWithUpstreamClaim(t *testing.T) {
	h, _, idSvc := newIdempotencyHandler(t)
	pool := integrationPool(t)
	adminSID := mkAdmin(t, pool, idSvc, "pm-up")
	ctx := context.Background()

	tag := uniq()
	providerID := "it-up-prov-" + tag[:8]
	externalID := "it-up-ext-" + tag
	canonicalID := providerID + "/" + externalID
	modelName := "IT Upstream Model " + tag

	if _, err := pool.Exec(ctx, `
INSERT INTO model_catalog_items (source, provider_id, provider_name, external_id, name, canonical_id, vendor_slug)
VALUES ('models.dev', $1, 'IT Provider', $2, $3, $4, 'it-vendor')`,
		providerID, externalID, modelName, canonicalID); err != nil {
		t.Fatalf("预置目录镜像失败: %v", err)
	}

	var proposalID int64
	if err := pool.QueryRow(ctx, `
INSERT INTO proposals (kind, payload, source)
VALUES ('new_model', jsonb_build_object(
    'name', $1::text,
    'source', 'models.dev',
    'provider_id', $2::text,
    'external_id', $3::text
), 'user')
RETURNING id`, modelName, providerID, externalID).Scan(&proposalID); err != nil {
		t.Fatalf("插入上游模型提案失败: %v", err)
	}

	var createdModelID int64
	t.Cleanup(func() {
		if _, err := pool.Exec(ctx, `DELETE FROM model_offerings WHERE authority='models.dev' AND provider_id=$1 AND external_id=$2`, providerID, externalID); err != nil {
			t.Errorf("清理 model_offerings 失败: %v", err)
		}
		if createdModelID != 0 {
			if _, err := pool.Exec(ctx, `DELETE FROM audit_log WHERE entity_type='model' AND entity_id=$1`, createdModelID); err != nil {
				t.Errorf("清理模型审计失败: %v", err)
			}
			if _, err := pool.Exec(ctx, `DELETE FROM models WHERE id=$1`, createdModelID); err != nil {
				t.Errorf("清理模型失败: %v", err)
			}
		}
		if _, err := pool.Exec(ctx, `DELETE FROM audit_log WHERE entity_type='proposal' AND entity_id=$1`, proposalID); err != nil {
			t.Errorf("清理提案审计失败: %v", err)
		}
		if _, err := pool.Exec(ctx, `DELETE FROM proposals WHERE id=$1`, proposalID); err != nil {
			t.Errorf("清理提案失败: %v", err)
		}
		if _, err := pool.Exec(ctx, `DELETE FROM model_catalog_items WHERE source='models.dev' AND provider_id=$1 AND external_id=$2`, providerID, externalID); err != nil {
			t.Errorf("清理目录镜像失败: %v", err)
		}
	})

	rec := httptest.NewRecorder()
	h.ReviewAdminProposal(rec, proposalReq(http.MethodPost, "/api/v1/admin/proposals/x/review", adminSID, `{"decision":"approve"}`), int(proposalID))
	if rec.Code != http.StatusOK {
		t.Fatalf("批准上游模型提案 = %d，想要 200；body=%s", rec.Code, rec.Body.String())
	}

	if err := pool.QueryRow(ctx, `SELECT COALESCE(result_id, 0) FROM proposals WHERE id=$1`, proposalID).Scan(&createdModelID); err != nil {
		t.Fatalf("读 result_id 失败: %v", err)
	}
	if createdModelID == 0 {
		t.Fatalf("提案未记录产出 model id")
	}

	var slug string
	if err := pool.QueryRow(ctx, `SELECT slug FROM models WHERE id=$1`, createdModelID).Scan(&slug); err != nil {
		t.Fatalf("读 slug 失败: %v", err)
	}

	// 验证 model_offerings 认领已落库且 is_official 正确。
	var claimedModelID int64
	var isOfficial bool
	if err := pool.QueryRow(ctx, `
SELECT model_id, is_official FROM model_offerings
WHERE authority='models.dev' AND provider_id=$1 AND external_id=$2`, providerID, externalID).Scan(&claimedModelID, &isOfficial); err != nil {
		t.Fatalf("未找到认领行: %v", err)
	}
	if claimedModelID != createdModelID || !isOfficial {
		t.Fatalf("认领行不对：model_id=%d (want %d) is_official=%v", claimedModelID, createdModelID, isOfficial)
	}

	// 通过公开模型详情确认：状态为 candidate，来源已派生为 models.dev（上游目录）。
	getRec := httptest.NewRecorder()
	h.GetModel(getRec, httptest.NewRequest(http.MethodGet, "/api/v1/models/"+slug, nil), slug)
	if getRec.Code != http.StatusOK {
		t.Fatalf("读模型详情 = %d，想要 200；body=%s", getRec.Code, getRec.Body.String())
	}
	var profile api.ModelProfile
	if err := json.Unmarshal(getRec.Body.Bytes(), &profile); err != nil {
		t.Fatalf("解析模型详情失败: %v", err)
	}
	if profile.Model.Status != api.ModelStatusCandidate || profile.Model.Source != api.ModelsDev {
		t.Fatalf("模型状态/来源不对：status=%q source=%q，想要 candidate / models.dev", profile.Model.Status, profile.Model.Source)
	}
}

func TestIntegrationApproveNewModelProposalConflictsAndRejects(t *testing.T) {
	h, _, idSvc := newIdempotencyHandler(t)
	pool := integrationPool(t)
	adminSID := mkAdmin(t, pool, idSvc, "pm-conf")
	ctx := context.Background()

	tag := uniq()
	ownerSlug := "it-pm-owner-" + tag
	ownerName := "IT Owner " + tag
	depSlug := "it-pm-dep-" + tag
	depName := "IT Deprecated " + tag
	prov := "it-cprov-" + tag[:8]
	extActive := "ext-active-" + tag
	extDep := "ext-dep-" + tag
	batchName := "IT Batch OK " + tag

	var ownerID, depID int64
	if err := pool.QueryRow(ctx, `INSERT INTO models (slug, name, status, resolution) VALUES ($1, $2, 'active', 'exact') RETURNING id`, ownerSlug, ownerName).Scan(&ownerID); err != nil {
		t.Fatalf("建占坑 active 模型失败: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO models (slug, name, status, resolution, merged_into_id) VALUES ($1, $2, 'deprecated', 'exact', $3) RETURNING id`, depSlug, depName, ownerID).Scan(&depID); err != nil {
		t.Fatalf("建占坑 deprecated 模型失败: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO model_catalog_items (source, provider_id, provider_name, external_id, name)
VALUES ('models.dev', $1, 'Prov', $2, 'Catalog Active'),
       ('models.dev', $1, 'Prov', $3, 'Catalog Dep')`, prov, extActive, extDep); err != nil {
		t.Fatalf("建目录镜像失败: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO model_offerings (model_id, authority, provider_id, provider_name, external_id)
VALUES ($1, 'models.dev', $2, 'Prov', $3),
       ($4, 'models.dev', $2, 'Prov', $5)`, ownerID, prov, extActive, depID, extDep); err != nil {
		t.Fatalf("建认领行失败: %v", err)
	}

	var propIDs []int64
	insertProp := func(payload string) int64 {
		t.Helper()
		var id int64
		if err := pool.QueryRow(ctx, `INSERT INTO proposals (kind, payload, source) VALUES ('new_model', $1::jsonb, 'user') RETURNING id`, payload).Scan(&id); err != nil {
			t.Fatalf("插提案失败: %v", err)
		}
		propIDs = append(propIDs, id)
		return id
	}

	var batchModelID int64
	t.Cleanup(func() {
		if batchModelID != 0 {
			_, _ = pool.Exec(ctx, `DELETE FROM audit_log WHERE entity_type='model' AND entity_id=$1`, batchModelID)
			_, _ = pool.Exec(ctx, `DELETE FROM models WHERE id=$1`, batchModelID)
		}
		for _, id := range propIDs {
			_, _ = pool.Exec(ctx, `DELETE FROM audit_log WHERE entity_type='proposal' AND entity_id=$1`, id)
			_, _ = pool.Exec(ctx, `DELETE FROM proposals WHERE id=$1`, id)
		}
		_, _ = pool.Exec(ctx, `DELETE FROM model_offerings WHERE authority='models.dev' AND provider_id=$1`, prov)
		_, _ = pool.Exec(ctx, `DELETE FROM model_catalog_items WHERE source='models.dev' AND provider_id=$1`, prov)
		_, _ = pool.Exec(ctx, `UPDATE models SET merged_into_id=NULL WHERE id=$1`, depID)
		_, _ = pool.Exec(ctx, `DELETE FROM models WHERE id IN ($1, $2)`, depID, ownerID)
	})

	// 1. 上游三元组已被 active 模型认领 → 409 并指明归属模型 slug，提案仍 pending。
	pClaimedActive := insertProp(`{"name":"Brand New A ` + tag + `","source":"models.dev","provider_id":"` + prov + `","external_id":"` + extActive + `"}`)
	rec := httptest.NewRecorder()
	h.ReviewAdminProposal(rec, proposalReq(http.MethodPost, "/api/v1/admin/proposals/x/review", adminSID, `{"decision":"approve"}`), int(pClaimedActive))
	if rec.Code != http.StatusConflict {
		t.Fatalf("三元组已被 active 认领 = %d，想要 409；body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "/models/"+ownerSlug) {
		t.Fatalf("409 文案应指明认领归属 /models/%s，实际：%s", ownerSlug, rec.Body.String())
	}

	// 2. 上游三元组已被已退役且合并的模型认领 → 409 并指明归宿模型。
	pClaimedDep := insertProp(`{"name":"Brand New B ` + tag + `","source":"models.dev","provider_id":"` + prov + `","external_id":"` + extDep + `"}`)
	rec = httptest.NewRecorder()
	h.ReviewAdminProposal(rec, proposalReq(http.MethodPost, "/api/v1/admin/proposals/x/review", adminSID, `{"decision":"approve"}`), int(pClaimedDep))
	if rec.Code != http.StatusConflict {
		t.Fatalf("三元组已被 deprecated 认领 = %d，想要 409；body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "/models/"+depSlug) || !strings.Contains(rec.Body.String(), "/models/"+ownerSlug) {
		t.Fatalf("409 文案应指明退役认领方与合并归宿，实际：%s", rec.Body.String())
	}

	// 3. 模型名与已有模型冲突 → 409 并复用手动新建的冲突文案。
	pNameClash := insertProp(`{"name":"` + ownerName + `"}`)
	rec = httptest.NewRecorder()
	h.ReviewAdminProposal(rec, proposalReq(http.MethodPost, "/api/v1/admin/proposals/x/review", adminSID, `{"decision":"approve"}`), int(pNameClash))
	if rec.Code != http.StatusConflict {
		t.Fatalf("撞名批准 = %d，想要 409；body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "/models/"+ownerSlug) {
		t.Fatalf("撞名 409 文案应指向 /models/%s，实际：%s", ownerSlug, rec.Body.String())
	}

	// 4. 兜底名 / 目录缺失 / 非法来源 → 422 且提案保持 pending。
	for _, raw := range []string{
		`{"name":"unspecified"}`,
		`{"name":"Missing Cat ` + tag + `","source":"models.dev","provider_id":"` + prov + `","external_id":"does-not-exist"}`,
		`{"name":"Bad Src ` + tag + `","source":"endpoint","provider_id":"https://x","external_id":"m"}`,
	} {
		pid := insertProp(raw)
		r := httptest.NewRecorder()
		h.ReviewAdminProposal(r, proposalReq(http.MethodPost, "/api/v1/admin/proposals/x/review", adminSID, `{"decision":"approve"}`), int(pid))
		if r.Code != http.StatusUnprocessableEntity {
			t.Fatalf("非法提案 %s = %d，想要 422；body=%s", raw, r.Code, r.Body.String())
		}
		var st string
		if err := pool.QueryRow(ctx, `SELECT status FROM proposals WHERE id=$1`, pid).Scan(&st); err != nil || st != "pending" {
			t.Fatalf("失败后提案状态 = %q err=%v，必须仍是 pending", st, err)
		}
	}

	// 5. 批量批准：1 条合法 + 1 条认领冲突 → 200，前者 ok=true，后者 ok=false 且含归属 slug。
	pBatchOK := insertProp(`{"name":"` + batchName + `"}`)
	batchBody, _ := json.Marshal(map[string]any{
		"ids":      []int{int(pBatchOK), int(pClaimedActive)},
		"decision": "approve",
	})
	bRec := httptest.NewRecorder()
	h.BatchReviewAdminProposals(bRec, proposalReq(http.MethodPost, "/api/v1/admin/proposals/batch", adminSID, string(batchBody)))
	if bRec.Code != http.StatusOK {
		t.Fatalf("批量批准 = %d，想要 200；body=%s", bRec.Code, bRec.Body.String())
	}
	var bRes api.ProposalBatchResult
	if err := json.Unmarshal(bRec.Body.Bytes(), &bRes); err != nil {
		t.Fatalf("解析批量结果失败: %v", err)
	}
	if len(bRes.Results) != 2 || !bRes.Results[0].Ok || bRes.Results[1].Ok ||
		bRes.Results[1].Error == nil || !strings.Contains(*bRes.Results[1].Error, "/models/"+ownerSlug) {
		t.Fatalf("批量逐条结果不对：%+v", bRes.Results)
	}
	_ = pool.QueryRow(ctx, `SELECT COALESCE(result_id, 0) FROM proposals WHERE id=$1`, pBatchOK).Scan(&batchModelID)
}
