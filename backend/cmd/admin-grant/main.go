// admin-grant：把某个邮箱对应的账号提到管理员（或其它已登记角色）。
//
// 用法（在 backend/ 目录下）：
//
//	DATABASE_URL=postgres://... go run ./cmd/admin-grant -email you@example.com \
//	    -username you -display 你的名字 -password '至少十个字符'
//
// 存在这个命令的原因：生产上「第一个管理员从哪来」没有别的答案。
// 治理域 §7.3 的 `POST /admin/users/{id}/role` 要求调用方**已经是**管理员，
// 于是它永远无法产生第一个；在那之前只能 psql 手写 UPDATE，而手写 UPDATE 会绕过
// 角色名校验（accounts.role 没有外键），一个拼错的角色名会安静地留在库里，
// 表现为「这个人什么权限都没有」而不是「配置错了」。
//
// 幂等：账号已在就只提权，不重置口令；目标角色已是当前角色就什么都不写。
// 重跑安全——第一次中途失败（比如提权那步断了）之后重跑会补齐，不会建出第二个账号。
//
// 口令不落日志、不入参数字面量（会进 shell 历史）：推荐用环境变量传，
//
//	ADMIN_GRANT_PASSWORD='...' go run ./cmd/admin-grant -email you@example.com -username you
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/identity"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/platform/database"
)

func main() {
	var (
		email       = flag.String("email", "", "要提权的账号邮箱（必填）")
		role        = flag.String("role", "admin", "目标角色，必须是 roles 表里登记过的")
		username    = flag.String("username", "", "账号不存在时用来建号的用户名")
		displayName = flag.String("display", "", "账号不存在时建号的展示名（可空，空则显示用户名）")
		password    = flag.String("password", "", "账号不存在时建号的口令；建议改用 ADMIN_GRANT_PASSWORD 环境变量")
	)
	flag.Parse()

	if strings.TrimSpace(*email) == "" {
		fail("必须提供 -email")
	}
	pwd := *password
	if pwd == "" {
		// 环境变量优先于命令行参数：命令行会进 shell 历史，密码不该留在那里。
		pwd = os.Getenv("ADMIN_GRANT_PASSWORD")
	}

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		fail("DATABASE_URL 未设置")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := database.Open(ctx, dsn)
	if err != nil {
		fail("连接数据库失败: %v", err)
	}
	defer pool.Close()

	// verification 传 nil：这条路只走 Register 与改角色，不碰限流与验证码。
	// 传一个假的反而会让人以为这里有验证码逻辑。
	svc := identity.New(pool, nil, nil)

	res, err := svc.GrantRole(ctx, identity.GrantInput{
		Email:       *email,
		Role:        *role,
		Username:    *username,
		DisplayName: *displayName,
		Password:    pwd,
	})
	if err != nil {
		fail("%v", err)
	}

	switch {
	case res.Created:
		fmt.Printf("已建号并提权：%s → %s（账号 #%d，用户名 %s）\n", res.Account.Email, res.Role, res.Account.ID, res.Account.Username)
	case res.Changed:
		fmt.Printf("已提权：%s → %s（账号 #%d，用户名 %s）\n", res.Account.Email, res.Role, res.Account.ID, res.Account.Username)
	default:
		fmt.Printf("无需改动：%s 已经是 %s（账号 #%d）\n", res.Account.Email, res.Role, res.Account.ID)
	}
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "admin-grant: "+format+"\n", args...)
	os.Exit(1)
}
