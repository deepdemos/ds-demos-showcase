package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/identity"
)

// uniq 给每次运行造一个唯一后缀。
//
// 固定名字的用例会自我毒化：第一次跑留下的行让第二次在建模型那步撞
// models_name_key 唯一约束，而失败信息看起来像实现坏了。这个坑在本仓库踩过两次
// （模型导入的「未收录名字」与这批用例的第一版）。
func uniq() string {
	return strconv.FormatInt(time.Now().UnixNano(), 36)
}

// postBatch 发一次批量导入。
func postBatch(t *testing.T, h *Handler, sid string, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/models/import/apply-batch",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if sid != "" {
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: sid})
	}
	rec := httptest.NewRecorder()
	NewRouter(h, nil, nil).ServeHTTP(rec, req)
	return rec
}

type batchResultItem struct {
	ProviderId string `json:"providerId"`
	ExternalId string `json:"externalId"`
	Status     string `json:"status"`
	Model      *struct {
		Slug   string `json:"slug"`
		Name   string `json:"name"`
		Status string `json:"status"`
	} `json:"model"`
	Error *string `json:"error"`
}

type batchResult struct {
	Results []batchResultItem `json:"results"`
}

func decodeBatch(t *testing.T, rec *httptest.ResponseRecorder) batchResult {
	t.Helper()
	var got batchResult
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("响应不是合法 JSON: %v（body=%s）", err, rec.Body.String())
	}
	return got
}

func errOf(it batchResultItem) string {
	if it.Error == nil {
		return ""
	}
	return *it.Error
}

// 三条一起导：各自新建，结果顺序与请求一致，库里三行 active。
func TestIntegrationBatchImportCreatesAll(t *testing.T) {
	h, idSvc, pool := newModelTestHandler(t)
	sid := mkAdmin(t, pool, idSvc, "batch1")

	u := uniq()
	names := []string{"批一-" + u, "批二-" + u, "批三-" + u}
	keys := []string{"it-b1-a-" + u, "it-b1-b-" + u, "it-b1-c-" + u}
	for i := range keys {
		putCatalogItem(t, pool, "it-prov", keys[i], names[i], nil)
	}

	body := fmt.Sprintf(`{"items":[
		{"providerId":"it-prov","externalId":"%s","fields":["name"]},
		{"providerId":"it-prov","externalId":"%s","fields":["name"]},
		{"providerId":"it-prov","externalId":"%s","fields":["name"]}
	]}`, keys[0], keys[1], keys[2])
	rec := postBatch(t, h, sid, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("批量导入应回 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	got := decodeBatch(t, rec)
	if len(got.Results) != 3 {
		t.Fatalf("结果条数 = %d，想要 3", len(got.Results))
	}
	for i := range got.Results {
		if got.Results[i].ExternalId != keys[i] {
			t.Fatalf("第 %d 条 = %q，想要 %q（结果顺序必须与请求一致）",
				i, got.Results[i].ExternalId, keys[i])
		}
		if got.Results[i].Status != "created" {
			t.Fatalf("%s 的状态 = %q，想要 created（%s）", keys[i], got.Results[i].Status, errOf(got.Results[i]))
		}
		if got.Results[i].Model == nil || got.Results[i].Model.Status != "active" {
			t.Fatalf("%s 应当带上新建的模型且 status=active", keys[i])
		}
	}

	var rows int64
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM models WHERE name = ANY($1) AND status = 'active'`, names).Scan(&rows); err != nil {
		t.Fatalf("查模型失败: %v", err)
	}
	if rows != 3 {
		t.Fatalf("落库模型数 = %d，想要 3", rows)
	}
}

// 中间那条在目录里找不到时，前后两条照样落库——这就是「逐条事务」的意义。
//
// 为什么用「目录里没有」而不是「已被认领」：认领了同一个上游条目、且那条模型还活着时，
// apply 的语义是补全那个模型（completed），不是失败。真正的 409 只在认领它的模型
// 已经 deprecated 时才出现（模型域 §4.7.4：不给已经并到别人身上的名字续命）。
func TestIntegrationBatchImportKeepsSuccessesWhenOneFails(t *testing.T) {
	h, idSvc, pool := newModelTestHandler(t)
	sid := mkAdmin(t, pool, idSvc, "batch2")

	u := uniq()
	nameA, nameC := "混批一-"+u, "混批三-"+u
	keyA, keyB, keyC := "it-b2-a-"+u, "it-b2-b-"+u, "it-b2-c-"+u
	putCatalogItem(t, pool, "it-prov", keyA, nameA, nil)
	putCatalogItem(t, pool, "it-prov", keyC, nameC, nil)
	// keyB 故意不往目录里写：模拟「预览之后目录刷新，这条没了」。

	body := fmt.Sprintf(`{"items":[
		{"providerId":"it-prov","externalId":"%s","fields":["name"]},
		{"providerId":"it-prov","externalId":"%s","fields":["name"]},
		{"providerId":"it-prov","externalId":"%s","fields":["name"]}
	]}`, keyA, keyB, keyC)
	rec := postBatch(t, h, sid, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("批量导入应回 200（条目的失败在 results 里），实际 %d：%s", rec.Code, rec.Body.String())
	}
	got := decodeBatch(t, rec)
	if len(got.Results) != 3 {
		t.Fatalf("结果条数 = %d，想要 3", len(got.Results))
	}
	if got.Results[0].Status != "created" || got.Results[2].Status != "created" {
		t.Fatalf("前后两条应当 created，实际 %q / %q", got.Results[0].Status, got.Results[2].Status)
	}
	if got.Results[1].Status != "failed" {
		t.Fatalf("目录里没有那条应当 failed，实际 %q", got.Results[1].Status)
	}
	if errOf(got.Results[1]) == "" {
		t.Fatal("失败条目必须带原因，否则调用方只能猜")
	}

	// 关键：失败的那条没有挡住别的两条。
	var rows int64
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM models WHERE name = ANY($1)`, []string{nameA, nameC}).Scan(&rows); err != nil {
		t.Fatalf("查模型失败: %v", err)
	}
	if rows != 2 {
		t.Fatalf("成功落库 = %d，想要 2（逐条事务，失败不连坐）", rows)
	}
}

// 批内同一个上游条目出现两次 = 拼装请求的 bug，整批 400，一条都不写。
func TestIntegrationBatchImportRejectsDuplicateRefs(t *testing.T) {
	h, idSvc, pool := newModelTestHandler(t)
	sid := mkAdmin(t, pool, idSvc, "batch3")

	u := uniq()
	name := "重复-" + u
	key := "it-b3-a-" + u
	putCatalogItem(t, pool, "it-prov", key, name, nil)

	body := fmt.Sprintf(`{"items":[
		{"providerId":"it-prov","externalId":"%s","fields":["name"]},
		{"providerId":"it-prov","externalId":"%s","fields":["name"]}
	]}`, key, key)
	rec := postBatch(t, h, sid, body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("批内重复应回 400，实际 %d：%s", rec.Code, rec.Body.String())
	}
	var rows int64
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM models WHERE name = $1`, name).Scan(&rows); err != nil {
		t.Fatalf("查模型失败: %v", err)
	}
	if rows != 0 {
		t.Fatalf("整批拒绝时不该落库，实际 %d 行", rows)
	}
}

// 超过 50 条整批 400：上限与契约的 maxItems 一致。
func TestIntegrationBatchImportRejectsTooMany(t *testing.T) {
	h, idSvc, pool := newModelTestHandler(t)
	sid := mkAdmin(t, pool, idSvc, "batch4")

	u := uniq()
	items := make([]string, 0, maxBatchImportItems+1)
	for i := 0; i <= maxBatchImportItems; i++ {
		items = append(items, fmt.Sprintf(
			`{"providerId":"it-prov","externalId":"it-b4-%s-%d","fields":["name"]}`, u, i))
	}
	rec := postBatch(t, h, sid, `{"items":[`+strings.Join(items, ",")+`]}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("超过上限应回 400，实际 %d：%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "最多导入") {
		t.Fatalf("错误里要说清上限，实际：%s", rec.Body.String())
	}
}

// 未知字段名整批 400，且一条都不写。
func TestIntegrationBatchImportRejectsUnknownField(t *testing.T) {
	h, idSvc, pool := newModelTestHandler(t)
	sid := mkAdmin(t, pool, idSvc, "batch5")

	u := uniq()
	name := "未知字段-" + u
	key := "it-b5-a-" + u
	putCatalogItem(t, pool, "it-prov", key, name, nil)

	body := fmt.Sprintf(`{"items":[
		{"providerId":"it-prov","externalId":"%s","fields":["name","noSuchField"]}
	]}`, key)
	rec := postBatch(t, h, sid, body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("未知字段应回 400，实际 %d：%s", rec.Code, rec.Body.String())
	}
	var rows int64
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM models WHERE name = $1`, name).Scan(&rows); err != nil {
		t.Fatalf("查模型失败: %v", err)
	}
	if rows != 0 {
		t.Fatalf("形态错误时不该落库，实际 %d 行", rows)
	}
}

// 权限与单条一致：未登录 401、普通用户 403。
func TestIntegrationBatchImportPermissions(t *testing.T) {
	h, idSvc, pool := newModelTestHandler(t)

	body := `{"items":[{"providerId":"it-prov","externalId":"it-b6-perm","fields":["name"]}]}`
	if rec := postBatch(t, h, "", body); rec.Code != http.StatusUnauthorized {
		t.Fatalf("未登录应回 401，实际 %d", rec.Code)
	}

	// 用户名上限 20 字符，纳秒 36 进制有 10 位，取末 6 位够唯一。
	u := uniq()
	short := u[len(u)-6:]
	acc, sid := registerVerified(t, pool, idSvc, identity.RegisterInput{
		Email:       "it-bt-" + short + "@example.com",
		Password:    "batchtest123",
		Username:    "it-bt-" + short,
		DisplayName: "普通用户",
	})
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE sub = $1`, acc.Sub)
		_, _ = pool.Exec(context.Background(), `DELETE FROM accounts WHERE sub = $1`, acc.Sub)
	})
	if rec := postBatch(t, h, sid, body); rec.Code != http.StatusForbidden {
		t.Fatalf("无 model.import 权限应回 403，实际 %d", rec.Code)
	}
}

// 已有模型的补全走 completed，不是 created。
func TestIntegrationBatchImportMarksCompleted(t *testing.T) {
	h, idSvc, pool := newModelTestHandler(t)
	sid := mkAdmin(t, pool, idSvc, "batch7")

	u := uniq()
	key := "it-b7-a-" + u
	putCatalogItem(t, pool, "it-prov", key, "补全-"+u, nil)
	body := fmt.Sprintf(`{"items":[{"providerId":"it-prov","externalId":"%s","fields":["name"]}]}`, key)

	if rec := postBatch(t, h, sid, body); rec.Code != http.StatusOK {
		t.Fatalf("第一次应回 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	second := postBatch(t, h, sid, body)
	got := decodeBatch(t, second)
	if len(got.Results) != 1 || got.Results[0].Status != "completed" {
		t.Fatalf("第二次应当是 completed（补全），实际 %+v", got.Results)
	}
}
