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
	Port             int
	LogLevel         string
	MasterKey        string
	GLMAPIKey        string
	MIMOAPIKey       string
	LongcatAPIKey    string
	EasyClawAPIKey   string
	OpenRouterAPIKey string // OpenRouter 免费模型网关
	DeepVEnabled     bool   // DeepV Server 是否启用
	DeepVWorkDir     string // DeepV 工作目录（用于获取 Git 信息）
	Env              string
}

// Load 从环境变量加载配置
func Load() (*Config, error) {
	_ = godotenv.Load()

	cfg := &Config{
		Port:             getEnvInt("PORT", 4000),
		LogLevel:         getEnv("LOG_LEVEL", "info"),
		MasterKey:        getEnv("LITELLM_MASTER_KEY", ""),
		GLMAPIKey:        getEnv("GLM_API_KEY", ""),
		MIMOAPIKey:       getEnv("MIMO_API_KEY", ""),
		LongcatAPIKey:    getEnv("LONGCAT_API_KEY", ""),
		EasyClawAPIKey:   getEnv("EASYCLAW_API_KEY", ""),
		OpenRouterAPIKey: getEnv("OPENROUTER_API_KEY", ""),
		DeepVEnabled:     getEnvBool("DEEPV_ENABLED", false),
		DeepVWorkDir:     getEnv("DEEPV_WORK_DIR", ""),
		Env:              getEnv("ENV", "development"),
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
