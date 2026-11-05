package demo

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

// 浏览计数（DEMO-041）。
//
// # 为什么计数入口是独立端点而不是在详情里顺带加
//
// 详情是 GET。给它加副作用，等于让「爬虫抓一遍」「文档里试一下」「浏览器预取」
// 「curl 验证一下」全都变成一次浏览。分出来之后，只有真把页面渲染出来的人才会调它。
//
// # 一次浏览的定义
//
// 同一个人（登录用户按账号，匿名按 IP 哈希）对同一作品，同一个自然日只算一次。
// 去重行与 +1 在**同一条 SQL** 里（`RecordDemoView` 的 CTE）：
// 只有去重行真的插进去了，外层才会执行 +1，两步之间没有可以失败的间隙。
// 分成两条语句时，第二步失败会让这个人当天的这次浏览永久丢失——去重行已经在了。

// RecordView 记一次浏览。返回 true 表示这一次真的算数。
//
// 作品之外的因素一律不报错：slug 不存在、作品下架、已经软删——都返回 (false, nil)。
// 理由是这个端点被页面渲染后自动调用，报错只会把「作品不存在」这种正常情况
// 变成前端控制台里的一片红。真正的可见性判定在详情端点上，那才是该给 404 的地方。
func (s *Service) RecordView(ctx context.Context, slug, viewerKey string) (bool, error) {
	if viewerKey == "" || slug == "" {
		return false, nil
	}

	_, err := s.q.RecordDemoView(ctx, store.RecordDemoViewParams{Slug: slug, ViewerKey: viewerKey})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// 作品不可见，或者今天已经看过 —— 查询是 `ON CONFLICT DO NOTHING RETURNING`，
			// 两种「没有插入」都表现为没有返回行。
			// 两种都不该报错：这是本功能的正常路径，不是异常。
			return false, nil
		}
		return false, fmt.Errorf("demo: 记浏览失败: %w", err)
	}
	return true, nil
}
