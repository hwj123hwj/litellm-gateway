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

	// 注册提供商
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
		// GLM coding plan（OpenAI 兼容接口，复用智谱 key）
		router.RegisterProvider("glm-free", provider.NewOpenAIProvider(&provider.Config{
			Name:   "glm-free",
			URL:    "https://open.bigmodel.cn/api/coding/paas/v4/chat/completions",
			APIKey: cfg.GLMAPIKey,
		}))
	}

	// OpenRouter 免费模型（启动时动态拉取，fallback 链自动构建）
	var openRouterChain []string
	if cfg.OpenRouterAPIKey != "" {
		freeModels, err := provider.FetchFreeModels(cfg.OpenRouterAPIKey, 5)
		if err != nil {
			logger.Printf("Warning: failed to fetch OpenRouter free models: %v", err)
		} else {
			logger.Printf("OpenRouter: loaded %d free models", len(freeModels))
			for i, m := range freeModels {
				name := fmt.Sprintf("openrouter-free-%d", i)
				p := provider.NewOpenRouterProvider(name, m.ID, cfg.OpenRouterAPIKey)
				router.RegisterProvider(name, p)
				openRouterChain = append(openRouterChain, name)
				// 为每个模型注册简称 chain，如 "nemotron" "qwen3-coder"
				if alias := provider.ModelAlias(m.ID); alias != "" {
					router.RegisterChain(alias, []string{name})
				}
				logger.Printf("  [%d] %s (ctx=%dK)", i, m.ID, m.ContextLength/1000)
			}
		}
	}

	// 注册 fallback 链（对齐 config.yaml 模型别名）
	router.RegisterChain("coding", []string{"glm", "mimo", "longcat"})
	router.RegisterChain("glm-haiku", []string{"glm"})
	router.RegisterChain("glm-sonnet", []string{"glm"})
	router.RegisterChain("glm-opus", []string{"glm"})
	router.RegisterChain("mimo-haiku", []string{"mimo"})
	router.RegisterChain("mimo-sonnet", []string{"mimo"})
	router.RegisterChain("mimo-opus", []string{"mimo"})
	router.RegisterChain("longcat-sonnet", []string{"longcat"})
	router.RegisterChain("longcat-opus", []string{"longcat"})
	router.RegisterChain("coding-anthropic", []string{"glm-anthropic", "mimo-anthropic", "longcat-anthropic"})
	router.RegisterChain("glm-haiku-anthropic", []string{"glm-anthropic"})
	router.RegisterChain("glm-sonnet-anthropic", []string{"glm-anthropic"})
	router.RegisterChain("glm-opus-anthropic", []string{"glm-anthropic"})
	router.RegisterChain("mimo-haiku-anthropic", []string{"mimo-anthropic"})
	router.RegisterChain("mimo-sonnet-anthropic", []string{"mimo-anthropic"})
	router.RegisterChain("mimo-opus-anthropic", []string{"mimo-anthropic"})
	router.RegisterChain("longcat-sonnet-anthropic", []string{"longcat-anthropic"})
	router.RegisterChain("longcat-opus-anthropic", []string{"longcat-anthropic"})
	if cfg.EasyClawAPIKey != "" {
		router.RegisterChain("easyclaw-sonnet", []string{"easyclaw"})
		router.RegisterChain("easyclaw-opus", []string{"easyclaw"})
		router.RegisterChain("claude-sonnet-4-6", []string{"easyclaw"})
	}
	if cfg.GLMAPIKey != "" {
		// 免费/极低成本模型入口
		router.RegisterChain("free", []string{"glm-free"})
		router.RegisterChain("glm-flash", []string{"glm-free"})
	}
	if len(openRouterChain) > 0 {
		// OpenRouter 免费模型链，覆盖 glm-free 成为 free 的主链
		router.RegisterChain("free", openRouterChain)
		router.RegisterChain("openrouter-free", openRouterChain)
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
