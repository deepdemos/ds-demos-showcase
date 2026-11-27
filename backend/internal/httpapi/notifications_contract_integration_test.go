package httpapi

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/identity"
)

// 通知旧列收口（#592 / contract 半步）：把 demo_slug / demo_title 从库里删掉。
//
// 删列会把**只为它们存在**的东西一起带走，所以这条用例不只断言「两列没了」：
//
//   - 约束：ck_notifications_slug（旧的长度闸）与 ck_notifications_title（旧的标题闸）
//     应当跟着列一起消失；主键 / 两条外键 / 新列的三条 CHECK 一条都不能少。
//   - 索引：本表两条索引都没引用旧列，删列不该把它们带走——它们分别承载列表排序与
//     未读计数，丢了不会报错，只会慢慢变慢。
//
// 反过来，**长度闸本身不能跟着旧列消失**：旧 ck_notifications_title（≤200）的等价值
// 在 ck_notifications_object_title 上，对象键的 1..128 在 ck_notifications_object_key 上。
// 所以这里对三条新 CHECK 各做一次真实写入，撞 23514 且撞对了约束名才算过——
// 只读目录的话，「把闸删了」也能绿（同类教训见 checkpoints_integration_test.go 文件头）。
func TestIntegrationNotificationLegacyColumnsDropped(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()
	_, _, idSvc := newIdempotencyHandler(t)

	account, _ := registerVerified(t, pool, idSvc, identity.RegisterInput{
		Email:       "it-notif-contract@example.com",
		Password:    "correct-horse-battery",
		Username:    "it-notif-contract",
		DisplayName: "旧列收口",
	})
	t.Cleanup(func() { cleanupAuthor(t, pool, account.Sub) })

	has := func(list, name string) bool {
		for _, item := range strings.Split(list, ",") {
			if item == name {
				return true
			}
		}
		return false
	}

	// 1) 列：旧列没了，对象三列还在。
	var columns string
	if err := pool.QueryRow(ctx, `
SELECT coalesce(string_agg(attname::text, ',' ORDER BY attnum), '')
FROM pg_attribute
WHERE attrelid = 'notifications'::regclass AND attnum > 0 AND NOT attisdropped`).Scan(&columns); err != nil {
		t.Fatalf("读 notifications 的列失败: %v", err)
	}
	for _, legacy := range []string{"demo_slug", "demo_title"} {
		if has(columns, legacy) {
			t.Fatalf("旧列 %s 应当已被迁移删掉，notifications 现在还有它（列：%s）", legacy, columns)
		}
	}
	for _, kept := range []string{"object_type", "object_key", "object_title"} {
		if !has(columns, kept) {
			t.Fatalf("对象列 %s 不该被删（列：%s）", kept, columns)
		}
	}

	// 2) 约束：旧列专属的 CHECK 跟着列走，其余一条不能少。
	var constraints string
	if err := pool.QueryRow(ctx, `
SELECT coalesce(string_agg(conname, ',' ORDER BY conname), '')
FROM pg_constraint
WHERE conrelid = 'notifications'::regclass`).Scan(&constraints); err != nil {
		t.Fatalf("读 notifications 的约束失败: %v", err)
	}
	for _, gone := range []string{"ck_notifications_slug", "ck_notifications_title"} {
		if has(constraints, gone) {
			t.Fatalf("旧列专属的约束 %s 应当跟着列一起删掉（约束：%s）", gone, constraints)
		}
	}
	for _, kept := range []string{
		"notifications_pkey",
		"fk_notifications_account",
		"fk_notifications_kind",
		"ck_notifications_object_type",
		"ck_notifications_object_key",
		"ck_notifications_object_title",
	} {
		if !has(constraints, kept) {
			t.Fatalf("约束 %s 不该被删列带走（约束：%s）", kept, constraints)
		}
	}

	// 3) 索引：两条索引都没引用旧列，删列不该把它们带走。
	var indexes string
	if err := pool.QueryRow(ctx, `
SELECT coalesce(string_agg(indexname, ',' ORDER BY indexname), '')
FROM pg_indexes
WHERE schemaname = current_schema() AND tablename = 'notifications'`).Scan(&indexes); err != nil {
		t.Fatalf("读 notifications 的索引失败: %v", err)
	}
	for _, kept := range []string{"ix_notifications_time", "ix_notifications_unread"} {
		if !has(indexes, kept) {
			t.Fatalf("索引 %s 不该被删列带走（索引：%s）", kept, indexes)
		}
	}

	// 4) 真实写入：只给对象三列就能落一行——这就是 contract 半步的意义，
	//    库结构不再要求「每条通知都得挂着一件作品」。
	insert := func(objectType, objectKey, objectTitle string) error {
		_, err := pool.Exec(ctx, `
INSERT INTO notifications (account_id, kind, object_type, object_key, object_title)
VALUES ((SELECT id FROM accounts WHERE sub = $1), 'review.approved', $2, $3, $4)`,
			account.Sub, objectType, objectKey, objectTitle)
		return err
	}
	if err := insert("demo", "it-notif-contract-demo", "收口作品"); err != nil {
		t.Fatalf("只写对象三列应当能落一行（还依赖旧列就说明收口没做完）: %v", err)
	}

	cases := []struct{ name, want, objectType, objectKey, objectTitle string }{
		{"对象键为空", "ck_notifications_object_key", "demo", "", "空键"},
		{"对象键超长", "ck_notifications_object_key", "demo", strings.Repeat("a", 129), "超长键"},
		{"标题超长", "ck_notifications_object_title", "demo", "it-notif-contract-long", strings.Repeat("标", 201)},
		{"对象类型大写", "ck_notifications_object_type", "Demo", "it-notif-contract-type", "大写类型"},
	}
	for _, tc := range cases {
		err := insert(tc.objectType, tc.objectKey, tc.objectTitle)
		if err == nil {
			t.Fatalf("%s 应当撞 %s，实际写进去了", tc.name, tc.want)
		}
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
			t.Fatalf("%s 应当撞 CHECK（23514），实际: %v", tc.name, err)
		}
		if pgErr.ConstraintName != tc.want {
			t.Fatalf("%s 撞的应当是 %s，实际 %s", tc.name, tc.want, pgErr.ConstraintName)
		}
	}
}
