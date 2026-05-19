package main

import (
	"fmt"
	"log"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/weijian/go-llm-gateway/internal/auth"
	"github.com/weijian/go-llm-gateway/internal/config"
	"github.com/weijian/go-llm-gateway/internal/handlers"
	"github.com/weijian/go-llm-gateway/internal/middleware"
	"github.com/weijian/go-llm-gateway/internal/provider"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	logger := log.New(os.Stdout, "[gateway] ", log.LstdFlags)
	logger.Printf("Starting go-llm-gateway on port %d", cfg.Port)

	// 初始化路由器
	router := provider.NewRouter(logger)

	// 尝试从 providers.yaml 加载配置
	configPath := "providers.yaml"
	if _, err := os.Stat(configPath); err == nil {
		logger.Printf("Loading providers from %s", configPath)
		if _, err := provider.SetupProvidersFromConfig(router, configPath, logger); err != nil {
			logger.Printf("Warning: failed to load providers.yaml: %v, using defaults", err)
		}
	} else {
		logger.Printf("providers.yaml not found, using default providers")
		setupDefaultProviders(router, cfg, logger)
	}

	// DeepV Server 提供商（需要特殊认证，始终在代码中处理）
	if cfg.DeepVEnabled {
		setupDeepVProviders(router, cfg, logger)
	}

	// OpenRouter 免费模型（动态拉取）
	if cfg.OpenRouterAPIKey != "" {
		setupOpenRouterProviders(router, cfg, logger)
	}

	// 创建 Gin 引擎
	gin.SetMode(gin.ReleaseMode)
	engine := gin.New()
	engine.Use(middleware.Logging(logger))
	engine.Use(auth.BearerAuth(cfg.MasterKey, logger))

	// 注册路由
	msgHandler := handlers.NewMessageHandler(router, logger)
	chatHandler := handlers.NewChatCompletionsHandler(router, logger)
	modelHandler := handlers.NewModelHandler(router, logger)
	healthHandler := handlers.NewHealthHandler(router, logger)

	engine.POST("/v1/messages", msgHandler.Handle)
	engine.POST("/v1/chat/completions", chatHandler.Handle)
	engine.GET("/v1/models", modelHandler.Handle)
	engine.GET("/health", healthHandler.Handle)
	// 兼容不带 /v1 前缀的客户端
	engine.POST("/messages", msgHandler.Handle)
	engine.POST("/chat/completions", chatHandler.Handle)
	engine.GET("/models", modelHandler.Handle)

	addr := fmt.Sprintf(":%d", cfg.Port)
	logger.Printf("Server listening on %s", addr)
	if err := engine.Run(addr); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}

// setupDefaultProviders 设置默认提供商（当 providers.yaml 不存在时使用）
func setupDefaultProviders(router *provider.Router, cfg *config.Config, logger *log.Logger) {
	if cfg.GLMAPIKey != "" {
		router.RegisterProvider("glm-anthropic", provider.NewAnthropicProvider(&provider.Config{
			Name:      "glm-anthropic",
			URL:       "https://open.bigmodel.cn/api/anthropic/v1/messages",
			APIKey:    cfg.GLMAPIKey,
			UseBearer: false,
		}))
		router.RegisterProvider("glm", provider.NewOpenAIProvider(&provider.Config{
			Name:   "glm",
			URL:    "https://open.bigmodel.cn/api/coding/paas/v4/chat/completions",
			APIKey: cfg.GLMAPIKey,
		}))
	}
	if cfg.MIMOAPIKey != "" {
		router.RegisterProvider("mimo-anthropic", provider.NewAnthropicProvider(&provider.Config{
			Name:      "mimo-anthropic",
			URL:       "https://token-plan-cn.xiaomimimo.com/anthropic/v1/messages",
			APIKey:    cfg.MIMOAPIKey,
			UseBearer: false,
		}))
		router.RegisterProvider("mimo", provider.NewOpenAIProvider(&provider.Config{
			Name:   "mimo",
			URL:    "https://token-plan-cn.xiaomimimo.com/v1/chat/completions",
			APIKey: cfg.MIMOAPIKey,
		}))
	}
	if cfg.LongcatAPIKey != "" {
		router.RegisterProvider("longcat-anthropic", provider.NewAnthropicProvider(&provider.Config{
			Name:      "longcat-anthropic",
			URL:       "https://api.longcat.chat/anthropic/v1/messages",
			APIKey:    cfg.LongcatAPIKey,
			UseBearer: true,
		}))
		router.RegisterProvider("longcat", provider.NewOpenAIProvider(&provider.Config{
			Name:   "longcat",
			URL:    "https://api.longcat.chat/openai/chat/completions",
			APIKey: cfg.LongcatAPIKey,
		}))
	}
	if cfg.EasyClawAPIKey != "" {
		router.RegisterProvider("easyclaw", provider.NewOpenAIProvider(&provider.Config{
			Name:   "easyclaw",
			URL:    "https://api.easyclaw.work/v1/chat/completions",
			APIKey: cfg.EasyClawAPIKey,
		}))
	}
	if cfg.GLMAPIKey != "" {
		router.RegisterProvider("glm-free", provider.NewOpenAIProvider(&provider.Config{
			Name:   "glm-free",
			URL:    "https://open.bigmodel.cn/api/coding/paas/v4/chat/completions",
			APIKey: cfg.GLMAPIKey,
		}))
	}

	// 注册 fallback 链
	router.RegisterChain("coding", []string{"glm", "mimo", "longcat"})
	router.RegisterChain("coding-anthropic", []string{"glm-anthropic", "mimo-anthropic", "longcat-anthropic"})
	if cfg.EasyClawAPIKey != "" {
		router.RegisterChain("easyclaw-sonnet", []string{"easyclaw"})
		router.RegisterChain("easyclaw-opus", []string{"easyclaw"})
	}
	if cfg.GLMAPIKey != "" {
		router.RegisterChain("free", []string{"glm-free"})
	}
}

// setupDeepVProviders 设置 DeepV Server 提供商
func setupDeepVProviders(router *provider.Router, cfg *config.Config, logger *log.Logger) {
	workDir := cfg.DeepVWorkDir
	if workDir == "" {
		workDir, _ = os.Getwd()
	}
	deepvURL := "https://api-code.deepvlab.ai/v1/chat/messages"

	router.RegisterProvider("deepv-deepseek", provider.NewDeepVProvider(&provider.Config{
		Name: "deepv-deepseek",
		URL:  deepvURL,
	}, workDir, "deepseek-v4-flash"))
	router.RegisterProvider("deepv-glm5", provider.NewDeepVProvider(&provider.Config{
		Name: "deepv-glm5",
		URL:  deepvURL,
	}, workDir, "glm-5"))
	router.RegisterProvider("deepv-claude", provider.NewDeepVProvider(&provider.Config{
		Name: "deepv-claude",
		URL:  deepvURL,
	}, workDir, "claude-sonnet-4-6"))

	router.RegisterChain("deepseek-flash", []string{"deepv-deepseek"})
	router.RegisterChain("glm-5", []string{"deepv-glm5"})
	router.RegisterChain("claude-sonnet-4.6", []string{"deepv-claude"})
	router.RegisterChain("claude-sonnet-4-6", []string{"deepv-claude"})

	logger.Printf("DeepV Server enabled, workdir=%s", workDir)
}

// setupOpenRouterProviders 设置 OpenRouter 免费模型
func setupOpenRouterProviders(router *provider.Router, cfg *config.Config, logger *log.Logger) {
	freeModels, err := provider.FetchFreeModels(cfg.OpenRouterAPIKey, 5)
	if err != nil {
		logger.Printf("Warning: failed to fetch OpenRouter free models: %v", err)
		return
	}

	logger.Printf("OpenRouter: loaded %d free models", len(freeModels))
	var openRouterChain []string
	for i, m := range freeModels {
		name := fmt.Sprintf("openrouter-free-%d", i)
		p := provider.NewOpenRouterProvider(name, m.ID, cfg.OpenRouterAPIKey)
		router.RegisterProvider(name, p)
		openRouterChain = append(openRouterChain, name)
		if alias := provider.ModelAlias(m.ID); alias != "" {
			router.RegisterChain(alias, []string{name})
		}
		logger.Printf("  [%d] %s (ctx=%dK)", i, m.ID, m.ContextLength/1000)
	}

	if len(openRouterChain) > 0 {
		router.RegisterChain("free", openRouterChain)
		router.RegisterChain("openrouter-free", openRouterChain)
	}
}
