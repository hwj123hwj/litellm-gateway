package middleware

import "testing"

func TestIsMetricsExcludedPath(t *testing.T) {
	tests := []struct {
		path     string
		excluded bool
	}{
		{path: "/", excluded: true},
		{path: "/dashboard", excluded: true},
		{path: "/dashboard/settings", excluded: true},
		{path: "/assets/index.js", excluded: true},
		{path: "/health", excluded: true},
		{path: "/readyz", excluded: true},
		{path: "/admin/dashboard", excluded: true},
		{path: "/v1/chat/completions", excluded: false},
		{path: "/models", excluded: false},
	}

	for _, test := range tests {
		if got := isMetricsExcludedPath(test.path); got != test.excluded {
			t.Errorf("isMetricsExcludedPath(%q) = %v, want %v", test.path, got, test.excluded)
		}
	}
}
