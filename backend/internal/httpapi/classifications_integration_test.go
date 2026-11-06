package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/api"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/demo"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/identity"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/platform/storage"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

// 分类的读写与筛选。
//
// 用的是真库：`classifications` 的词表来自迁移 0018，假库拼不出「92 条里哪几条
// 被引用了几次」，而那正是 demoCount 唯一有意义的判断。
//
// 每个用例自己插需要的分类项（slug 带用例前缀），结束时清掉——词表是共享的，
// 用例之间互相污染过一次就再也查不出计数为什么不对。

func mkClassification(t *testing.T, pool *pgxpool.Pool, dimension, slug, label string) int64 {
	t.Helper()
	var id int64
	if err := pool.QueryRow(context.Background(),
		`INSERT INTO classifications (dimension, slug, label) VALUES ($1, $2, $3)
		 ON CONFLICT (dimension, slug) DO UPDATE SET label = EXCLUDED.label
		 RETURNING id`, dimension, slug, label).Scan(&id); err != nil {
		t.Fatalf("建分类失败: %v", err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), "DELETE FROM classifications WHERE id = $1", id); err != nil {
			t.Logf("清理分类失败: %v", err)
		}
	})
	return id
}

func submitWithClasses(t *testing.T, h *Handler, sid, title, body string, classSlugs []string) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for k, v := range map[string]string{"title": title, "description": "简介", "demoType": "web"} {
		if err := mw.WriteField(k, v); err != nil {
			t.Fatal(err)
		}
	}
	for _, s := range classSlugs {
		if err := mw.WriteField("classificationSlugs", s); err != nil {
			t.Fatal(err)
		}
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
	rec := httptest.NewRecorder()
	NewRouter(h, nil, nil).ServeHTTP(rec, req)
	return rec
}

// mkPlainPublished 再造一件「已发布 + 上列表 + 没有分类」的作品。
//
// 为什么必须有第二件：只有一件可见作品时，「筛选生效」与「筛选完全不生效」
// 得到的列表条数是一样的——那条断言会绿，但它证明不了任何事。
// 加一件不带分类的对照，筛选一旦失效列表就会多出一条。
func mkPlainPublished(t *testing.T, pool *pgxpool.Pool, slug string) {
	t.Helper()
	ctx := context.Background()
	if _, err := pool.Exec(ctx,
		`INSERT INTO demos (slug, title, description, moderation_state, lifecycle_state)
		 VALUES ($1, '对照作品', '简介', 'approved', 'published')`, slug); err != nil {
		t.Fatalf("建对照作品失败: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		WITH rev AS (
			INSERT INTO demo_revisions (demo_id, rev_no, demo_type, source)
			SELECT id, 1, 'web', 'upload' FROM demos WHERE slug = $1
			RETURNING id, demo_id
		)
		UPDATE demos d SET current_revision_id = rev.id, published_revision_id = rev.id
		FROM rev WHERE d.id = rev.demo_id`, slug); err != nil {
		t.Fatalf("给对照作品造对外版本失败: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO demo_channels (demo_id, channel, visibility, lang)
		 SELECT id, 'deep', 'listed', 'zh' FROM demos WHERE slug = $1`, slug); err != nil {
		t.Fatalf("给对照作品插渠道失败: %v", err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), "DELETE FROM demos WHERE slug = $1", slug); err != nil {
			t.Logf("清理对照作品失败: %v", err)
		}
	})
}

func newClassTestHandler(t *testing.T) (*Handler, *identity.Service, *pgxpool.Pool) {
	t.Helper()
	pool := integrationPool(t)
	q := store.New(pool)
	idSvc := integrationIdentity(t, pool)
	files, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("初始化存储失败: %v", err)
	}
	demoSvc := demo.New(pool, q, files, storage.DefaultLimits(), 200<<20, 0, nil)
	return New(pool, q, idSvc, nil, demoSvc), idSvc, pool
}

func classCatalog(t *testing.T, h *Handler) api.ClassificationCatalog {
	t.Helper()
	rec := httptest.NewRecorder()
	NewRouter(h, nil, nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/classifications", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("分类目录 = %d，想要 200；响应体：%s", rec.Code, rec.Body.String())
	}
	var out api.ClassificationCatalog
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("解析分类目录失败: %v", err)
	}
	return out
}

// 目录端点必须在空库上回 `[]` 而不是 null，并且把三个维度都带上。
func TestIntegrationClassificationCatalog(t *testing.T) {
	h, _, pool := newClassTestHandler(t)
	seed(t, pool)

	if _, err := pool.Exec(context.Background(), "DELETE FROM demo_classifications"); err != nil {
		t.Fatalf("清空作品分类失败: %v", err)
	}
	genreID := mkClassification(t, pool, "genre", "it-cls-game", "小游戏")
	mkClassification(t, pool, "technique", "it-cls-3d", "3D建模")

	cat := classCatalog(t, h)
	if len(cat.Dimensions) != 3 {
		t.Fatalf("维度数 = %d，想要 3（genre / technique / subject）", len(cat.Dimensions))
	}
	// 顺序由 classification_dimensions.sort 决定：形态在前、题材在后。
	if cat.Dimensions[0].Dimension != api.ClassificationCatalogDimensionsDimensionGenre {
		t.Fatalf("第一个维度 =s %q，想要 genre", cat.Dimensions[0].Dimension)
	}
	if cat.Items == nil {
		t.Fatal("items 应是空数组而不是 null——null 会让前端多一条分支")
	}

	// 计数必须是「已发布且上列表」的作品数：把种子里的 it-published 挂上去。
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO demo_classifications (demo_id, dimension, classification_id)
		 SELECT id, 'genre', $1 FROM demos WHERE slug = 'it-published'`, genreID); err != nil {
		t.Fatalf("挂分类失败: %v", err)
	}

	cat = classCatalog(t, h)
	var got int
	for _, it := range cat.Items {
		if it.Slug == "it-cls-game" {
			got = it.DemoCount
			if it.Label != "小游戏" {
				t.Fatalf("label = %q，想要 小游戏", it.Label)
			}
		}
	}
	if got != 1 {
		t.Fatalf("demoCount = %d，想要 1（it-published 是已发布且上列表的）", got)
	}

	// 再造一件不带分类的可见作品：没有它，「筛选不生效」与「筛选生效」的列表
	// 条数一样，那条断言会绿在一个什么都没证明的地方。
	mkPlainPublished(t, pool, "it-cls-plain")

	// 真正的判据不是上面那个数字，而是「筛选器上的数目」与「点进去的条数」一致。
	// 这两条路径一旦有一边多算了 hidden 渠道、少算了 published_revision，就会出现
	// 「游戏 230 件」而列表只有 12 条——同一件事的两个答案。所以这里直接比。
	listRec := httptest.NewRecorder()
	NewRouter(h, nil, nil).ServeHTTP(listRec,
		httptest.NewRequest(http.MethodGet, "/api/v1/demos?genre=it-cls-game", nil))
	if listRec.Code != http.StatusOK {
		t.Fatalf("按分类筛选 = %d，想要 200；%s", listRec.Code, listRec.Body.String())
	}
	var list api.DemoList
	if err := json.Unmarshal(listRec.Body.Bytes(), &list); err != nil {
		t.Fatalf("解析列表失败: %v", err)
	}
	if list.Total != got {
		t.Fatalf("筛选器上的 demoCount = %d，列表却给了 total = %d —— 两条路径的可见性判据不同源",
			got, list.Total)
	}
	if len(list.Items) != 1 || list.Items[0].Slug != "it-published" {
		t.Fatalf("按分类筛出的作品 = %+v，想要 [it-published]", list.Items)
	}
	// 列表也要带分类：卡片要能直接标出「小游戏」而不必再发一次请求。
	if len(list.Items[0].Classifications) != 1 || list.Items[0].Classifications[0].Label != "小游戏" {
		t.Fatalf("列表项的分类 = %+v，想要 [小游戏]", list.Items[0].Classifications)
	}
}

// 投稿带分类 → 作者看得到；未收录的 slug → 400 且点出是哪个。
func TestIntegrationSubmitDeclaresClassifications(t *testing.T) {
	h, idSvc, pool := newClassTestHandler(t)
	seed(t, pool)
	acc, sid := registerVerified(t, pool, idSvc, identity.RegisterInput{
		Email: "it-cls@example.com", Password: "classtest123", Username: "it-cls", DisplayName: "分类作者",
	})
	t.Cleanup(func() { cleanupAuthor(t, pool, acc.Sub) })

	mkClassification(t, pool, "genre", "it-cls-game", "小游戏")
	mkClassification(t, pool, "technique", "it-cls-3d", "3D建模")
	mkClassification(t, pool, "subject", "it-cls-cube", "魔方")

	rec := submitWithClasses(t, h, sid, "带分类的作品", "<h1>hi</h1>", []string{"it-cls-game", "it-cls-3d", "it-cls-cube"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("投稿 = %d，想要 201；响应体：%s", rec.Code, rec.Body.String())
	}
	var res api.SubmissionResult
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("解析回执失败: %v", err)
	}

	detail := getDemoAs(t, h, res.Slug, sid)
	if detail.Code != http.StatusOK {
		t.Fatalf("作者看自己的稿件 = %d，想要 200", detail.Code)
	}
	var d api.DemoDetail
	if err := json.Unmarshal(detail.Body.Bytes(), &d); err != nil {
		t.Fatalf("解析详情失败: %v", err)
	}
	if len(d.Classifications) != 3 {
		t.Fatalf("分类数 = %d，想要 3", len(d.Classifications))
	}
	// 顺序：genre → technique → subject，由维度表的 sort 决定。
	want := []string{"it-cls-game", "it-cls-3d", "it-cls-cube"}
	for i, c := range d.Classifications {
		if c.Slug != want[i] {
			t.Fatalf("第 %d 条分类 = %q，想要 %q（顺序应由维度 sort 决定）", i, c.Slug, want[i])
		}
	}

	// 未收录的 slug 必须报错而不是静默丢掉：作者以为打了标签、库里没有，
	// 而他自己看不见这件事。
	bad := submitWithClasses(t, h, sid, "带坏分类的作品", "<h1>hi</h1>", []string{"it-cls-nope"})
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("声明不存在的分类 = %d，想要 400；响应体：%s", bad.Code, bad.Body.String())
	}
	var errBody map[string]string
	if err := json.Unmarshal(bad.Body.Bytes(), &errBody); err != nil {
		t.Fatalf("解析错误体失败: %v", err)
	}
	if !bytes.Contains([]byte(errBody["message"]), []byte("it-cls-nope")) {
		t.Fatalf("错误文案应点出是哪个 slug，实际 %q", errBody["message"])
	}
}

// 分类挂作品、模型挂版本：改稿换模型会变，分类不会。
func TestIntegrationClassificationsSurviveNewRevision(t *testing.T) {
	h, idSvc, pool := newClassTestHandler(t)
	seed(t, pool)
	acc, sid := registerVerified(t, pool, idSvc, identity.RegisterInput{
		Email: "it-cls2@example.com", Password: "classtest123", Username: "it-cls2", DisplayName: "分类作者2",
	})
	t.Cleanup(func() { cleanupAuthor(t, pool, acc.Sub) })

	mkClassification(t, pool, "genre", "it-cls-game", "小游戏")
	rec := submitWithClasses(t, h, sid, "分类要活过改稿", "<h1>v1</h1>", []string{"it-cls-game"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("投稿 = %d，想要 201；%s", rec.Code, rec.Body.String())
	}
	var res api.SubmissionResult
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("解析回执失败: %v", err)
	}

	// 改稿时只改标题、不带分类字段。
	if upd := updateDemo(t, h, sid, res.Slug, "换了标题", "简介", nil); upd.Code != http.StatusOK {
		t.Fatalf("更新 = %d，想要 200；%s", upd.Code, upd.Body.String())
	}

	detail := getDemoAs(t, h, res.Slug, sid)
	var d api.DemoDetail
	if err := json.Unmarshal(detail.Body.Bytes(), &d); err != nil {
		t.Fatalf("解析详情失败: %v", err)
	}
	// 这是「分类是作品级」的直接后果，也是它与 modelSlugs 最大的不同：
	// 模型声明整体替换（不传=清空），而分类在这条路径上必须还在。
	if len(d.Classifications) != 1 || d.Classifications[0].Slug != "it-cls-game" {
		t.Fatalf("改稿后分类 = %+v，想要仍是 [it-cls-game]", d.Classifications)
	}
}
