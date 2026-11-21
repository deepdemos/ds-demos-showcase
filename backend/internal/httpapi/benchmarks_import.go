package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/api"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/domain/demo"
	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

// 第三方基准快照导入（#526 / 模型域 §4.5）。
//
// # 为什么是一个端点 + dryRun，而不是 preview / apply 两段
//
// models.dev 的导入是两段，因为它的预览是**字段级 diff** —— 改的是已有行，要人逐字段勾选。
// 基准第一次导入是**从无到有**：字段级 diff 没有内容可看。人工真正要看的是两样 ——
// 哪些模型名没匹配上、哪些行会被改。dryRun 一次给全。
//
// # dryRun 为什么真的写一遍再回滚
//
// 「将要发生什么」唯一可信的算法就是**真的发生一遍**：用 upsert 的 `xmax = 0` 区分新建与
// 更新，比在 Go 里预先查存在性再猜要准（并发下也不会给出假条数）。回滚之后一行不留。
//
// # 一张闸门
//
// `display_tier` 为 `cite` / `link` 的榜不得带分数：`GET /api/v1/models/{slug}` 会把
// `benchmark_scores` 原样吐成 JSON，而这类源（如 Artificial Analysis）禁止经 API 分发
// 结构化数据。**整批 422**，不是跳过 —— 跳过会让人以为已经入库。

// benchmarkImportCounts 是四张表各自的新建 / 更新条数。
type benchmarkImportCounts struct {
	created api.BenchmarkImportCounts
	updated api.BenchmarkImportCounts
}

type benchmarkAuditEntry struct {
	id     int64
	action string
	before []byte
	after  []byte
}

// benchmarkImporter 持有一轮导入的全部中间态。
type benchmarkImporter struct {
	q      *store.Queries
	counts benchmarkImportCounts

	// 三层父行解析结果：slug → id、slug∥version → id、slug∥version∥key → id。
	benchmarkID map[string]int64
	versionID   map[string]int64
	metricID    map[string]int64

	// tier 记住每个 slug 的陈列档，用来判定分数闸门。
	tier map[string]string

	unmatched   []string
	seenUnmatch map[string]bool
	audit       []benchmarkAuditEntry
}

// ImportBenchmarks 对应 POST /api/v1/admin/benchmarks/import。
func (h *Handler) ImportBenchmarks(w http.ResponseWriter, r *http.Request) {
	if h.queries == nil || h.identity == nil || h.pool == nil {
		unavailable(w, errors.New("database not configured"))
		return
	}
	actor, ok := h.benchmarkImportActor(w, r)
	if !ok {
		return
	}

	var body api.BenchmarkImportRequest
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, api.InvalidParam, "请求体不是合法的 JSON")
		return
	}
	dryRun := body.DryRun != nil && *body.DryRun

	ctx, cancel := context.WithTimeout(r.Context(), benchmarkImportTimeout)
	defer cancel()

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		unavailable(w, err)
		return
	}
	// dryRun 一律回滚；真实导入只有全部成功才提交。defer 兜住所有提前 return 的分支。
	defer func() { _ = tx.Rollback(context.Background()) }()

	importer := &benchmarkImporter{
		q:           h.queries.WithTx(tx),
		benchmarkID: map[string]int64{},
		versionID:   map[string]int64{},
		metricID:    map[string]int64{},
		tier:        map[string]string{},
		seenUnmatch: map[string]bool{},
	}
	result, failure := importer.run(ctx, body.Payload)
	if failure != nil {
		writeError(w, failure.status, failure.code, failure.message)
		return
	}

	if !dryRun {
		for _, entry := range importer.audit {
			if err := h.queries.InsertAuditLog(ctx, store.InsertAuditLogParams{
				ActorID:    pgtype.Int8{Int64: actor.AccountID, Valid: actor.AccountID != 0},
				ActorRole:  actor.Role,
				Action:     entry.action,
				EntityType: "benchmark",
				EntityID:   entry.id,
				Before:     entry.before,
				After:      entry.after,
			}); err != nil {
				slog.Warn("写基准导入审计失败", "benchmark", entry.id, "err", err)
			}
		}
		if err := tx.Commit(ctx); err != nil {
			unavailable(w, err)
			return
		}
	}

	result.DryRun = dryRun
	writeJSON(w, http.StatusOK, result)
}

// benchmarkImportActor 是鉴权点：登录 + `benchmark.import`。
func (h *Handler) benchmarkImportActor(w http.ResponseWriter, r *http.Request) (demo.Actor, bool) {
	ctx, cancel := context.WithTimeout(r.Context(), queryTimeout)
	defer cancel()
	account, err := h.identity.Me(ctx, readSessionCookie(r))
	if err != nil {
		h.writeIdentityError(w, err)
		return demo.Actor{}, false
	}
	ok, err := h.queries.HasPermission(ctx, store.HasPermissionParams{
		Sub:           account.Sub,
		PermissionKey: "benchmark.import",
	})
	if err != nil {
		unavailable(w, err)
		return demo.Actor{}, false
	}
	if !ok {
		writeError(w, http.StatusForbidden, api.Forbidden, "没有基准导入权限")
		return demo.Actor{}, false
	}
	return demo.Actor{AccountID: account.ID, UserID: account.UserID, Role: account.Role}, true
}

const benchmarkImportTimeout = 60 * time.Second

// benchmarkInvalid 是「形状对但内容不成立」的统一出口（422）。
func benchmarkInvalid(format string, args ...interface{}) *proposalFailure {
	return proposalFail(http.StatusUnprocessableEntity, api.InvalidParam, fmt.Sprintf(format, args...))
}

// run 依次走五步 upsert。任何一步失败都返回 failure，由调用方决定回滚与状态码。
func (im *benchmarkImporter) run(ctx context.Context, payload api.BenchmarkImportPayload) (api.BenchmarkImportResult, *proposalFailure) {
	result := api.BenchmarkImportResult{
		Created:         api.BenchmarkImportCounts{},
		Updated:         api.BenchmarkImportCounts{},
		UnmatchedModels: []string{},
	}
	benchmarks := derefBenchmarks(payload.Benchmarks)
	versions := derefVersions(payload.Versions)
	metrics := derefMetrics(payload.Metrics)
	scores := derefScores(payload.Scores)

	if len(benchmarks)+len(versions)+len(metrics)+len(scores) == 0 {
		return result, benchmarkInvalid("快照是空的：benchmarks / versions / metrics / scores 一个都没给")
	}

	// 先立闸门，再写任何东西：cite / link 档带分数是**整批拒绝**，不是逐条跳过。
	for _, b := range benchmarks {
		tier := benchmarkTier(b)
		if tier != "table" && tier != "cite" && tier != "link" {
			return result, benchmarkInvalid("榜 %s 的 displayTier 只能是 table / cite / link", b.Slug)
		}
		if _, dup := im.tier[b.Slug]; dup {
			return result, benchmarkInvalid("同一份快照里榜 %s 出现了两次", b.Slug)
		}
		im.tier[b.Slug] = tier
	}
	for _, s := range scores {
		tier, known := im.tier[s.BenchmarkSlug]
		if !known {
			continue // 引用不存在的榜由父行校验统一报，这里不抢答
		}
		if tier != "table" {
			return result, benchmarkInvalid("榜 %s 是 %s 档，不能带分数：这类源禁止经 API 分发结构化数据（models 端点会把这些分数原样吐出去）", s.BenchmarkSlug, tier)
		}
	}

	// 第 1 步：benchmarks by slug。
	for _, b := range benchmarks {
		params := store.UpsertBenchmarkParams{
			Slug:        strings.TrimSpace(b.Slug),
			Name:        strings.TrimSpace(b.Name),
			Publisher:   derefTrimmed(b.Publisher),
			HomepageUrl: derefTrimmed(b.HomepageUrl),
			Category:    derefTrimmed(b.Category),
			Description: derefTrimmed(b.Description),
			DisplayTier: benchmarkTier(b),
			License:     derefTrimmed(b.License),
			LicenseUrl:  derefTrimmed(b.LicenseUrl),
			Attribution: derefTrimmed(b.Attribution),
		}
		if params.Slug == "" || params.Name == "" {
			return result, benchmarkInvalid("榜的 slug 与 name 都不能为空")
		}
		row, err := im.q.UpsertBenchmark(ctx, params)
		if err != nil {
			return result, benchmarkStoreFailure(err)
		}
		im.benchmarkID[params.Slug] = row.ID
		im.bump(&im.counts, "benchmarks", row.Inserted)
		im.record(row.ID, row.Inserted, params)
	}

	// 第 2 步：benchmark_versions by (benchmark_id, version)。
	for _, v := range versions {
		benchmarkID, ok := im.benchmarkID[v.BenchmarkSlug]
		if !ok {
			return result, benchmarkInvalid("版本 %s/%s 引用了这份快照里没有的榜 %s", v.BenchmarkSlug, v.Version, v.BenchmarkSlug)
		}
		params := store.UpsertBenchmarkVersionParams{
			BenchmarkID:      benchmarkID,
			Version:          strings.TrimSpace(v.Version),
			AsOf:             pgtype.Date{Time: v.AsOf.Time, Valid: true},
			SourceType:       firstNonEmpty(derefTrimmed(v.SourceType), "leaderboard"),
			Coverage:         firstNonEmpty(derefTrimmed(v.Coverage), "full"),
			ParticipantCount: pgtype.Int8{Int64: int64(derefInt(v.ParticipantCount)), Valid: v.ParticipantCount != nil},
			ScoreMin:         pgtype.Float8{Float64: derefFloat(v.ScoreMin), Valid: v.ScoreMin != nil},
			ScoreMax:         pgtype.Float8{Float64: derefFloat(v.ScoreMax), Valid: v.ScoreMax != nil},
			SourceUrl:        derefTrimmed(v.SourceUrl),
			Note:             derefTrimmed(v.Note),
		}
		if params.Version == "" {
			return result, benchmarkInvalid("榜 %s 的版本号不能为空", v.BenchmarkSlug)
		}
		row, err := im.q.UpsertBenchmarkVersion(ctx, params)
		if err != nil {
			return result, benchmarkStoreFailure(err)
		}
		im.versionID[benchmarkVersionKey(v.BenchmarkSlug, params.Version)] = row.ID
		im.bump(&im.counts, "versions", row.Inserted)
	}

	// 第 3 步：benchmark_metrics by (version_id, key)。
	for _, m := range metrics {
		versionID, ok := im.versionID[benchmarkVersionKey(m.BenchmarkSlug, m.Version)]
		if !ok {
			return result, benchmarkInvalid("指标 %s/%s/%s 引用了这份快照里没有的版本", m.BenchmarkSlug, m.Version, m.Key)
		}
		params := store.UpsertBenchmarkMetricParams{
			VersionID:      versionID,
			Key:            strings.TrimSpace(m.Key),
			Label:          firstNonEmpty(strings.TrimSpace(m.Label), strings.TrimSpace(m.Key)),
			MetricKind:     firstNonEmpty(derefTrimmed(m.MetricKind), "score"),
			Unit:           derefTrimmed(m.Unit),
			ScaleMin:       pgtype.Float8{Float64: derefFloat(m.ScaleMin), Valid: m.ScaleMin != nil},
			ScaleMax:       pgtype.Float8{Float64: derefFloat(m.ScaleMax), Valid: m.ScaleMax != nil},
			HigherIsBetter: m.HigherIsBetter == nil || *m.HigherIsBetter,
			IsPrimary:      m.IsPrimary != nil && *m.IsPrimary,
			Sort:           int32(derefInt(m.Sort)),
			Note:           derefTrimmed(m.Note),
		}
		if params.Key == "" {
			return result, benchmarkInvalid("指标 key 不能为空")
		}
		row, err := im.q.UpsertBenchmarkMetric(ctx, params)
		if err != nil {
			return result, benchmarkStoreFailure(err)
		}
		im.metricID[benchmarkMetricKey(m.BenchmarkSlug, m.Version, params.Key)] = row.ID
		im.bump(&im.counts, "metrics", row.Inserted)
	}

	// 第 4 步：benchmark_scores by (metric_id, external_name_key)，同时解析模型。
	for _, s := range scores {
		metricID, ok := im.metricID[benchmarkMetricKey(s.BenchmarkSlug, s.Version, s.MetricKey)]
		if !ok {
			return result, benchmarkInvalid("分数 %s/%s/%s 引用了这份快照里没有的指标", s.BenchmarkSlug, s.Version, s.MetricKey)
		}
		modelID, matched, err := im.resolve(ctx, s.ExternalName)
		if err != nil {
			return result, benchmarkStoreFailure(err)
		}
		matchStatus := "unmatched"
		if matched {
			matchStatus = "matched"
		} else {
			name := strings.TrimSpace(s.ExternalName)
			if !im.seenUnmatch[name] {
				im.seenUnmatch[name] = true
				im.unmatched = append(im.unmatched, name)
			}
		}
		params := store.UpsertBenchmarkScoreParams{
			MetricID:         metricID,
			ModelID:          modelID,
			ExternalName:     strings.TrimSpace(s.ExternalName),
			ExternalNameKey:  benchmarkNameKey(s.ExternalName),
			MatchStatus:      matchStatus,
			RawScore:         s.RawScore,
			Rank:             pgtype.Int4{Int32: int32(derefInt(s.Rank)), Valid: s.Rank != nil},
			Ranked:           s.Ranked == nil || *s.Ranked,
			SourceUrl:        derefTrimmed(s.SourceUrl),
			Note:             derefTrimmed(s.Note),
			SampleSize:       pgtype.Int8{Int64: int64(derefInt(s.SampleSize)), Valid: s.SampleSize != nil},
			UncertaintyLower: pgtype.Float8{Float64: derefFloat(s.UncertaintyLower), Valid: s.UncertaintyLower != nil},
			UncertaintyUpper: pgtype.Float8{Float64: derefFloat(s.UncertaintyUpper), Valid: s.UncertaintyUpper != nil},
		}
		if params.ExternalName == "" || params.ExternalNameKey == "" {
			return result, benchmarkInvalid("分数的 externalName 不能为空")
		}
		row, err := im.q.UpsertBenchmarkScore(ctx, params)
		if err != nil {
			return result, benchmarkStoreFailure(err)
		}
		im.bump(&im.counts, "scores", row.Inserted)
	}

	result.Created = im.counts.created
	result.Updated = im.counts.updated
	result.UnmatchedModels = im.unmatched
	return result, nil
}

// benchmarkKeySep 是复合键的分隔符：0x1f（单元分隔符）不会出现在 slug / 版本号 / 指标 key 里。
const benchmarkKeySep = "\x1f"

func benchmarkVersionKey(slug, version string) string {
	return slug + benchmarkKeySep + version
}

func benchmarkMetricKey(slug, version, key string) string {
	return slug + benchmarkKeySep + version + benchmarkKeySep + key
}

// benchmarkNameKey 把上游模型名折成解析键：小写、去空白与控制符。
//
// **这是当前口径的子集**：别名表里存的是「规范写法」，而规范化的最终裁决权在 #527。
// 键算错的直接后果是解析不到 → 名字进 `unmatchedModels`，人看得见、可重导，不会写错行。
func benchmarkNameKey(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// benchmarkTier 取陈列档；没给就是 table（与列默认值一致）。
func benchmarkTier(b api.BenchmarkImportBenchmark) string {
	if b.DisplayTier == nil {
		return "table"
	}
	return string(*b.DisplayTier)
}

func benchmarkStoreFailure(err error) *proposalFailure {
	slog.Error("基准导入写库失败", "err", err)
	return proposalFail(http.StatusServiceUnavailable, api.Unavailable, "服务暂时不可用")
}

// bump 按表名累加新建 / 更新计数。
func (im *benchmarkImporter) bump(counts *benchmarkImportCounts, table string, inserted bool) {
	target := &counts.updated
	if inserted {
		target = &counts.created
	}
	switch table {
	case "benchmarks":
		target.Benchmarks++
	case "versions":
		target.Versions++
	case "metrics":
		target.Metrics++
	case "scores":
		target.Scores++
	}
}

// record 记一条待写审计：动作按「新建 / 更新」二选一，`after` 存提交上来的那一行。
func (im *benchmarkImporter) record(id int64, inserted bool, row interface{}) {
	action := "update"
	if inserted {
		action = "create"
	}
	encoded, err := json.Marshal(row)
	if err != nil {
		encoded = nil
	}
	im.audit = append(im.audit, benchmarkAuditEntry{id: id, action: action, after: encoded})
}

// resolve 把上游模型名解析成站内模型；解析不到不是错误，是「等别名补上再重导」。
func (im *benchmarkImporter) resolve(ctx context.Context, name string) (pgtype.Int8, bool, error) {
	key := benchmarkNameKey(name)
	if key == "" {
		return pgtype.Int8{}, false, nil
	}
	id, err := im.q.ResolveModelForBenchmark(ctx, key)
	if errors.Is(err, pgx.ErrNoRows) {
		return pgtype.Int8{}, false, nil
	}
	if err != nil {
		return pgtype.Int8{}, false, err
	}
	return pgtype.Int8{Int64: id, Valid: true}, true, nil
}

func derefBenchmarks(v *[]api.BenchmarkImportBenchmark) []api.BenchmarkImportBenchmark {
	if v == nil {
		return nil
	}
	return *v
}

func derefVersions(v *[]api.BenchmarkImportVersion) []api.BenchmarkImportVersion {
	if v == nil {
		return nil
	}
	return *v
}

func derefMetrics(v *[]api.BenchmarkImportMetric) []api.BenchmarkImportMetric {
	if v == nil {
		return nil
	}
	return *v
}

func derefScores(v *[]api.BenchmarkImportScore) []api.BenchmarkImportScore {
	if v == nil {
		return nil
	}
	return *v
}

func derefTrimmed(v *string) string {
	if v == nil {
		return ""
	}
	return strings.TrimSpace(*v)
}

func derefInt(v *int) int {
	if v == nil {
		return 0
	}
	return *v
}

func derefFloat(v *float64) float64 {
	if v == nil {
		return 0
	}
	return *v
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
