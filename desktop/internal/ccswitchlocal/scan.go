// Package ccswitchlocal 在桌面端所在的机器上读取 cc-switch 配置，供
// 「本机上传迁移」使用：网关在远程主机（如迷你主机）而 cc-switch 装在本机
// 时，把配置读出来上传给网关，解析与转换仍由网关完成。
//
// 只读：新版读 cc-switch.db 的 providers 表（SQLite 只读打开，带 busy
// timeout，不与 cc-switch 应用冲突）；旧版读 config.json 原文并只保留
// apps 部分（prompts/skills 等个人内容不上传）。
package ccswitchlocal

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// ProviderRow 与网关 ccswitchconfig.ProviderRow 的 JSON 形状一致。
type ProviderRow struct {
	ID             string `json:"id"`
	AppType        string `json:"app_type"`
	Name           string `json:"name"`
	SettingsConfig string `json:"settings_config"`
	Category       string `json:"category"`
	Meta           string `json:"meta"`
	IsCurrent      bool   `json:"is_current"`
}

// Payload 是上传给网关的请求体。LegacyJSON 与 Rows 二选一，Label 仅供
// 界面展示、不参与序列化。
type Payload struct {
	LegacyJSON string        `json:"legacy_json,omitempty"`
	Rows       []ProviderRow `json:"rows,omitempty"`
	Label      string        `json:"-"`
}

// DefaultHome 返回本机 cc-switch 目录（CCSWITCH_HOME 可覆盖）。
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

// Scan 读取本机 cc-switch 配置。新版 SQLite 优先，其次旧版 config.json。
func Scan(home string) (*Payload, error) {
	dbPath := filepath.Join(home, "cc-switch.db")
	if info, err := os.Stat(dbPath); err == nil && !info.IsDir() {
		return scanDB(dbPath)
	}
	legacyPath := filepath.Join(home, "config.json")
	if info, err := os.Stat(legacyPath); err == nil && !info.IsDir() {
		return scanLegacy(legacyPath)
	}
	return nil, fmt.Errorf("本机未找到 cc-switch 配置：%s 与 %s 都不存在", dbPath, legacyPath)
}

func scanDB(path string) (*Payload, error) {
	dsn := "file:" + filepath.ToSlash(path) + "?mode=ro&_pragma=busy_timeout%3d5000"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("打开本机 cc-switch 数据库: %w", err)
	}
	defer db.Close()

	rows, err := db.Query(`SELECT id, app_type, name, settings_config, category, is_current, meta
		FROM providers ORDER BY app_type, sort_index`)
	if err != nil {
		return nil, fmt.Errorf("读取本机 providers 表: %w（cc-switch 是否为旧版本？）", err)
	}
	defer rows.Close()

	payload := &Payload{Label: "~/.cc-switch/cc-switch.db（本机上传）"}
	for rows.Next() {
		var row ProviderRow
		var category, meta sql.NullString
		if err := rows.Scan(&row.ID, &row.AppType, &row.Name, &row.SettingsConfig, &category, &row.IsCurrent, &meta); err != nil {
			return nil, fmt.Errorf("读取本机供应商行: %w", err)
		}
		row.Category = category.String
		row.Meta = meta.String
		payload.Rows = append(payload.Rows, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(payload.Rows) == 0 {
		return nil, fmt.Errorf("本机 cc-switch 数据库中没有供应商条目")
	}
	return payload, nil
}

func scanLegacy(path string) (*Payload, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取本机 cc-switch 配置: %w", err)
	}
	// 只保留 apps 部分：迁移只需要它，prompts/skills 等个人内容不上传。
	var document map[string]json.RawMessage
	if err := json.Unmarshal(raw, &document); err == nil {
		if apps, ok := document["apps"]; ok {
			stripped, err := json.Marshal(map[string]json.RawMessage{"apps": apps})
			if err == nil {
				raw = stripped
			}
		}
	}
	return &Payload{LegacyJSON: string(raw), Label: "~/.cc-switch/config.json（本机上传）"}, nil
}
