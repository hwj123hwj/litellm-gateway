package zcodeconfig

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

const existingConfig = `{
  "schemaVersion": 1,
  "config": {
    "providerConfigRules": {
      "providerRules": [
        {
          "providerId": "eb0ac13e-3d4c-4107-a30a-008dd000707e",
          "providerName": "自定义网关",
          "config": {
            "group": "standard-personal",
            "access": {"type": "api-key", "apiKey": "sk-existing"},
            "api": {"type": "openai-responses", "baseUrl": "http://127.0.0.1:4001"},
            "personalModelIds": ["coding"],
            "modelOrder": ["coding"]
          }
        }
      ]
    },
    "modelConfigRules": {"providerModelRules": [{"modelId": "coding"}]}
  }
}`

func TestSetup_UpdatesExistingRule(t *testing.T) {
	home := t.TempDir()
	path := ConfigPath(home)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(existingConfig), 0o600); err != nil {
		t.Fatal(err)
	}

	_, _, err := Setup(SetupOptions{
		ZCodeHome: home,
		Endpoint:  "http://127.0.0.1:4001",
		ModelIDs:  []string{"coding", "glm-5.3"},
	})
	if err != nil {
		t.Fatal(err)
	}

	raw, _ := os.ReadFile(path)
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	rules := doc["config"].(map[string]any)["providerConfigRules"].(map[string]any)["providerRules"].([]any)
	if len(rules) != 1 {
		t.Fatalf("rules = %d, want 1（不允许新建第二条）", len(rules))
	}
	rule := rules[0].(map[string]any)
	if rule["providerName"] != "自定义网关" {
		t.Errorf("providerName = %v", rule["providerName"])
	}
	cfg := rule["config"].(map[string]any)
	ids := cfg["personalModelIds"].([]any)
	if len(ids) != 2 || ids[0] != "coding" || ids[1] != "glm-5.3" {
		t.Errorf("personalModelIds = %v", ids)
	}
	// access 不被改动
	if cfg["access"].(map[string]any)["apiKey"] != "sk-existing" {
		t.Errorf("existing apiKey must be preserved")
	}
	// 其他顶层段保留
	if _, ok := doc["config"].(map[string]any)["modelConfigRules"]; !ok {
		t.Errorf("modelConfigRules must be preserved")
	}
}

func TestSetup_CreatesRuleWhenMissing(t *testing.T) {
	home := t.TempDir()
	_, _, err := Setup(SetupOptions{
		ZCodeHome: home,
		Endpoint:  "http://127.0.0.1:4001",
		APIKey:    "sk-master",
		ModelIDs:  []string{"coding"},
	})
	if err != nil {
		t.Fatal(err)
	}

	raw, _ := os.ReadFile(ConfigPath(home))
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	rules := doc["config"].(map[string]any)["providerConfigRules"].(map[string]any)["providerRules"].([]any)
	if len(rules) != 1 {
		t.Fatalf("rules = %d, want 1", len(rules))
	}
	rule := rules[0].(map[string]any)
	if rule["providerName"] != DefaultProviderName {
		t.Errorf("providerName = %v", rule["providerName"])
	}
	if id, _ := rule["providerId"].(string); len(id) != 36 {
		t.Errorf("providerId = %q, want uuid v4", id)
	}
	cfg := rule["config"].(map[string]any)
	if cfg["access"].(map[string]any)["apiKey"] != "sk-master" {
		t.Errorf("created rule must carry apiKey")
	}
}
