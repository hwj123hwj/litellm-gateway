package provider

import (
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"time"
)

func TestProviderConfigUsesLegacyAliAPIKey(t *testing.T) {
	t.Setenv("ALI_API_KEY", "")
	t.Setenv("DASHSCOPE_API_KEY", "")
	t.Setenv("ALIYUN_MAAS_API_KEY", "legacy-ali-key")

	configured, err := NewOpenAIProviderFromConfig(&ProviderConfig{
		Name:      "ali",
		Type:      "openai",
		URL:       "https://example.invalid/v1/chat/completions",
		APIKeyEnv: "ALI_API_KEY",
	})
	if err != nil {
		t.Fatalf("expected legacy Ali key to configure provider: %v", err)
	}
	if configured.APIKey() != "legacy-ali-key" {
		t.Fatalf("expected legacy Ali key, got %q", configured.APIKey())
	}
}

func TestProviderConfigAppliesRequestTimeout(t *testing.T) {
	t.Setenv("GLM_API_KEY", "glm-test-key")

	configured, err := NewOpenAIProviderFromConfig(&ProviderConfig{
		Name:                  "glm",
		Type:                  "openai",
		URL:                   "https://example.invalid/v1/chat/completions",
		APIKeyEnv:             "GLM_API_KEY",
		RequestTimeoutSeconds: 900,
	})
	if err != nil {
		t.Fatalf("expected provider to configure: %v", err)
	}

	openAI, ok := configured.(*OpenAIProvider)
	if !ok {
		t.Fatalf("expected *OpenAIProvider, got %T", configured)
	}
	// 超时语义：限制首字节时间 + 流式空闲看门狗，不再是 client 总时长
	if got := openAI.requestTimeout(); got != 900*time.Second {
		t.Fatalf("request timeout = %s, want 15m", got)
	}
	transport, ok := openAI.client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("expected *http.Transport, got %T", openAI.client.Transport)
	}
	if transport.ResponseHeaderTimeout != 900*time.Second {
		t.Fatalf("ResponseHeaderTimeout = %s, want 15m", transport.ResponseHeaderTimeout)
	}
	if openAI.client.Timeout != 0 {
		t.Fatalf("client.Timeout 应保持未设置（流式总时长不受限），实际 %s", openAI.client.Timeout)
	}
}

func TestOpenAIProviderUsesSafeDefaultRequestTimeout(t *testing.T) {
	provider := NewOpenAIProvider(&Config{Name: "test"})
	if got := provider.requestTimeout(); got != defaultRequestTimeout {
		t.Fatalf("request timeout = %s, want %s", got, defaultRequestTimeout)
	}
}

func TestProvidersConfigKeepsKnowledgeCompileFallbackOrder(t *testing.T) {
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	configPath := filepath.Join(filepath.Dir(sourceFile), "..", "..", "providers.yaml")
	config, err := LoadProvidersConfig(configPath)
	if err != nil {
		t.Fatalf("load providers.yaml: %v", err)
	}

	want := []string{"qwen3.8-max-preview", "copilot", "glm-5.2"}
	if got := config.Chains["glm-opus"]; !reflect.DeepEqual(got, want) {
		t.Fatalf("glm-opus fallback chain = %#v, want %#v", got, want)
	}
}

// provider 实例名默认直接用上游模型 ID，避免 glm-glm-4.7 这类重复前缀；
// 只有跨供应商同名时才退回限定形式。
func TestProviderInstanceNameUsesUpstreamModelID(t *testing.T) {
	used := map[string]bool{}

	if got := providerInstanceName("glm", "glm-5.2", used); got != "glm-5.2" {
		t.Fatalf("provider instance name = %q, want %q", got, "glm-5.2")
	}
	used["glm-5.2"] = true

	// 第二个供应商声明同名模型时必须仍然唯一。
	if got := providerInstanceName("backup", "glm-5.2", used); got != "backup-glm-5.2" {
		t.Fatalf("duplicate model instance name = %q, want %q", got, "backup-glm-5.2")
	}
}

// 上游模型 ID 是对外主名，历史别名同时可用（旧客户端不失效）。
func TestSetupProvidersExposesUpstreamNameAndLegacyAlias(t *testing.T) {
	router := NewRouter(log.New(io.Discard, "", 0))
	path := writeTempProvidersConfig(t, `
providers:
  - name: glm
    type: openai
    url: https://example.invalid/v1/chat/completions
    api_key_env: GLM_TEST_KEY
    models:
      - id: glm-4.7
        aliases: [glm-haiku]
        capabilities: [text]
`)
	t.Setenv("GLM_TEST_KEY", "test-key")

	if _, err := SetupProvidersFromConfig(router, path, log.New(io.Discard, "", 0)); err != nil {
		t.Fatalf("setup providers: %v", err)
	}

	for _, modelName := range []string{"glm-4.7", "glm-haiku"} {
		providers, err := router.Route(modelName)
		if err != nil {
			t.Fatalf("route %s: %v", modelName, err)
		}
		if len(providers) != 1 {
			t.Fatalf("route %s returned %d providers, want 1", modelName, len(providers))
		}
		// 包装器把请求模型改写为绑定的上游名，两条入口最终都发往 glm-4.7。
		bound, ok := providers[0].(BoundModelProvider)
		if !ok {
			t.Fatalf("route %s provider does not implement BoundModelProvider", modelName)
		}
		if got := bound.BoundModel(); got != "glm-4.7" {
			t.Fatalf("route %s bound model = %q, want %q", modelName, got, "glm-4.7")
		}
	}
}

func writeTempProvidersConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "providers.yaml")
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatalf("write providers.yaml: %v", err)
	}
	return path
}
