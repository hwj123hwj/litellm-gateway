package piconfig

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestMergeModelsConfigPreservesExistingProviders(t *testing.T) {
	existing := []byte(`{"providers":{"existing":{"baseUrl":"https://example.test/v1"}}}`)

	merged, err := MergeModelsConfig(existing, "http://127.0.0.1:4001/v1", "!llm-gateway auth print-master-key")
	if err != nil {
		t.Fatalf("MergeModelsConfig() error = %v", err)
	}

	var document map[string]any
	if err := json.Unmarshal(merged, &document); err != nil {
		t.Fatalf("unmarshal merged config: %v", err)
	}
	providers := document["providers"].(map[string]any)
	if _, ok := providers["existing"]; !ok {
		t.Fatal("existing provider was removed")
	}
	gateway := providers[ProviderID].(map[string]any)
	if gateway["baseUrl"] != "http://127.0.0.1:4001/v1" {
		t.Fatalf("baseUrl = %v", gateway["baseUrl"])
	}
	if gateway["apiKey"] != "!llm-gateway auth print-master-key" {
		t.Fatalf("apiKey = %v", gateway["apiKey"])
	}
	if gateway["authHeader"] != true {
		t.Fatalf("authHeader = %v", gateway["authHeader"])
	}
}

func TestMergeModelsConfigCreatesDocumentAndNormalizesEndpoint(t *testing.T) {
	merged, err := MergeModelsConfig(nil, "http://localhost:4001/", "!gateway auth print-master-key")
	if err != nil {
		t.Fatalf("MergeModelsConfig() error = %v", err)
	}

	var document struct {
		Providers map[string]struct {
			BaseURL string `json:"baseUrl"`
			API     string `json:"api"`
			Models  []struct {
				ID string `json:"id"`
			} `json:"models"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(merged, &document); err != nil {
		t.Fatalf("unmarshal merged config: %v", err)
	}
	gateway := document.Providers[ProviderID]
	if gateway.BaseURL != "http://localhost:4001/v1" {
		t.Fatalf("baseUrl = %q", gateway.BaseURL)
	}
	if gateway.API != "openai-completions" {
		t.Fatalf("api = %q", gateway.API)
	}
	if len(gateway.Models) == 0 || gateway.Models[0].ID != "coding" {
		t.Fatalf("models = %#v", gateway.Models)
	}
}

func TestMergeModelsConfigRejectsInvalidJSON(t *testing.T) {
	if _, err := MergeModelsConfig([]byte("not-json"), "http://localhost:4001", "!gateway auth print-master-key"); err == nil {
		t.Fatal("expected invalid JSON error")
	}
}

func TestSetupWritesOnlyGatewayProvider(t *testing.T) {
	root := t.TempDir()
	gatewayHome := filepath.Join(root, "gateway")
	piHome := filepath.Join(root, "pi")
	if err := os.MkdirAll(gatewayHome, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gatewayHome, ".env"), []byte("PORT=8111\n"), 0600); err != nil {
		t.Fatal(err)
	}
	modelsPath := filepath.Join(piHome, "agent", "models.json")
	if err := os.MkdirAll(filepath.Dir(modelsPath), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(modelsPath, []byte(`{"providers":{"kept":{"api":"openai-completions"}}}`), 0600); err != nil {
		t.Fatal(err)
	}

	_, writtenPath, err := Setup(SetupOptions{GatewayHome: gatewayHome, PiHome: piHome})
	if err != nil {
		t.Fatalf("Setup() error = %v", err)
	}
	if writtenPath != modelsPath {
		t.Fatalf("written path = %q", writtenPath)
	}
	written, err := os.ReadFile(modelsPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(written) == `{"providers":{"kept":{"api":"openai-completions"}}}` {
		t.Fatal("models config was not updated")
	}
	if !json.Valid(written) {
		t.Fatal("written models config is invalid JSON")
	}
}

func TestMasterKeyReadsGatewayConfig(t *testing.T) {
	gatewayHome := t.TempDir()
	if err := os.WriteFile(filepath.Join(gatewayHome, ".env"), []byte("LITELLM_MASTER_KEY=sk-test\n"), 0600); err != nil {
		t.Fatal(err)
	}
	key, err := MasterKey(gatewayHome)
	if err != nil {
		t.Fatalf("MasterKey() error = %v", err)
	}
	if key != "sk-test" {
		t.Fatalf("key = %q", key)
	}
}
