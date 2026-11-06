package httpapi

import (
	"context"
	"testing"
	"time"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/identity"
)

// 发布时间只算**对外可见**的渠道。
//
// # 为什么要有这条
//
// 早先这条子查询取的是全部渠道的最早 `published_at`，把 `hidden` 也算了进去。
// `hidden` 的语义是「内部预览，对外不可见」——「先发 hidden 确认，再上 listed」
// 正是这个可见性存在的理由，而按旧口径，详情页上显示的发布时间会比真正对外
// 的那天早几个月。
//
// 更值得记的是它怎么来的：单一出处门禁报「可见性副本从 5 涨到 6」，我为了让它
// 绿，把过滤删了。门禁只数文本、不判语义，删掉之后它当然绿了——语义却退了一版。
// 现在的做法是判据只活在视图 demo_visible_channels 里，调用处只问视图。
//
// 反向验证：把子查询改回 `FROM demo_channels c`（不过滤），第一条断言立刻红。
func TestIntegrationDetailPublishedAtCountsVisibleChannelsOnly(t *testing.T) {
	h, _, idSvc := newIdempotencyHandler(t, 0)
	pool := integrationPool(t)
	ctx := context.Background()

	acc, sid := registerVerified(t, pool, idSvc, identity.RegisterInput{
		Email: "it-published-at@example.com", Password: "publishedat123",
		Username: "it-published-at", DisplayName: "发布时间作者",
	})
	t.Cleanup(func() { cleanupAuthor(t, pool, acc.Sub) })

	slug := submitHTML(t, h, sid, "发布时间作品", `<!doctype html><p>v1</p>`)
	publishNow(t, pool, slug)

	// 造现场：对外可见的渠道晚 20 天发布，内部预览渠道早 1 天。
	// 两者都以渠道行的 created_at 为基准，否则会撞 ck_channel_time。
	if _, err := pool.Exec(ctx, `
		UPDATE demo_channels SET published_at = created_at + interval '20 days'
		 WHERE demo_id = (SELECT id FROM demos WHERE slug = $1)`, slug); err != nil {
		t.Fatalf("改对外渠道发布时间失败: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO demo_channels (demo_id, channel, visibility, published_at, created_at, updated_at)
		SELECT demo_id, 'astra', 'hidden', created_at + interval '1 day', created_at, created_at
		  FROM demo_channels
		 WHERE demo_id = (SELECT id FROM demos WHERE slug = $1) AND channel = 'deep'`, slug); err != nil {
		t.Fatalf("插入 hidden 渠道失败: %v", err)
	}

	var want time.Time
	if err := pool.QueryRow(ctx, `
		SELECT min(c.published_at) FROM demo_visible_channels c
		 WHERE c.demo_id = (SELECT id FROM demos WHERE slug = $1)`, slug).Scan(&want); err != nil {
		t.Fatalf("读期望发布时间失败: %v", err)
	}

	detail := decodeDetail(t, getDemoAs(t, h, slug, ""))
	if detail.PublishedAt == nil {
		t.Fatalf("访客看到 publishedAt 为空，想要 %s", want.Format(time.RFC3339))
	}
	if !detail.PublishedAt.Equal(want) {
		t.Fatalf("publishedAt = %s，想要 %s（把 hidden 渠道算进去了？）",
			detail.PublishedAt.Format(time.RFC3339), want.Format(time.RFC3339))
	}

	// 反面确认：这个时刻确实晚于 hidden 渠道那次发布，否则上面的断言不区分两种口径。
	var hiddenAt time.Time
	if err := pool.QueryRow(ctx, `
		SELECT published_at FROM demo_channels c
		 WHERE c.demo_id = (SELECT id FROM demos WHERE slug = $1) AND c.channel = 'astra'`, slug).Scan(&hiddenAt); err != nil {
		t.Fatalf("读 hidden 渠道发布时间失败: %v", err)
	}
	if !want.After(hiddenAt) {
		t.Fatalf("现场造错了：可见渠道 %s 不晚于 hidden 渠道 %s", want, hiddenAt)
	}
}
