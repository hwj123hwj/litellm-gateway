// Package dshconfig 把网关的模型清单同步进 deepseek-harness（dsh）的
// profile patch 配置（~/.dsh/profiles/desktop/cordis.patch.yml）。
// 只更新 llm-deepseek / llm-pi-ai 两个条目的模型列表，其余条目与字段原样保留；
// 条目不存在时跳过并在状态里报告（避免给未安装的 bundle 写入未知配置）。
package dshconfig

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// 已知的两个 LLM patch 条目 id（与 dsh 桌面 profile 现状一致）。
const (
	EntryDeepseek = "llm-deepseek"
	EntryPiAI     = "llm-pi-ai"
)

// DefaultHome 返回 dsh 配置根目录（~/.dsh）。
func DefaultHome() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".dsh"
	}
	return filepath.Join(home, ".dsh")
}

// PatchPath 返回 profile patch 文件路径。
func PatchPath(home string) string {
	return filepath.Join(home, "profiles", "desktop", "cordis.patch.yml")
}

// SetupOptions 描述一次同步。
type SetupOptions struct {
	DshHome  string
	Endpoint string // 网关根地址，如 http://127.0.0.1:4001
	ModelIDs []string
	DryRun   bool
}

// Setup 执行同步，返回合并后的 YAML 与文件路径。skipped 为不存在的条目 id。
func Setup(options SetupOptions) ([]byte, string, []string, error) {
	if options.DshHome == "" || options.Endpoint == "" {
		return nil, "", nil, fmt.Errorf("dsh home and endpoint are required")
	}
	if len(options.ModelIDs) == 0 {
		return nil, "", nil, fmt.Errorf("model id list is required")
	}

	path := PatchPath(options.DshHome)
	var document []any
	raw, err := os.ReadFile(path)
	switch {
	case err == nil:
		if err := yaml.Unmarshal(raw, &document); err != nil {
			return nil, path, nil, fmt.Errorf("parse %s: %w", path, err)
		}
	case os.IsNotExist(err):
		document = []any{}
	default:
		return nil, path, nil, fmt.Errorf("read dsh patch config: %w", err)
	}

	models := make([]any, 0, len(options.ModelIDs))
	for _, id := range options.ModelIDs {
		models = append(models, map[string]any{"id": id, "name": id})
	}

	var skipped []string
	updated := map[string]bool{}
	for _, item := range document {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		id, _ := entry["id"].(string)
		config, _ := entry["config"].(map[string]any)
		if config == nil {
			continue
		}
		switch id {
		case EntryDeepseek:
			config["baseURL"] = strings.TrimRight(options.Endpoint, "/") + "/v1/messages"
			config["models"] = models
			updated[id] = true
		case EntryPiAI:
			providers, _ := config["providers"].(map[string]any)
			if providers == nil {
				providers = map[string]any{}
				config["providers"] = providers
			}
			anthropic, _ := providers["anthropic"].(map[string]any)
			if anthropic == nil {
				anthropic = map[string]any{"apiKeyEnv": "ANTHROPIC_API_KEY"}
				providers["anthropic"] = anthropic
			}
			anthropic["baseURL"] = strings.TrimRight(options.Endpoint, "/")
			anthropic["models"] = models
			updated[id] = true
		}
	}
	for _, id := range []string{EntryDeepseek, EntryPiAI} {
		if !updated[id] {
			skipped = append(skipped, id)
		}
	}

	encoded, err := yaml.Marshal(document)
	if err != nil {
		return nil, path, skipped, fmt.Errorf("encode dsh patch config: %w", err)
	}
	if options.DryRun {
		return encoded, path, skipped, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, path, skipped, fmt.Errorf("create dsh config directory: %w", err)
	}
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		return nil, path, skipped, fmt.Errorf("write dsh patch config: %w", err)
	}
	return encoded, path, skipped, nil
}

// CurrentModels 读取两个条目当前的模型 id 列表（供状态展示）。
// 返回 (ids, entryExists, entryMissing, error)。
func CurrentModels(dshHome string) (ids []string, exists map[string]bool, missing []string, err error) {
	exists = map[string]bool{}
	ids = []string{}
	seen := map[string]bool{}

	raw, readErr := os.ReadFile(PatchPath(dshHome))
	if os.IsNotExist(readErr) {
		return ids, exists, []string{EntryDeepseek, EntryPiAI}, nil
	}
	if readErr != nil {
		return nil, nil, nil, readErr
	}
	var document []any
	if err := yaml.Unmarshal(raw, &document); err != nil {
		return nil, nil, nil, err
	}
	for _, item := range document {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		id, _ := entry["id"].(string)
		if id != EntryDeepseek && id != EntryPiAI {
			continue
		}
		exists[id] = true
		config, _ := entry["config"].(map[string]any)
		if config == nil {
			continue
		}
		var models any = config["models"]
		if id == EntryPiAI {
			providers, _ := config["providers"].(map[string]any)
			if anthropic, ok := providers["anthropic"].(map[string]any); ok {
				models = anthropic["models"]
			}
		}
		if list, ok := models.([]any); ok {
			for _, m := range list {
				if model, ok := m.(map[string]any); ok {
					if modelID, _ := model["id"].(string); modelID != "" && !seen[modelID] {
						seen[modelID] = true
						ids = append(ids, modelID)
					}
				}
			}
		}
	}
	for _, id := range []string{EntryDeepseek, EntryPiAI} {
		if !exists[id] {
			missing = append(missing, id)
		}
	}
	return ids, exists, missing, nil
}
