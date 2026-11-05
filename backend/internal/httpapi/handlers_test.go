package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/api"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/identity"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

// ---- 假实现：不起数据库就能测「行 → 响应」的判定 ----

type fakeRow struct {
	values []any
	err    error
}

func (r fakeRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	for i := range dest {
		switch d := dest[i].(type) {
		case *int64:
			*d = r.values[i].(int64)
		case *int32:
			*d = r.values[i].(int32)
		case *string:
			*d = r.values[i].(string)
		case *pgtype.Text:
			*d = r.values[i].(pgtype.Text)
		case *pgtype.Timestamptz:
			*d = r.values[i].(pgtype.Timestamptz)
		case *bool:
			*d = r.values[i].(bool)
		default:
			panic("fakeRow: unsupported scan target")
		}
	}
	return nil
}

type fakeDB struct{ row pgx.Row }

func (fakeDB) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}

func (fakeDB) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errors.New("fakeDB: Query 不该被调用")
}

// QueryRow 按 SQL 文本分派。
//
// 为什么要分派：`fakeDB` 只有一行数据，而一个请求可能先后查两张表
// （例如下载先查作品、再查清单）。用同一行糊弄所有查询会让 Scan 类型对不上，
// 症状是一句莫名其妙的 `interface conversion: interface {} is int64, not string`。
// 清单那张表在这里一律返回「没有清单」——绝大多数用例要的是目录回退那条路，
// 需要清单的场景用真库（manifest_integration_test.go）。
func (f fakeDB) QueryRow(_ context.Context, sql string, _ ...any) pgx.Row {
	if strings.Contains(sql, "demo_revision_files") {
		return fakeRow{err: pgx.ErrNoRows}
	}
	return f.row
}

type failingPinger struct{ msg string }

func (f failingPinger) Ping(context.Context) error { return errors.New(f.msg) }

func txt(s string) pgtype.Text { return pgtype.Text{String: s, Valid: true} }
func ts(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: true}
}

func decode(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("invalid json: %v (body=%q)", err, string(body))
	}
	return parsed
}

// ---- /healthz ----

func TestHealthzWithoutPinger(t *testing.T) {
	response := httptest.NewRecorder()
	newTestHandler(nil, nil, nil).GetHealthz(response, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", response.Code)
	}
	if got := decode(t, response.Body.Bytes())["status"]; got != "ok" {
		t.Fatalf("expected status ok, got %v", got)
	}
}

func TestHealthzDatabaseDownDoesNotLeak(t *testing.T) {
	const secret = "postgres://user:pass@10.1.2.3:5432/deepdemos"
	response := httptest.NewRecorder()
	newTestHandler(failingPinger{msg: secret}, nil, nil).
		GetHealthz(response, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", response.Code)
	}
	body := response.Body.String()
	if strings.Contains(body, "10.1.2.3") || strings.Contains(body, "postgres://") {
		t.Fatalf("响应体泄漏了内部信息: %s", body)
	}
	if decode(t, response.Body.Bytes())["code"] != "unavailable" {
		t.Fatalf("expected code unavailable, got %s", body)
	}
}

// ---- 列表 ----

func TestListDemosWithoutDatabase(t *testing.T) {
	response := httptest.NewRecorder()
	newTestHandler(nil, nil, nil).ListDemos(response, httptest.NewRequest(http.MethodGet, "/api/v1/demos", nil),
		api.ListDemosParams{})

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", response.Code)
	}
}

func TestListDemosRejectsOversizedPageSize(t *testing.T) {
	// 契约里 pageSize 上限 200。超了要 400，不能静默截断成 200——
	// 静默截断会让调用方以为拿到了全部。
	queries := store.New(fakeDB{})
	tooBig := 201
	response := httptest.NewRecorder()
	newTestHandler(nil, queries, nil).ListDemos(response, httptest.NewRequest(http.MethodGet, "/api/v1/demos", nil),
		api.ListDemosParams{PageSize: &tooBig})

	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", response.Code)
	}
	if decode(t, response.Body.Bytes())["code"] != "invalid_param" {
		t.Fatalf("expected invalid_param, body=%s", response.Body.String())
	}
}

// ---- 详情 ----

func detailRow(state string, title string, deleted, erased bool) store.GetDemoBySlugRow {
	now := time.Now().UTC()
	return store.GetDemoBySlugRow{
		ID:              1,
		Slug:            "demo-slug",
		Title:           pgtype.Text{String: title, Valid: title != ""},
		Description:     pgtype.Text{String: "desc", Valid: title != ""},
		CoverUrl:        pgtype.Text{String: "https://example.test/c.png", Valid: title != ""},
		LifecycleState:  state,
		ModerationState: "approved",
		DemoType:        "web",
		SingleFile:      "",
		// detailRow 默认给「渠道对外可见」+「有对外版本」，因为绝大多数用例要的是
		// 「正常可见的作品」。需要 hidden 渠道或用例要造「没有对外版本」的，
		// 自己改这两列。
		HasVisibleChannel:    true,
		HasPublishedRevision: true,
		PublishedRevNo:       1,
		// 默认按「访客在看」造：没有作者 sub，当前版本与对外版本同源。
		// 要验作者视图的用例自己把 AuthorSub 填上。
		AuthorSub:          "",
		CurrentTitle:       title,
		CurrentDescription: "desc",
		CurrentDemoType:    "web",
		CurrentRevNo:       1,
		CreatedAt:          ts(now),
		UpdatedAt:          ts(now),
		DeletedAt:          pgtype.Timestamptz{Valid: deleted},
		ErasedAt:           pgtype.Timestamptz{Valid: erased},
	}
}

func getDemo(t *testing.T, row store.GetDemoBySlugRow) *httptest.ResponseRecorder {
	t.Helper()
	response := httptest.NewRecorder()
	newTestHandler(nil, store.New(fakeDB{row: fakeRow{values: []any{
		row.ID, row.Slug, row.Title, row.Description, row.CoverUrl,
		row.LifecycleState, row.ModerationState, row.CreatedAt, row.UpdatedAt, row.DeletedAt, row.ErasedAt,
		row.DemoType, row.SingleFile, row.HasPublishedRevision, row.PublishedRevNo,
		row.AuthorSub, row.CurrentTitle, row.CurrentDescription, row.CurrentDemoType, row.CurrentRevNo,
		row.HasVisibleChannel,
	}}}), nil).GetDemo(response, httptest.NewRequest(http.MethodGet, "/api/v1/demos/demo-slug", nil), "demo-slug")
	return response
}

func TestGetDemoPublished(t *testing.T) {
	response := getDemo(t, detailRow("published", "标题", false, false))
	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", response.Code)
	}
	body := decode(t, response.Body.Bytes())
	if body["title"] != "标题" {
		t.Fatalf("expected title, got %v", body["title"])
	}
}

func TestGetDemoArchivedIsTombstone(t *testing.T) {
	// 归档的作品仍然可见，但标题与简介为 null——「这里曾经有过一个作品」。
	response := getDemo(t, detailRow("archived", "标题", false, false))
	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", response.Code)
	}
	body := decode(t, response.Body.Bytes())
	if body["title"] != nil {
		t.Fatalf("墓碑态 title 必须为 null，实际 %v", body["title"])
	}
	if body["description"] != nil {
		t.Fatalf("墓碑态 description 必须为 null，实际 %v", body["description"])
	}
	if body["lifecycleState"] != "archived" {
		t.Fatalf("expected archived, got %v", body["lifecycleState"])
	}
}

// 软删的作品**对外**不存在，连墓碑都不给。
//
// 这条曾经断言 200 + 墓碑（注释还写着「LIFE-008 统一口径」），而生命周期 §4.1 写的是
// 相反的：「`deleted` / `erased` 对外就是不存在，给墓碑反而等于承认这里有过东西」。
// 墓碑只属于 `archived` / `removed` 这两种「曾经公开过」的档位。
//
// 拿这条旧断言当依据的话，删除一件违规作品会留下一个「作者已删除」的页面——
// 那正是作者删除时最不想留下的痕迹。
func TestGetDemoSoftDeletedIsNotFoundForVisitor(t *testing.T) {
	response := getDemo(t, detailRow("published", "标题", true, false))
	if response.Code != http.StatusNotFound {
		t.Fatalf("访客看软删作品 = %d，想要 404", response.Code)
	}
}

func TestGetDemoDraftIs404(t *testing.T) {
	for _, state := range []string{"draft", "pending", "rejected"} {
		response := getDemo(t, detailRow(state, "标题", false, false))
		if response.Code != http.StatusNotFound {
			t.Fatalf("%s: expected 404, got %d", state, response.Code)
		}
	}
}

func TestGetDemoErasedIs404(t *testing.T) {
	response := getDemo(t, detailRow("published", "标题", false, true))
	if response.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", response.Code)
	}
}

func TestGetDemoNotFound(t *testing.T) {
	response := httptest.NewRecorder()
	newTestHandler(nil, store.New(fakeDB{row: fakeRow{err: pgx.ErrNoRows}}), nil).
		GetDemo(response, httptest.NewRequest(http.MethodGet, "/api/v1/demos/x", nil), "x")

	if response.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", response.Code)
	}
	if decode(t, response.Body.Bytes())["code"] != "not_found" {
		t.Fatalf("expected not_found, got %s", response.Body.String())
	}
}

// newTestHandler 是测试里的 Handler 构造器：不投递邮件（mailer 传 nil），
// 这样断言「204 且不发信」时行为与生产一致。
func newTestHandler(p Pinger, q *store.Queries, id *identity.Service) *Handler {
	return New(p, q, id, nil, nil)
}

// 已发布但没审过的作品不对任何人出内容。
//
// 这个组合当前没有端点能造出来（触发器 trg_demos_guard 还会拦「还有 listed 渠道时
// 退审」），但它一旦能出现，就是「审核形同虚设」的唯一入口——所以要在纯函数层钉住。
// hidden 渠道的作品对外不存在：详情必须 404。
//
// 之前漏了这条判定，于是「列表里看不见、详情页却完整 200」——知道 slug 的人仍能读到
// 标题、简介、封面，而 iframe 预览已经 404，页面变成「有正文、无内容」的半截状态。
func TestGetDemoHiddenChannelIs404(t *testing.T) {
	row := detailRow("published", "标题", false, false)
	row.HasVisibleChannel = false
	response := getDemo(t, row)
	if response.Code != http.StatusNotFound {
		t.Fatalf("hidden 渠道期望 404，实际 %d（body=%s）", response.Code, response.Body.String())
	}
}

// 没有对外版本 = 从没审过、也没发布过，对外不存在。
//
// 这条用例原来的写法是「published + rejected → 404」，**语义已经过期**（迁移 0014）：
// 审核态现在描述「当前版本」，作者改稿后它就是 rejected/pending，而对外内容
// 仍是上一版。继续按审核态判会让作者一提交更新，作品就从作品库消失。
func TestGetDemoWithoutPublishedRevision(t *testing.T) {
	row := detailRow("published", "标题", false, false)
	row.HasPublishedRevision = false
	row.PublishedRevNo = 0
	response := getDemo(t, row)
	if response.Code != http.StatusNotFound {
		t.Fatalf("没有对外版本：expected 404，got %d（body=%s）", response.Code, response.Body.String())
	}
}

// 反过来钉一条：**审核态是 rejected 但有对外版本时，详情仍然 200**。
// 这是版本分离之后唯一新增的可见性组合，也是「作者改稿→复审中」这个真实状态。
func TestGetDemoRejectedButHasPublishedRevision(t *testing.T) {
	row := detailRow("published", "标题", false, false)
	row.ModerationState = "rejected"
	response := getDemo(t, row)
	if response.Code != http.StatusOK {
		t.Fatalf("rejected 但有对外版本：expected 200，got %d（body=%s）", response.Code, response.Body.String())
	}
}
