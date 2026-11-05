package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/api"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/identity"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/kernel/verification"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

// 集成测试：连真库跑一遍真实 SQL。
//
// **默认跳过**——没设 TEST_DATABASE_URL 时不跑，本地不需要装 PostgreSQL。
// CI 里由 postgres service 提供库，并先灌 db/schema/*.sql。
//
// 这是有意的取舍（见 docs/development-workflow.md）：不为少数人要跑的测试强迫所有人装库。
// 代价是可能出现「本地绿、CI 红」，承担方式是 CI 红时先看是不是集成测试。
func integrationPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL 未设置，跳过集成测试（CI 里会设）")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("连接测试库失败: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func seed(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	if _, err := pool.Exec(ctx, "DELETE FROM demos"); err != nil {
		t.Fatalf("清空 demos 失败: %v", err)
	}
	rows := []struct {
		slug, state string
		deleted     bool
		erased      bool
	}{
		{"it-published", "published", false, false},
		{"it-archived", "archived", false, false},
		{"it-draft", "draft", false, false},
		{"it-erased", "published", false, true},
	}
	for _, r := range rows {
		// 被擦除的行必须把内容列全部置空——db/schema/30-demo.sql 的 ck_demos_erased
		// 就是这么要求的（擦除 = 内容清空 + 状态封存）。这里照约束来，不绕过。
		title, description := "标题 "+r.slug, "简介"
		if r.erased {
			title, description = "", ""
		}
		// ck_demos_time_order 要求：erased_at 非空时 deleted_at 必须非空，且 erased_at >= deleted_at。
		// 也就是「先删后擦」——擦除是删除之后的进一步动作。这里照约束构造数据。
		deleted := r.deleted || r.erased
		_, err := pool.Exec(ctx, `
			INSERT INTO demos (slug, title, description, moderation_state, lifecycle_state, deleted_at, erased_at)
			VALUES ($1, NULLIF($2, ''), NULLIF($3, ''), 'approved', $4,
			        CASE WHEN $5 THEN now() ELSE NULL END,
			        CASE WHEN $6 THEN now() ELSE NULL END)`,
			r.slug, title, description, r.state, deleted, r.erased)
		if err != nil {
			t.Fatalf("插入 %s 失败: %v", r.slug, err)
		}
	}

	// 版本分离（迁移 0014）之后，「能上架渠道 / 详情能读到内容」的前提是有一个
	// 对外版本指针。只造 demos 行是不够的——这是这个种子原来漏掉的一环，
	// 以前靠「审核态 approved」蒙对了。
	if _, err := pool.Exec(ctx, `
		WITH rev AS (
			INSERT INTO demo_revisions (demo_id, rev_no, demo_type, source)
			SELECT id, 1, 'web', 'upload' FROM demos
			WHERE slug IN ('it-published', 'it-archived')
			ON CONFLICT (demo_id, rev_no) DO NOTHING
			RETURNING id, demo_id
		)
		UPDATE demos d
		SET current_revision_id = rev.id, published_revision_id = rev.id
		FROM rev WHERE d.id = rev.demo_id`); err != nil {
		t.Fatalf("给已发布作品造对外版本失败: %v", err)
	}

	// 列表的可见性谓词要求至少一条 visibility='listed' 的渠道行（迁移 0013 会把
	// 存量数据补齐，但测试自己造的行没有渠道）。只给真正该出现在列表里的那一条。
	if _, err := pool.Exec(ctx, `
		INSERT INTO demo_channels (demo_id, channel, visibility, lang)
		SELECT id, 'deep', 'listed', 'zh' FROM demos WHERE slug = 'it-published'
		ON CONFLICT (demo_id, channel) DO NOTHING`); err != nil {
		t.Fatalf("插入渠道行失败: %v", err)
	}
}

// integrationIdentity 装一个完整的身份域（含第②层要的验证码机制）。
func integrationIdentity(t *testing.T, pool *pgxpool.Pool) *identity.Service {
	t.Helper()
	q := store.New(pool)
	verif := verification.New(q, []byte("test-verification-secret"))
	return identity.New(pool, []byte("test-ip-secret"), verif)
}

func TestIntegrationListAndDetail(t *testing.T) {
	pool := integrationPool(t)
	seed(t, pool)

	h := New(pool, store.New(pool), integrationIdentity(t, pool), nil, nil)

	// 列表：只有 published 且未擦除的那一条。
	listRec := httptest.NewRecorder()
	h.ListDemos(listRec, httptest.NewRequest(http.MethodGet, "/api/v1/demos", nil), api.ListDemosParams{})
	var list map[string]any
	if err := json.Unmarshal(listRec.Body.Bytes(), &list); err != nil {
		t.Fatalf("列表响应不是 JSON: %v", err)
	}
	if got := list["total"]; got != float64(1) {
		t.Fatalf("expected total 1（只有 it-published），got %v", got)
	}

	// 详情：三种状态，三种结果。
	cases := []struct {
		slug string
		code int
	}{
		{"it-published", http.StatusOK},
		{"it-archived", http.StatusOK}, // 墓碑
		{"it-draft", http.StatusNotFound},
		{"it-erased", http.StatusNotFound},
		{"it-does-not-exist", http.StatusNotFound},
	}
	for _, tc := range cases {
		rec := httptest.NewRecorder()
		h.GetDemo(rec, httptest.NewRequest(http.MethodGet, "/api/v1/demos/"+tc.slug, nil), tc.slug)
		if rec.Code != tc.code {
			t.Fatalf("%s: expected %d, got %d（body=%s）", tc.slug, tc.code, rec.Code, rec.Body.String())
		}
		if tc.slug == "it-archived" {
			var body map[string]any
			_ = json.Unmarshal(rec.Body.Bytes(), &body)
			// 墓碑保留标题、不给内容：分享链接落到这里要能看出是哪个作品没了。
			if body["title"] == nil || body["title"] == "" {
				t.Fatalf("墓碑态 title 应保留，实际 %v", body["title"])
			}
			if body["description"] != nil {
				t.Fatalf("墓碑态 description 必须为 null，实际 %v", body["description"])
			}
		}
	}
}

// seedMe 造一个「已登录」的最小现场：账号 + 资料 + 主邮箱 + 一条有效会话。
func seedMe(t *testing.T, pool *pgxpool.Pool, sessionID string) {
	t.Helper()
	ctx := context.Background()
	for _, q := range []string{"DELETE FROM demo_stats", "DELETE FROM demo_channels", "DELETE FROM demo_revisions", "DELETE FROM demos", "DELETE FROM web_sessions", "DELETE FROM users", "DELETE FROM accounts"} {
		if _, err := pool.Exec(ctx, q); err != nil {
			t.Fatalf("%s 失败: %v", q, err)
		}
	}
	// 用一个 CTE 一次装完五张表（含 users 投影行——/me 的查询要 join 它）。PostgreSQL 的数据修改型 CTE 即使没人读也会执行完，
	// 所以 p / e 不需要被主语句引用。少填任何一个 NOT NULL 列都会在这里当场报错。
	const seedSQL = `
WITH a AS (
    INSERT INTO accounts (sub, status, role, level)
    VALUES ('ITSESSION0000000000000000AA', 'active', 'admin', 3) RETURNING id
), p AS (
    INSERT INTO profiles (account_id, username, display_name)
    SELECT id, 'it-me', '集成测试' FROM a
), e AS (
    INSERT INTO account_emails (account_id, email, is_primary, verified_at)
    SELECT id, 'it-me@example.com', true, now() FROM a
), u AS (
    INSERT INTO users (sub) VALUES ('ITSESSION0000000000000000AA')
)
INSERT INTO web_sessions (id, account_sub, username, display_name, role, status, verified_at)
SELECT $1, 'ITSESSION0000000000000000AA', 'it-me', '集成测试', 'admin', 'active', now() FROM a`
	if _, err := pool.Exec(ctx, seedSQL, sessionID); err != nil {
		t.Fatalf("装配账号失败: %v", err)
	}
}

func TestIntegrationGetMe(t *testing.T) {
	pool := integrationPool(t)
	const sid = "it-session-aaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	seedMe(t, pool, sid)
	h := New(pool, store.New(pool), integrationIdentity(t, pool), nil, nil)
	call := func(cookie string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: cookie})
		rec := httptest.NewRecorder()
		h.GetMe(rec, req)
		return rec
	}

	rec := call(sid)
	if rec.Code != http.StatusOK {
		t.Fatalf("有效会话应 200，实际 %d（body=%s）", rec.Code, rec.Body.String())
	}
	var me api.Me
	if err := json.Unmarshal(rec.Body.Bytes(), &me); err != nil {
		t.Fatalf("响应不是合法 Me: %v", err)
	}
	if me.Username != "it-me" || me.Role != "admin" || me.Status != api.AccountStatusActive ||
		me.Email != "it-me@example.com" || me.Level != 3 {
		t.Fatalf("字段不对: %+v", me)
	}

	// 401 的各种情况必须给出逐字节相同的响应体——任何差异都等于会话探测器。
	want := call("").Body.String()
	for _, tc := range []struct{ name, cookie, mutate string }{
		{"未知会话", "it-does-not-exist", ""},
		// 注销账号的会话必须失效，不能指望「注销流程记得吊销」。
		{"已注销账号", sid, `UPDATE accounts SET status = 'deleted', closed_at = now()
			WHERE sub = 'ITSESSION0000000000000000AA'`},
		{"已吊销", sid, `UPDATE web_sessions SET revoked_at = now() WHERE id = '` + sid + `'`},
	} {
		if tc.mutate != "" {
			if _, err := pool.Exec(context.Background(), tc.mutate); err != nil {
				t.Fatalf("%s：改数据失败: %v", tc.name, err)
			}
		}
		rec := call(tc.cookie)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s 应 401，实际 %d", tc.name, rec.Code)
		}
		if rec.Body.String() != want {
			t.Fatalf("%s 与未登录的响应体不一致：%q vs %q", tc.name, rec.Body.String(), want)
		}
	}
}

func TestIntegrationRegister(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()
	for _, q := range []string{"DELETE FROM demo_stats", "DELETE FROM demo_channels", "DELETE FROM demo_revisions", "DELETE FROM demos", "DELETE FROM web_sessions", "DELETE FROM users", "DELETE FROM accounts"} {
		if _, err := pool.Exec(ctx, q); err != nil {
			t.Fatalf("%s 失败: %v", q, err)
		}
	}
	h := New(pool, store.New(pool), integrationIdentity(t, pool), nil, nil)

	register := func(body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		h.PostAuthRegister(rec, req)
		return rec
	}

	// 正常注册：201 + 不下发会话 + 不回声密码。
	rec := register(`{"email":"it-reg@example.com","password":"correct-horse-battery","username":"it-reg","displayName":"注册测试"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("注册应 201，实际 %d（body=%s）", rec.Code, rec.Body.String())
	}
	// 注册不再发会话：设计 §8.1 要求 `pending` 账号拿不到任何 token。
	if cookies := rec.Result().Cookies(); len(cookies) != 0 {
		t.Fatalf("注册不该下发 cookie，实际 %+v", cookies)
	}
	if strings.Contains(rec.Body.String(), "correct-horse-battery") {
		t.Fatal("响应体里出现了明文密码")
	}
	var accepted api.RegisterAccepted
	if err := json.Unmarshal(rec.Body.Bytes(), &accepted); err != nil {
		t.Fatalf("响应不是合法 RegisterAccepted: %v", err)
	}
	// mailer 传的是 nil，所以这里必须是 false——「信没发出去」要说真话。
	if accepted.Email != "it-reg@example.com" || accepted.VerificationSent {
		t.Fatalf("字段不对: %+v", accepted)
	}

	// 库里是新账号，状态 pending。
	var sub, status string
	if err := pool.QueryRow(ctx, `
		SELECT a.sub, a.status FROM accounts a
		JOIN account_emails e ON e.account_id = a.id
		WHERE lower(e.email) = 'it-reg@example.com'`).Scan(&sub, &status); err != nil {
		t.Fatalf("查账号失败: %v", err)
	}
	if status != "pending" {
		t.Fatalf("注册后状态 = %q，想要 pending", status)
	}

	// 拿到验证码（走真实的重发路径？不行——注册那条码在 1 分钟内不能再要）。
	// 直接从库里取：测试要验的是 HTTP 层的验证端点，不是投递。
	code := pendingCode(t, pool, "it-reg@example.com")
	verify := func(body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/verify-email", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		h.PostAuthVerifyEmail(rec, req)
		return rec
	}
	if rec := verify(`{"email":"it-reg@example.com","code":"` + code + `"}`); rec.Code != http.StatusNoContent {
		t.Fatalf("验证应 204，实际 %d（body=%s）", rec.Code, rec.Body.String())
	}
	// 验证过之后就登得进来。
	loginRec := httptest.NewRecorder()
	loginReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login",
		strings.NewReader(`{"identifier":"it-reg@example.com","password":"correct-horse-battery"}`))
	loginReq.Header.Set("Content-Type", "application/json")
	h.PostAuthLogin(loginRec, loginReq)
	if loginRec.Code != http.StatusOK {
		t.Fatalf("验证后登录应 200，实际 %d（body=%s）", loginRec.Code, loginRec.Body.String())
	}

	// 未验证的账号登录：403 + email_not_verified，不是 401「密码不对」。
	if rec := register(`{"email":"it-unverified@example.com","password":"correct-horse-battery","username":"it-unv","displayName":"x"}`); rec.Code != http.StatusCreated {
		t.Fatalf("注册应 201，实际 %d", rec.Code)
	}
	pendingRec := httptest.NewRecorder()
	pendingReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login",
		strings.NewReader(`{"identifier":"it-unverified@example.com","password":"correct-horse-battery"}`))
	pendingReq.Header.Set("Content-Type", "application/json")
	h.PostAuthLogin(pendingRec, pendingReq)
	if pendingRec.Code != http.StatusForbidden {
		t.Fatalf("未验证账号登录应 403，实际 %d（body=%s）", pendingRec.Code, pendingRec.Body.String())
	}
	var errBody api.Error
	if err := json.Unmarshal(pendingRec.Body.Bytes(), &errBody); err != nil {
		t.Fatalf("错误响应不合法: %v", err)
	}
	if errBody.Code != api.EmailNotVerified {
		t.Fatalf("错误码 = %q，想要 email_not_verified", errBody.Code)
	}

	// 验证码错：401。
	if rec := verify(`{"email":"it-unverified@example.com","code":"000000"}`); rec.Code != http.StatusUnauthorized {
		t.Fatalf("错码应 401，实际 %d（body=%s）", rec.Code, rec.Body.String())
	}

	// 数据库里只留哈希：明文不得入库。
	var stored string
	if err := pool.QueryRow(ctx, `
		SELECT c.secret_hash FROM credentials c
		JOIN accounts a ON a.id = c.account_id WHERE a.sub = $1`, sub).Scan(&stored); err != nil {
		t.Fatalf("查凭证失败: %v", err)
	}
	if !strings.HasPrefix(stored, "$argon2id$") || strings.Contains(stored, "correct-horse-battery") {
		t.Fatalf("凭证不是 argon2id 哈希: %q", stored)
	}

	// 邮箱撞车（大小写不同也算）：409。
	if rec := register(`{"email":"IT-REG@example.com","password":"correct-horse-battery","username":"other-one","displayName":"x"}`); rec.Code != http.StatusConflict {
		t.Fatalf("邮箱重复应 409，实际 %d（body=%s）", rec.Code, rec.Body.String())
	}
	// 用户名撞车（大小写不同也算）：409。
	if rec := register(`{"email":"other@example.com","password":"correct-horse-battery","username":"IT-REG","displayName":"x"}`); rec.Code != http.StatusConflict {
		t.Fatalf("用户名重复应 409，实际 %d（body=%s）", rec.Code, rec.Body.String())
	}
	// 密码太短：400，且**没有建出半个账号**。
	if rec := register(`{"email":"short@example.com","password":"short","username":"it-short","displayName":"x"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("短密码应 400，实际 %d", rec.Code)
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM profiles WHERE lower(username) IN ('it-reg','other-one','it-short','it-unv')`).Scan(&n); err != nil {
		t.Fatalf("计数失败: %v", err)
	}
	if n != 2 {
		t.Fatalf("失败路径不应留下账号行，实际 %d 条（应为 it-reg + it-unv）", n)
	}
}

func TestIntegrationLoginLogout(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()
	for _, q := range []string{"DELETE FROM demo_stats", "DELETE FROM demo_channels", "DELETE FROM demo_revisions", "DELETE FROM demos", "DELETE FROM web_sessions", "DELETE FROM users", "DELETE FROM accounts"} {
		if _, err := pool.Exec(ctx, q); err != nil {
			t.Fatalf("%s 失败: %v", q, err)
		}
	}
	h := New(pool, store.New(pool), integrationIdentity(t, pool), nil, nil)

	post := func(path, body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if path == "/api/v1/auth/register" {
			h.PostAuthRegister(rec, req)
		} else {
			h.PostAuthLogin(rec, req)
		}
		return rec
	}

	if rec := post("/api/v1/auth/register", `{"email":"it-login@example.com","password":"correct-horse-battery","username":"it-login","displayName":"登录测试"}`); rec.Code != http.StatusCreated {
		t.Fatalf("注册应 201，实际 %d（body=%s）", rec.Code, rec.Body.String())
	}
	// 注册只到 pending，登录用例要的是「能登」这个前提，所以先把邮箱验证掉。
	verifyEmailForTest(t, pool, h, "it-login@example.com")

	// 邮箱、用户名、大小写变体都能登进去，且每次发的是不同会话。
	for _, id := range []string{"it-login@example.com", "IT-LOGIN@example.com", "it-login", "IT-LOGIN"} {
		rec := post("/api/v1/auth/login", `{"identifier":"`+id+`","password":"correct-horse-battery"}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s 登录应 200，实际 %d（body=%s）", id, rec.Code, rec.Body.String())
		}
		cookies := rec.Result().Cookies()
		if len(cookies) != 1 || !cookies[0].HttpOnly || !cookies[0].Secure {
			t.Fatalf("%s 的会话 cookie 不对: %+v", id, cookies)
		}
		meReq := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
		meReq.AddCookie(cookies[0])
		meRec := httptest.NewRecorder()
		h.GetMe(meRec, meReq)
		if meRec.Code != http.StatusOK {
			t.Fatalf("%s 登录后 /me 应 200，实际 %d", id, meRec.Code)
		}
	}

	// 密码错 / 账号不存在：都是 401，且**响应体相同**（不给账号枚举留信号）。
	wrong := post("/api/v1/auth/login", `{"identifier":"it-login","password":"wrong-password-xx"}`)
	nobody := post("/api/v1/auth/login", `{"identifier":"nobody-here","password":"wrong-password-xx"}`)
	if wrong.Code != http.StatusUnauthorized || nobody.Code != http.StatusUnauthorized {
		t.Fatalf("都应 401，实际 %d / %d", wrong.Code, nobody.Code)
	}
	if wrong.Body.String() != nobody.Body.String() {
		t.Fatalf("密码错与账号不存在的响应体必须一致：%q vs %q", wrong.Body.String(), nobody.Body.String())
	}

	// 登出：204 + 清 cookie；服务端会话立刻失效；重复登出仍然 204。
	loginRec := post("/api/v1/auth/login", `{"identifier":"it-login","password":"correct-horse-battery"}`)
	cookie := loginRec.Result().Cookies()[0]

	logoutReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	logoutReq.AddCookie(cookie)
	logoutRec := httptest.NewRecorder()
	h.PostAuthLogout(logoutRec, logoutReq)
	if logoutRec.Code != http.StatusNoContent {
		t.Fatalf("登出应 204，实际 %d", logoutRec.Code)
	}
	cleared := logoutRec.Result().Cookies()
	if len(cleared) != 1 || cleared[0].MaxAge != -1 {
		t.Fatalf("登出应下发 Max-Age=-1 的清除 cookie，实际 %+v", cleared)
	}

	// 关键：**即使客户端保留旧 cookie**，服务端会话也已吊销。
	meReq := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	meReq.AddCookie(cookie)
	meRec := httptest.NewRecorder()
	h.GetMe(meRec, meReq)
	if meRec.Code != http.StatusUnauthorized {
		t.Fatalf("登出后旧 cookie 必须失效，实际 %d", meRec.Code)
	}

	again := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	again.AddCookie(cookie)
	againRec := httptest.NewRecorder()
	h.PostAuthLogout(againRec, again)
	if againRec.Code != http.StatusNoContent {
		t.Fatalf("重复登出应仍然 204，实际 %d", againRec.Code)
	}
}

func TestIntegrationLoginRateLimit(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()
	for _, q := range []string{
		"DELETE FROM login_attempts", "DELETE FROM web_sessions", "DELETE FROM users", "DELETE FROM accounts",
	} {
		if _, err := pool.Exec(ctx, q); err != nil {
			t.Fatalf("%s 失败: %v", q, err)
		}
	}
	h := New(pool, store.New(pool), integrationIdentity(t, pool), nil, nil)

	register := httptest.NewRecorder()
	regReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register",
		strings.NewReader(`{"email":"it-limit@example.com","password":"correct-horse-battery","username":"it-limit","displayName":"限流测试"}`))
	regReq.Header.Set("Content-Type", "application/json")
	h.PostAuthRegister(register, regReq)
	if register.Code != http.StatusCreated {
		t.Fatalf("注册应 201，实际 %d（body=%s）", register.Code, register.Body.String())
	}
	// 这条用例要测的是登录第②层限流，不是邮箱验证：先把验证做掉，
	// 否则「密码正确但没带验证码」会先被 403 email_not_verified 拦下。
	verifyEmailForTest(t, pool, h, "it-limit@example.com")

	// 登录辅助：RemoteAddr 固定，body 里按需带验证码。
	login := func(ip, password, code string) *httptest.ResponseRecorder {
		body := `{"identifier":"it-limit","password":"` + password + `"`
		if code != "" {
			body += `,"emailCode":"` + code + `"`
		}
		body += `}`
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = ip + ":5555"
		h.PostAuthLogin(rec, req)
		return rec
	}

	// 前 5 次错密码：普通 401。
	for i := 0; i < 5; i++ {
		if rec := login("203.0.113.7", "wrong-password-xx", ""); rec.Code != http.StatusUnauthorized {
			t.Fatalf("第 %d 次错误密码应 401，实际 %d（body=%s）", i+1, rec.Code, rec.Body.String())
		}
	}

	// 第②层生效：**密码正确但没带验证码**也拿不到会话，返回 verification_required。
	blocked := login("203.0.113.7", "correct-horse-battery", "")
	if blocked.Code != http.StatusUnauthorized {
		t.Fatalf("失败达阈值后应 401，实际 %d（body=%s）", blocked.Code, blocked.Body.String())
	}
	if !strings.Contains(blocked.Body.String(), "verification_required") {
		t.Fatalf("应返回 verification_required，实际 %s", blocked.Body.String())
	}

	// **换一个 IP 也照样被拦**——第②层按标识符计数，与 IP 无关。
	// 这正是「多 IP 定向撞库」的防护点：没有它，攻击者换个 IP 就能继续试。
	if rec := login("198.51.100.9", "correct-horse-battery", ""); rec.Code != http.StatusUnauthorized ||
		!strings.Contains(rec.Body.String(), "verification_required") {
		t.Fatalf("换 IP 后仍应要求验证码，实际 %d（body=%s）", rec.Code, rec.Body.String())
	}

	// 第①层（账号+IP 10 次 → 429）仍然在，只是被第②层挡在前面：要走到 10 次，
	// 每次都得带着（错的）验证码，这样每次都会被记一次失败。
	// 已有 6 条失败（5 次错密码 + 1 次没带码），再补 4 条凑满第①层的 10 次。
	for i := 0; i < 4; i++ {
		if rec := login("203.0.113.7", "correct-horse-battery", "000000"); rec.Code != http.StatusUnauthorized {
			t.Fatalf("第 %d 次（带错码）应 401，实际 %d（body=%s）", i+1, rec.Code, rec.Body.String())
		}
	}
	locked := login("203.0.113.7", "correct-horse-battery", "000000")
	if locked.Code != http.StatusTooManyRequests {
		t.Fatalf("账号+IP 满 10 次应 429，实际 %d（body=%s）", locked.Code, locked.Body.String())
	}
	if locked.Header().Get("Retry-After") == "" {
		t.Fatal("429 必须带 Retry-After")
	}
}

// 封禁账号在传输层的表现：登录 403 forbidden；**已经发出去的 cookie 也立刻作废**。
//
// 会话表里的 status 是签发时的快照，封禁不会回头改它——所以拒绝必须读
// accounts 的当前值。只改登录判定的话，在线的人换个页签就绕过去了。
func TestIntegrationBannedAccountHTTP(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()
	for _, q := range []string{"DELETE FROM demo_stats", "DELETE FROM demo_channels", "DELETE FROM demo_revisions", "DELETE FROM demos", "DELETE FROM web_sessions", "DELETE FROM users", "DELETE FROM accounts"} {
		if _, err := pool.Exec(ctx, q); err != nil {
			t.Fatalf("%s 失败: %v", q, err)
		}
	}
	h := New(pool, store.New(pool), integrationIdentity(t, pool), nil, nil)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", strings.NewReader(
		`{"email":"it-banned@example.com","password":"correct-horse-battery","username":"it-banned","displayName":"封禁测试"}`))
	req.Header.Set("Content-Type", "application/json")
	h.PostAuthRegister(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("注册应 201，实际 %d（body=%s）", rec.Code, rec.Body.String())
	}
	// 注册不再发会话，所以要自己拿一个：先验证邮箱，再登录。
	verifyEmailForTest(t, pool, h, "it-banned@example.com")
	loginRec := httptest.NewRecorder()
	loginReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(
		`{"identifier":"it-banned","password":"correct-horse-battery"}`))
	loginReq.Header.Set("Content-Type", "application/json")
	h.PostAuthLogin(loginRec, loginReq)
	if loginRec.Code != http.StatusOK {
		t.Fatalf("登录应 200，实际 %d（body=%s）", loginRec.Code, loginRec.Body.String())
	}
	cookies := loginRec.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatal("登录应下发会话 cookie")
	}

	// banned 必须有来源状态：db/schema/10-identity.sql 的 ck_accounts_previous_status
	// 要求 previous_status ∈ {active, suspended}，这里照约束写，不绕过。
	if _, err := pool.Exec(ctx,
		"UPDATE accounts SET previous_status=status, status='banned' WHERE id IN (SELECT account_id FROM profiles WHERE lower(username)='it-banned')"); err != nil {
		t.Fatalf("设置封禁失败: %v", err)
	}

	// ① 旧 cookie 立刻失效。403 而不是 401：401 会让人以为该重新登录，
	//    而重新登录只会再吃一次 403。
	meReq := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	meReq.AddCookie(cookies[0])
	meRec := httptest.NewRecorder()
	h.GetMe(meRec, meReq)
	if meRec.Code != http.StatusForbidden {
		t.Fatalf("封禁后 /me 应 403，实际 %d（body=%s）", meRec.Code, meRec.Body.String())
	}
	if !strings.Contains(meRec.Body.String(), "forbidden") {
		t.Fatalf("应返回 forbidden，实际 %s", meRec.Body.String())
	}

	// ② 重新登录：403 + 明说封禁，而不是「密码不对」——否则他会一直重试密码。
	lrec := httptest.NewRecorder()
	lreq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(
		`{"identifier":"it-banned","password":"correct-horse-battery"}`))
	lreq.Header.Set("Content-Type", "application/json")
	h.PostAuthLogin(lrec, lreq)
	if lrec.Code != http.StatusForbidden {
		t.Fatalf("封禁账号登录应 403，实际 %d（body=%s）", lrec.Code, lrec.Body.String())
	}
}

// 可见性三值的两处落地：渠道 unlisted/hidden 不该进列表，未过审的 published 不该出内容。
func TestIntegrationVisibilityAndModeration(t *testing.T) {
	pool := integrationPool(t)
	seed(t, pool)

	h := New(pool, store.New(pool), integrationIdentity(t, pool), nil, nil)
	listTotal := func() float64 {
		rec := httptest.NewRecorder()
		h.ListDemos(rec, httptest.NewRequest(http.MethodGet, "/api/v1/demos", nil), api.ListDemosParams{})
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("列表响应不是 JSON: %v", err)
		}
		n, _ := body["total"].(float64)
		return n
	}

	ctx := context.Background()
	if got := listTotal(); got != 1 {
		t.Fatalf("基线：expected 1，got %v", got)
	}

	// 渠道改成 hidden → 列表里应当消失。这条以前是死的（查询根本没有渠道条件）。
	if _, err := pool.Exec(ctx, `UPDATE demo_channels SET visibility = 'hidden'`); err != nil {
		t.Fatalf("改渠道可见性失败: %v", err)
	}
	if got := listTotal(); got != 0 {
		t.Fatalf("隐藏渠道后：expected 0，got %v", got)
	}
	// 注意顺序：触发器 trg_demos_guard 拒绝「还有 listed 渠道时 approved → 非 approved」，
	// 所以先把渠道藏起来再退审。这也说明这条不变量真的在拦，不是纸面条款。
	//
	// published 但没审过 → 列表不出现，详情 404。当前没有端点能造出这个组合，
	// 但它是「审核形同虚设」的唯一入口，值得在数据层钉住。
	if _, err := pool.Exec(ctx, `UPDATE demos SET moderation_state = 'rejected' WHERE slug = 'it-published'`); err != nil {
		t.Fatalf("改审核态失败: %v", err)
	}
	if got := listTotal(); got != 0 {
		t.Fatalf("未过审后列表：expected 0，got %v", got)
	}
	rec := httptest.NewRecorder()
	h.GetDemo(rec, httptest.NewRequest(http.MethodGet, "/api/v1/demos/it-published", nil), "it-published")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("未过审的详情：expected 404，got %d", rec.Code)
	}
}
