package identity

import (
	"context"
	"errors"
	"testing"
)

// 运维命令 `cmd/admin-grant` 背后的那段逻辑：提权 / 建号并提权 / 幂等 / 拒绝未登记角色。
//
// 这些用例连真库跑（TEST_DATABASE_URL 未设置时跳过）。为什么不只测 cmd：
// 命令本身只是一层 flag 解析，会出错的判断全在 GrantRole 里，而那里要碰
// accounts / profiles / account_emails / password_credentials 四张表。

func TestIntegrationGrantRolePromotesExistingAccount(t *testing.T) {
	pool := testPool(t)
	s := testService(t, pool)
	email, _ := mkUser(t, s, "grant-promote")

	res, err := s.GrantRole(context.Background(), GrantInput{Email: email, Role: "admin"})
	if err != nil {
		t.Fatalf("提权失败: %v", err)
	}
	if res.Created {
		t.Error("账号本来就在，Created 不该是 true")
	}
	if !res.Changed {
		t.Error("角色从 user 变成 admin，Changed 应该是 true")
	}
	if res.Account.Role != "admin" {
		t.Errorf("返回值里的角色 = %q，期望 admin", res.Account.Role)
	}
	// 命令的回执靠这个字段说「改的是谁」——它空过（返回行的 SELECT 里没有 email 列），
	// 于是命令行打出了「已提权： → admin」。这里钉住。
	if res.Account.Email != email {
		t.Errorf("返回值里的邮箱 = %q，期望 %q", res.Account.Email, email)
	}

	// 落库了才算数：只看返回值的话，一次「只在内存里改了」的 bug 也能绿。
	var role string
	if err := pool.QueryRow(context.Background(),
		"SELECT role FROM accounts WHERE id = $1", res.Account.ID).Scan(&role); err != nil {
		t.Fatalf("查角色失败: %v", err)
	}
	if role != "admin" {
		t.Errorf("库里的角色 = %q，期望 admin", role)
	}
}

// 重跑不能出错、不能重复建号——运维命令最常被跑第二次（第一次跑一半断掉）。
func TestIntegrationGrantRoleIsIdempotent(t *testing.T) {
	pool := testPool(t)
	s := testService(t, pool)
	email, _ := mkUser(t, s, "grant-idem")

	first, err := s.GrantRole(context.Background(), GrantInput{Email: email, Role: "admin"})
	if err != nil {
		t.Fatalf("第一次提权失败: %v", err)
	}
	second, err := s.GrantRole(context.Background(), GrantInput{Email: email, Role: "admin"})
	if err != nil {
		t.Fatalf("第二次提权失败: %v", err)
	}
	if second.Changed {
		t.Error("已经是 admin 了，第二次不该报告改动")
	}
	if second.Account.ID != first.Account.ID {
		t.Errorf("两次拿到不同账号: %d vs %d", first.Account.ID, second.Account.ID)
	}
}

// 账号不存在时建号并直接提权——这就是「生产第一个管理员」的那条路。
func TestIntegrationGrantRoleCreatesMissingAccount(t *testing.T) {
	pool := testPool(t)
	s := testService(t, pool)
	const email = "it-grant-create@example.com"
	const username = "it-grant-create"
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(),
			"DELETE FROM accounts WHERE id IN (SELECT account_id FROM profiles WHERE lower(username) = lower($1))",
			username)
	})

	res, err := s.GrantRole(context.Background(), GrantInput{
		Email:       email,
		Role:        "admin",
		Username:    username,
		DisplayName: "第一个管理员",
		Password:    "correct-horse-battery",
	})
	if err != nil {
		t.Fatalf("建号提权失败: %v", err)
	}
	if !res.Created {
		t.Error("账号本来不存在，Created 应该是 true")
	}
	if res.Account.Role != "admin" {
		t.Errorf("角色 = %q，期望 admin（建号后必须紧接着提权）", res.Account.Role)
	}
	// 口令必须真的能登录：复用 Register 的意义就在这里，别自己拼 INSERT 拼出个没口令的账号。
	if _, _, err := s.Login(context.Background(), LoginInput{
		Identifier: email,
		Password:   "correct-horse-battery",
		IP:         "10.9.9.9",
	}); err != nil {
		t.Fatalf("建出来的账号登录不了: %v", err)
	}
}

// 拼错的角色名必须当场拒绝。accounts.role 没有外键，放过去就是一个「什么权限都没有」的账号。
func TestIntegrationGrantRoleRejectsUnknownRole(t *testing.T) {
	pool := testPool(t)
	s := testService(t, pool)
	email, _ := mkUser(t, s, "grant-badrole")

	res, err := s.GrantRole(context.Background(), GrantInput{Email: email, Role: "superuser"})
	if !errors.Is(err, ErrUnknownRole) {
		t.Fatalf("期望 ErrUnknownRole，实际 %v", err)
	}
	if res.Changed || res.Created {
		t.Error("报错时不该留下任何改动")
	}
	var role string
	if err := pool.QueryRow(context.Background(),
		"SELECT role FROM accounts WHERE id = (SELECT account_id FROM account_emails WHERE email = $1)",
		email).Scan(&role); err != nil {
		t.Fatalf("查角色失败: %v", err)
	}
	if role != "user" {
		t.Errorf("角色被改成了 %q，期望仍是 user", role)
	}
}

// 账号不存在又没给建号信息：要明确说「缺什么」，不能掉进「密码长度不对」这种误导。
func TestIntegrationGrantRoleRequiresCredentialsForNewAccount(t *testing.T) {
	pool := testPool(t)
	s := testService(t, pool)

	_, err := s.GrantRole(context.Background(), GrantInput{
		Email: "it-grant-nocreds@example.com",
		Role:  "admin",
	})
	if !errors.Is(err, ErrMissingForCreate) {
		t.Fatalf("期望 ErrMissingForCreate，实际 %v", err)
	}
}

// 封禁账号不能被提权：提权输入是邮箱，而邮箱会写错；封禁账号往往正是出过事的那个人。
func TestIntegrationGrantRoleRefusesBannedAccount(t *testing.T) {
	pool := testPool(t)
	s := testService(t, pool)
	email, _ := mkUser(t, s, "grant-banned")

	// banned 要求 previous_status 非空（ck_accounts_previous_status），所以一起写。
	if _, err := pool.Exec(context.Background(),
		"UPDATE accounts SET status = 'banned', previous_status = 'active' WHERE id = (SELECT account_id FROM account_emails WHERE email = $1)",
		email); err != nil {
		t.Fatalf("置为封禁失败: %v", err)
	}

	_, err := s.GrantRole(context.Background(), GrantInput{Email: email, Role: "admin"})
	if !errors.Is(err, ErrAccountNotActive) {
		t.Fatalf("期望 ErrAccountNotActive，实际 %v", err)
	}
	var role string
	if err := pool.QueryRow(context.Background(),
		"SELECT role FROM accounts WHERE id = (SELECT account_id FROM account_emails WHERE email = $1)",
		email).Scan(&role); err != nil {
		t.Fatalf("查角色失败: %v", err)
	}
	if role != "user" {
		t.Errorf("角色被改成了 %q，封禁账号不该被提权", role)
	}
}
