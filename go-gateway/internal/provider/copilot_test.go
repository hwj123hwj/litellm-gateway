package provider

import (
	"testing"
)

// Copilot 只服务 GPT 系列：除 gpt-4o-mini 外，任何模型名都回落到 gpt-4o。
// 网关里的别名已移除（copilot-*、auto、glm-opus 不再存在）。
func TestCopilotMapModel(t *testing.T) {
	p := &CopilotProvider{}

	tests := []struct {
		input    string
		expected string
	}{
		{"copilot", "gpt-4o"},
		{"gpt-4o-mini", "gpt-4o-mini"},
		{"coding", "gpt-4o"},
		{"unknown-model", "gpt-4o"},
	}

	for _, tt := range tests {
		got := p.mapModel(tt.input)
		if got != tt.expected {
			t.Errorf("mapModel(%q) = %q; want %q", tt.input, got, tt.expected)
		}
	}
}
