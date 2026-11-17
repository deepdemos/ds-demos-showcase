package demo

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// 端点上报（#445 的 4d）的领域层验收。
//
// 这一组钉的是「上报落库之后是什么」：调用行的键与规范化、幂等、拒收口径，
// 以及两处会静默把「用的是哪一版」变回「未固定」的坑——改稿与回滚
// （docs/已知松点.md 第 64 条点名的正是这两处）。multipart 怎么取字段是 httpapi 的事。

// submitWithCalls 投一件作品，带模型声明与端点上报，返回 slug。
func submitWithCalls(t *testing.T, s *Service, authorID int64, title string, slugs, calls []string) string {
	t.Helper()
	zipPath := buildZip(t, map[string]string{"index.html": "<!doctype html><p>ep</p>"})
	info, err := os.Stat(zipPath)
	if err != nil {
		t.Fatalf("读包大小失败: %v", err)
	}
	res, err := s.Submit(context.Background(), SubmitInput{
		AuthorID:    authorID,
		Title:       title,
		Description: "端点上报",
		DemoType:    "web",
		PackagePath: zipPath,
		PackageName: "demo.zip",
		PackageSize: info.Size(),
		ModelSlugs:  slugs,
		ModelCalls:  calls,
	})
	if err != nil {
		t.Fatalf("投稿失败: %v", err)
	}
	return res.Slug
}

// cleanupEndpointTest 先删作品再删模型。
//
// 顺序不能换：demo_revision_models.model_id 是 ON DELETE RESTRICT，作品还在时删模型
// 会被外键挡住。清理失败不判红（那是环境问题，不是被测行为），但要留痕：残留的声明
// 会让下一轮「解析不到」那条路径根本走不到，而失败信息里看不出是上一轮留下的。
func cleanupEndpointTest(t *testing.T, pool *pgxpool.Pool, authorID, modelID int64) {
	t.Helper()
	t.Cleanup(func() {
		ctx := context.Background()
		if _, err := pool.Exec(ctx, `DELETE FROM demos WHERE author_id = $1`, authorID); err != nil {
			t.Logf("清理作品失败（会污染下一轮）：%v", err)
		}
		for _, q := range []string{
			`DELETE FROM model_offerings WHERE model_id = $1`,
			`DELETE FROM model_checkpoints WHERE model_id = $1`,
			`DELETE FROM models WHERE id = $1`,
		} {
			if _, err := pool.Exec(ctx, q, modelID); err != nil {
				t.Logf("清理模型失败（会污染下一轮）：%v", err)
			}
		}
	})
}

// checkpointLabelOf 读「某一版给某个模型记的版本」，没记过时返回空串。
func checkpointLabelOf(t *testing.T, pool *pgxpool.Pool, revisionID, modelID int64) string {
	t.Helper()
	var label string
	err := pool.QueryRow(context.Background(), `
		SELECT c.label
		FROM demo_revision_models rm
		JOIN model_checkpoints c ON c.id = rm.checkpoint_id
		WHERE rm.revision_id = $1 AND rm.model_id = $2`, revisionID, modelID).Scan(&label)
	if errors.Is(err, pgx.ErrNoRows) {
		return ""
	}
	if err != nil {
		t.Fatalf("读版本失败: %v", err)
	}
	return label
}

// hasDeclaration 判断某一版有没有声明某个模型。
func hasDeclaration(t *testing.T, pool *pgxpool.Pool, revisionID, modelID int64) bool {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM demo_revision_models WHERE revision_id = $1 AND model_id = $2`,
		revisionID, modelID).Scan(&n); err != nil {
		t.Fatalf("数声明行失败: %v", err)
	}
	return n > 0
}

// endpointOffering 是一条端点调用行读出来的样子。
type endpointOffering struct {
	ProviderID   string
	ProviderName string
	Pinning      string
	Checkpoint   string
}

// offeringOf 读 (authority='endpoint', external_id) 那一行。
func offeringOf(t *testing.T, pool *pgxpool.Pool, externalID string) (endpointOffering, bool) {
	t.Helper()
	var o endpointOffering
	var cpLabel *string
	err := pool.QueryRow(context.Background(), `
		SELECT r.provider_id, r.provider_name, r.pinning, c.label
		FROM model_offerings r
		LEFT JOIN model_checkpoints c ON c.id = r.checkpoint_id
		WHERE r.authority = 'endpoint' AND r.external_id = $1`, externalID).
		Scan(&o.ProviderID, &o.ProviderName, &o.Pinning, &cpLabel)
	if errors.Is(err, pgx.ErrNoRows) {
		return o, false
	}
	if err != nil {
		t.Fatalf("读调用行失败: %v", err)
	}
	if cpLabel != nil {
		o.Checkpoint = *cpLabel
	}
	return o, true
}

// countEndpointOfferings 数某个上报名字有几行调用。
//
// 规范化失败时同一个名字会长出多行，所以「几行」本身就是判据（D02 的验收）。
func countEndpointOfferings(t *testing.T, pool *pgxpool.Pool, externalID string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM model_offerings WHERE authority = 'endpoint' AND external_id = $1`,
		externalID).Scan(&n); err != nil {
		t.Fatalf("数调用行失败: %v", err)
	}
	return n
}

// TestIntegrationEndpointReportWritesOffering 钉住一条上报落库的形状。
//
// 键是 (authority='endpoint', provider_id=规范化后的 base_url, external_id=上报的名字)；
// 带版本就是 pinning='exact'，版本行由上报顺手建、备注记来源。
func TestIntegrationEndpointReportWritesOffering(t *testing.T) {
	pool := testPool(t)
	s := testService(t, pool)
	author := mkAuthor(t, pool, "ep-basic")
	modelID := mkAliasTestModel(t, pool, "it-ep-basic", "上报模型甲")
	cleanupEndpointTest(t, pool, author, modelID)

	slug := submitWithCalls(t, s, author, "端点上报·基本形状", []string{"it-ep-basic"},
		[]string{`{"ref":"it-ep-basic","endpoint":"HTTPS://API.Example.COM:443/v1/","checkpoint":"1017"}`})

	o, ok := offeringOf(t, pool, "it-ep-basic")
	if !ok {
		t.Fatalf("没写调用行")
	}
	if o.ProviderID != "https://api.example.com/v1" {
		t.Fatalf("provider_id = %q，想要规范化后的 https://api.example.com/v1", o.ProviderID)
	}
	if o.ProviderName != "api.example.com" {
		t.Fatalf("provider_name = %q，想要 api.example.com", o.ProviderName)
	}
	if o.Pinning != "exact" || o.Checkpoint != "1017" {
		t.Fatalf("pinning/版本 = %q/%q，想要 exact/1017", o.Pinning, o.Checkpoint)
	}
	if got := checkpointLabelOf(t, pool, currentRevisionID(t, pool, slug), modelID); got != "1017" {
		t.Fatalf("声明上的版本 = %q，想要 1017", got)
	}

	// 版本行是上报建的，必须留下「谁建的」——出问题时第一句话就是这句。
	var notes string
	if err := pool.QueryRow(context.Background(),
		`SELECT notes FROM model_checkpoints WHERE model_id = $1 AND label = '1017'`, modelID).Scan(&notes); err != nil {
		t.Fatalf("读版本备注失败: %v", err)
	}
	if notes != checkpointFromReport {
		t.Fatalf("版本备注 = %q，想要 %q", notes, checkpointFromReport)
	}
}

// TestIntegrationEndpointReportIsIdempotent 钉住 D01：同一 (出处, 名字) 重复上报只留一行。
//
// 「后一次覆盖前一次」是刻意的：这张表记的是「现在这个名字指谁」，历史在版本表里。
// 反过来（第一次赢）会让作者改了上报之后库里永远是旧的，而界面上什么都没变。
func TestIntegrationEndpointReportIsIdempotent(t *testing.T) {
	pool := testPool(t)
	s := testService(t, pool)
	author := mkAuthor(t, pool, "ep-idem")
	modelID := mkAliasTestModel(t, pool, "it-ep-idem", "上报模型乙")
	cleanupEndpointTest(t, pool, author, modelID)

	submitWithCalls(t, s, author, "幂等·第一次", []string{"it-ep-idem"},
		[]string{`{"ref":"it-ep-idem","endpoint":"https://api.example.com/v1","checkpoint":"1017"}`})
	submitWithCalls(t, s, author, "幂等·第二次", []string{"it-ep-idem"},
		[]string{`{"ref":"it-ep-idem","endpoint":"https://api.example.com/v1","checkpoint":"1103"}`})

	if n := countEndpointOfferings(t, pool, "it-ep-idem"); n != 1 {
		t.Fatalf("同一 (出处, 名字) 应当只有 1 行，实际 %d 行", n)
	}
	o, _ := offeringOf(t, pool, "it-ep-idem")
	if o.Checkpoint != "1103" || o.Pinning != "exact" {
		t.Fatalf("第二次上报应当覆盖第一次：版本 = %q，pinning = %q", o.Checkpoint, o.Pinning)
	}

	// 再报一次不带版本：pinning 退回 unknown，版本清空——「不知道」要能覆盖「知道」，
	// 否则撤回一个错误的版本声明没有路径。
	submitWithCalls(t, s, author, "幂等·撤回版本", nil,
		[]string{`{"ref":"it-ep-idem","endpoint":"https://api.example.com/v1"}`})
	o, _ = offeringOf(t, pool, "it-ep-idem")
	if o.Pinning != "unknown" || o.Checkpoint != "" {
		t.Fatalf("不带版本的上报 = %q/%q，想要 unknown/空", o.Pinning, o.Checkpoint)
	}
}

// TestIntegrationEndpointNormalizationCollapsesVariants 钉住 D02：
// 同一个出处的不同写法必须收敛成一行。
//
// 尾斜杠、默认端口、大小写、查询串、空端口五种写法在真实上报里都会出现（插件抄的是
// 配置里的 base_url，各家写法不同）。不收敛的话同一个端点会在库里变成多行，而
// UNIQUE 拦不住——两行都是合法的键，「现在这个名字指谁」就失准了。
func TestIntegrationEndpointNormalizationCollapsesVariants(t *testing.T) {
	pool := testPool(t)
	s := testService(t, pool)
	author := mkAuthor(t, pool, "ep-norm")
	modelID := mkAliasTestModel(t, pool, "it-ep-norm", "上报模型丙")
	cleanupEndpointTest(t, pool, author, modelID)

	variants := []string{
		"https://api.example.com/v1/",
		"HTTPS://API.EXAMPLE.COM:443/v1",
		"https://api.example.com/v1?trace=1#frag",
		"https://api.example.com:443/v1//",
		"https://api.example.com:/v1", // 空端口：SplitHostPort 会失败，Hostname/Port 不会
	}
	for i, v := range variants {
		submitWithCalls(t, s, author, "规范化·第"+string(rune('1'+i))+"次", []string{"it-ep-norm"},
			[]string{`{"ref":"it-ep-norm","endpoint":"` + v + `"}`})
	}

	if n := countEndpointOfferings(t, pool, "it-ep-norm"); n != 1 {
		t.Fatalf("五种写法应当收敛成 1 行，实际 %d 行", n)
	}
	o, _ := offeringOf(t, pool, "it-ep-norm")
	if o.ProviderID != "https://api.example.com/v1" {
		t.Fatalf("provider_id = %q，想要 https://api.example.com/v1", o.ProviderID)
	}

	// IPv6 字面量：带默认端口与不带是同一个出处，而且拼出来的地址必须还能解析回来
	// （[::1] 去掉方括号就成了 ::1，provider_name 会变成空串）。
	v6ID := mkAliasTestModel(t, pool, "it-ep-norm6", "上报模型丙6")
	cleanupEndpointTest(t, pool, author, v6ID)
	for _, v := range []string{"https://[::1]:443/v1", "https://[::1]/v1"} {
		submitWithCalls(t, s, author, "规范化·IPv6", []string{"it-ep-norm6"},
			[]string{`{"ref":"it-ep-norm6","endpoint":"` + v + `"}`})
	}
	if n := countEndpointOfferings(t, pool, "it-ep-norm6"); n != 1 {
		t.Fatalf("IPv6 带不带默认端口应当收敛成 1 行，实际 %d 行", n)
	}
	if o, _ := offeringOf(t, pool, "it-ep-norm6"); o.ProviderID != "https://[::1]/v1" || o.ProviderName != "[::1]" {
		t.Fatalf("IPv6 的 provider_id/name = %q/%q，想要 https://[::1]/v1 与 [::1]", o.ProviderID, o.ProviderName)
	}

	// %2F 不能被解成 /：那是两个不同的端点。解了会把两行并成一行，而
	// ON CONFLICT DO UPDATE 会静默改掉前一行的归属。
	escID := mkAliasTestModel(t, pool, "it-ep-esc", "上报模型丙2")
	cleanupEndpointTest(t, pool, author, escID)
	for _, v := range []string{"https://api.example.com/a%2Fb", "https://api.example.com/a/b"} {
		submitWithCalls(t, s, author, "规范化·转义斜杠", []string{"it-ep-esc"},
			[]string{`{"ref":"it-ep-esc","endpoint":"` + v + `"}`})
	}
	if n := countEndpointOfferings(t, pool, "it-ep-esc"); n != 2 {
		t.Fatalf("编码过的斜杠与真斜杠是两个端点，应当 2 行，实际 %d 行", n)
	}
}

// TestIntegrationEndpointRejectsCredentials 钉住 D03：凭据形状的地址一律 400。
//
// 拒收而不是「静默丢掉那一段」：作者以为上报成功了，库里却少了一截，
// 这种错比 400 难查得多。
func TestIntegrationEndpointRejectsCredentials(t *testing.T) {
	pool := testPool(t)
	s := testService(t, pool)
	author := mkAuthor(t, pool, "ep-bad")
	modelID := mkAliasTestModel(t, pool, "it-ep-bad", "上报模型丁")
	cleanupEndpointTest(t, pool, author, modelID)

	longPath := "https://api.example.com/" + strings.Repeat("a", 600)
	cases := []struct {
		name     string
		endpoint string
		want     string
	}{
		{"带用户名密码", "https://someone:hunter2@api.example.com/v1", "不能带用户名或密码"},
		{"查询串带 api_key", "https://api.example.com/v1?api_key=abc", "不能带凭据"},
		{"查询串带 token", "https://api.example.com/v1?access_token=abc", "不能带凭据"},
		{"不是 http(s)", "ftp://api.example.com/v1", "要以 http:// 或 https:// 开头"},
		{"缺 scheme", "api.example.com/v1", "要以 http:// 或 https:// 开头"},
		{"缺主机名", "https://", "缺主机名"},
		{"超长", longPath, "最长 512 个字符"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			zipPath := buildZip(t, map[string]string{"index.html": "<!doctype html><p>x</p>"})
			info, err := os.Stat(zipPath)
			if err != nil {
				t.Fatalf("读包大小失败: %v", err)
			}
			_, err = s.Submit(context.Background(), SubmitInput{
				AuthorID:    author,
				Title:       "端点上报·拒收 " + c.name,
				DemoType:    "web",
				PackagePath: zipPath,
				PackageName: "demo.zip",
				PackageSize: info.Size(),
				ModelCalls:  []string{`{"ref":"it-ep-bad","endpoint":"` + c.endpoint + `"}`},
			})
			if !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("期望 ErrInvalidInput（→400），实际 %v", err)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Fatalf("错误信息里应当有 %q，实际 %q", c.want, err.Error())
			}
		})
	}

	// 全部被拒：一行调用都不该留下。
	if n := countEndpointOfferings(t, pool, "it-ep-bad"); n != 0 {
		t.Fatalf("被拒的上报不该写调用行，实际 %d 行", n)
	}
}

// TestIntegrationEndpointRejectsMalformedCalls 钉住上报本身的形状校验：
// 空名字（D06）、不是 JSON、拼错的键、单条超长、条数超限。
//
// 拼错的键必须报错而不是忽略：把 endpont 静默丢掉，结果是「我明明上报了端点，
// 库里却是 unknown」，比 400 难查得多。
func TestIntegrationEndpointRejectsMalformedCalls(t *testing.T) {
	pool := testPool(t)
	s := testService(t, pool)
	author := mkAuthor(t, pool, "ep-json")
	modelID := mkAliasTestModel(t, pool, "it-ep-json", "上报模型戊")
	cleanupEndpointTest(t, pool, author, modelID)

	tooMany := make([]string, 0, maxModelCalls+1)
	for i := 0; i <= maxModelCalls; i++ {
		tooMany = append(tooMany, `{"ref":"it-ep-json"}`)
	}

	cases := []struct {
		name  string
		calls []string
		want  string
	}{
		{"空名字", []string{`{"ref":"   "}`}, "缺模型名"},
		{"不是 JSON", []string{`ref=it-ep-json`}, "JSON 对象"},
		{"拼错的键", []string{`{"ref":"it-ep-json","endpont":"https://a.com"}`}, "JSON 对象"},
		{"两个对象", []string{`{"ref":"it-ep-json"}{"ref":"it-ep-json"}`}, "只能有一个 JSON 对象"},
		{"单条超长", []string{`{"ref":"it-ep-json","endpoint":"https://a.com/` + strings.Repeat("x", 2100) + `"}`}, "最长 2048 个字符"},
		{"条数超限", tooMany, "最多 50 条"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			zipPath := buildZip(t, map[string]string{"index.html": "<!doctype html><p>x</p>"})
			info, err := os.Stat(zipPath)
			if err != nil {
				t.Fatalf("读包大小失败: %v", err)
			}
			_, err = s.Submit(context.Background(), SubmitInput{
				AuthorID:    author,
				Title:       "端点上报·形状 " + c.name,
				DemoType:    "web",
				PackagePath: zipPath,
				PackageName: "demo.zip",
				PackageSize: info.Size(),
				ModelCalls:  c.calls,
			})
			if !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("期望 ErrInvalidInput（→400），实际 %v", err)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Fatalf("错误信息里应当有 %q，实际 %q", c.want, err.Error())
			}
		})
	}
}

// TestIntegrationEndpointReportAloneDeclaresModel 钉住「一条上报也算一条声明」。
//
// 插件上报的是调用，不是「勾选」；如果只写 modelCalls 不算声明，作者就得在表单里
// 把同一个模型再勾一遍，两处迟早不一致。
func TestIntegrationEndpointReportAloneDeclaresModel(t *testing.T) {
	pool := testPool(t)
	s := testService(t, pool)
	author := mkAuthor(t, pool, "ep-alone")
	modelID := mkAliasTestModel(t, pool, "it-ep-alone", "上报模型己")
	cleanupEndpointTest(t, pool, author, modelID)

	slug := submitWithCalls(t, s, author, "只报端点不勾模型", nil,
		[]string{`{"ref":"it-ep-alone","endpoint":"https://api.example.com/v1"}`})

	rev := currentRevisionID(t, pool, slug)
	if !hasDeclaration(t, pool, rev, modelID) {
		t.Fatalf("只写 modelCalls 也应当声明模型")
	}
	// 没报版本 → 声明上就是「未固定」，不能默认成最新版。
	if got := checkpointLabelOf(t, pool, rev, modelID); got != "" {
		t.Fatalf("没报版本时声明上的版本 = %q，想要空", got)
	}
}

// TestIntegrationEndpointReportUnknownRefIsRejected 钉住 d2 选 A：
// 上报里解析不到的名字一律 400，与 modelSlugs 同一条判据。
//
// 不建模型是 #465 的口径：模型表是平台主数据，不是投稿的副产物。
func TestIntegrationEndpointReportUnknownRefIsRejected(t *testing.T) {
	pool := testPool(t)
	s := testService(t, pool)
	author := mkAuthor(t, pool, "ep-unknown")

	unknown := "上报里没收录的模型"
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM models WHERE name = $1`, unknown)
	})

	zipPath := buildZip(t, map[string]string{"index.html": "<!doctype html><p>x</p>"})
	info, err := os.Stat(zipPath)
	if err != nil {
		t.Fatalf("读包大小失败: %v", err)
	}
	_, err = s.Submit(context.Background(), SubmitInput{
		AuthorID:    author,
		Title:       "上报未收录的模型",
		DemoType:    "web",
		PackagePath: zipPath,
		PackageName: "demo.zip",
		PackageSize: info.Size(),
		ModelCalls:  []string{`{"ref":"` + unknown + `","endpoint":"https://api.example.com/v1"}`},
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("期望 ErrInvalidInput（→400），实际 %v", err)
	}
	if !strings.Contains(err.Error(), unknown) {
		t.Fatalf("错误信息里应当点名 %q，实际 %q", unknown, err.Error())
	}
	if n := countEndpointOfferings(t, pool, unknown); n != 0 {
		t.Fatalf("被拒的上报不该写调用行，实际 %d 行", n)
	}
}

// TestIntegrationEndpointReportBeatsNameSuffix 钉住 D07：
// 名字里带不带版本号不参与判断，**上报的版本优先**。
//
// 名字里的 1017 是给人看的（旧站把版本写进了名字），上报的 0910 才是作者说的那一版。
// 反过来（名字赢）会让「我调的是 0910」被一个字符串盖掉，而且没有任何提示。
func TestIntegrationEndpointReportBeatsNameSuffix(t *testing.T) {
	pool := testPool(t)
	s := testService(t, pool)
	author := mkAuthor(t, pool, "ep-suffix")
	modelID := mkAliasTestModel(t, pool, "it-ep-suffix", "后缀模型-1017")
	cleanupEndpointTest(t, pool, author, modelID)

	slug := submitWithCalls(t, s, author, "名字说 1017，上报说 0910", []string{"it-ep-suffix"},
		[]string{`{"ref":"it-ep-suffix","endpoint":"https://api.example.com/v1","checkpoint":"0910"}`})

	if got := checkpointLabelOf(t, pool, currentRevisionID(t, pool, slug), modelID); got != "0910" {
		t.Fatalf("声明上的版本 = %q，想要上报的 0910（名字里的 1017 不参与判断）", got)
	}
}

// TestIntegrationUpdateCarriesCheckpointOver 钉住改稿的第一处坑：
// 只改标题时，上一版记过的版本要跟到新版本。
//
// 不带的后果：作者改一次标题，「1017 版」就静默变回「未固定」——
// 与「我没改模型声明」的意图相反，而且界面上看不出来（docs/已知松点.md 第 64 条）。
func TestIntegrationUpdateCarriesCheckpointOver(t *testing.T) {
	pool := testPool(t)
	s := testService(t, pool)
	author := mkAuthor(t, pool, "ep-carry")
	modelID := mkAliasTestModel(t, pool, "it-ep-carry", "上报模型庚")
	cleanupEndpointTest(t, pool, author, modelID)

	ctx := context.Background()
	slug := submitWithCalls(t, s, author, "改稿带版本·第一版", []string{"it-ep-carry"},
		[]string{`{"ref":"it-ep-carry","endpoint":"https://api.example.com/v1","checkpoint":"1017"}`})

	// 表单照旧把勾选的模型提交一遍，但不带 modelCalls。
	if _, err := s.Update(ctx, UpdateInput{
		AuthorID:   author,
		Slug:       slug,
		Title:      "改稿带版本·只改标题",
		ModelSlugs: []string{"it-ep-carry"},
	}); err != nil {
		t.Fatalf("改稿失败: %v", err)
	}
	rev := currentRevisionID(t, pool, slug)
	if got := checkpointLabelOf(t, pool, rev, modelID); got != "1017" {
		t.Fatalf("只改标题后版本 = %q，想要带过来的 1017", got)
	}

	// 显式上报了新版本：上报赢，且调用行也跟着更新。
	if _, err := s.Update(ctx, UpdateInput{
		AuthorID:   author,
		Slug:       slug,
		Title:      "改稿带版本·换版本",
		ModelSlugs: []string{"it-ep-carry"},
		ModelCalls: []string{`{"ref":"it-ep-carry","endpoint":"https://api.example.com/v1","checkpoint":"1103"}`},
	}); err != nil {
		t.Fatalf("改稿失败: %v", err)
	}
	rev = currentRevisionID(t, pool, slug)
	if got := checkpointLabelOf(t, pool, rev, modelID); got != "1103" {
		t.Fatalf("上报新版本后声明上的版本 = %q，想要 1103", got)
	}
	if o, _ := offeringOf(t, pool, "it-ep-carry"); o.Checkpoint != "1103" {
		t.Fatalf("调用行上的版本 = %q，想要 1103", o.Checkpoint)
	}
}

// TestIntegrationRestoreCopiesCheckpoint 钉住第二处坑：回滚到某一版时，
// 那一版记过的模型版本要一起复制到新版本。
//
// 不复制的话，回滚一次「内容回到旧版、版本声明却丢了」——与「回滚到那一版」相反。
func TestIntegrationRestoreCopiesCheckpoint(t *testing.T) {
	pool := testPool(t)
	s := testService(t, pool)
	author := mkAuthor(t, pool, "ep-restore")
	modelID := mkAliasTestModel(t, pool, "it-ep-restore", "上报模型辛")
	cleanupEndpointTest(t, pool, author, modelID)

	ctx := context.Background()
	slug := submitWithCalls(t, s, author, "回滚带版本·第一版", []string{"it-ep-restore"},
		[]string{`{"ref":"it-ep-restore","endpoint":"https://api.example.com/v1","checkpoint":"1017"}`})
	approveAndPublish(t, pool, slug)

	zipPath := buildZip(t, map[string]string{"index.html": "<!doctype html><p>v2</p>"})
	info, err := os.Stat(zipPath)
	if err != nil {
		t.Fatalf("读包大小失败: %v", err)
	}
	if _, err := s.Update(ctx, UpdateInput{
		AuthorID:    author,
		Slug:        slug,
		Title:       "回滚带版本·第二版",
		DemoType:    "web",
		PackagePath: zipPath,
		PackageName: "demo.zip",
		PackageSize: info.Size(),
		ModelSlugs:  []string{"it-ep-restore"},
	}); err != nil {
		t.Fatalf("改稿失败: %v", err)
	}

	if _, err := s.Restore(ctx, slug, 1, author); err != nil {
		t.Fatalf("回滚失败: %v", err)
	}
	rev := currentRevisionID(t, pool, slug)
	if got := checkpointLabelOf(t, pool, rev, modelID); got != "1017" {
		t.Fatalf("回滚后新版本上的版本 = %q，想要从第 1 版复制的 1017", got)
	}
}
