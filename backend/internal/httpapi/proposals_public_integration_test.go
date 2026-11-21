package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/api"
)

// 提案提交与撤回（#524 / 实体域 §4.9）。
//
// 这里钉的是**入口的三条边界**：谁能提（登录）、能提什么（白名单）、重复提交会怎样
// （幂等 202 而不是堆两行）。审核与执行器不在这里 —— 那是 review 链路与 #525。

// 一份形状正确的提交；下面的用例只改其中一两处。
const validBenchmarkProposal = `{"kind":"new_benchmark","payload":{"name":"SWE-bench Verified","publisher":"OpenAI","homepage_url":"https://www.swebench.com/","description":"真实 GitHub issue 的修复率","category":"coding"}}`

func proposalPublicReq(method, path, sid, body string) *http.Request {
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, path, nil)
	} else {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	}
	if sid != "" {
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: sid})
	}
	return req
}

func submitProposal(t *testing.T, h *Handler, sid, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.SubmitProposal(rec, proposalPublicReq(http.MethodPost, "/api/v1/proposals", sid, body))
	return rec
}

func withdrawProposal(t *testing.T, h *Handler, sid string, id int) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	path := "/api/v1/proposals/" + strconv.Itoa(id) + "/withdraw"
	h.WithdrawProposal(rec, proposalPublicReq(http.MethodPost, path, sid, ""), id)
	return rec
}

func decodeProposalJSON[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	if rec.Code != http.StatusAccepted && rec.Code != http.StatusOK {
		t.Fatalf("状态 = %d，想要 202/200（body=%s）", rec.Code, rec.Body.String())
	}
	var out T
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("响应不是合法 JSON: %v（body=%s）", err, rec.Body.String())
	}
	return out
}

// TestIntegrationSubmitProposalRejects 钉住入口的拒绝边界。
//
// 每条各有各的理由：未登录是权限边界；白名单外的 kind 回了 202 就等于承诺有人会看；
// payload 形状错了要在**提交时**说清，而不是让审核员看到一条没法处理的行。
func TestIntegrationSubmitProposalRejects(t *testing.T) {
	h, idSvc, pool := newSortHandler(t)
	author, sid := registerVerified(t, pool, idSvc, registerInputFor(t, "prop-rej"))
	t.Cleanup(func() { cleanupAuthor(t, pool, author.Sub) })

	// 1) 未登录。
	if rec := submitProposal(t, h, "", validBenchmarkProposal); rec.Code != http.StatusUnauthorized {
		t.Fatalf("未登录提交 = %d，想要 401：%s", rec.Code, rec.Body.String())
	}

	// 2) 白名单之外的 kind：422，而不是收下来堆着。
	report := `{"kind":"report","payload":{"target_type":"demo","target_id":1}}`
	if rec := submitProposal(t, h, sid, report); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("report kind = %d，想要 422：%s", rec.Code, rec.Body.String())
	}

	// 3) 缺必填字段。
	missingName := `{"kind":"new_benchmark","payload":{"publisher":"OpenAI"}}`
	if rec := submitProposal(t, h, sid, missingName); rec.Code != http.StatusBadRequest {
		t.Fatalf("缺 name = %d，想要 400：%s", rec.Code, rec.Body.String())
	}

	// 4) 不认识的字段：静默忽略会让提交人以为它被记下了。
	unknown := `{"kind":"new_benchmark","payload":{"name":"X","scores":[1,2]}}`
	if rec := submitProposal(t, h, sid, unknown); rec.Code != http.StatusBadRequest {
		t.Fatalf("多余的 scores 字段 = %d，想要 400：%s", rec.Code, rec.Body.String())
	}

	// 5) 非 http(s) 的地址会被渲染成页面上的链接，直接拒。
	badURL := `{"kind":"new_benchmark","payload":{"name":"X","homepage_url":"javascript:alert(1)"}}`
	if rec := submitProposal(t, h, sid, badURL); rec.Code != http.StatusBadRequest {
		t.Fatalf("javascript: 地址 = %d，想要 400：%s", rec.Code, rec.Body.String())
	}

	// 6) 可选字段超长：必须报错，不能静默丢弃 —— 202 会让提交人以为它入库了。
	overlongPublisher := `{"kind":"new_benchmark","payload":{"name":"X","publisher":"ppppppppppppppppppppppppppppppppppppppppppppppppppppppppppppppppp"}}`
	if rec := submitProposal(t, h, sid, overlongPublisher); rec.Code != http.StatusBadRequest {
		t.Fatalf("发布方超长 = %d，想要 400：%s", rec.Code, rec.Body.String())
	}

	// 7) 可选字段类型不对：同上，静默忽略等于把它悄悄删掉。
	wrongType := `{"kind":"new_benchmark","payload":{"name":"X","publisher":123}}`
	if rec := submitProposal(t, h, sid, wrongType); rec.Code != http.StatusBadRequest {
		t.Fatalf("发布方不是字符串 = %d，想要 400：%s", rec.Code, rec.Body.String())
	}

	// 8) 枚举外的方向标签。
	badCategory := `{"kind":"new_benchmark","payload":{"name":"X","category":"vibes"}}`
	if rec := submitProposal(t, h, sid, badCategory); rec.Code != http.StatusBadRequest {
		t.Fatalf("枚举外的 category = %d，想要 400：%s", rec.Code, rec.Body.String())
	}

	var pending int
	if err := pool.QueryRow(context.Background(), "SELECT count(*) FROM proposals WHERE kind = 'new_benchmark' AND status = 'pending'").Scan(&pending); err != nil {
		t.Fatalf("查 pending 提案失败: %v", err)
	}
	if pending != 0 {
		t.Fatalf("被拒的提交不该落库，实际有 %d 行", pending)
	}
}

// TestIntegrationSubmitProposalDedupes 同一个榜名只留一条 pending。
func TestIntegrationSubmitProposalDedupes(t *testing.T) {
	h, idSvc, pool := newSortHandler(t)
	author, sid := registerVerified(t, pool, idSvc, registerInputFor(t, "prop-dup"))
	t.Cleanup(func() { cleanupAuthor(t, pool, author.Sub) })
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM proposals WHERE dedupe_key = $1", "swe-bench-verified")
	})

	first := decodeProposalJSON[api.ProposalSubmissionResult](t, submitProposal(t, h, sid, validBenchmarkProposal))
	if !first.Created {
		t.Fatalf("第一次提交 created = false，想要 true")
	}

	second := decodeProposalJSON[api.ProposalSubmissionResult](t, submitProposal(t, h, sid, validBenchmarkProposal))
	if second.Created {
		t.Fatalf("重复提交 created = true，想要 false（幂等，不是新建）")
	}
	if second.Id != first.Id {
		t.Fatalf("重复提交 id = %d，想要原 id %d", second.Id, first.Id)
	}

	var rows int
	if err := pool.QueryRow(context.Background(), "SELECT count(*) FROM proposals WHERE dedupe_key = $1 AND status = 'pending'", "swe-bench-verified").Scan(&rows); err != nil {
		t.Fatalf("查提案行数失败: %v", err)
	}
	if rows != 1 {
		t.Fatalf("pending 行数 = %d，想要 1", rows)
	}

	// 撤回之后可以重新提交：去重只看 pending。
	if rec := withdrawProposal(t, h, sid, first.Id); rec.Code != http.StatusOK {
		t.Fatalf("撤回 = %d，想要 200：%s", rec.Code, rec.Body.String())
	}
	third := decodeProposalJSON[api.ProposalSubmissionResult](t, submitProposal(t, h, sid, validBenchmarkProposal))
	if !third.Created || third.Id == first.Id {
		t.Fatalf("撤回后再提 = %+v，想要新建一行", third)
	}
}

// TestIntegrationWithdrawProposal 撤回只对自己的 pending 生效。
func TestIntegrationWithdrawProposal(t *testing.T) {
	h, idSvc, pool := newSortHandler(t)
	owner, ownerSID := registerVerified(t, pool, idSvc, registerInputFor(t, "prop-own"))
	t.Cleanup(func() { cleanupAuthor(t, pool, owner.Sub) })
	other, otherSID := registerVerified(t, pool, idSvc, registerInputFor(t, "prop-oth"))
	t.Cleanup(func() { cleanupAuthor(t, pool, other.Sub) })
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM proposals WHERE dedupe_key = $1", "swe-bench-verified")
	})

	created := decodeProposalJSON[api.ProposalSubmissionResult](t, submitProposal(t, h, ownerSID, validBenchmarkProposal))

	// 别人的提案：404，不承认存在。
	if rec := withdrawProposal(t, h, otherSID, created.Id); rec.Code != http.StatusNotFound {
		t.Fatalf("撤回别人的提案 = %d，想要 404：%s", rec.Code, rec.Body.String())
	}

	// 不存在：404。
	if rec := withdrawProposal(t, h, ownerSID, 999999); rec.Code != http.StatusNotFound {
		t.Fatalf("撤回不存在的提案 = %d，想要 404：%s", rec.Code, rec.Body.String())
	}

	// 自己的：200 + withdrawn。
	withdrawn := decodeProposalJSON[api.ProposalWithdrawn](t, withdrawProposal(t, h, ownerSID, created.Id))
	if withdrawn.Status != api.ProposalWithdrawnStatus("withdrawn") {
		t.Fatalf("撤回后状态 = %q，想要 withdrawn", withdrawn.Status)
	}

	// 再撤一次：已经不在 pending，409 而不是 404 —— 这条提案确实存在，只是走过了。
	if rec := withdrawProposal(t, h, ownerSID, created.Id); rec.Code != http.StatusConflict {
		t.Fatalf("重复撤回 = %d，想要 409：%s", rec.Code, rec.Body.String())
	}

	var status string
	if err := pool.QueryRow(context.Background(), "SELECT status FROM proposals WHERE id = $1", created.Id).Scan(&status); err != nil {
		t.Fatalf("查提案状态失败: %v", err)
	}
	if status != "withdrawn" {
		t.Fatalf("库里状态 = %q，想要 withdrawn", status)
	}
}

// TestIntegrationSubmitBenchmarkCollision 目录里已有的榜不再收提案。
//
// 判据是「这个榜已经在目录里」而不是「有人提过了」：前者再收一条只会让审核员去裁决
// 一件已经成立的事，而 409 的文案必须直接给出已有条目的地址，否则提交人只能猜。
func TestIntegrationSubmitBenchmarkCollision(t *testing.T) {
	h, idSvc, pool := newSortHandler(t)
	author, sid := registerVerified(t, pool, idSvc, registerInputFor(t, "bench-clash"))
	t.Cleanup(func() { cleanupAuthor(t, pool, author.Sub) })

	var id int64
	if err := pool.QueryRow(context.Background(),
		"INSERT INTO benchmarks (slug, name) VALUES ($1, $2) RETURNING id",
		"swe-bench-verified", "SWE-bench Verified").Scan(&id); err != nil {
		t.Fatalf("预置基准失败: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM benchmarks WHERE id = $1", id) })

	rec := submitProposal(t, h, sid, validBenchmarkProposal)
	if rec.Code != http.StatusConflict {
		t.Fatalf("撞车提交 = %d，想要 409：%s", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, "/benchmarks/swe-bench-verified") {
		t.Fatalf("409 文案没指向已有条目：%s", body)
	}

	var count int
	if err := pool.QueryRow(context.Background(),
		"SELECT count(*) FROM proposals WHERE dedupe_key = $1", "swe-bench-verified").Scan(&count); err != nil {
		t.Fatalf("查提案失败: %v", err)
	}
	if count != 0 {
		t.Fatalf("撞车的提交不该落库，实际 %d 行", count)
	}
}

// TestIntegrationSubmitProposalRateLimited 同一账号 24 小时内有上限。
//
// 重复提交走的是幂等 202（不新建行，也就不该吃配额），所以这条用例故意用**第二个不同的**
// 榜名来撞限额 —— 否则验的就不是频控而是去重。
func TestIntegrationSubmitProposalRateLimited(t *testing.T) {
	h, idSvc, pool := newSortHandler(t)
	author, sid := registerVerified(t, pool, idSvc, registerInputFor(t, "bench-rate"))
	t.Cleanup(func() { cleanupAuthor(t, pool, author.Sub) })
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM site_settings WHERE key = $1", "proposal.max_per_day")
		_, _ = pool.Exec(context.Background(), "DELETE FROM proposals WHERE dedupe_key IN ($1, $2)", "swe-bench-verified", "another-board")
	})

	if _, err := pool.Exec(context.Background(), `
INSERT INTO site_settings (key, scope, value_type, value)
VALUES ('proposal.max_per_day', 'site', 'number', '1'::jsonb)
ON CONFLICT (key) DO UPDATE SET value = '1'::jsonb`); err != nil {
		t.Fatalf("压低上限失败: %v", err)
	}

	first := submitProposal(t, h, sid, validBenchmarkProposal)
	if first.Code != http.StatusAccepted {
		t.Fatalf("第一条 = %d，想要 202：%s", first.Code, first.Body.String())
	}

	// 同一条内容再提一次：去重优先，仍然是 202 —— 不新建行，也就不该被判超额。
	again := submitProposal(t, h, sid, validBenchmarkProposal)
	if again.Code != http.StatusAccepted {
		t.Fatalf("重复提交 = %d，想要 202（去重先于频控）：%s", again.Code, again.Body.String())
	}

	// 换一个榜名：这才真的会新建行，于是撞上限。
	other := submitProposal(t, h, sid, `{"kind":"new_benchmark","payload":{"name":"Another Board"}}`)
	if other.Code != http.StatusTooManyRequests {
		t.Fatalf("超额提交 = %d，想要 429：%s", other.Code, other.Body.String())
	}
}

// TestIntegrationApproveNewBenchmarkProposal 通过之后落成 benchmarks 一行。
func TestIntegrationApproveNewBenchmarkProposal(t *testing.T) {
	h, idSvc, pool := newSortHandler(t)
	author, sid := registerVerified(t, pool, idSvc, registerInputFor(t, "bench-ok"))
	t.Cleanup(func() { cleanupAuthor(t, pool, author.Sub) })
	admin := mkAdmin(t, pool, idSvc, "bench-adm")
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM benchmarks WHERE slug = $1", "swe-bench-verified")
	})

	created := decodeProposalJSON[api.ProposalSubmissionResult](t, submitProposal(t, h, sid, validBenchmarkProposal))

	rec := httptest.NewRecorder()
	h.ReviewAdminProposal(rec, proposalReq(http.MethodPost, "/api/v1/admin/proposals/x/review", admin,
		`{"decision":"approve"}`), created.Id)
	if rec.Code != http.StatusOK {
		t.Fatalf("通过基准提案 = %d，想要 200（body=%s）", rec.Code, rec.Body.String())
	}

	var status, resultKind string
	var resultID int64
	if err := pool.QueryRow(context.Background(),
		`SELECT status, COALESCE(result_kind,''), COALESCE(result_id,0) FROM proposals WHERE id=$1`, created.Id).
		Scan(&status, &resultKind, &resultID); err != nil {
		t.Fatalf("读提案失败: %v", err)
	}
	if status != "approved" || resultKind != "benchmark" || resultID == 0 {
		t.Fatalf("提案 = %s / %s / %d，想要 approved / benchmark / 非零", status, resultKind, resultID)
	}

	var slug, name, publisher, homepage, category, description string
	if err := pool.QueryRow(context.Background(),
		`SELECT slug, name, publisher, homepage_url, category, description FROM benchmarks WHERE id=$1`, resultID).
		Scan(&slug, &name, &publisher, &homepage, &category, &description); err != nil {
		t.Fatalf("基准条目未落库: %v", err)
	}
	if slug != "swe-bench-verified" || name != "SWE-bench Verified" || publisher != "OpenAI" || category != "coding" {
		t.Fatalf("条目 = %q / %q / %q / %q", slug, name, publisher, category)
	}
	if homepage == "" || description == "" {
		t.Fatalf("主页或简介丢了: %q / %q", homepage, description)
	}
}
