package piconfig

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/joho/godotenv"
)

const ProviderID = "llm-gateway"

const DefaultTokenCommand = "!llm-gateway auth print-master-key"

type SetupOptions struct {
	GatewayHome string
	PiHome      string
	Endpoint    string
	DryRun      bool
}

func Setup(options SetupOptions) ([]byte, string, error) {
	if options.GatewayHome == "" || options.PiHome == "" {
		return nil, "", fmt.Errorf("gateway and Pi home directories are required")
	}

	endpoint, err := resolveEndpoint(options.Endpoint, options.GatewayHome)
	if err != nil {
		return nil, "", err
	}
	modelsPath := filepath.Join(options.PiHome, "agent", "models.json")
	existing, err := os.ReadFile(modelsPath)
	if err != nil && !os.IsNotExist(err) {
		return nil, "", fmt.Errorf("read Pi models config: %w", err)
	}

	merged, err := MergeModelsConfig(existing, endpoint, DefaultTokenCommand)
	if err != nil {
		return nil, "", err
	}
	if options.DryRun {
		return merged, modelsPath, nil
	}
	if err := os.MkdirAll(filepath.Dir(modelsPath), 0700); err != nil {
		return nil, "", fmt.Errorf("create Pi config directory: %w", err)
	}
	if err := os.WriteFile(modelsPath, merged, 0600); err != nil {
		return nil, "", fmt.Errorf("write Pi models config: %w", err)
	}
	return merged, modelsPath, nil
}

func MergeModelsConfig(existing []byte, endpoint, tokenCommand string) ([]byte, error) {
	document := map[string]any{}
	if len(existing) > 0 {
		if err := json.Unmarshal(existing, &document); err != nil {
			return nil, fmt.Errorf("parse Pi models.json: %w", err)
		}
	}

	providers, ok := document["providers"].(map[string]any)
	if !ok || providers == nil {
		providers = map[string]any{}
		document["providers"] = providers
	}

	providers[ProviderID] = gatewayProvider(normalizeEndpoint(endpoint), tokenCommand)
	encoded, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode Pi models.json: %w", err)
	}
	return append(encoded, '\n'), nil
}

func MasterKey(gatewayHome string) (string, error) {
	values, err := godotenv.Read(filepath.Join(gatewayHome, ".env"))
	if err != nil {
		return "", fmt.Errorf("read gateway .env: %w", err)
	}
	key := strings.TrimSpace(values["LITELLM_MASTER_KEY"])
	if key == "" {
		return "", fmt.Errorf("LITELLM_MASTER_KEY is missing from gateway .env")
	}
	return key, nil
}

func gatewayProvider(endpoint, tokenCommand string) map[string]any {
	return map[string]any{
		"baseUrl":    endpoint,
		"api":        "openai-completions",
		"apiKey":     tokenCommand,
		"authHeader": true,
		"compat": map[string]any{
			"supportsDeveloperRole":    false,
			"supportsReasoningEffort":  false,
			"supportsUsageInStreaming": false,
		},
		"models": []map[string]any{
			{"id": "coding", "name": "LLM Gateway Coding"},
			{"id": "glm-sonnet", "name": "LLM Gateway GLM Sonnet"},
			{"id": "glm-opus", "name": "LLM Gateway GLM Opus"},
			{"id": "mimo-sonnet", "name": "LLM Gateway MiMo Sonnet"},
			{"id": "longcat-sonnet", "name": "LLM Gateway LongCat Sonnet"},
			{"id": "easyclaw-sonnet", "name": "LLM Gateway Claude Sonnet"},
		},
	}
}

func resolveEndpoint(endpoint, gatewayHome string) (string, error) {
	if endpoint != "" {
		return normalizeEndpoint(endpoint), nil
	}
	values, err := godotenv.Read(filepath.Join(gatewayHome, ".env"))
	if err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("read gateway .env: %w", err)
	}
	port := strings.TrimSpace(values["PORT"])
	if port == "" {
		port = "4001"
	}
	return "http://127.0.0.1:" + port + "/v1", nil
}

func normalizeEndpoint(endpoint string) string {
	trimmed := strings.TrimRight(strings.TrimSpace(endpoint), "/")
	if !strings.HasSuffix(trimmed, "/v1") {
		trimmed += "/v1"
	}
	return trimmed
}
