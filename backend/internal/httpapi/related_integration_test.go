package httpapi

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/api"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/identity"
)

// 相关作品（内容信号：共用分类 +3 / 共用模型 +2 / 同一作者 +1）。
//
// 这一组用例必须自己造分类与模型关联：种子作品里只有一件有分类，
// 纯靠种子数据的话「相关」永远是空的，测试会变成「断言空数组」这种什么也不证明的形状。

func newRelatedHandler(t *testing.T) (*Handler, *identity.Service, *pgxpool.Pool) {
	t.Helper()
	return newSortHandler(t)
}

// classificationIDs 按 slug 取分类 id（genre/technique/subject 都在这张表）。
func classificationIDs(t *testing.T, pool *pgxpool.Pool, slugs ...string) []int64 {
	t.Helper()
	ids := make([]int64, 0, len(slugs))
	for _, slug := range slugs {
		var id int64
		if err := pool.QueryRow(context.Background(),
			`SELECT id FROM classifications WHERE slug = $1`, slug).Scan(&id); err != nil {
			t.Fatalf("取分类 %q 失败: %v", slug, err)
		}
		ids = append(ids, id)
	}
	return ids
}

// attachClassifications 直接写关联表：投稿接口不接收分类 id，只接收 slug。
func attachClassifications(t *testing.T, pool *pgxpool.Pool, slug string, classIDs []int64) {
	t.Helper()
	for _, id := range classIDs {
		if _, err := pool.Exec(context.Background(), `
INSERT INTO demo_classifications (demo_id, dimension, classification_id)
SELECT d.id, c.dimension, c.id
FROM demos d, classifications c
WHERE d.slug = $1 AND c.id = $2
ON CONFLICT DO NOTHING`, slug, id); err != nil {
			t.Fatalf("写分类关联失败: %v", err)
		}
	}
}

// attachModelByName 给作品的当前版本挂一个模型（模型不存在就建一个候选行）。
func attachModelByName(t *testing.T, pool *pgxpool.Pool, slug, modelName string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `
INSERT INTO models (slug, name, status, resolution)
VALUES (lower(replace($1, ' ', '-')), $1, 'candidate', 'exact')
ON CONFLICT (name) DO NOTHING`, modelName); err != nil {
		t.Fatalf("建模型失败: %v", err)
	}
	if _, err := pool.Exec(context.Background(), `
INSERT INTO demo_revision_models (revision_id, model_id)
SELECT d.current_revision_id, m.id FROM demos d, models m
WHERE d.slug = $1 AND m.name = $2
ON CONFLICT DO NOTHING`, slug, modelName); err != nil {
		t.Fatalf("写模型关联失败: %v", err)
	}
}

func related(t *testing.T, h *Handler, slug, query string) api.RelatedList {
	t.Helper()
	rec := getWithSession(t, h, "/api/v1/demos/"+slug+"/related"+query, "")
	if rec.Code != 200 {
		t.Fatalf("相关作品 %s = %d，想要 200；响应体：%s", slug, rec.Code, rec.Body.String())
	}
	var body api.RelatedList
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	return body
}

func relatedSlugs(t *testing.T, h *Handler, slug, query string) []string {
	t.Helper()
	return slugsOf(related(t, h, slug, query))
}

func slugsOf(body api.RelatedList) []string {
	out := make([]string, 0, len(body.Items))
	for _, it := range body.Items {
		out = append(out, it.Slug)
	}
	return out
}

// 共用分类权重最高：共用分类的作品排在只同作者的作品前面。
func TestIntegrationRelatedPrefersSharedClassification(t *testing.T) {
	h, idSvc, pool := newRelatedHandler(t)
	author, sid := registerVerified(t, pool, idSvc, registerInputFor(t, "相关作者"))
	t.Cleanup(func() { cleanupAuthor(t, pool, author.Sub) })
	resetPublished(t, pool)

	ids := classificationIDs(t, pool, "game", "3D建模")
	src := publishRated(t, h, pool, sid, "源作品", ratingStats{view: 1}, 0)
	sameCls := publishRated(t, h, pool, sid, "同分类", ratingStats{}, 0)
	otherAuthor, otherSid := registerVerified(t, pool, idSvc, registerInputFor(t, "另一个作者"))
	t.Cleanup(func() { cleanupAuthor(t, pool, otherAuthor.Sub) })
	alien := publishRated(t, h, pool, otherSid, "毫无关系", ratingStats{}, 0)

	attachClassifications(t, pool, src, ids)
	attachClassifications(t, pool, sameCls, ids)

	got := relatedSlugs(t, h, src, "")
	if len(got) != 1 || got[0] != sameCls {
		t.Fatalf("相关作品 = %v，想要 [%s]（共用分类 +3；无关的 %s 不该出现）", got, sameCls, alien)
	}
}

// 共用模型 +2：没有共用分类时，同模型的作品也能被找出来。
func TestIntegrationRelatedUsesModelSignal(t *testing.T) {
	h, idSvc, pool := newRelatedHandler(t)
	author, sid := registerVerified(t, pool, idSvc, registerInputFor(t, "模型信号作者"))
	t.Cleanup(func() { cleanupAuthor(t, pool, author.Sub) })
	resetPublished(t, pool)

	// 三件作品三个作者：这条用例只验模型信号。「同作者 +1」会把别的作品带进来，
	// 那样模型信号失效时用例照样通过——失败样本必须只剩一个变量。
	peer, peerSid := registerVerified(t, pool, idSvc, registerInputFor(t, "同模型作者"))
	t.Cleanup(func() { cleanupAuthor(t, pool, peer.Sub) })
	other, otherSid := registerVerified(t, pool, idSvc, registerInputFor(t, "无关作者"))
	t.Cleanup(func() { cleanupAuthor(t, pool, other.Sub) })

	src := publishRated(t, h, pool, sid, "同模型源", ratingStats{}, 0)
	sameModel := publishRated(t, h, pool, peerSid, "同模型邻", ratingStats{}, 0)
	publishRated(t, h, pool, otherSid, "无关作品", ratingStats{}, 0)

	attachModelByName(t, pool, src, "DeepSeek V4 Flash")
	attachModelByName(t, pool, sameModel, "DeepSeek V4 Flash")

	got := relatedSlugs(t, h, src, "")
	if len(got) != 1 || got[0] != sameModel {
		t.Fatalf("相关作品 = %v，想要 [%s]", got, sameModel)
	}
}

// 同作者 +1：没有分类也没有模型时，作者自己的其他作品仍算相关。
func TestIntegrationRelatedUsesAuthorSignal(t *testing.T) {
	h, idSvc, pool := newRelatedHandler(t)
	author, sid := registerVerified(t, pool, idSvc, registerInputFor(t, "同作者"))
	t.Cleanup(func() { cleanupAuthor(t, pool, author.Sub) })
	resetPublished(t, pool)

	src := publishRated(t, h, pool, sid, "我的作品一", ratingStats{}, 0)
	sibling := publishRated(t, h, pool, sid, "我的作品二", ratingStats{}, 0)

	rec := related(t, h, src, "")
	got := slugsOf(rec)
	if len(got) != 1 || got[0] != sibling {
		t.Fatalf("相关作品 = %v，想要 [%s]", got, sibling)
	}
	// 契约里 classifications 是 required。空数组合法，null 不合法——null 会让前端
	// 多一条只有「这件作品没打分类」时才会走到的分支，是最容易漏测的那一格。
	if rec.Items[0].Classifications == nil {
		t.Fatal("没有分类的作品，classifications 应当是空数组而不是 null")
	}
}

// 得分 0 的作品不返回：相关作品区空着，而不是塞一批无关的凑数。
func TestIntegrationRelatedReturnsEmptyRatherThanFiller(t *testing.T) {
	h, idSvc, pool := newRelatedHandler(t)
	author, sid := registerVerified(t, pool, idSvc, registerInputFor(t, "孤立作者"))
	t.Cleanup(func() { cleanupAuthor(t, pool, author.Sub) })
	resetPublished(t, pool)

	src := publishRated(t, h, pool, sid, "孤立作品", ratingStats{}, 0)

	other, otherSid := registerVerified(t, pool, idSvc, registerInputFor(t, "别人"))
	t.Cleanup(func() { cleanupAuthor(t, pool, other.Sub) })
	publishRated(t, h, pool, otherSid, "别人的作品", ratingStats{}, 0)

	got := relatedSlugs(t, h, src, "")
	if len(got) != 0 {
		t.Fatalf("没有相关信号时返回了 %v，想要空数组", got)
	}
}

// 下架作品既不出现在结果里，也不能作为源（SOC-017 同口径）。
func TestIntegrationRelatedSkipsUnlisted(t *testing.T) {
	h, idSvc, pool := newRelatedHandler(t)
	author, sid := registerVerified(t, pool, idSvc, registerInputFor(t, "下架相关"))
	t.Cleanup(func() { cleanupAuthor(t, pool, author.Sub) })
	resetPublished(t, pool)

	src := publishRated(t, h, pool, sid, "源", ratingStats{}, 0)
	sibling := publishRated(t, h, pool, sid, "兄弟", ratingStats{}, 0)

	if got := relatedSlugs(t, h, src, ""); len(got) != 1 || got[0] != sibling {
		t.Fatalf("下架前相关 = %v，想要 [%s]", got, sibling)
	}

	if _, err := pool.Exec(context.Background(),
		`UPDATE demos SET lifecycle_state = 'archived' WHERE slug = $1`, sibling); err != nil {
		t.Fatalf("下架失败: %v", err)
	}
	if got := relatedSlugs(t, h, src, ""); len(got) != 0 {
		t.Fatalf("下架后相关 = %v，想要空数组", got)
	}

	// 源自己下架后不再是公开作品，所以是 404 而不是空数组——与详情同一口径。
	if rec := getWithSession(t, h, "/api/v1/demos/"+sibling+"/related", ""); rec.Code != 404 {
		t.Fatalf("已下架作品作为源 = %d，想要 404", rec.Code)
	}
}

// 源作品必须公开可见，否则 404（与详情同一口径）。
func TestIntegrationRelatedHidesNonPublicSource(t *testing.T) {
	h, idSvc, pool := newRelatedHandler(t)
	author, sid := registerVerified(t, pool, idSvc, registerInputFor(t, "草稿作者"))
	t.Cleanup(func() { cleanupAuthor(t, pool, author.Sub) })
	resetPublished(t, pool)

	draft := submitHTML(t, h, sid, "还没发布的", `<!doctype html><p>draft</p>`)

	rec := getWithSession(t, h, "/api/v1/demos/"+draft+"/related", "")
	if rec.Code != 404 {
		t.Fatalf("草稿作为源 = %d，想要 404；响应体：%s", rec.Code, rec.Body.String())
	}
}

// limit 越界回 400，正常值生效。
func TestIntegrationRelatedLimit(t *testing.T) {
	h, idSvc, pool := newRelatedHandler(t)
	author, sid := registerVerified(t, pool, idSvc, registerInputFor(t, "limit作者"))
	t.Cleanup(func() { cleanupAuthor(t, pool, author.Sub) })
	resetPublished(t, pool)

	src := publishRated(t, h, pool, sid, "源", ratingStats{}, 0)
	for _, name := range []string{"甲", "乙", "丙"} {
		publishRated(t, h, pool, sid, name, ratingStats{}, 0)
	}

	if got := relatedSlugs(t, h, src, "?limit=2"); len(got) != 2 {
		t.Fatalf("limit=2 返回 %d 条，想要 2", len(got))
	}
	for _, bad := range []string{"?limit=0", "?limit=13"} {
		rec := getWithSession(t, h, "/api/v1/demos/"+src+"/related"+bad, "")
		if rec.Code != 400 {
			t.Fatalf("%s = %d，想要 400", bad, rec.Code)
		}
	}
}

// 模型信号用「当前版本」而不是对外版本：作者改稿换了模型，相关作品应立刻跟着换。
func TestIntegrationRelatedUsesCurrentRevisionModels(t *testing.T) {
	h, idSvc, pool := newRelatedHandler(t)
	author, sid := registerVerified(t, pool, idSvc, registerInputFor(t, "版本作者"))
	t.Cleanup(func() { cleanupAuthor(t, pool, author.Sub) })
	resetPublished(t, pool)

	// 三件都换到别的作者名下：这条用例只验模型信号，作者信号必须被排除。
	other, otherSid := registerVerified(t, pool, idSvc, registerInputFor(t, "版本邻座"))
	t.Cleanup(func() { cleanupAuthor(t, pool, other.Sub) })

	src := publishRated(t, h, pool, sid, "源版本", ratingStats{}, 0)
	oldModel := publishRated(t, h, pool, otherSid, "同老模型", ratingStats{}, 0)
	newModel := publishRated(t, h, pool, otherSid, "同新模型", ratingStats{}, 0)

	attachModelByName(t, pool, src, "模型甲")
	attachModelByName(t, pool, oldModel, "模型甲")
	attachModelByName(t, pool, newModel, "模型乙")

	if got := relatedSlugs(t, h, src, ""); len(got) != 1 || got[0] != oldModel {
		t.Fatalf("换模型前相关 = %v，想要 [%s]", got, oldModel)
	}

	// 模拟作者改稿：当前版本挂的模型换成乙。
	if _, err := pool.Exec(context.Background(), `
DELETE FROM demo_revision_models rm
USING demos d, models m
WHERE rm.revision_id = d.current_revision_id AND d.slug = $1 AND m.id = rm.model_id AND m.name = '模型甲'`, src); err != nil {
		t.Fatalf("摘掉旧模型失败: %v", err)
	}
	attachModelByName(t, pool, src, "模型乙")

	got := relatedSlugs(t, h, src, "")
	if len(got) != 1 || got[0] != newModel {
		t.Fatalf("换模型后相关 = %v，想要 [%s]（模型信号按当前版本）", got, newModel)
	}
}
