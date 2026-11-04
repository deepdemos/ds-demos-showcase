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

func (f fakeDB) QueryRow(context.Context, string, ...any) pgx.Row { return f.row }

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
		// detailRow 默认给「渠道对外可见」，因为绝大多数用例要的是「正常可见的作品」。
		// 需要 hidden 渠道的用例自己改这一列。
		HasVisibleChannel: true,
		CreatedAt:         ts(now),
		UpdatedAt:         ts(now),
		DeletedAt:         pgtype.Timestamptz{Valid: deleted},
		ErasedAt:          pgtype.Timestamptz{Valid: erased},
	}
}

func getDemo(t *testing.T, row store.GetDemoBySlugRow) *httptest.ResponseRecorder {
	t.Helper()
	response := httptest.NewRecorder()
	newTestHandler(nil, store.New(fakeDB{row: fakeRow{values: []any{
		row.ID, row.Slug, row.Title, row.Description, row.CoverUrl,
		row.LifecycleState, row.ModerationState, row.CreatedAt, row.UpdatedAt, row.DeletedAt, row.ErasedAt,
		row.DemoType, row.SingleFile, row.HasVisibleChannel,
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

func TestGetDemoSoftDeletedIsTombstone(t *testing.T) {
	// 作者删除（deleted_at 有值）也留墓碑，不返回 404——LIFE-008 统一口径。
	response := getDemo(t, detailRow("published", "标题", true, false))
	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", response.Code)
	}
	if decode(t, response.Body.Bytes())["title"] != nil {
		t.Fatal("软删的 title 必须为 null")
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

func TestGetDemoPublishedButNotApproved(t *testing.T) {
	row := detailRow("published", "标题", false, false)
	row.ModerationState = "rejected"
	response := getDemo(t, row)
	if response.Code != http.StatusNotFound {
		t.Fatalf("未过审的 published：expected 404，got %d", response.Code)
	}
}
