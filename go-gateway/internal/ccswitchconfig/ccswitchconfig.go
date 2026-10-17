// Package ccswitchconfig 把 cc-switch（Claude Code / Codex 等客户端的供应商
// 切换器）里已经配好的上游供应商迁移进网关：读取 ~/.cc-switch 下的新版
// SQLite 数据库（cc-switch.db，v4+）或旧版 config.json（v3 及更早），提取
// 端点、API Key 与模型名提示，转换为 providers.yaml 条目和 .env 密钥变量。
//
// 迁移是单向只读的：不修改 cc-switch 的任何数据。API Key 只写入网关 .env，
// 不回显到 Admin API 响应、日志或测试数据。
package ccswitchconfig

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	toml "github.com/pelletier/go-toml/v2"
	_ "modernc.org/sqlite"
)

// Entry 是 cc-switch 里一条供应商记录，已按 app 类型解析出公共字段。
// 字段语义与 cc-switch v4.0.x 源码保持一致：
//   - claude：settings_config.env 里的 ANTHROPIC_BASE_URL / ANTHROPIC_AUTH_TOKEN
//     （或 ANTHROPIC_API_KEY），meta.apiFormat 区分上游协议
//     （anthropic 默认 / openai_chat / openai_responses）；
//   - codex：auth.OPENAI_API_KEY + config TOML 的 model_providers.<active>.base_url
//     与 wire_api，auth_mode=chatgpt 表示 OAuth 订阅凭据；
//   - openclaw：baseUrl / apiKey / api（openai-completions 或 anthropic-messages）
//     与自带的 models 列表。
type Entry struct {
	AppType   string
	ID        string
	Name      string
	Category  string
	IsCurrent bool

	BaseURL string
	APIKey  string // 仅用于写入网关 .env，不序列化、不打日志

	APIFormat          string // claude/openclaw 条目的上游协议标注
	WireAPI            string // codex 条目：chat / responses
	AuthMode           string // codex 条目：chatgpt 表示 OAuth 订阅
	RequiresOpenAIAuth bool

	// ParseError 非空表示该条目的 settings_config 损坏，无法参与转换。
	ParseError string

	ModelHints []string
}

// ScanResult 是一次扫描的结果。
type ScanResult struct {
	Source  string // 实际读取的文件路径
	Entries []Entry
}

// DefaultHome 返回 cc-switch 配置根目录（CCSWITCH_HOME 可覆盖）。
func DefaultHome() string {
	if home := os.Getenv("CCSWITCH_HOME"); home != "" {
		return home
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".cc-switch"
	}
	return filepath.Join(home, ".cc-switch")
}

// DBPath 返回新版（v4+）SQLite 数据库路径。
func DBPath(home string) string { return filepath.Join(home, "cc-switch.db") }

// LegacyConfigPath 返回旧版（v3 及更早）config.json 路径。
func LegacyConfigPath(home string) string { return filepath.Join(home, "config.json") }

// Scan 读取 cc-switch 配置。优先新版 SQLite 数据库，其次旧版 config.json；
// 两者都不存在时返回错误。
func Scan(home string) (*ScanResult, error) {
	dbPath := DBPath(home)
	if fileExists(dbPath) {
		return scanDB(dbPath)
	}
	legacy := LegacyConfigPath(home)
	if fileExists(legacy) {
		return scanLegacy(legacy)
	}
	return nil, fmt.Errorf("未找到 cc-switch 配置：%s 与 %s 都不存在", dbPath, legacy)
}

// scanDB 以只读方式读取 cc-switch.db 的 providers 表。cc-switch 应用运行时
// 数据库处于 WAL 模式，busy_timeout 保证与它并发读不报 SQLITE_BUSY。
func scanDB(path string) (*ScanResult, error) {
	dsn := "file:" + filepath.ToSlash(path) + "?mode=ro&_pragma=busy_timeout%3d5000"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("打开 cc-switch 数据库: %w", err)
	}
	defer db.Close()

	rows, err := db.Query(`SELECT id, app_type, name, settings_config, category, is_current, meta
		FROM providers ORDER BY app_type, sort_index`)
	if err != nil {
		return nil, fmt.Errorf("读取 providers 表: %w", err)
	}
	defer rows.Close()

	scan := &ScanResult{Source: path}
	for rows.Next() {
		var id, appType, name, settingsJSON string
		var category, metaJSON sql.NullString
		var isCurrent bool
		if err := rows.Scan(&id, &appType, &name, &settingsJSON, &category, &isCurrent, &metaJSON); err != nil {
			return nil, fmt.Errorf("读取供应商行: %w", err)
		}
		entry, err := parseEntry(appType, id, name, settingsJSON, metaJSON.String, category.String, isCurrent)
		if err != nil {
			// 单条损坏不阻断整体迁移，保留名字与原因信息便于排查。
			entry = Entry{AppType: appType, ID: id, Name: name, Category: category.String, IsCurrent: isCurrent}
			entry.ParseError = err.Error()
		}
		scan.Entries = append(scan.Entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍历 providers 表: %w", err)
	}
	return scan, nil
}

// legacyDocument 对应旧版 config.json（MultiAppConfig）里需要的部分：
// apps.<app_type>.{current, providers{}}，provider 字段为 camelCase。
type legacyDocument struct {
	Apps map[string]struct {
		Current   string `json:"current"`
		Providers map[string]struct {
			ID             string          `json:"id"`
			Name           string          `json:"name"`
			SettingsConfig json.RawMessage `json:"settingsConfig"`
			Category       string          `json:"category"`
			Meta           json.RawMessage `json:"meta"`
		}
	} `json:"apps"`
}

// scanLegacy 读取旧版 config.json。provider 的 settingsConfig 字段是
// camelCase（serde rename），其内容与新版 settings_config 列相同。
func scanLegacy(path string) (*ScanResult, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取 cc-switch 配置: %w", err)
	}
	entries, err := ParseLegacyJSON(raw)
	if err != nil {
		return nil, fmt.Errorf("解析 %s: %w", path, err)
	}
	return &ScanResult{Source: path, Entries: entries}, nil
}

// ParseLegacyJSON 从旧版 config.json 原文解析条目（桌面端上传路径共用）。
func ParseLegacyJSON(raw []byte) ([]Entry, error) {
	var doc legacyDocument
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("config.json 不是合法 JSON: %w", err)
	}

	entries := []Entry{}
	// map 迭代无序，先排序 app 类型保证结果稳定。
	appTypes := make([]string, 0, len(doc.Apps))
	for appType := range doc.Apps {
		appTypes = append(appTypes, appType)
	}
	sortStrings(appTypes)
	for _, appType := range appTypes {
		manager := doc.Apps[appType]
		ids := make([]string, 0, len(manager.Providers))
		for id := range manager.Providers {
			ids = append(ids, id)
		}
		sortStrings(ids)
		for _, id := range ids {
			p := manager.Providers[id]
			if p.ID != "" {
				id = p.ID
			}
			entry, err := parseEntry(appType, id, p.Name, string(p.SettingsConfig), string(p.Meta), p.Category, id == manager.Current)
			if err != nil {
				entry = Entry{AppType: appType, ID: id, Name: p.Name, Category: p.Category}
				entry.ParseError = err.Error()
			}
			entries = append(entries, entry)
		}
	}
	return entries, nil
}

// ProviderRow 是 cc-switch providers 表的一行。桌面端在装有 cc-switch 的
// 机器上读出这些行并上传给网关（远程迁移），网关按本机扫描同一套逻辑解析。
type ProviderRow struct {
	ID             string `json:"id"`
	AppType        string `json:"app_type"`
	Name           string `json:"name"`
	SettingsConfig string `json:"settings_config"`
	Category       string `json:"category"`
	Meta           string `json:"meta"`
	IsCurrent      bool   `json:"is_current"`
}

// ParseProviderRows 把上传的 providers 行解析为条目。行序由调用方保证
// （桌面端按 app_type、sort_index 排序后上传）。
func ParseProviderRows(rows []ProviderRow) []Entry {
	entries := make([]Entry, 0, len(rows))
	for _, row := range rows {
		entry, err := parseEntry(row.AppType, row.ID, row.Name, row.SettingsConfig, row.Meta, row.Category, row.IsCurrent)
		if err != nil {
			entry = Entry{AppType: row.AppType, ID: row.ID, Name: row.Name, Category: row.Category, IsCurrent: row.IsCurrent}
			entry.ParseError = err.Error()
		}
		entries = append(entries, entry)
	}
	return entries
}

// parseEntry 把 settings_config（各 app 类型形态不同）与 meta 解析为 Entry。
func parseEntry(appType, id, name, settingsJSON, metaJSON, category string, isCurrent bool) (Entry, error) {
	entry := Entry{AppType: appType, ID: id, Name: name, Category: category, IsCurrent: isCurrent}

	var settings map[string]any
	if settingsJSON != "" {
		if err := json.Unmarshal([]byte(settingsJSON), &settings); err != nil {
			return entry, fmt.Errorf("settings_config 不是合法 JSON: %w", err)
		}
	}
	var meta map[string]any
	if metaJSON != "" {
		_ = json.Unmarshal([]byte(metaJSON), &meta) // meta 损坏不致命，按缺省处理
	}

	switch appType {
	case "claude":
		env, _ := settings["env"].(map[string]any)
		entry.BaseURL = stringValue(env["ANTHROPIC_BASE_URL"])
		entry.APIKey = firstNonEmpty(stringValue(env["ANTHROPIC_AUTH_TOKEN"]), stringValue(env["ANTHROPIC_API_KEY"]))
		for _, key := range []string{
			"ANTHROPIC_MODEL",
			"ANTHROPIC_DEFAULT_SONNET_MODEL",
			"ANTHROPIC_DEFAULT_OPUS_MODEL",
			"ANTHROPIC_DEFAULT_HAIKU_MODEL",
		} {
			if v := stringValue(env[key]); v != "" {
				entry.ModelHints = appendUnique(entry.ModelHints, v)
			}
		}
		entry.APIFormat = stringValue(meta["apiFormat"])
	case "codex":
		auth, _ := settings["auth"].(map[string]any)
		entry.AuthMode = stringValue(auth["auth_mode"])
		entry.APIKey = stringValue(auth["OPENAI_API_KEY"])
		entry.parseCodexTOML(stringValue(settings["config"]))
	case "openclaw":
		entry.BaseURL = stringValue(settings["baseUrl"])
		entry.APIKey = stringValue(settings["apiKey"])
		entry.APIFormat = stringValue(settings["api"])
		if models, ok := settings["models"].([]any); ok {
			for _, m := range models {
				if model, ok := m.(map[string]any); ok {
					if id := stringValue(model["id"]); id != "" {
						entry.ModelHints = appendUnique(entry.ModelHints, id)
					}
				}
			}
		}
	case "gemini":
		env, _ := settings["env"].(map[string]any)
		entry.BaseURL = stringValue(env["GOOGLE_GEMINI_BASE_URL"])
		entry.APIKey = firstNonEmpty(stringValue(env["GEMINI_API_KEY"]), stringValue(env["GOOGLE_API_KEY"]))
	}
	return entry, nil
}

// parseCodexTOML 解析 codex 条目的 config.toml 字符串：base_url 取
// model_providers.<model_provider>.base_url（兜底顶层 base_url），同时读出
// wire_api、requires_openai_auth 与顶层 model 模型提示。语义与 cc-switch
// 的 extract_codex_base_url 一致。
func (e *Entry) parseCodexTOML(configText string) {
	if strings.TrimSpace(configText) == "" {
		return
	}
	var doc map[string]any
	if err := toml.Unmarshal([]byte(configText), &doc); err != nil {
		// 解析失败时保留 auth 里已取到的信息，base_url 留空由后续判断跳过。
		return
	}
	if active := stringValue(doc["model_provider"]); active != "" {
		if providers, ok := doc["model_providers"].(map[string]any); ok {
			if table, ok := providers[active].(map[string]any); ok {
				e.BaseURL = stringValue(table["base_url"])
				e.WireAPI = stringValue(table["wire_api"])
				if b, ok := table["requires_openai_auth"].(bool); ok {
					e.RequiresOpenAIAuth = b
				}
			}
		}
	}
	if e.BaseURL == "" {
		e.BaseURL = stringValue(doc["base_url"])
	}
	if model := stringValue(doc["model"]); model != "" {
		e.ModelHints = appendUnique(e.ModelHints, model)
	}
}

func stringValue(v any) string {
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func appendUnique(list []string, value string) []string {
	for _, existing := range list {
		if existing == value {
			return list
		}
	}
	return append(list, value)
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func sortStrings(list []string) {
	for i := 1; i < len(list); i++ {
		for j := i; j > 0 && list[j] < list[j-1]; j-- {
			list[j], list[j-1] = list[j-1], list[j]
		}
	}
}
