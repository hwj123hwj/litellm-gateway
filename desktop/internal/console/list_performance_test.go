package console

import (
	"fmt"
	"testing"

	"github.com/egoist/mygo/ui"
)

func performanceModels(n int) []Model {
	models := make([]Model, n)
	for i := range models {
		models[i] = Model{Name: fmt.Sprintf("bench-model-%05d", i), Provider: "BENCH", Status: "online", Capabilities: []string{"text"}}
	}
	return models
}

// Measure warmed view updates, including layout, but not window/GPU presentation.
func BenchmarkListView(b *testing.B) {
	for _, n := range []int{1000, 10000} {
		for _, query := range []string{"", "bench"} {
			label := "empty"
			if query != "" {
				label = "search"
			}
			b.Run(fmt.Sprintf("models/%d/%s", n, label), func(b *testing.B) {
				a := &App{page: "models", loaded: true, models: performanceModels(n), modelFilter: query}
				benchmarkListView(b, a)
			})
		}
	}
	for _, query := range []string{"", "bench"} {
		label := "empty"
		if query != "" {
			label = "search"
		}
		b.Run("logs/200/"+label, func(b *testing.B) {
			a := &App{page: "logs", loaded: true, logFilter: query, logLimit: "200", logSelected: -1}
			for i := range 200 {
				a.logs = append(a.logs, Log{Model: "coding", Provider: "BENCH", RequestID: fmt.Sprint(i), Code: 200})
			}
			benchmarkListView(b, a)
		})
	}
}

func benchmarkListView(b *testing.B, a *App) {
	tt := ui.NewTester(a.View, 1280, 850)
	for range 10 {
		tt.Frame()
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		tt.Frame()
	}
}
