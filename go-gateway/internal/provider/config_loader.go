package provider

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// ProviderConfig 提供商配置
type ProviderConfig struct {
	Name                  string        `yaml:"name"`
	Type                  string        `yaml:"type"` // openai 或 anthropic
	URL                   string        `yaml:"url"`
	APIKeyEnv             string        `yaml:"api_key_env"`
	RequestTimeoutSeconds int           `yaml:"request_timeout_seconds,omitempty"`
	Models                []ModelConfig `yaml:"models"`
}

// ModelConfig 模型配置
type ModelConfig struct {
	ID              string   `yaml:"id"`
	Aliases         []string `yaml:"aliases"`
	ProviderName    string   `yaml:"provider_name,omitempty"` // 可选：自定义 provider 实例名，默认为模型 ID
	Protocol        string   `yaml:"protocol,omitempty"`
	Capabilities    []string `yaml:"capabilities,omitempty"`
	InputModalities []string `yaml:"input_modalities,omitempty"`
	MaxInputTokens  int      `yaml:"max_input_tokens,omitempty"`
	MaxOutputTokens int      `yaml:"max_output_tokens,omitempty"`
}

// ProvidersConfig 提供商配置文件
type ProvidersConfig struct {
	Providers []ProviderConfig    `yaml:"providers"`
	Chains    map[string][]string `yaml:"chains"`
}

// ProviderRegistry 提供商注册表
type ProviderRegistry struct {
	mu       sync.RWMutex
	registry map[string]func(*ProviderConfig) (Provider, error)
}

// NewProviderRegistry 创建新的提供商注册表
func NewProviderRegistry() *ProviderRegistry {
	r := &ProviderRegistry{
		registry: make(map[string]func(*ProviderConfig) (Provider, error)),
	}

	// 注册默认的提供商工厂
	r.Register("openai", NewOpenAIProviderFromConfig)
	r.Register("anthropic", NewAnthropicProviderFromConfig)
	r.Register("chatgpt", NewChatGPTProviderFromConfig)

	return r
}

// Register 注册提供商工厂函数
func (r *ProviderRegistry) Register(providerType string, factory func(*ProviderConfig) (Provider, error)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.registry[providerType] = factory
}

// Create 创建提供商实例
func (r *ProviderRegistry) Create(cfg *ProviderConfig) (Provider, error) {
	r.mu.RLock()
	factory, ok := r.registry[cfg.Type]
	r.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("unknown provider type: %s", cfg.Type)
	}

	return factory(cfg)
}

// LoadProvidersConfig 从 YAML 文件加载配置
func LoadProvidersConfig(path string) (*ProvidersConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config file: %w", err)
	}

	var cfg ProvidersConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse yaml: %w", err)
	}

	return &cfg, nil
}

// NewOpenAIProviderFromConfig 从配置创建 OpenAI 提供商
func NewOpenAIProviderFromConfig(cfg *ProviderConfig) (Provider, error) {
	apiKey := providerAPIKeyFromEnv(cfg.APIKeyEnv)
	if apiKey == "" {
		return nil, fmt.Errorf("API key not found: %s", cfg.APIKeyEnv)
	}

	return NewOpenAIProvider(&Config{
		Name:           cfg.Name,
		URL:            cfg.URL,
		APIKey:         apiKey,
		RequestTimeout: time.Duration(cfg.RequestTimeoutSeconds) * time.Second,
	}), nil
}

// NewAnthropicProviderFromConfig 从配置创建 Anthropic 提供商
func NewAnthropicProviderFromConfig(cfg *ProviderConfig) (Provider, error) {
	apiKey := providerAPIKeyFromEnv(cfg.APIKeyEnv)
	if apiKey == "" {
		return nil, fmt.Errorf("API key not found: %s", cfg.APIKeyEnv)
	}

	return NewAnthropicProvider(&Config{
		Name:   cfg.Name,
		URL:    cfg.URL,
		APIKey: apiKey,
	}), nil
}

// NewChatGPTProviderFromConfig creates the subscription-backed provider. It
// intentionally does not use api_key_env: credentials come from Codex CLI or
// Pi's OAuth auth.json instead of an API key.
func NewChatGPTProviderFromConfig(_ *ProviderConfig) (Provider, error) {
	proxyURL := os.Getenv("HTTP_PROXY")
	if proxyURL == "" {
		proxyURL = os.Getenv("HTTPS_PROXY")
	}
	if proxyURL == "" {
		proxyURL = os.Getenv("https_proxy")
	}
	chatgpt := NewChatGPTProviderWithAuthPath(proxyURL, os.Getenv("CHATGPT_AUTH_FILE"))
	if !chatgpt.IsHealthy(context.Background()) {
		return nil, fmt.Errorf("ChatGPT OAuth credential is unavailable")
	}
	return chatgpt, nil
}

func providerAPIKeyFromEnv(envKey string) string {
	keys := []string{envKey}
	if envKey == "ALI_API_KEY" {
		keys = append(keys, "ALIYUN_MAAS_API_KEY", "DASHSCOPE_API_KEY")
	}
	for _, key := range keys {
		if value := os.Getenv(key); value != "" {
			return value
		}
	}
	return ""
}

// SetupProvidersFromConfig 从配置文件设置提供商和路由
func SetupProvidersFromConfig(router *Router, configPath string, logger interface{ Printf(string, ...interface{}) }) (map[string]string, error) {
	cfg, err := LoadProvidersConfig(configPath)
	if err != nil {
		return nil, err
	}

	registry := NewProviderRegistry()
	modelToProvider := make(map[string]string)
	// provider 实例名默认直接用上游模型 ID（与上游一致，避免 glm-glm-4.7 这种
	// 供应商名与模型名重复的双写）。若不同供应商声明了同名模型，则退回
	// 供应商-模型 的限定形式，保证实例名仍然唯一。
	usedProviderNames := make(map[string]bool)

	// 注册提供商
	for _, pc := range cfg.Providers {
		// 创建基础 provider
		baseProvider, err := registry.Create(&pc)
		if err != nil {
			logger.Printf("Warning: failed to create provider %s: %v", pc.Name, err)
			continue
		}

		// ChatGPT Codex is a native Responses passthrough provider. Register the
		// shared provider directly so the marker interface is preserved and the
		// model catalog remains authoritative in providers.yaml.
		if pc.Type == "chatgpt" {
			router.RegisterProvider(pc.Name, baseProvider)
			for _, mc := range pc.Models {
				capabilities := normalizeModelCapabilities(mc.Capabilities)
				protocol := mc.Protocol
				if protocol == "" {
					protocol = "responses"
				}
				registerModel := func(modelName string) {
					router.RegisterChain(modelName, []string{pc.Name})
					router.RegisterModel(ModelInfo{
						ID:              modelName,
						Provider:        pc.Name,
						Protocol:        protocol,
						Capabilities:    capabilities,
						InputModalities: modelInputModalities(capabilities, mc.InputModalities),
						MaxInputTokens:  mc.MaxInputTokens,
						MaxOutputTokens: mc.MaxOutputTokens,
					})
					modelToProvider[modelName] = pc.Name
				}
				// 与下面的分支保持一致：上游模型 ID 对外主名，别名作为兼容入口。
				registerModel(mc.ID)
				modelToProvider[mc.ID] = pc.Name
				for _, alias := range mc.Aliases {
					registerModel(alias)
					modelToProvider[alias] = pc.Name
				}
			}
			logger.Printf("Registered provider: %s (type=%s, url=%s)", pc.Name, pc.Type, pc.URL)
			continue
		}

		// 为每个模型创建独立的 provider 实例（绑定模型 ID）
		for _, mc := range pc.Models {
			providerName := mc.ProviderName
			if providerName == "" {
				providerName = providerInstanceName(pc.Name, mc.ID, usedProviderNames)
			}
			usedProviderNames[providerName] = true

			// 创建绑定模型的 provider，并把能力元数据绑定到这个具体模型。
			capabilities := normalizeModelCapabilities(mc.Capabilities)
			boundProvider := NewBoundModelProviderWrapper(baseProvider, mc.ID, capabilities)
			router.RegisterProvider(providerName, boundProvider)
			protocol := mc.Protocol
			if protocol == "" {
				protocol = pc.Type
			}
			registerModel := func(modelName string) {
				router.RegisterModel(ModelInfo{
					ID:              modelName,
					Provider:        pc.Name,
					Protocol:        protocol,
					Capabilities:    capabilities,
					InputModalities: modelInputModalities(capabilities, mc.InputModalities),
					MaxInputTokens:  mc.MaxInputTokens,
					MaxOutputTokens: mc.MaxOutputTokens,
				})
			}

			// 上游模型 ID 是对外主名；aliases 是兼容旧客户端的历史代号。
			// 两者都注册，所以既可以用上游真名调用，原有别名也不会失效。
			router.RegisterChain(mc.ID, []string{providerName})
			registerModel(mc.ID)
			modelToProvider[mc.ID] = providerName
			if len(mc.Aliases) > 0 {
				for _, alias := range mc.Aliases {
					router.RegisterChain(alias, []string{providerName})
					registerModel(alias)
					modelToProvider[alias] = providerName
				}
				logger.Printf("Registered model: %s (aliases %v, provider=%s)", mc.ID, mc.Aliases, providerName)
			} else {
				logger.Printf("Registered model: %s (provider=%s)", mc.ID, providerName)
			}
		}

		logger.Printf("Registered provider: %s (type=%s, url=%s)", pc.Name, pc.Type, pc.URL)
	}

	// 注册 fallback 链
	for chainName, providers := range cfg.Chains {
		router.RegisterChain(chainName, providers)
		logger.Printf("Registered chain: %s -> %v", chainName, providers)
	}

	return modelToProvider, nil
}

// BoundModelProviderWrapper 包装一个 provider 并绑定特定模型
type BoundModelProviderWrapper struct {
	Provider
	boundModel   string
	capabilities []string
}

// NewBoundModelProviderWrapper 创建绑定模型的 provider 包装器
func NewBoundModelProviderWrapper(p Provider, model string, declared ...[]string) *BoundModelProviderWrapper {
	capabilities := normalizeModelCapabilities(nil)
	if len(declared) > 0 {
		capabilities = normalizeModelCapabilities(declared[0])
	}
	return &BoundModelProviderWrapper{Provider: p, boundModel: model, capabilities: capabilities}
}

// BoundModel 返回绑定的模型名
func (w *BoundModelProviderWrapper) BoundModel() string {
	return w.boundModel
}

func (w *BoundModelProviderWrapper) Capabilities() []string {
	return append([]string(nil), w.capabilities...)
}

// ForwardStream 转发到底层支持流式的 provider
func (w *BoundModelProviderWrapper) ForwardStream(ctx context.Context, req *Request, out io.Writer) error {
	sp, ok := w.Provider.(StreamProvider)
	if !ok {
		return fmt.Errorf("provider %s does not support streaming", w.Provider.Name())
	}
	return sp.ForwardStream(ctx, req, out)
}

// Probe 探测底层 provider 时带上绑定的模型名：同一个上游地址下，不同模型的
// 可用性可能不同（例如账号只对部分模型有权限），用绑定名才测得到真实的那一个。
func (w *BoundModelProviderWrapper) Probe(ctx context.Context) ProbeResult {
	if prober, ok := w.Provider.(modelProber); ok {
		return prober.ProbeModel(ctx, w.boundModel)
	}
	if prober, ok := w.Provider.(Prober); ok {
		return prober.Probe(ctx)
	}
	return ProbeResult{Status: ProbeUnknown, Detail: "provider 不支持上游探测"}
}

// providerInstanceName 决定 provider 实例名。
//
// 默认直接用上游模型 ID：面板和路由里显示的就是上游真名（glm-4.7、glm-5.2），
// 不再出现「glm-glm-4.7」这种供应商名与模型名重复的双写。只有当另一个供应商
// 已经占用了同一个模型 ID 时，才退回「供应商-模型」形式以保证唯一。
func providerInstanceName(providerName, modelID string, used map[string]bool) string {
	if !used[modelID] {
		return modelID
	}
	qualified := fmt.Sprintf("%s-%s", providerName, modelID)
	// 极端情况下限定名也可能撞车（两个供应商都叫同一个名字），继续加后缀兜底。
	for suffix := 2; used[qualified]; suffix++ {
		qualified = fmt.Sprintf("%s-%s-%d", providerName, modelID, suffix)
	}
	return qualified
}

func normalizeModelCapabilities(capabilities []string) []string {
	if len(capabilities) == 0 {
		// Existing YAML files did not declare capabilities. Preserve their
		// text/tool/streaming behavior; multimodal support must be explicit.
		return []string{CapabilityText, CapabilityToolCall, CapabilityStreaming, CapabilityReasoning}
	}
	seen := make(map[string]bool, len(capabilities))
	result := make([]string, 0, len(capabilities))
	for _, capability := range capabilities {
		if capability == "" || seen[capability] {
			continue
		}
		seen[capability] = true
		result = append(result, capability)
	}
	if !seen[CapabilityText] {
		result = append(result, CapabilityText)
	}
	return result
}

func modelInputModalities(capabilities, explicit []string) []string {
	if len(explicit) > 0 {
		return append([]string(nil), explicit...)
	}
	modalities := []string{}
	for _, capability := range capabilities {
		switch capability {
		case CapabilityText:
			modalities = append(modalities, "text")
		case CapabilityVision:
			modalities = append(modalities, "image")
		case CapabilityVideo:
			modalities = append(modalities, "video")
		case CapabilityFile:
			modalities = append(modalities, "file")
		case CapabilityAudio:
			modalities = append(modalities, "audio")
		}
	}
	return modalities
}
