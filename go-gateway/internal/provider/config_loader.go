package provider

import (
	"fmt"
	"os"
	"sync"

	"gopkg.in/yaml.v3"
)

// ProviderConfig 提供商配置
type ProviderConfig struct {
	Name      string        `yaml:"name"`
	Type      string        `yaml:"type"` // openai 或 anthropic
	URL       string        `yaml:"url"`
	APIKeyEnv string        `yaml:"api_key_env"`
	Models    []ModelConfig `yaml:"models"`
}

// ModelConfig 模型配置
type ModelConfig struct {
	ID      string   `yaml:"id"`
	Aliases []string `yaml:"aliases"`
}

// ProvidersConfig 提供商配置文件
type ProvidersConfig struct {
	Providers []ProviderConfig `yaml:"providers"`
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
	apiKey := os.Getenv(cfg.APIKeyEnv)
	if apiKey == "" {
		return nil, fmt.Errorf("API key not found: %s", cfg.APIKeyEnv)
	}

	return NewOpenAIProvider(&Config{
		Name:   cfg.Name,
		URL:    cfg.URL,
		APIKey: apiKey,
	}), nil
}

// NewAnthropicProviderFromConfig 从配置创建 Anthropic 提供商
func NewAnthropicProviderFromConfig(cfg *ProviderConfig) (Provider, error) {
	apiKey := os.Getenv(cfg.APIKeyEnv)
	if apiKey == "" {
		return nil, fmt.Errorf("API key not found: %s", cfg.APIKeyEnv)
	}

	return NewAnthropicProvider(&Config{
		Name:   cfg.Name,
		URL:    cfg.URL,
		APIKey: apiKey,
	}), nil
}

// SetupProvidersFromConfig 从配置文件设置提供商和路由
func SetupProvidersFromConfig(router *Router, configPath string, logger interface{ Printf(string, ...interface{}) }) (map[string]string, error) {
	cfg, err := LoadProvidersConfig(configPath)
	if err != nil {
		return nil, err
	}

	registry := NewProviderRegistry()
	modelToProvider := make(map[string]string)

	// 注册提供商
	for _, pc := range cfg.Providers {
		p, err := registry.Create(&pc)
		if err != nil {
			logger.Printf("Warning: failed to create provider %s: %v", pc.Name, err)
			continue
		}

		router.RegisterProvider(pc.Name, p)
		logger.Printf("Registered provider: %s (type=%s, url=%s)", pc.Name, pc.Type, pc.URL)

		// 为每个模型注册链
		for _, mc := range pc.Models {
			router.RegisterChain(mc.ID, []string{pc.Name})
			modelToProvider[mc.ID] = pc.Name

			// 注册别名
			for _, alias := range mc.Aliases {
				router.RegisterChain(alias, []string{pc.Name})
				modelToProvider[alias] = pc.Name
			}
		}
	}

	// 注册 fallback 链
	for chainName, providers := range cfg.Chains {
		router.RegisterChain(chainName, providers)
		logger.Printf("Registered chain: %s -> %v", chainName, providers)
	}

	return modelToProvider, nil
}
