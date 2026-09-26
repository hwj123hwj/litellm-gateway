// Package zcodeconfig 把网关的模型清单同步进 ZCode 桌面端的 provider 配置
// （~/.zcode/v2/provider_config.json）。只更新网关对应 provider 规则的模型列表，
// 其余字段与规则一律不动；文件不存在时从零创建。
package zcodeconfig

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	// DefaultProviderName 是新建规则时使用的显示名；更新时按 baseUrl 匹配，
	// 匹配不到才按此名新建。
	DefaultProviderName = "LLM Gateway"
	defaultAPIType      = "openai-responses"
	defaultGroup        = "standard-personal"
)

// DefaultHome 返回 ZCode 配置根目录（~/.zcode）。
func DefaultHome() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".zcode"
	}
	return filepath.Join(home, ".zcode")
}

// ConfigPath 返回 provider 配置文件路径。
func ConfigPath(home string) string {
	return filepath.Join(home, "v2", "provider_config.json")
}

// SetupOptions 描述一次同步。
type SetupOptions struct {
	ZCodeHome string
	Endpoint  string // 网关根地址，如 http://127.0.0.1:4001
	APIKey    string // 仅新建规则时写入 access.apiKey；已有规则不动
	ModelIDs  []string
	DryRun    bool
}

// Setup 执行同步，返回合并后的 JSON 与文件路径。
func Setup(options SetupOptions) ([]byte, string, error) {
	if options.ZCodeHome == "" || options.Endpoint == "" {
		return nil, "", fmt.Errorf("zcode home and endpoint are required")
	}
	if len(options.ModelIDs) == 0 {
		return nil, "", fmt.Errorf("model id list is required")
	}

	path := ConfigPath(options.ZCodeHome)
	document := map[string]any{}
	if raw, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(raw, &document); err != nil {
			return nil, path, fmt.Errorf("parse %s: %w", path, err)
		}
	} else if !os.IsNotExist(err) {
		return nil, path, fmt.Errorf("read ZCode provider config: %w", err)
	}

	if err := mergeRule(document, options); err != nil {
		return nil, path, err
	}

	encoded, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return nil, path, fmt.Errorf("encode ZCode provider config: %w", err)
	}
	encoded = append(encoded, '\n')
	if options.DryRun {
		return encoded, path, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, path, fmt.Errorf("create ZCode config directory: %w", err)
	}
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		return nil, path, fmt.Errorf("write ZCode provider config: %w", err)
	}
	return encoded, path, nil
}

// mergeRule 定位网关对应的 provider 规则并更新模型列表；找不到则新建。
func mergeRule(document map[string]any, options SetupOptions) error {
	config, _ := document["config"].(map[string]any)
	if config == nil {
		config = map[string]any{}
		document["config"] = config
	}
	rulesWrapper, _ := config["providerConfigRules"].(map[string]any)
	if rulesWrapper == nil {
		rulesWrapper = map[string]any{}
		config["providerConfigRules"] = rulesWrapper
	}
	rules, _ := rulesWrapper["providerRules"].([]any)
	if rules == nil {
		rules = []any{}
		rulesWrapper["providerRules"] = rules
	}

	target, index := findRule(rules, options.Endpoint)
	if index < 0 {
		rule := map[string]any{
			"providerId":   newUUID(),
			"providerName": DefaultProviderName,
			"config": map[string]any{
				"group":  defaultGroup,
				"access": map[string]any{"type": "api-key", "apiKey": options.APIKey},
				"api":    map[string]any{"type": defaultAPIType, "baseUrl": options.Endpoint},
			},
		}
		rulesWrapper["providerRules"] = append(rules, rule)
		return nil
	}
	_ = target

	rule := rules[index].(map[string]any)
	ruleConfig, _ := rule["config"].(map[string]any)
	if ruleConfig == nil {
		ruleConfig = map[string]any{}
		rule["config"] = ruleConfig
	}
	ids := append([]string(nil), options.ModelIDs...)
	ruleConfig["personalModelIds"] = ids
	ruleConfig["modelOrder"] = ids
	return nil
}

// findRule 按 api.baseUrl 的 host:port 匹配网关规则；providerName 兜底。
func findRule(rules []any, endpoint string) (map[string]any, int) {
	want := hostPort(endpoint)
	for i, r := range rules {
		rule, ok := r.(map[string]any)
		if !ok {
			continue
		}
		ruleConfig, _ := rule["config"].(map[string]any)
		api, _ := ruleConfig["api"].(map[string]any)
		if api != nil {
			if base, _ := api["baseUrl"].(string); base != "" && hostPort(base) == want && want != "" {
				return rule, i
			}
		}
		if name, _ := rule["providerName"].(string); name == DefaultProviderName {
			return rule, i
		}
	}
	return nil, -1
}

// hostPort 提取 URL 的 host:port；解析失败返回空串。
func hostPort(rawURL string) string {
	rawURL = strings.TrimSpace(rawURL)
	marker := "://"
	idx := strings.Index(rawURL, marker)
	if idx >= 0 {
		rawURL = rawURL[idx+len(marker):]
	}
	if cut := strings.IndexAny(rawURL, "/?#"); cut >= 0 {
		rawURL = rawURL[:cut]
	}
	return strings.ToLower(rawURL)
}

// newUUID 生成 RFC 4122 v4 UUID（ZCode 用它标识 provider 规则）。
func newUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
