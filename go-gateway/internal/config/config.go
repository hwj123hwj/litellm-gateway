package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

// Config 应用全局配置
type Config struct {
	Port               int
	LogLevel           string
	MasterKey          string
	AdminToken         string // 独立的管理端点 token（可选）
	GLMAPIKey          string
	DashscopeAPIKey    string
	CopilotToken       string // GitHub Copilot token（短期有效，需要定期刷新）
	CopilotGithubToken string // GitHub OAuth token（用于刷新 Copilot token）
	HTTPProxy          string // HTTP 代理地址（如 http://127.0.0.1:7890，用于访问 ChatGPT）
	Env                string
}

// Load 从环境变量加载配置
func Load() (*Config, error) {
	_ = godotenv.Load()

	cfg := &Config{
		Port:               getEnvInt("PORT", 4000),
		LogLevel:           getEnv("LOG_LEVEL", "info"),
		MasterKey:          getEnv("LITELLM_MASTER_KEY", ""),
		AdminToken:         getEnv("ADMIN_TOKEN", ""),
		GLMAPIKey:          getEnv("GLM_API_KEY", ""),
		DashscopeAPIKey:    getEnv("DASHSCOPE_API_KEY", ""),
		CopilotToken:       getEnv("COPILOT_TOKEN", ""),
		CopilotGithubToken: getEnv("COPILOT_GITHUB_TOKEN", ""),
		HTTPProxy:          getEnv("HTTP_PROXY", ""),
		Env:                getEnv("ENV", "development"),
	}

	if cfg.MasterKey == "" {
		return nil, fmt.Errorf("LITELLM_MASTER_KEY is required")
	}

	return cfg, nil
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	intVal, err := strconv.Atoi(value)
	if err != nil {
		return defaultValue
	}
	return intVal
}

func getEnvBool(key string, defaultValue bool) bool {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return strings.ToLower(value) == "true" || value == "1"
}
