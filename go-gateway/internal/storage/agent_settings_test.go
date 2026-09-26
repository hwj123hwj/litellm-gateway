package storage

import (
	"path/filepath"
	"testing"
)

func TestAgentSettingsRoundtrip(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "test.db"), nil)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	// 未设置的 key 返回空串而不是错误。
	if v, err := store.GetSetting("assistant_system_prompt"); err != nil || v != "" {
		t.Fatalf("unset key: got (%q, %v), want (\"\", nil)", v, err)
	}
	if err := store.SetSetting("assistant_system_prompt", "自定义人设"); err != nil {
		t.Fatalf("set: %v", err)
	}
	// 覆盖写。
	if err := store.SetSetting("assistant_system_prompt", ""); err != nil {
		t.Fatalf("set empty: %v", err)
	}
	if v, _ := store.GetSetting("assistant_system_prompt"); v != "" {
		t.Fatalf("after overwrite: got %q, want empty", v)
	}
}

func TestAssistantFeedbackRoundtrip(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "test.db"), nil)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	if err := store.AddAssistantFeedback("down", "答错了网关端口", "把 4001 说成 8080"); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := store.AddAssistantFeedback("up", "正确提案了记忆", ""); err != nil {
		t.Fatalf("add2: %v", err)
	}
	feedback, err := store.ListAssistantFeedback(10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(feedback) != 2 {
		t.Fatalf("got %d entries, want 2", len(feedback))
	}
	if feedback[0].Rating != "up" {
		t.Fatalf("newest first: got %q, want up", feedback[0].Rating)
	}
	if len(feedback[1].Note) == 0 {
		t.Fatal("note lost")
	}
}
