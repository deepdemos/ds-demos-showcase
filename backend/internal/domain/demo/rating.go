package demo

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

// 作品评分（DEMO-019 / DEMO-020）。
//
// # 一人一票，改分不追加
//
// 去重键是 `rater_key`：登录用户 `user:<id>`、匿名 `anon:<HMAC(ip)>`——与浏览计数
// 同一套口径（identity.Service.ViewerKey）。两处各算各的，同一个人就会变成两个人。
//
// # 匿名也能打（rate-anon A）
//
// 民间成果展的作品大多是给外人看的。要求先注册才能说一句「这个不错」，等于把最轻
// 的一次表态推到最重的门槛后面。代价是同一出口 IP 的多人共用一票——与浏览计数
// 第 39 条同一个已知松点。
//
// # 作者可以给自己打分（rate-self B）
//
// 不拦。作者给自己作品打 5 分这件事在页面上是看得见的（作者名与分数都在），
// 影响面只有自己的作品；为它加一条「不能给自己投票」的规则，会连带把
// 「作者参与自己的作品合集」这类正当操作也拦住。
//
// # 汇总为什么要重算而不是增量
//
// 改分与撤销都会让总和减少。增量写法要记住旧分（一次 UPDATE 里同时拿到 before/after），
// 而重算只有一条路径、错了再跑一次就能恢复。见 `RefreshDemoRatingStats` 的注释。

// RatingScore 是一次打分的取值范围。
const (
	MinRatingScore = 1
	MaxRatingScore = 5
)

// ErrRatingOutOfRange 表示分数不在 1–5。
var ErrRatingOutOfRange = errors.New("评分必须在 1 到 5 之间")

// Rating 是一件作品当前的评分汇总，外加「当前调用者打了几分」。
//
// `Mine` 为空 = 没打过。匿名调用者也带它：改分时界面要能回显自己刚才打的那一分。
type Rating struct {
	Average float64
	Count   int
	Mine    *int
}

// Rate 记一次打分（改分是 UPDATE）。viewerKey 为空时直接拒绝——
// 没有去重键就没法保证一人一票，写进去只会变成可以刷的分。
func (s *Service) Rate(ctx context.Context, slug, viewerKey string, accountID int64, score int) (Rating, error) {
	if score < MinRatingScore || score > MaxRatingScore {
		return Rating{}, ErrRatingOutOfRange
	}
	if slug == "" || viewerKey == "" {
		return Rating{}, ErrNotFound
	}

	var out Rating
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)

		demoID, err := q.UpsertDemoRating(ctx, store.UpsertDemoRatingParams{
			Slug:     slug,
			RaterKey: viewerKey,
			Score:    int32(score),
			UserID:   nullableID(accountID),
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				// 作品不存在 / 未发布 / 已删——与详情的 404 同一判据
				return ErrNotFound
			}
			return fmt.Errorf("demo: 写评分失败: %w", err)
		}

		stats, err := q.RefreshDemoRatingStats(ctx, demoID)
		if err != nil {
			return fmt.Errorf("demo: 重算评分汇总失败: %w", err)
		}

		mine := score
		out = Rating{
			Average: numericToFloat(stats.RatingAvg),
			Count:   int(stats.RatingCount),
			Mine:    &mine,
		}
		return nil
	})
	return out, err
}

// Unrate 撤销自己的一票。没打过也算成功——「我不想给它打分了」这个意图本来就成立。
func (s *Service) Unrate(ctx context.Context, slug, viewerKey string) (Rating, error) {
	if slug == "" || viewerKey == "" {
		return Rating{}, ErrNotFound
	}

	var out Rating
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)

		demoID, err := q.DeleteDemoRating(ctx, store.DeleteDemoRatingParams{
			Slug:     slug,
			RaterKey: viewerKey,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				// 没打过。作品是否存在要单独问一次：不存在要 404，存在就回当前汇总。
				id, lookupErr := q.DemoIDForRating(ctx, slug)
				if lookupErr != nil {
					if errors.Is(lookupErr, pgx.ErrNoRows) {
						return ErrNotFound
					}
					return fmt.Errorf("demo: 查作品失败: %w", lookupErr)
				}
				stats, err := q.RefreshDemoRatingStats(ctx, id)
				if err != nil {
					return fmt.Errorf("demo: 重算评分汇总失败: %w", err)
				}
				out = Rating{Average: numericToFloat(stats.RatingAvg), Count: int(stats.RatingCount)}
				return nil
			}
			return fmt.Errorf("demo: 删评分失败: %w", err)
		}

		stats, err := q.RefreshDemoRatingStats(ctx, demoID)
		if err != nil {
			return fmt.Errorf("demo: 重算评分汇总失败: %w", err)
		}
		out = Rating{Average: numericToFloat(stats.RatingAvg), Count: int(stats.RatingCount)}
		return nil
	})
	return out, err
}

// RatingOf 读一件作品的评分汇总，`viewerKey` 用来填 `Mine`（空则不填）。
//
// 读失败不报错：评分是详情页的附加信息，它挂了不该让详情页也 503。
// 这条原则与浏览计数一致（见 view.go）。
func (s *Service) RatingOf(ctx context.Context, slug, viewerKey string) Rating {
	out := Rating{}
	stats, err := s.q.RatingStatsBySlug(ctx, slug)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			s.log.Warn("读评分汇总失败", "slug", slug, "err", err)
		}
		return out
	}
	out.Average = numericToFloat(stats.RatingAvg)
	out.Count = int(stats.RatingCount)
	if viewerKey != "" {
		if score, err := s.q.MyDemoRating(ctx, store.MyDemoRatingParams{
			Slug:     slug,
			RaterKey: viewerKey,
		}); err == nil {
			mine := int(score)
			out.Mine = &mine
		}
	}
	return out
}

// nullableID 把 0 当成「没有账号」。匿名访客的 user_id 必须留空——
// 写入一个 0 会让 `user_id` 指向不存在的行（它有外键）。
func nullableID(id int64) pgtype.Int8 {
	if id == 0 {
		return pgtype.Int8{}
	}
	return pgtype.Int8{Int64: id, Valid: true}
}

// numericToFloat 把 numeric(3,2) 摊成 float64。
//
// 直接 `v.Float64` 读不出来：pgx 把 numeric 当字符串给，除非扫描到具体数值类型。
func numericToFloat(v pgtype.Numeric) float64 {
	f, err := v.Float64Value()
	if err != nil || !f.Valid {
		return 0
	}
	return f.Float64
}
