package httpapi

import (
	"fmt"
	"strconv"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/deepdemos/ds-demos-showcase/backend/internal/store"
)

// 总览矩阵的折行（#572）。
//
// 折行函数是「排序 + 上限 + 截断后过滤」唯一的手写出口，而这三件事错了页面看起来
// 仍然像正常矩阵：行序错了像是「模型没排好」，上限静默截断像是「榜只有这么多」。
// 所以这里用纯数据钉住三条判据，不依赖数据库：
//   - 行按有格子的列数降序、再按模型名 —— 名字序与覆盖数序故意不一致；
//   - 列超过 24 时只留前 24，且被截掉的列不能再有格子、不能再撑行；
//   - 行超过 200 时只留前 200，totalRows 仍然是截断前的条数。

// overviewFlat 造一行「某榜 × 某模型」的扁平行；modelSlug 为空表示该榜没有分数（LEFT JOIN 的空行）。
func overviewFlat(column, group, modelSlug, modelName string, score float64, rank int32) store.ListBenchmarkOverviewRow {
	row := store.ListBenchmarkOverviewRow{
		BenchmarkSlug: column,
		BenchmarkName: column,
		CategoryGroup: group,
		Unit:          "%",
		Version:       "v1",
		RawScore:      pgtype.Float8{Float64: score, Valid: true},
	}
	if modelSlug != "" {
		row.ModelSlug = pgtype.Text{String: modelSlug, Valid: true}
		row.ModelName = pgtype.Text{String: modelName, Valid: true}
	}
	if rank > 0 {
		row.Rank = pgtype.Int4{Int32: rank, Valid: true}
	}
	return row
}

func TestFoldBenchmarkOverviewSortsRowsByCoverageThenName(t *testing.T) {
	// SQL 的列序已经排好（能力组 → 榜名），折行不再碰列序。
	flats := []store.ListBenchmarkOverviewRow{
		{BenchmarkSlug: "col-code", BenchmarkName: "A 编码榜", CategoryGroup: "coding", Unit: "%", Version: "v1"},
		{BenchmarkSlug: "col-agent", BenchmarkName: "B agent 榜", CategoryGroup: "agent", Unit: "%", Version: "v1"},
		{BenchmarkSlug: "col-math", BenchmarkName: "C 数学榜", CategoryGroup: "math", Unit: "%", Version: "v1"},
		overviewFlat("col-code", "coding", "zeta", "Zeta", 80, 1),
		overviewFlat("col-code", "coding", "alpha", "Alpha", 70, 0),
		overviewFlat("col-agent", "agent", "zeta", "Zeta", 60, 2),
		overviewFlat("col-math", "math", "beta", "Beta", 50, 3),
		// 只有榜、没有分数：列照常出现，但不撑出任何行。
		{BenchmarkSlug: "col-empty", BenchmarkName: "D 空列榜", CategoryGroup: "other", Unit: "%", Version: "v1"},
	}

	out := foldBenchmarkOverview(flats)

	if len(out.Columns) != 4 {
		t.Fatalf("列数 = %d，想要 4（空列也要占一列）：%+v", len(out.Columns), out.Columns)
	}
	wantColumns := []string{"col-code", "col-agent", "col-math", "col-empty"}
	for i, want := range wantColumns {
		if out.Columns[i].BenchmarkSlug != want {
			t.Fatalf("第 %d 列 = %q，想要 %q：折行不该重排 SQL 给的列序", i, out.Columns[i].BenchmarkSlug, want)
		}
	}
	if out.Columns[0].Category != "coding" || out.Columns[1].Category != "agent" ||
		out.Columns[2].Category != "math" || out.Columns[3].Category != "other" {
		t.Fatalf("每列的能力组没有从 SQL 带出来：%+v", out.Columns)
	}

	// 覆盖数序（Zeta 2 格）与名字序（Alpha 在前）故意相反：按名字排就会先出 Alpha。
	wantRows := []string{"Zeta", "Alpha", "Beta"}
	if len(out.Rows) != len(wantRows) {
		t.Fatalf("行数 = %d，想要 %d：%+v", len(out.Rows), len(wantRows), out.Rows)
	}
	for i, want := range wantRows {
		if out.Rows[i].ModelName != want {
			t.Fatalf("第 %d 行 = %q，想要 %q：行要先按覆盖列数降序、再按模型名", i, out.Rows[i].ModelName, want)
		}
	}
	if len(out.Rows[0].Cells) != 2 {
		t.Fatalf("Zeta 的格子 = %d，想要 2：%+v", len(out.Rows[0].Cells), out.Rows[0].Cells)
	}
	if out.Rows[1].Cells[0].Rank != nil {
		t.Fatalf("Alpha 在 col-code 上的名次 = %v，想要 nil（源榜没给名次）", *out.Rows[1].Cells[0].Rank)
	}
	if out.TotalColumns != 4 || out.TotalRows != 3 {
		t.Fatalf("totalColumns/totalRows = %d/%d，想要 4/3", out.TotalColumns, out.TotalRows)
	}
}

func TestFoldBenchmarkOverviewTruncatesColumns(t *testing.T) {
	// 30 列：前 24 列各有一个自己的模型，后 6 列只有「只在被截掉的列上出现」的模型。
	flats := make([]store.ListBenchmarkOverviewRow, 0, 36)
	for i := 0; i < 30; i++ {
		slug := fmt.Sprintf("col-%02d", i)
		flats = append(flats, store.ListBenchmarkOverviewRow{
			BenchmarkSlug: slug,
			BenchmarkName: fmt.Sprintf("榜 %02d", i),
			CategoryGroup: "coding",
			Unit:          "%",
			Version:       "v1",
		})
		if i < benchmarkOverviewColumnLimit {
			flats = append(flats, overviewFlat(slug, "coding", fmt.Sprintf("model-%02d", i), fmt.Sprintf("Model %02d", i), float64(i), 1))
		}
	}
	flats = append(flats, overviewFlat("col-29", "coding", "model-late", "Late Model", 99, 1))

	out := foldBenchmarkOverview(flats)

	if len(out.Columns) != benchmarkOverviewColumnLimit {
		t.Fatalf("列数 = %d，想要上限 %d", len(out.Columns), benchmarkOverviewColumnLimit)
	}
	if out.TotalColumns != 30 {
		t.Fatalf("totalColumns = %d，想要 30（截断前的总数）", out.TotalColumns)
	}
	if out.Columns[len(out.Columns)-1].BenchmarkSlug != "col-23" {
		t.Fatalf("最后一列 = %q，想要 col-23（只留前 24 列）", out.Columns[len(out.Columns)-1].BenchmarkSlug)
	}
	if out.TotalRows != benchmarkOverviewColumnLimit {
		t.Fatalf("totalRows = %d，想要 %d：只在被截掉的列上有格子的模型不该成行", out.TotalRows, benchmarkOverviewColumnLimit)
	}
	for _, row := range out.Rows {
		if row.ModelSlug == "model-late" {
			t.Fatal("只在被截掉的列上有格子的模型成了行：页面上会是一整行「—」")
		}
		for _, cell := range row.Cells {
			if cell.BenchmarkSlug == "col-29" {
				t.Fatalf("被截掉的列 %q 仍留在格子里：%+v", cell.BenchmarkSlug, cell)
			}
		}
	}
}

func TestFoldBenchmarkOverviewTruncatesRows(t *testing.T) {
	flats := []store.ListBenchmarkOverviewRow{{
		BenchmarkSlug: "col-only", BenchmarkName: "唯一一列", CategoryGroup: "general", Unit: "%", Version: "v1",
	}}
	for i := 0; i <= benchmarkOverviewRowLimit; i++ {
		name := fmt.Sprintf("Model %03d", i)
		flats = append(flats, overviewFlat("col-only", "general", "model-"+strconv.Itoa(i), name, float64(i), 1))
	}

	out := foldBenchmarkOverview(flats)

	if len(out.Rows) != benchmarkOverviewRowLimit {
		t.Fatalf("行数 = %d，想要上限 %d", len(out.Rows), benchmarkOverviewRowLimit)
	}
	if out.TotalRows != benchmarkOverviewRowLimit+1 {
		t.Fatalf("totalRows = %d，想要 %d（截断前的总数）", out.TotalRows, benchmarkOverviewRowLimit+1)
	}
	if out.Rows[0].ModelName != "Model 000" || out.Rows[len(out.Rows)-1].ModelName != "Model 199" {
		t.Fatalf("保留的行不是前 %d 条：首 %q 末 %q", benchmarkOverviewRowLimit, out.Rows[0].ModelName, out.Rows[len(out.Rows)-1].ModelName)
	}
}

func TestFoldBenchmarkOverviewEmptyMatrix(t *testing.T) {
	out := foldBenchmarkOverview(nil)

	if out.Columns == nil || out.Rows == nil {
		t.Fatalf("空矩阵的两个数组必须是 [] 而不是 null：columns=%v rows=%v", out.Columns, out.Rows)
	}
	if len(out.Columns) != 0 || len(out.Rows) != 0 || out.TotalColumns != 0 || out.TotalRows != 0 {
		t.Fatalf("空矩阵应该是 0 列 0 行 0 总数：%+v", out)
	}
}
