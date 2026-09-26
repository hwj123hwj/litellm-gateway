package dshconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const existingPatch = `# 注释说明行（重写后会丢失，靠 .pre-sync.bak 兜底）
- id: agent-preset-registry
  name: "@deepseek-ai/dsh-agent-preset-registry"
  config:
    default: standard
- id: llm-deepseek
  name: "@deepseek-ai/dsh-llm-deepseek-api-key"
  config:
    baseURL: http://localhost:4001/v1/messages
    models:
      - id: old-model
        name: old-model
- id: llm-pi-ai
  name: "@deepseek-ai/dsh-llm-pi-ai"
  config:
    providers:
      anthropic:
        baseURL: http://localhost:4001
        models:
          - id: old-model
            name: old-model
        apiKeyEnv: ANTHROPIC_API_KEY
`

func TestSetup_UpdatesBothEntries(t *testing.T) {
	home := t.TempDir()
	path := PatchPath(home)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(existingPatch), 0o600); err != nil {
		t.Fatal(err)
	}

	_, _, skipped, err := Setup(SetupOptions{
		DshHome:  home,
		Endpoint: "http://127.0.0.1:4001",
		ModelIDs: []string{"coding", "glm-5.3"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(skipped) != 0 {
		t.Fatalf("skipped = %v, want empty", skipped)
	}

	updated, _ := os.ReadFile(path)
	text := string(updated)
	if !strings.Contains(text, "coding") || !strings.Contains(text, "glm-5.3") {
		t.Errorf("new models missing:\n%s", text)
	}
	if strings.Contains(text, "old-model") {
		t.Errorf("stale model must be removed:\n%s", text)
	}
	// 其他条目保留
	if !strings.Contains(text, "agent-preset-registry") {
		t.Errorf("unrelated entries must be preserved:\n%s", text)
	}
	// apiKeyEnv 保留
	if !strings.Contains(text, "ANTHROPIC_API_KEY") {
		t.Errorf("apiKeyEnv must be preserved:\n%s", text)
	}
	// llm-deepseek 的 baseURL 带上 /v1/messages
	if !strings.Contains(text, "http://127.0.0.1:4001/v1/messages") {
		t.Errorf("deepseek baseURL wrong:\n%s", text)
	}

	// CurrentModels 能读回新清单
	ids, exists, missing, err := CurrentModels(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || !exists[EntryDeepseek] || !exists[EntryPiAI] || len(missing) != 0 {
		t.Fatalf("CurrentModels = %v, %v, %v", ids, exists, missing)
	}
}

func TestSetup_SkipsMissingEntries(t *testing.T) {
	home := t.TempDir()
	_, _, skipped, err := Setup(SetupOptions{
		DshHome:  home,
		Endpoint: "http://127.0.0.1:4001",
		ModelIDs: []string{"coding"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(skipped) != 2 || skipped[0] != EntryDeepseek || skipped[1] != EntryPiAI {
		t.Fatalf("skipped = %v, want both entries skipped（文件不存在时不盲建条目）", skipped)
	}
}
