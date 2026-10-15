package console

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestSidebarBadgesReflectIssuesAndCandidates(t *testing.T) {
	a, _, _ := fixture(t)
	a.page = "overview"
	a.loaded = true
	// Set 1 candidate memory, 1 offline provider, 1 expired channel
	a.memories = []Memory{{ID: 1, Scope: "global", Status: "candidate", Statement: "测试候选记忆"}}
	a.providers = []Provider{{Name: "faulty-p", Status: "offline"}}
	a.channels = []AccountChannel{{
		ID:       "anthropic",
		LoggedIn: true,
		Accounts: []ChannelAccount{{
			Nickname:  "acc-1",
			ExpiresAt: "2020-01-01T00:00:00Z", // expired
			Expired:   true,
		}},
	}}

	tt := ui.NewTester(a.View, 1100, 800)
	tt.Frame()

	// Sidebar should render badges with count
	if !tt.HasText("1") {
		t.Fatal("sidebar badges missing count")
	}
}

func TestLogsTableSortingAndSplitVertical(t *testing.T) {
	a, _, _ := fixture(t)
	a.page = "logs"
	a.loaded = true
	a.logs = []Log{
		{Model: "slow-model", Provider: "p-slow", RequestID: "req-1", Code: 200, Latency: 1200},
		{Model: "fast-model", Provider: "p-fast", RequestID: "req-2", Code: 500, Latency: 50},
	}

	tt := ui.NewTester(a.View, 1280, 850)
	tt.Frame()

	// Initial table order
	if !tt.HasText("slow-model") || !tt.HasText("fast-model") {
		t.Fatal("table missing models")
	}

	// Sort by latency
	click(t, tt, "延迟")
	tt.Frame()
	if a.logSort.Column != "延迟" {
		t.Fatalf("sort column not set to 延迟, got %q", a.logSort.Column)
	}

	// Select first row to activate SplitVertical
	a.logSelected = 0
	tt.Frame()
	if !tt.HasText("复制 ID") {
		t.Fatal("detail pane not rendered in split view")
	}
}

func TestOverviewProvidersSorting(t *testing.T) {
	a, _, _ := fixture(t)
	a.page = "overview"
	a.loaded = true
	a.dashboard.Providers = []Provider{
		{Name: "zeta", Status: "online", Requests: 10, AvgLatency: 200},
		{Name: "alpha", Status: "offline", Requests: 50, AvgLatency: 30},
	}

	tt := ui.NewTester(a.View, 1100, 800)
	tt.Frame()

	click(t, tt, "延迟")
	tt.Frame()
	if a.overviewSort.Column != "延迟" {
		t.Fatalf("overviewSort not set to 延迟, got %q", a.overviewSort.Column)
	}
}

func TestMemoryScopeSegmentedSelection(t *testing.T) {
	a, _, _ := fixture(t)
	a.page = "memory"
	a.loaded = true
	a.memoryScope = "global"

	tt := ui.NewTester(a.View, 1100, 800)
	tt.Frame()

	// Should show segmented choices
	if !tt.HasText("global") || !tt.HasText("client") || !tt.HasText("project") {
		t.Fatal("memoryScope segments missing")
	}

	click(t, tt, "client")
	tt.Frame()
	if a.memoryScope != "client" {
		t.Fatalf("expected memoryScope to be client, got %s", a.memoryScope)
	}
}

func TestVibrancyWindowSupport(t *testing.T) {
	a, _, _ := fixture(t)
	a.page = "overview"
	a.loaded = true

	tt := ui.NewTester(a.View, 1100, 800)
	tt.SetVibrancy(true)
	tt.Frame()

	if !tt.HasText("EasyGateway") {
		t.Fatal("vibrancy view failed to render header")
	}
}

func TestOverviewProviderSearchAndSorting(t *testing.T) {
	a, _, _ := fixture(t)
	a.page = "overview"
	a.loaded = true
	a.dashboard.Summary = Summary{
		TodayRequests: 282,
		SuccessRate:   99.6,
		TodayTokens:   119800000,
		AvgLatency:    10400,
	}
	a.dashboard.Providers = []Provider{
		{Name: "gemini-flash", Status: "online", Requests: 280, AvgLatency: 85},
		{Name: "offline-p", Status: "offline", Requests: 0, AvgLatency: 0},
		{Name: "unknown-p", Status: "unknown", Requests: 0, AvgLatency: 0},
	}

	tt := ui.NewTester(a.View, 1100, 800)
	tt.Frame()

	// Verify KPI metrics display properly
	if !tt.HasText("119.8M") {
		t.Fatal("tokens metric missing or formatted incorrectly")
	}
	if !tt.HasText("今日请求") || !tt.HasText("282") {
		t.Fatal("today requests metric missing")
	}

	// Filter providers
	a.overviewFilter = "gemini"
	tt.Frame()
	if !tt.HasText("gemini-flash") {
		t.Fatal("filtered gemini-flash should be visible")
	}
	if tt.HasText("offline-p") {
		t.Fatal("offline-p should be filtered out")
	}

	// Clear filter
	a.overviewFilter = ""
	tt.Frame()
	if !tt.HasText("offline-p") {
		t.Fatal("offline-p should be visible after filter cleared")
	}
}

func TestMarkdownCodeBlockEditorHighlighting(t *testing.T) {
	md := &markdownDocument{}
	codeSnippet := "```go\nfunc hello() string {\n\treturn \"world\"\n}\n```"

	tt := ui.NewTester(func(c *ui.Context) {
		md.render(c, codeSnippet)
	}, 800, 600)
	tt.Frame()

	if !tt.HasText("复制代码") {
		t.Fatal("code block missing copy button")
	}
	if !tt.HasText("go") {
		t.Fatal("code block missing language header")
	}

	click(t, tt, "复制代码")
	if tt.Clipboard() != strings.TrimSuffix(strings.TrimPrefix(codeSnippet, "```go\n"), "\n```") {
		t.Fatal("copy changed code", tt.Clipboard())
	}
}

func TestDescendingLatencyPreservesEqualRows(t *testing.T) {
	for _, page := range []string{"overview", "logs"} {
		t.Run(page, func(t *testing.T) {
			a, _, _ := fixture(t)
			a.page, a.loaded = page, true
			a.dashboard.Providers = []Provider{
				{Name: "first-equal", AvgLatency: 20},
				{Name: "second-equal", AvgLatency: 20},
				{Name: "slowest", AvgLatency: 30},
			}
			a.logs = []Log{
				{Model: "first-equal", RequestID: "req-1", Latency: 20},
				{Model: "second-equal", RequestID: "req-2", Latency: 20},
				{Model: "slowest", RequestID: "req-3", Latency: 30},
			}
			a.overviewSort = ui.SortOrder{Column: "延迟", Descending: true}
			a.logSort = a.overviewSort
			tt := ui.NewTester(a.View, 1280, 850)
			var rows []string
			for _, text := range tt.Texts() {
				switch text {
				case "first-equal", "second-equal", "slowest":
					rows = append(rows, text)
				}
			}
			if strings.Join(rows, ",") != "slowest,first-equal,second-equal" {
				t.Fatalf("descending latency lost stable row order: %v", rows)
			}
		})
	}
}
