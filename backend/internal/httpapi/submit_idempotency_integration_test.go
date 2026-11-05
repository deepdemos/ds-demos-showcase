package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/api"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/demo"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/identity"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/platform/storage"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

// submitWithKey 用指定的幂等键投一次稿，返回回执与 HTTP 状态码。
//
// key 为空串表示**不带这个头**——那是另一条路径（不幂等），必须能分开测，
// 否则「没给 key」和「给了空 key」会被同一段代码吃掉。
func submitWithKey(t *testing.T, h *Handler, sid, title, body, key string) (int, api.SubmissionResult) {
	t.Helper()

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if err := mw.WriteField("title", title); err != nil {
		t.Fatal(err)
	}
	if err := mw.WriteField("description", "幂等测试用"); err != nil {
		t.Fatal(err)
	}
	if err := mw.WriteField("demoType", "web"); err != nil {
		t.Fatal(err)
	}
	part, err := mw.CreateFormFile("package", "index.html")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte(body)); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/demos", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: sid})

	var params api.SubmitDemoParams
	if key != "" {
		k := key
		params.IdempotencyKey = &k
	}

	rec := httptest.NewRecorder()
	h.SubmitDemo(rec, req, params)

	var out api.SubmissionResult
	if rec.Code == http.StatusCreated || rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("回执不是合法 JSON: %v（%s）", err, rec.Body.String())
		}
	}
	return rec.Code, out
}

/**
 * 幂等键太长要在领域层就挡住，回 400。
 *
 * 交给数据库的话，作者会收到一条 23514 的「落库失败」——那是把输入问题
 * 说成了我们这边坏了。契约里 maxLength 是 128，这里与它保持一致。
 */
func TestIntegrationSubmitRejectsLongIdempotencyKey(t *testing.T) {
	h, _, idSvc := newIdempotencyHandler(t, 5)
	pool := integrationPool(t)

	acc, sid := registerVerified(t, pool, idSvc, identity.RegisterInput{
		Email:       "idem-long@example.com",
		Password:    "correct horse battery",
		Username:    "idem-long",
		DisplayName: "长键",
	})
	t.Cleanup(func() { cleanupAuthor(t, pool, acc.Sub) })

	code, _ := submitWithKey(t, h, sid, "长键探针", "<h1>x</h1>", strings.Repeat("k", 129))
	if code != http.StatusBadRequest {
		t.Fatalf("129 个字符的幂等键 = %d，想要 400（契约 maxLength 是 128）", code)
	}
}

func newIdempotencyHandler(t *testing.T, maxPending int) (*Handler, *store.Queries, *identity.Service) {
	t.Helper()
	pool := integrationPool(t)
	q := store.New(pool)
	idSvc := integrationIdentity(t, pool)
	files, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("初始化存储失败: %v", err)
	}
	demoSvc := demo.New(pool, q, files, storage.DefaultLimits(), 200<<20, maxPending, nil)
	return New(pool, q, idSvc, nil, demoSvc), q, idSvc
}

// 同一个幂等键投两次，只会有一条作品。
//
// 这条钉的是自动化上传最真实的失败：脚本把一个 200 MiB 的包传上去、等服务端
// 回执的那几秒里连接断了。脚本不知道服务端收没收到，只能重传——没有幂等键，
// 这就是两条一模一样的作品，等着人工去删。
func TestIntegrationSubmitIsIdempotent(t *testing.T) {
	h, _, idSvc := newIdempotencyHandler(t, 0)
	pool := integrationPool(t)

	acc, sid := registerVerified(t, pool, idSvc, identity.RegisterInput{
		Email:       "it-idem@example.com",
		Password:    "idempotency123",
		Username:    "it-idem",
		DisplayName: "幂等作者",
	})
	t.Cleanup(func() { cleanupAuthor(t, pool, acc.Sub) })

	const key = "it-idem-key-1"

	code, first := submitWithKey(t, h, sid, "幂等投稿", `<!doctype html><p>v1</p>`, key)
	if code != http.StatusCreated {
		t.Fatalf("第一次投稿 = %d，想要 201（%+v）", code, first)
	}

	// 第二次连内容都换了：幂等键的语义是「这件事做过了」，不是「这两次一样」。
	code, second := submitWithKey(t, h, sid, "幂等投稿", `<!doctype html><p>v2</p>`, key)
	if code != http.StatusOK {
		t.Fatalf("重放 = %d，想要 200（不是 201：这次没有新建）", code)
	}
	if second.Slug != first.Slug {
		t.Fatalf("重放的 slug = %q，想要 %q（必须还回当初那条）", second.Slug, first.Slug)
	}

	var n int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM demos WHERE idempotency_key = $1`, key).Scan(&n); err != nil {
		t.Fatalf("查作品条数失败: %v", err)
	}
	if n != 1 {
		t.Fatalf("同一个幂等键落库 %d 条，想要 1 条", n)
	}
}

// 幂等键按作者隔离：两个人各自生成了同一个 key（UUID 撞车的概率极低，
// 但客户端自己拼的 key 不保证唯一），不该互相顶掉对方的投稿。
//
// 更要紧的是另一半：不能因为 key 撞了就让 B 读到 A 的作品。
func TestIntegrationIdempotencyKeyIsScopedByAuthor(t *testing.T) {
	h, _, idSvc := newIdempotencyHandler(t, 0)
	pool := integrationPool(t)

	accA, sidA := registerVerified(t, pool, idSvc, identity.RegisterInput{
		Email: "it-idem-a@example.com", Password: "idempotency123",
		Username: "it-idem-a", DisplayName: "作者甲",
	})
	t.Cleanup(func() { cleanupAuthor(t, pool, accA.Sub) })
	accB, sidB := registerVerified(t, pool, idSvc, identity.RegisterInput{
		Email: "it-idem-b@example.com", Password: "idempotency123",
		Username: "it-idem-b", DisplayName: "作者乙",
	})
	t.Cleanup(func() { cleanupAuthor(t, pool, accB.Sub) })

	const key = "it-idem-shared-key"

	codeA, firstA := submitWithKey(t, h, sidA, "甲的作品", `<!doctype html><p>a</p>`, key)
	if codeA != http.StatusCreated {
		t.Fatalf("甲投稿 = %d，想要 201", codeA)
	}

	codeB, firstB := submitWithKey(t, h, sidB, "乙的作品", `<!doctype html><p>b</p>`, key)
	if codeB != http.StatusCreated {
		t.Fatalf("乙用同一个 key 投稿 = %d，想要 201（key 不该跨作者生效，否则乙会拿到甲的作品）", codeB)
	}
	if firstB.Slug == firstA.Slug {
		t.Fatalf("乙拿到了甲的作品 %q——幂等查询必须带 author_id 限定", firstB.Slug)
	}
}

// 不给幂等键时完全不幂等：两次投稿就是两条作品。
//
// 这条是上面几条的对照——没有它，上面几条全靠「幂等逻辑生效」解释，
// 也可能是「本来就只建一条」。
func TestIntegrationSubmitWithoutKeyCreatesTwo(t *testing.T) {
	h, _, idSvc := newIdempotencyHandler(t, 0)
	pool := integrationPool(t)

	acc, sid := registerVerified(t, pool, idSvc, identity.RegisterInput{
		Email: "it-nokey@example.com", Password: "idempotency123",
		Username: "it-nokey", DisplayName: "不带键的作者",
	})
	t.Cleanup(func() { cleanupAuthor(t, pool, acc.Sub) })

	code1, first := submitWithKey(t, h, sid, "不带键", `<!doctype html><p>x</p>`, "")
	code2, second := submitWithKey(t, h, sid, "不带键", `<!doctype html><p>x</p>`, "")
	if code1 != http.StatusCreated || code2 != http.StatusCreated {
		t.Fatalf("两次投稿 = %d / %d，都想要 201", code1, code2)
	}
	if first.Slug == second.Slug {
		t.Fatalf("没有幂等键时两次投稿应当是两条作品，实际都是 %q", first.Slug)
	}
}

// 审核态：作者看真实值，访客恒为 approved。
//
// 最有说服力的是第三段——作者改稿之后新版本是 pending，访客看到的仍是
// 已发布那一版，所以访客那边必须是 approved。如果照抄库里的值，
// 访客会看到「这条作品还没审完就上线了」，而那正是版本分离要消除的误会。
func TestIntegrationDetailModerationStateVisibility(t *testing.T) {
	h, _, idSvc := newIdempotencyHandler(t, 0)
	pool := integrationPool(t)

	acc, sid := registerVerified(t, pool, idSvc, identity.RegisterInput{
		Email: "it-modstate@example.com", Password: "idempotency123",
		Username: "it-modstate", DisplayName: "审核态作者",
	})
	t.Cleanup(func() { cleanupAuthor(t, pool, acc.Sub) })

	slug := submitHTML(t, h, sid, "审核态作品", `<!doctype html><p>v1</p>`)

	// 刚投稿：作者看 pending，访客根本看不到（草稿 + 待审）。
	authorDetail := decodeDetail(t, getDemoAs(t, h, slug, sid))
	if authorDetail.ModerationState != api.DemoDetailModerationStatePending {
		t.Fatalf("作者看到的审核态 = %q，想要 pending", authorDetail.ModerationState)
	}
	if rec := getDemoAs(t, h, slug, ""); rec.Code != http.StatusNotFound {
		t.Fatalf("访客看未发布作品 = %d，想要 404", rec.Code)
	}

	// 发布之后：访客看得到，且审核态是 approved。
	publishNow(t, pool, slug)
	visitorDetail := decodeDetail(t, getDemoAs(t, h, slug, ""))
	if visitorDetail.ModerationState != api.DemoDetailModerationStateApproved {
		t.Fatalf("访客看到的审核态 = %q，想要 approved", visitorDetail.ModerationState)
	}

	// 作者改稿：新版本回到 pending，但对外那一版没动。
	if rec := updateDemo(t, h, sid, slug, "审核态作品 v2", "改稿", []byte(`<!doctype html><p>v2</p>`)); rec.Code != http.StatusOK {
		t.Fatalf("改稿 = %d，想要 200（%s）", rec.Code, rec.Body.String())
	}
	authorAfter := decodeDetail(t, getDemoAs(t, h, slug, sid))
	if authorAfter.ModerationState != api.DemoDetailModerationStatePending {
		t.Fatalf("改稿后作者看到的审核态 = %q，想要 pending", authorAfter.ModerationState)
	}
	visitorAfter := decodeDetail(t, getDemoAs(t, h, slug, ""))
	if visitorAfter.ModerationState != api.DemoDetailModerationStateApproved {
		t.Fatalf("改稿后访客看到的审核态 = %q，想要 approved（对外那一版没变）", visitorAfter.ModerationState)
	}
}

// 配额满时的 429 必须带 Retry-After。
//
// 登录限流早就带这个头（`identity.go`），投稿配额却不带——同一类「现在不行、
// 过会儿再来」的响应，两套标准。没有它，脚本只能立刻重试（打成循环）或直接放弃。
func TestIntegrationSubmitQuotaSendsRetryAfter(t *testing.T) {
	h, _, idSvc := newIdempotencyHandler(t, 1)
	pool := integrationPool(t)

	acc, sid := registerVerified(t, pool, idSvc, identity.RegisterInput{
		Email: "it-quota@example.com", Password: "idempotency123",
		Username: "it-quota", DisplayName: "配额作者",
	})
	t.Cleanup(func() { cleanupAuthor(t, pool, acc.Sub) })

	if code, _ := submitWithKey(t, h, sid, "配额一", `<!doctype html><p>1</p>`, ""); code != http.StatusCreated {
		t.Fatalf("第一条 = %d，想要 201", code)
	}

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if err := mw.WriteField("title", "配额二"); err != nil {
		t.Fatal(err)
	}
	if err := mw.WriteField("demoType", "web"); err != nil {
		t.Fatal(err)
	}
	part, err := mw.CreateFormFile("package", "index.html")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte(`<!doctype html><p>2</p>`)); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/demos", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: sid})
	rec := httptest.NewRecorder()
	h.SubmitDemo(rec, req, api.SubmitDemoParams{})

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("超配额 = %d，想要 429（%s）", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Retry-After"); got != "3600" {
		t.Fatalf("Retry-After = %q，想要 \"3600\"（与 demo.PendingRetryHint 一致）", got)
	}
}

// decodeDetail 解出详情响应的 `DemoDetail`，顺带断言状态码是 200。
func decodeDetail(t *testing.T, rec *httptest.ResponseRecorder) api.DemoDetail {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("详情 = %d，想要 200（%s）", rec.Code, rec.Body.String())
	}
	var out api.DemoDetail
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("详情不是合法 JSON: %v（%s）", err, rec.Body.String())
	}
	return out
}

/**
 * 非法 UTF-8 是输入问题，必须回 400。
 *
 * 不挡的话这些字节会被送进 PG，PG 报 22021，最后落到 writeDemoError 的
 * default 分支变成 503「服务暂时不可用」——作者会以为我们坏了。
 * 真栈上第一次撞到它，是 Windows 的 curl 按 GBK 把中文标题发了出来。
 */
func TestIntegrationSubmitRejectsInvalidUTF8(t *testing.T) {
	h, _, idSvc := newIdempotencyHandler(t, 5)
	pool := integrationPool(t)

	acc, sid := registerVerified(t, pool, idSvc, identity.RegisterInput{
		Email:       "idem-gbk@example.com",
		Password:    "correct horse battery",
		Username:    "idem-gbk",
		DisplayName: "坏编码",
	})
	t.Cleanup(func() { cleanupAuthor(t, pool, acc.Sub) })

	// 0xc5 0xe4 是 GBK 的「迥」，不是合法 UTF-8。
	code, _ := submitWithKey(t, h, sid, string([]byte{0xc5, 0xe4}), "<h1>x</h1>", "")
	if code != http.StatusBadRequest {
		t.Fatalf("非法 UTF-8 的标题 = %d，想要 400（不是 503）", code)
	}
}
