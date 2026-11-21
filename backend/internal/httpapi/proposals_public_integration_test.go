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

	// 6) 枚举外的方向标签。
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
		_, _ = pool.Exec(context.Background(), "DELETE FROM proposals WHERE dedupe_key = $1", "swe-benchverified")
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
	if err := pool.QueryRow(context.Background(), "SELECT count(*) FROM proposals WHERE dedupe_key = $1 AND status = 'pending'", "swe-benchverified").Scan(&rows); err != nil {
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
		_, _ = pool.Exec(context.Background(), "DELETE FROM proposals WHERE dedupe_key = $1", "swe-benchverified")
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
