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
		router.RegisterProvider("glm", provider.NewAnthropicProvider(&provider.Config{
			Name:      "glm",
			URL:       "https://open.bigmodel.cn/api/anthropic/v1/messages",
			APIKey:    cfg.GLMAPIKey,
			UseBearer: false,
		}))
	}
	if cfg.MIMOAPIKey != "" {
		router.RegisterProvider("mimo", provider.NewAnthropicProvider(&provider.Config{
			Name:      "mimo",
			URL:       "https://token-plan-cn.xiaomimimo.com/anthropic/v1/messages",
			APIKey:    cfg.MIMOAPIKey,
			UseBearer: false,
		}))
	}
	if cfg.LongcatAPIKey != "" {
		router.RegisterProvider("longcat", provider.NewAnthropicProvider(&provider.Config{
			Name:      "longcat",
			URL:       "https://api.longcat.chat/anthropic/v1/messages",
			APIKey:    cfg.LongcatAPIKey,
			UseBearer: true,
		}))
	}
	if cfg.EasyClawAPIKey != "" {
		router.RegisterProvider("easyclaw", provider.NewOpenAIProvider(&provider.Config{
			Name:   "easyclaw",
			URL:    "https://api.easyclaw.work/v1/chat/completions",
			APIKey: cfg.EasyClawAPIKey,
		}))
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
	if cfg.EasyClawAPIKey != "" {
		router.RegisterChain("easyclaw-sonnet", []string{"easyclaw"})
		router.RegisterChain("easyclaw-opus", []string{"easyclaw"})
		router.RegisterChain("claude-sonnet-4-6", []string{"easyclaw"})
	}

	// 创建 Gin 引擎
	gin.SetMode(gin.ReleaseMode)
	engine := gin.New()
	engine.Use(middleware.Logging(logger))
	engine.Use(auth.BearerAuth(cfg.MasterKey, logger))

	// 注册路由
	msgHandler := handlers.NewMessageHandler(router, logger)
	modelHandler := handlers.NewModelHandler(router, logger)
	healthHandler := handlers.NewHealthHandler(router, logger)

	engine.POST("/v1/messages", msgHandler.Handle)
	engine.GET("/v1/models", modelHandler.Handle)
	engine.GET("/health", healthHandler.Handle)

	addr := fmt.Sprintf(":%d", cfg.Port)
	logger.Printf("Server listening on %s", addr)
	if err := engine.Run(addr); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
