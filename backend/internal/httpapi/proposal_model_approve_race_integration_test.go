package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/api"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/identity"
)

// new_model 提案批准的统一入口、权限边界与并发抢占回归（#593）：
//
//  1. 统一裁决入口 POST /api/v1/admin/reviews/proposal/{id}：
//     - 未登录 401、无 proposal.review 权限 403；
//     - moderator（仅持 proposal.review，无 model.create / model.govern）可批准并建出 candidate 模型；
//     - 非法 payload（兜底名）经统一入口同样回 422，提案保持 pending；
//  2. 并发竞态（go test -race）：
//     - 同提案并发两次批准：恰有 1 胜（200）1 负（409），仅 1 行模型与各 1 条审计；
//     - 两条不同提案并发抢同一上游三元组：恰有 1 胜（200）1 负（409 带胜出模型 slug），
//       败方事务整笔回滚（无半截模型、无孤儿审计、提案保持 pending）。

func TestIntegrationApproveNewModelProposalUnifiedAndAuth(t *testing.T) {
	h, _, idSvc := newIdempotencyHandler(t)
	pool := integrationPool(t)
	ctx := context.Background()
	tag := uniq()

	plain, plainSID := registerVerified(t, pool, idSvc, identity.RegisterInput{
		Email:       "it-pm-plain-" + tag + "@example.com",
		Password:    "correct-horse-battery",
		Username:    "it-pm-plain-" + tag[:8],
		DisplayName: "普通用户",
	})
	mod, modSID := registerVerified(t, pool, idSvc, identity.RegisterInput{
		Email:       "it-pm-mod-" + tag + "@example.com",
		Password:    "correct-horse-battery",
		Username:    "it-pm-mod-" + tag[:8],
		DisplayName: "提案审核员",
	})
	if _, err := pool.Exec(ctx, `UPDATE accounts SET role = 'moderator' WHERE sub = $1`, mod.Sub); err != nil {
		t.Fatalf("设置 moderator 失败: %v", err)
	}
	t.Cleanup(func() {
		cleanupAuthor(t, pool, plain.Sub)
		cleanupAuthor(t, pool, mod.Sub)
	})

	modelName := "IT Unified Mod Model " + tag
	var okPropID, badPropID int64
	if err := pool.QueryRow(ctx, `
INSERT INTO proposals (kind, payload, source)
VALUES ('new_model', jsonb_build_object('name', $1::text, 'vendor', '审校厂商'), 'user')
RETURNING id`, modelName).Scan(&okPropID); err != nil {
		t.Fatalf("插入合法模型提案失败: %v", err)
	}
	if err := pool.QueryRow(ctx, `
INSERT INTO proposals (kind, payload, source)
VALUES ('new_model', '{"name":"unspecified"}'::jsonb, 'user')
RETURNING id`).Scan(&badPropID); err != nil {
		t.Fatalf("插入兜底名提案失败: %v", err)
	}

	var createdModelID int64
	// LIFO：比 cleanupAuthor 先执行，确保 proposals/models 先清理干净。
	t.Cleanup(func() {
		if createdModelID != 0 {
			if _, err := pool.Exec(ctx, `DELETE FROM audit_log WHERE entity_type='model' AND entity_id=$1`, createdModelID); err != nil {
				t.Errorf("清理模型审计失败: %v", err)
			}
			if _, err := pool.Exec(ctx, `DELETE FROM models WHERE id=$1`, createdModelID); err != nil {
				t.Errorf("清理候选模型失败: %v", err)
			}
		}
		for _, pid := range []int64{okPropID, badPropID} {
			if _, err := pool.Exec(ctx, `DELETE FROM audit_log WHERE entity_type='proposal' AND entity_id=$1`, pid); err != nil {
				t.Errorf("清理提案审计 %d 失败: %v", pid, err)
			}
			if _, err := pool.Exec(ctx, `DELETE FROM proposals WHERE id=$1`, pid); err != nil {
				t.Errorf("清理提案 %d 失败: %v", pid, err)
			}
		}
	})

	okIDStr := strconv.FormatInt(okPropID, 10)
	badIDStr := strconv.FormatInt(badPropID, 10)
	batchPayload := `{"ids":[` + okIDStr + `],"decision":"approve"}`

	// 未登录 → 401；普通用户 → 403（覆盖统一入口、单条入口与批量入口）。
	for _, tc := range []struct {
		name string
		sid  string
		want int
	}{
		{"未登录", "", http.StatusUnauthorized},
		{"普通用户", plainSID, http.StatusForbidden},
	} {
		r1 := httptest.NewRecorder()
		h.ReviewEntity(r1, unifiedReviewReq(tc.sid, okIDStr, `{"decision":"approve"}`), api.ReviewEntityParamsKindProposal, okIDStr)
		if r1.Code != tc.want {
			t.Fatalf("%s 统一入口 = %d，想要 %d", tc.name, r1.Code, tc.want)
		}
		r2 := httptest.NewRecorder()
		h.ReviewAdminProposal(r2, proposalReq(http.MethodPost, "/api/v1/admin/proposals/x/review", tc.sid, `{"decision":"approve"}`), int(okPropID))
		if r2.Code != tc.want {
			t.Fatalf("%s 单条入口 = %d，想要 %d", tc.name, r2.Code, tc.want)
		}
		r3 := httptest.NewRecorder()
		h.BatchReviewAdminProposals(r3, proposalReq(http.MethodPost, "/api/v1/admin/proposals/batch", tc.sid, batchPayload))
		if r3.Code != tc.want {
			t.Fatalf("%s 批量入口 = %d，想要 %d", tc.name, r3.Code, tc.want)
		}
	}

	// 兜底名提案走统一入口 → 422 且保持 pending。
	badRec := httptest.NewRecorder()
	h.ReviewEntity(badRec, unifiedReviewReq(modSID, badIDStr, `{"decision":"approve"}`), api.ReviewEntityParamsKindProposal, badIDStr)
	if badRec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("统一入口兜底名 = %d，想要 422；body=%s", badRec.Code, badRec.Body.String())
	}

	// moderator 通过统一入口批准合法 new_model 提案 → 200，产出 candidate 模型与双审计。
	okRec := httptest.NewRecorder()
	h.ReviewEntity(okRec, unifiedReviewReq(modSID, okIDStr, `{"decision":"approve"}`), api.ReviewEntityParamsKindProposal, okIDStr)
	if err := pool.QueryRow(ctx, `SELECT COALESCE(result_id, 0) FROM proposals WHERE id=$1`, okPropID).Scan(&createdModelID); err != nil {
		t.Fatalf("读统一入口产出 model_id 失败: %v", err)
	}
	if okRec.Code != http.StatusOK {
		t.Fatalf("moderator 统一入口批准 = %d，想要 200；body=%s", okRec.Code, okRec.Body.String())
	}
	var out api.ReviewResult
	if err := json.Unmarshal(okRec.Body.Bytes(), &out); err != nil {
		t.Fatalf("解析统一入口结果失败: %v", err)
	}
	if out.Slug != okIDStr || out.ModerationState != api.ReviewResultModerationStateApproved ||
		out.Kind == nil || *out.Kind != api.ReviewResultKindProposal {
		t.Fatalf("统一入口响应不对：%+v", out)
	}
	if createdModelID == 0 {
		t.Fatalf("统一入口批准后未回填 result_id")
	}
	var status string
	if err := pool.QueryRow(ctx, `SELECT status FROM models WHERE id=$1`, createdModelID).Scan(&status); err != nil || status != "candidate" {
		t.Fatalf("moderator 建出的模型状态 = %q err=%v，想要 candidate", status, err)
	}
}

func TestIntegrationApproveNewModelProposalConcurrentRace(t *testing.T) {
	h, _, idSvc := newIdempotencyHandler(t)
	pool := integrationPool(t)
	adminSID := mkAdmin(t, pool, idSvc, "pm-race")
	ctx := context.Background()

	t.Run("same_proposal", func(t *testing.T) {
		tag := uniq()
		name := "IT Race Same Prop " + tag
		var pid int64
		if err := pool.QueryRow(ctx, `
INSERT INTO proposals (kind, payload, source)
VALUES ('new_model', jsonb_build_object('name', $1::text), 'user')
RETURNING id`, name).Scan(&pid); err != nil {
			t.Fatalf("插并发提案失败: %v", err)
		}
		t.Cleanup(func() {
			if _, err := pool.Exec(ctx, `DELETE FROM audit_log WHERE entity_type='model' AND entity_id IN (SELECT id FROM models WHERE name=$1)`, name); err != nil {
				t.Errorf("清理模型审计失败: %v", err)
			}
			if _, err := pool.Exec(ctx, `DELETE FROM models WHERE name=$1`, name); err != nil {
				t.Errorf("清理并发模型失败: %v", err)
			}
			if _, err := pool.Exec(ctx, `DELETE FROM audit_log WHERE entity_type='proposal' AND entity_id=$1`, pid); err != nil {
				t.Errorf("清理提案审计失败: %v", err)
			}
			if _, err := pool.Exec(ctx, `DELETE FROM proposals WHERE id=$1`, pid); err != nil {
				t.Errorf("清理提案失败: %v", err)
			}
		})

		codes := make([]int, 2)
		start := make(chan struct{})
		var wg sync.WaitGroup
		for i := range 2 {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				<-start
				rec := httptest.NewRecorder()
				h.ReviewAdminProposal(rec, proposalReq(http.MethodPost, "/api/v1/admin/proposals/x/review", adminSID, `{"decision":"approve"}`), int(pid))
				codes[idx] = rec.Code
			}(i)
		}
		close(start)
		wg.Wait()

		okCount, conflictCount := 0, 0
		for _, c := range codes {
			switch c {
			case http.StatusOK:
				okCount++
			case http.StatusConflict:
				conflictCount++
			}
		}
		if okCount != 1 || conflictCount != 1 {
			t.Fatalf("同提案并发批准状态码 = %v，想要 1 个 200 与 1 个 409", codes)
		}
		var modelCount, auditCount int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM models WHERE name=$1`, name).Scan(&modelCount); err != nil || modelCount != 1 {
			t.Fatalf("并发后模型行数 = %d err=%v，想要 1", modelCount, err)
		}
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE entity_type='proposal' AND entity_id=$1 AND action='review'`, pid).Scan(&auditCount); err != nil || auditCount != 1 {
			t.Fatalf("并发后提案审计行数 = %d err=%v，想要 1", auditCount, err)
		}
	})

	t.Run("same_upstream_triple", func(t *testing.T) {
		tag := uniq()
		prov := "it-rprov-" + tag[:8]
		ext := "it-rext-" + tag
		nameA := "IT Race Triple A " + tag
		nameB := "IT Race Triple B " + tag

		if _, err := pool.Exec(ctx, `
INSERT INTO model_catalog_items (source, provider_id, provider_name, external_id, name)
VALUES ('models.dev', $1, 'Race Prov', $2, 'Race Catalog Model')`, prov, ext); err != nil {
			t.Fatalf("建竞态目录镜像失败: %v", err)
		}
		var pidA, pidB int64
		if err := pool.QueryRow(ctx, `
INSERT INTO proposals (kind, payload, source)
VALUES ('new_model', jsonb_build_object('name', $1::text, 'source', 'models.dev', 'provider_id', $2::text, 'external_id', $3::text), 'user')
RETURNING id`, nameA, prov, ext).Scan(&pidA); err != nil {
			t.Fatalf("插竞态提案 A 失败: %v", err)
		}
		if err := pool.QueryRow(ctx, `
INSERT INTO proposals (kind, payload, source)
VALUES ('new_model', jsonb_build_object('name', $1::text, 'source', 'models.dev', 'provider_id', $2::text, 'external_id', $3::text), 'user')
RETURNING id`, nameB, prov, ext).Scan(&pidB); err != nil {
			t.Fatalf("插竞态提案 B 失败: %v", err)
		}
		t.Cleanup(func() {
			if _, err := pool.Exec(ctx, `DELETE FROM model_offerings WHERE authority='models.dev' AND provider_id=$1 AND external_id=$2`, prov, ext); err != nil {
				t.Errorf("清理竞态认领行失败: %v", err)
			}
			if _, err := pool.Exec(ctx, `DELETE FROM audit_log WHERE entity_type='model' AND entity_id IN (SELECT id FROM models WHERE name IN ($1, $2))`, nameA, nameB); err != nil {
				t.Errorf("清理竞态模型审计失败: %v", err)
			}
			if _, err := pool.Exec(ctx, `DELETE FROM models WHERE name IN ($1, $2)`, nameA, nameB); err != nil {
				t.Errorf("清理竞态模型失败: %v", err)
			}
			if _, err := pool.Exec(ctx, `DELETE FROM audit_log WHERE entity_type='proposal' AND entity_id IN ($1, $2)`, pidA, pidB); err != nil {
				t.Errorf("清理竞态提案审计失败: %v", err)
			}
			if _, err := pool.Exec(ctx, `DELETE FROM proposals WHERE id IN ($1, $2)`, pidA, pidB); err != nil {
				t.Errorf("清理竞态提案失败: %v", err)
			}
			if _, err := pool.Exec(ctx, `DELETE FROM model_catalog_items WHERE source='models.dev' AND provider_id=$1 AND external_id=$2`, prov, ext); err != nil {
				t.Errorf("清理竞态目录镜像失败: %v", err)
			}
		})

		type result struct {
			code int
			body string
		}
		res := make([]result, 2)
		pids := []int64{pidA, pidB}
		start := make(chan struct{})
		var wg sync.WaitGroup
		for i := range 2 {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				<-start
				rec := httptest.NewRecorder()
				h.ReviewAdminProposal(rec, proposalReq(http.MethodPost, "/api/v1/admin/proposals/x/review", adminSID, `{"decision":"approve"}`), int(pids[idx]))
				res[idx] = result{code: rec.Code, body: rec.Body.String()}
			}(i)
		}
		close(start)
		wg.Wait()

		okCount, conflictCount := 0, 0
		var conflictBody string
		for _, r := range res {
			switch r.code {
			case http.StatusOK:
				okCount++
			case http.StatusConflict:
				conflictCount++
				conflictBody = r.body
			}
		}
		if okCount != 1 || conflictCount != 1 {
			t.Fatalf("并发抢占同一三元组结果 = %+v，想要 1 个 200 与 1 个 409", res)
		}

		// 验证只有 1 行模型、1 条认领、1 条 model:create 审计，且败方提案仍 pending。
		var winnerID int64
		var winnerSlug string
		var modelCount int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM models WHERE name IN ($1, $2)`, nameA, nameB).Scan(&modelCount); err != nil || modelCount != 1 {
			t.Fatalf("败方未整笔回滚：models 行数 = %d err=%v，想要 1", modelCount, err)
		}
		if err := pool.QueryRow(ctx, `SELECT id, slug FROM models WHERE name IN ($1, $2)`, nameA, nameB).Scan(&winnerID, &winnerSlug); err != nil {
			t.Fatalf("读胜出模型失败: %v", err)
		}
		if !strings.Contains(conflictBody, "/models/"+winnerSlug) {
			t.Fatalf("败方 409 文案应带胜出模型归属 /models/%s，实际：%s", winnerSlug, conflictBody)
		}
		var offeringModelID int64
		if err := pool.QueryRow(ctx, `SELECT model_id FROM model_offerings WHERE authority='models.dev' AND provider_id=$1 AND external_id=$2`, prov, ext).Scan(&offeringModelID); err != nil || offeringModelID != winnerID {
			t.Fatalf("认领归属 = %d err=%v，想要 %d", offeringModelID, err, winnerID)
		}
		var pendingCount, approvedCount int
		if err := pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE status='pending'), count(*) FILTER (WHERE status='approved' AND result_id=$3) FROM proposals WHERE id IN ($1, $2)`, pidA, pidB, winnerID).Scan(&pendingCount, &approvedCount); err != nil || pendingCount != 1 || approvedCount != 1 {
			t.Fatalf("提案状态分布不对：pending=%d approved=%d err=%v，想要各 1", pendingCount, approvedCount, err)
		}
		var modelAuditCount, propAuditCount int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE entity_type='model' AND entity_id=$1 AND action='create'`, winnerID).Scan(&modelAuditCount); err != nil || modelAuditCount != 1 {
			t.Fatalf("胜出模型 model:create 审计条数 = %d err=%v，想要 1", modelAuditCount, err)
		}
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE entity_type='proposal' AND entity_id IN ($1, $2) AND action='review'`, pidA, pidB).Scan(&propAuditCount); err != nil || propAuditCount != 1 {
			t.Fatalf("竞态提案 proposal:review 审计条数 = %d err=%v，想要 1", propAuditCount, err)
		}
	})
}
