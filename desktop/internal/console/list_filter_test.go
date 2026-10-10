package console

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestRowFilterSearchAndRefresh(t *testing.T) {
	models := []Model{{Name: "Alpha", Provider: "BENCH"}, {Name: "Beta", Provider: "云端"}, {Name: "Gamma", Provider: "bench"}}
	var f rowFilter[Model]
	calls := 0
	text := func(m Model) string { calls++; return modelSearchText(m) }
	rows, _ := f.apply(models, "", text)
	if rows.Len() != 3 || calls != 0 || &rows.source[0] != &models[0] {
		t.Fatal("unfiltered rows should reuse the source without building a search index")
	}
	for _, test := range []struct {
		query string
		want  []string
	}{{"BENCH", []string{"Alpha", "Gamma"}}, {"云端", []string{"Beta"}}, {"alpha bench", []string{"Alpha"}}, {"missing", nil}, {"", []string{"Alpha", "Beta", "Gamma"}}} {
		rows, _ = f.apply(models, test.query, text)
		if rows.Len() != len(test.want) {
			t.Fatalf("query %q: got %d matches, want %d", test.query, rows.Len(), len(test.want))
		}
		for i, name := range test.want {
			if rows.At(i).Name != name {
				t.Fatalf("query %q: row %d is %q", test.query, i, rows.At(i).Name)
			}
		}
	}
	if calls != len(models) {
		t.Fatal("changing a query rebuilt the source search index", calls)
	}
	f.apply(models, "bench", text)
	rows, changed := f.apply(models, "BENCH", text)
	if changed || calls != len(models) || rows.Len() != 2 {
		t.Fatal("identical case-insensitive search was not reused")
	}
	// Refresh replaces data even if the new response has exactly the same length.
	replacement := []Model{{Name: "New", Provider: "other"}, {Name: "Other", Provider: "other"}, {Name: "Last", Provider: "other"}}
	rows, changed = f.apply(replacement, "bench", text)
	if !changed || rows.Len() != 0 || calls != 6 {
		t.Fatal("refresh reused stale matching rows")
	}
	rows, changed = f.apply(models[:1], "bench", text)
	if !changed || rows.Len() != 1 || rows.At(0).Name != "Alpha" {
		t.Fatal("source length change did not invalidate the filter")
	}
	rows, _ = f.apply(nil, "bench", text)
	if rows.Len() != 0 || f.source != nil || len(f.searchText) != 0 {
		t.Fatal("clearing the source retained the old rows")
	}
}

func TestRowFilterSteadyUpdatesDoNotAllocate(t *testing.T) {
	models := performanceModels(10000)
	for _, query := range []string{"", "bench", "BENCH"} {
		var f rowFilter[Model]
		f.apply(models, query, modelSearchText)
		allocs := testing.AllocsPerRun(100, func() {
			rows, changed := f.apply(models, query, modelSearchText)
			if changed || rows.Len() != len(models) {
				t.Fatal("unchanged dataset recomputed")
			}
		})
		if allocs != 0 {
			t.Fatalf("query %q allocated %.0f times per update", query, allocs)
		}
	}
}

func TestModelSearchKeepsActionsOnMatchedRows(t *testing.T) {
	a, queue, _ := fixture(t)
	a.page = "models"
	a.models = []Model{{Name: "Alpha", Provider: "BENCH"}, {Name: "Beta", Provider: "云端"}}
	a.loaded = true
	tt := ui.NewTester(a.View, 1280, 850)
	click(t, tt, "搜索模型")
	tt.Type("云端")
	if a.modelFilter != "云端" {
		t.Fatal("model search did not accept keyboard input")
	}
	if tt.HasText("Alpha") || !tt.HasText("Beta") {
		t.Fatal("model view did not apply the search")
	}
	click(t, tt, "Beta 能力设置")
	if a.editorID != "Beta" {
		t.Fatal("filtered action targeted a different model")
	}
	a.editorOpen = false
	a.models = []Model{{Name: "Fresh", Provider: "云端"}}
	tt.Frame()
	if tt.HasText("Beta") || !tt.HasText("Fresh") {
		t.Fatal("model view retained rows after a refresh")
	}
	// In-flight host work is still cancelled and all cached source rows released.
	a.navigate("providers")
	a.resetHost()
	await(t, a, queue)
	if a.listViews.models.source != nil || a.listViews.models.searchText != nil {
		t.Fatal("host reset retained the model cache")
	}
}

func TestLogSearchClearsSelectionAndPreservesDetails(t *testing.T) {
	a, _, _ := fixture(t)
	a.page = "logs"
	a.loaded = true
	a.logs = []Log{{Model: "Alpha", Provider: "BENCH", RequestID: "req-1", Code: 200}, {Model: "Beta", Provider: "云端", RequestID: "req-2", Method: "POST", Path: "/v1/messages", Error: "限流", Code: 429}}
	tt := ui.NewTester(a.View, 1280, 850)
	a.logSelected = 0
	click(t, tt, "搜索日志")
	tt.Type("限流")
	if a.logSelected != -1 || tt.HasText("Alpha") || !tt.HasText("Beta") {
		t.Fatal("filter retained a selection from a different row")
	}
	a.logSelected = 0
	tt.Frame()
	if !tt.HasText("POST /v1/messages · req-2") {
		t.Fatal("filtered detail targeted a different log")
	}
	a.logs = []Log{{Model: "Fresh", Provider: "BENCH", RequestID: "req-3", Code: 200}}
	tt.Frame()
	if a.logSelected != -1 || !tt.HasText("暂无匹配的请求日志") {
		t.Fatal("new logs reused stale search results or selection")
	}
	click(t, tt, "搜索日志")
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Key(0, ui.KeyBackspace)
	if !tt.HasText("Fresh") {
		t.Fatal("clearing the search did not restore logs")
	}
	a.resetHost()
	if a.listViews.logs.source != nil || a.listViews.logs.searchText != nil {
		t.Fatal("host reset retained the log cache")
	}
}
