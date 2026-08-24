package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
	"github.com/weijian/go-llm-gateway/internal/archive"
)

// Config 应用全局配置
type Config struct {
	Port                    int
	LogLevel                string
	MasterKey               string
	AdminToken              string // 独立的管理端点 token（可选）
	GLMAPIKey               string
	AliAPIKey               string
	CopilotToken            string // GitHub Copilot token（短期有效，需要定期刷新）
	CopilotGithubToken      string // GitHub OAuth token（用于刷新 Copilot token）
	DeepVEnabled            bool   // DeepV Server（EasyCode/DeepVCode）是否启用
	DeepVWorkDir            string // DeepV 工作目录（用于获取 Git 信息）
	HTTPProxy               string // HTTP 代理地址（如 http://127.0.0.1:7890，用于访问 ChatGPT）
	ChatGPTAuthFile         string // ChatGPT/Pi OAuth auth.json 路径（可选）
	Env                     string
	CircuitFailureThreshold int
	CircuitRecoverySeconds  int
	CircuitSuccessThreshold int
	Archive                 archive.Config
}

// Load 从环境变量加载配置
func Load() (*Config, error) {
	_ = godotenv.Load()

	cfg := &Config{
		Port:                    getEnvInt("PORT", 4001),
		LogLevel:                getEnv("LOG_LEVEL", "info"),
		MasterKey:               getEnv("LITELLM_MASTER_KEY", ""),
		AdminToken:              getEnv("ADMIN_TOKEN", ""),
		GLMAPIKey:               getEnv("GLM_API_KEY", ""),
		AliAPIKey:               getAliAPIKey(),
		CopilotToken:            getEnv("COPILOT_TOKEN", ""),
		CopilotGithubToken:      getEnv("COPILOT_GITHUB_TOKEN", ""),
		DeepVEnabled:            getEnvBool("DEEPV_ENABLED", false),
		DeepVWorkDir:            getEnv("DEEPV_WORK_DIR", ""),
		HTTPProxy:               getEnv("HTTP_PROXY", ""),
		ChatGPTAuthFile:         getEnv("CHATGPT_AUTH_FILE", ""),
		Env:                     getEnv("ENV", "development"),
		CircuitFailureThreshold: getEnvInt("CIRCUIT_FAILURE_THRESHOLD", 3),
		CircuitRecoverySeconds:  getEnvInt("CIRCUIT_RECOVERY_SECONDS", 30),
		CircuitSuccessThreshold: getEnvInt("CIRCUIT_SUCCESS_THRESHOLD", 1),
		Archive:                 loadArchiveConfig(),
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

// getAliAPIKey keeps the documented ALI_API_KEY name authoritative while
// accepting names used by older local gateway installations. The provider
// config loader uses the same fallback so providers.yaml and the default
// registration path behave consistently.
func getAliAPIKey() string {
	for _, key := range []string{
		"ALI_API_KEY",
		"ALIYUN_MAAS_API_KEY",
		"DASHSCOPE_API_KEY",
	} {
		if value := os.Getenv(key); value != "" {
			return value
		}
	}
	return ""
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

// loadArchiveConfig reads the three ARCHIVE_* environment variables. Missing
// or invalid values fall back to archive.DefaultConfig, so the gateway is
// always safe to start even with an incomplete .env.
func loadArchiveConfig() archive.Config {
	cfg := archive.DefaultConfig()
	cfg.Enabled = getEnvBool("ARCHIVE_ENABLED", false)
	cfg.MaxBodyKB = getEnvInt("ARCHIVE_MAX_BODY_KB", cfg.MaxBodyKB)
	cfg.RetentionDays = getEnvInt("ARCHIVE_RETENTION_DAYS", cfg.RetentionDays)
	return cfg
}
