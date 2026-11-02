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
}

func TestIntegrationListAndDetail(t *testing.T) {
	pool := integrationPool(t)
	seed(t, pool)

	h := New(pool, store.New(pool), identity.New(pool, []byte("test-ip-secret")))

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
			if body["title"] != nil {
				t.Fatalf("墓碑态 title 必须为 null，实际 %v", body["title"])
			}
		}
	}
}

// seedMe 造一个「已登录」的最小现场：账号 + 资料 + 主邮箱 + 一条有效会话。
func seedMe(t *testing.T, pool *pgxpool.Pool, sessionID string) {
	t.Helper()
	ctx := context.Background()
	for _, q := range []string{"DELETE FROM web_sessions", "DELETE FROM accounts"} {
		if _, err := pool.Exec(ctx, q); err != nil {
			t.Fatalf("%s 失败: %v", q, err)
		}
	}
	// 用一个 CTE 一次装完四张表。PostgreSQL 的数据修改型 CTE 即使没人读也会执行完，
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
	h := New(pool, store.New(pool), identity.New(pool, []byte("test-ip-secret")))
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
	for _, q := range []string{"DELETE FROM web_sessions", "DELETE FROM accounts"} {
		if _, err := pool.Exec(ctx, q); err != nil {
			t.Fatalf("%s 失败: %v", q, err)
		}
	}
	h := New(pool, store.New(pool), identity.New(pool, []byte("test-ip-secret")))

	register := func(body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		h.PostAuthRegister(rec, req)
		return rec
	}

	// 正常注册：201 + 会话 cookie + 不回声密码。
	rec := register(`{"email":"it-reg@example.com","password":"correct-horse-battery","username":"it-reg","displayName":"注册测试"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("注册应 201，实际 %d（body=%s）", rec.Code, rec.Body.String())
	}
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != sessionCookie || !cookies[0].HttpOnly || !cookies[0].Secure {
		t.Fatalf("会话 cookie 不对: %+v", cookies)
	}
	if strings.Contains(rec.Body.String(), "correct-horse-battery") {
		t.Fatal("响应体里出现了明文密码")
	}
	var me api.Me
	if err := json.Unmarshal(rec.Body.Bytes(), &me); err != nil {
		t.Fatalf("响应不是合法 Me: %v", err)
	}
	if me.Username != "it-reg" || me.Status != api.AccountStatusActive || me.Sub == "" {
		t.Fatalf("字段不对: %+v", me)
	}

	// 刚注册拿到的 cookie 必须立刻能用。
	meReq := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	meReq.AddCookie(cookies[0])
	meRec := httptest.NewRecorder()
	h.GetMe(meRec, meReq)
	if meRec.Code != http.StatusOK {
		t.Fatalf("注册后 /me 应 200，实际 %d（body=%s）", meRec.Code, meRec.Body.String())
	}

	// 数据库里只留哈希：明文不得入库。
	var stored string
	if err := pool.QueryRow(ctx, `
		SELECT c.secret_hash FROM credentials c
		JOIN accounts a ON a.id = c.account_id WHERE a.sub = $1`, me.Sub).Scan(&stored); err != nil {
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
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM profiles WHERE lower(username) IN ('it-reg','other-one','it-short')`).Scan(&n); err != nil {
		t.Fatalf("计数失败: %v", err)
	}
	if n != 1 {
		t.Fatalf("失败路径不应留下账号行，实际 %d 条", n)
	}
}

func TestIntegrationLoginLogout(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()
	for _, q := range []string{"DELETE FROM web_sessions", "DELETE FROM accounts"} {
		if _, err := pool.Exec(ctx, q); err != nil {
			t.Fatalf("%s 失败: %v", q, err)
		}
	}
	h := New(pool, store.New(pool), identity.New(pool, []byte("test-ip-secret")))

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
		"DELETE FROM login_attempts", "DELETE FROM web_sessions", "DELETE FROM accounts",
	} {
		if _, err := pool.Exec(ctx, q); err != nil {
			t.Fatalf("%s 失败: %v", q, err)
		}
	}
	h := New(pool, store.New(pool), identity.New(pool, []byte("test-ip-secret")))

	register := httptest.NewRecorder()
	regReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register",
		strings.NewReader(`{"email":"it-limit@example.com","password":"correct-horse-battery","username":"it-limit","displayName":"限流测试"}`))
	regReq.Header.Set("Content-Type", "application/json")
	h.PostAuthRegister(register, regReq)
	if register.Code != http.StatusCreated {
		t.Fatalf("注册应 201，实际 %d（body=%s）", register.Code, register.Body.String())
	}

	// 同一个 IP、同一个账号连试 10 次错密码 → 第 11 次必须 429 且带 Retry-After。
	login := func(password string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login",
			strings.NewReader(`{"identifier":"it-limit","password":"`+password+`"}`))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "203.0.113.7:5555"
		h.PostAuthLogin(rec, req)
		return rec
	}
	for i := 0; i < 10; i++ {
		if rec := login("wrong-password-xx"); rec.Code != http.StatusUnauthorized {
			t.Fatalf("第 %d 次错误密码应 401，实际 %d（body=%s）", i+1, rec.Code, rec.Body.String())
		}
	}
	blocked := login("correct-horse-battery")
	if blocked.Code != http.StatusTooManyRequests {
		t.Fatalf("达到阈值后即使密码正确也应 429，实际 %d（body=%s）", blocked.Code, blocked.Body.String())
	}
	if blocked.Header().Get("Retry-After") == "" {
		t.Fatal("429 必须带 Retry-After")
	}

	// 换一个 IP 仍然被拦（第 1 层是「账号+IP」，但账号维度…）
	// 注意：§11.3 第 1 层是账号+IP，第 2 层（验证码）未实现，所以换 IP 的账号维度
	// 现在**不拦**——这是已知缺口，测试把当前行为钉住，等第 2 层落地时改这里。
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login",
		strings.NewReader(`{"identifier":"it-limit","password":"correct-horse-battery"}`))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "198.51.100.9:5555"
	h.PostAuthLogin(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("换 IP 后按当前实现应能登录（第 2 层未做），实际 %d（body=%s）", rec.Code, rec.Body.String())
	}

	// 第 1 层是**时间锁**：锁住的 15 分钟内，即使密码正确、即使账号刚在别处登录成功，
	// 同一个 IP 仍然拿不到 429。这是 §11.3 表里「429，锁 15 分钟」的直接含义。
	//
	// 而「成功登录清零」清的是**账号维度**（第 2 层的验证码门槛），不是这一层——
	// 第 2 层还没做，所以那条口径目前没有代码路径可验。
	if rec := login("correct-horse-battery"); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("锁定期内应保持 429，实际 %d（body=%s）", rec.Code, rec.Body.String())
	}
}
