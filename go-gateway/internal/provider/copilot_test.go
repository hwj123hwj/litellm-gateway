package provider

import (
	"testing"
)

func TestCopilotMapModel(t *testing.T) {
	p := &CopilotProvider{}

	tests := []struct {
		input    string
		expected string
	}{
		{"copilot", "gpt-4o"},
		{"auto", "gpt-4o"},
		{"copilot-auto", "gpt-4o"},
		{"copilot-opus", "gpt-4o"},
		{"copilot-sonnet", "gpt-4o"},
		{"copilot-haiku", "gpt-4o-mini"},
		{"gpt-4o-mini", "gpt-4o-mini"},
		{"unknown-model", "gpt-4o"},
	}

	for _, tt := range tests {
		got := p.mapModel(tt.input)
		if got != tt.expected {
			t.Errorf("mapModel(%q) = %q; want %q", tt.input, got, tt.expected)
		}
	}
}
