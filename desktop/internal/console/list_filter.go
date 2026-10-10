package console

import (
	"strings"

	"github.com/egoist/mygo/ui"
)

type listViewState struct {
	models    rowFilter[Model]
	logs      rowFilter[Log]
	modelList ui.ListState
}

// rowFilter keeps only one search result. API reloads replace the source slice;
// rows must not be edited in place while cached. Matching text is built lazily
// for searches, never for an unfiltered list or on every view update.
type rowFilter[T any] struct {
	source      []T
	input       string
	query       string
	initialized bool
	searchText  []string
	matches     []int
}

type visibleRows[T any] struct {
	source   []T
	indices  []int
	filtered bool
}

func (v visibleRows[T]) Len() int {
	if v.filtered {
		return len(v.indices)
	}
	return len(v.source)
}

func (v visibleRows[T]) At(i int) T {
	if v.filtered {
		i = v.indices[i]
	}
	return v.source[i]
}

func sameRows[T any](a, b []T) bool {
	return len(a) == len(b) && (len(a) == 0 || &a[0] == &b[0])
}

func (f *rowFilter[T]) apply(source []T, query string, text func(T) string) (visibleRows[T], bool) {
	if !f.initialized || query != f.input {
		f.input = query
		query = strings.ToLower(query)
	} else {
		query = f.query
	}
	sourceChanged := !sameRows(f.source, source)
	changed := !f.initialized || sourceChanged || f.query != query
	if changed {
		if sourceChanged {
			f.source = source
			f.searchText = nil
			f.matches = nil
		}
		f.initialized, f.query = true, query
		if query != "" {
			if len(f.searchText) != len(source) {
				f.searchText = make([]string, len(source))
				for i, row := range source {
					f.searchText[i] = strings.ToLower(text(row))
				}
			}
			if cap(f.matches) < len(source) {
				f.matches = make([]int, 0, len(source))
			} else {
				f.matches = f.matches[:0]
			}
			for i, row := range f.searchText {
				if strings.Contains(row, query) {
					f.matches = append(f.matches, i)
				}
			}
		}
	}
	return visibleRows[T]{source: source, indices: f.matches, filtered: query != ""}, changed
}

func modelSearchText(m Model) string { return m.Name + " " + m.Provider }

func logSearchText(l Log) string {
	return l.Model + " " + l.Provider + " " + l.RequestID + " " + l.Error
}
