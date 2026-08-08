package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/weijian/go-llm-gateway/internal/auth"
	"github.com/weijian/go-llm-gateway/internal/config"
	"github.com/weijian/go-llm-gateway/internal/handlers"
	"github.com/weijian/go-llm-gateway/internal/metrics"
	"github.com/weijian/go-llm-gateway/internal/middleware"
	"github.com/weijian/go-llm-gateway/internal/piconfig"
	"github.com/weijian/go-llm-gateway/internal/provider"
	"github.com/weijian/go-llm-gateway/internal/storage"
)

func main() {
	if len(os.Args) > 1 {
		if err := runCommand(os.Args[1:]); err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}
		return
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	logger := log.New(os.Stdout, "[gateway] ", log.LstdFlags)
	logger.Printf("Starting go-llm-gateway on port %d", cfg.Port)

	// 初始化指标收集器
	collector := metrics.NewCollector()

	// 初始化 SQLite 持久化存储
	dbPath := os.Getenv("PI_GO_DATA_DIR")
	if dbPath == "" {
		dbPath = "./data"
	}
	os.MkdirAll(dbPath, 0755)
	sqlitePath := dbPath + "/metrics.db"
	if store, err := storage.NewSQLiteStore(sqlitePath, logger); err != nil {
		logger.Printf("Warning: SQLite store init failed: %v, using memory only", err)
	} else {
		collector.SetStore(store)
		defer store.Close()
		logger.Printf("SQLite metrics store: %s", sqlitePath)

		// 启动后台清理任务（每天清理 30 天前的数据）
		go startCleanupTask(store, logger)
	}

	// 初始化路由器
	router := provider.NewRouterWithCircuitConfig(logger, provider.CircuitBreakerConfig{
		FailureThreshold: cfg.CircuitFailureThreshold,
		RecoveryTimeout:  time.Duration(cfg.CircuitRecoverySeconds) * time.Second,
		SuccessThreshold: cfg.CircuitSuccessThreshold,
	})

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

	// ChatGPT Codex（使用 OAuth token，需要代理）
	proxyURL := cfg.HTTPProxy
	if proxyURL == "" {
		proxyURL = os.Getenv("HTTPS_PROXY")
	}
	if proxyURL == "" {
		proxyURL = os.Getenv("https_proxy")
	}
	if proxyURL != "" {
		setupChatGPTProvider(router, proxyURL, logger)
	} else {
		logger.Printf("No HTTP_PROXY set, ChatGPT Codex provider disabled")
	}

	// GitHub Copilot
	if cfg.CopilotToken != "" {
		setupCopilotProviders(router, cfg, logger)
	}

	// 创建 Gin 引擎
	gin.SetMode(gin.ReleaseMode)
	engine := gin.New()

	// CORS 配置（允许 Android WebView 跨域请求）
	engine.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"*"},
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization", "Accept", "X-Requested-With", "X-Request-ID"},
		ExposeHeaders:    []string{"Content-Length", "Content-Type", "X-Request-ID", "X-Upstream-Request-ID"},
		AllowCredentials: false,
		MaxAge:           12 * time.Hour,
	}))

	engine.Use(middleware.RequestID())
	engine.Use(middleware.Logging(logger, collector))
	engine.Use(auth.BearerAuthWithAdminToken(cfg.MasterKey, cfg.AdminToken, logger))

	// 管理端点使用独立的 admin auth
	adminAuth := auth.AdminAuth(cfg.MasterKey, cfg.AdminToken, logger)

	// 注册路由
	msgHandler := handlers.NewMessageHandler(router, logger)
	chatHandler := handlers.NewChatCompletionsHandler(router, logger)
	responsesHandler := handlers.NewResponsesHandler(router, logger)
	modelHandler := handlers.NewModelHandler(router, logger)
	healthHandler := handlers.NewHealthHandler(router, logger)
	adminHandler := handlers.NewAdminHandler(router, collector, logger)

	engine.POST("/v1/messages", msgHandler.Handle)
	engine.POST("/v1/chat/completions", chatHandler.Handle)
	engine.POST("/v1/responses", responsesHandler.Handle)
	engine.GET("/v1/models", modelHandler.Handle)
	engine.GET("/health", healthHandler.Handle)
	// 兼容不带 /v1 前缀的客户端
	engine.POST("/messages", msgHandler.Handle)
	engine.POST("/chat/completions", chatHandler.Handle)
	engine.POST("/responses", responsesHandler.Handle)
	engine.GET("/models", modelHandler.Handle)

	// 管理面板 API
	admin := engine.Group("/admin")
	admin.Use(adminAuth)
	{
		admin.GET("/dashboard", adminHandler.HandleDashboard)
		admin.GET("/providers", adminHandler.HandleProviders)
		admin.PATCH("/providers/:name", adminHandler.HandleUpdateProvider)
		admin.POST("/providers/:name/reset", adminHandler.HandleResetProvider)
		admin.POST("/providers/:name/health-check", adminHandler.HandleCheckProvider)
		admin.GET("/models", adminHandler.HandleModels)
		admin.PUT("/models/:model", adminHandler.HandleUpdateModel)
		admin.GET("/routes", adminHandler.HandleRoutes)
		admin.PUT("/routes/:model", adminHandler.HandleUpdateRoute)
		admin.GET("/logs", adminHandler.HandleLogs)
		admin.GET("/health", adminHandler.HandleHealth)
		admin.GET("/config", adminHandler.HandleConfig)
		admin.GET("/stats", adminHandler.HandleStats)
	}

	addr := fmt.Sprintf(":%d", cfg.Port)
	logger.Printf("Server listening on %s", addr)
	if err := engine.Run(addr); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}

func runCommand(args []string) error {
	switch args[0] {
	case "setup":
		if len(args) < 2 || args[1] != "pi" {
			return fmt.Errorf("usage: llm-gateway setup pi [--endpoint URL] [--dry-run]")
		}
		return setupPi(args[2:])
	case "auth":
		if len(args) != 2 || args[1] != "print-master-key" {
			return fmt.Errorf("usage: llm-gateway auth print-master-key")
		}
		key, err := piconfig.MasterKey(defaultGatewayHome())
		if err != nil {
			return err
		}
		_, err = fmt.Fprint(os.Stdout, key)
		return err
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func setupPi(args []string) error {
	flags := flag.NewFlagSet("setup pi", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	endpoint := flags.String("endpoint", "", "Gateway base URL (defaults to the local gateway)")
	dryRun := flags.Bool("dry-run", false, "Print the resulting models.json without writing it")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %v", flags.Args())
	}

	merged, path, err := piconfig.Setup(piconfig.SetupOptions{
		GatewayHome: defaultGatewayHome(),
		PiHome:      defaultPiHome(),
		Endpoint:    *endpoint,
		DryRun:      *dryRun,
	})
	if err != nil {
		return err
	}
	if *dryRun {
		_, err = os.Stdout.Write(merged)
		if err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "Would write: %s\n", path)
		return nil
	}
	fmt.Fprintf(os.Stdout, "Pi configured: %s\nSelect llm-gateway/coding in Pi with /model.\n", path)
	return nil
}

func defaultGatewayHome() string {
	if home := os.Getenv("LLM_GATEWAY_HOME"); home != "" {
		return home
	}
	userHome, err := os.UserHomeDir()
	if err != nil {
		return ".llm-gateway"
	}
	return filepath.Join(userHome, ".llm-gateway")
}

func defaultPiHome() string {
	if home := os.Getenv("PI_HOME"); home != "" {
		return home
	}
	userHome, err := os.UserHomeDir()
	if err != nil {
		return ".pi"
	}
	return filepath.Join(userHome, ".pi")
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
		router.RegisterProvider("glm-free", provider.NewOpenAIProvider(&provider.Config{
			Name:   "glm-free",
			URL:    "https://open.bigmodel.cn/api/coding/paas/v4/chat/completions",
			APIKey: cfg.GLMAPIKey,
		}))
	}
	if cfg.AliAPIKey != "" {
		router.RegisterProvider("ali-anthropic", provider.NewAnthropicProvider(&provider.Config{
			Name:      "ali-anthropic",
			URL:       "https://token-plan.cn-beijing.maas.aliyuncs.com/apps/anthropic/v1/messages",
			APIKey:    cfg.AliAPIKey,
			UseBearer: true,
		}))
		router.RegisterProvider("ali", provider.NewOpenAIProvider(&provider.Config{
			Name:   "ali",
			URL:    "https://token-plan.cn-beijing.maas.aliyuncs.com/compatible-mode/v1/chat/completions",
			APIKey: cfg.AliAPIKey,
		}))
	}
	// 注册 fallback 链（与 providers.yaml 别名规则一致）
	codingProviders := []string{}
	if cfg.GLMAPIKey != "" {
		codingProviders = append(codingProviders, "glm")
	}
	if cfg.AliAPIKey != "" {
		codingProviders = append(codingProviders, "ali")
	}
	if len(codingProviders) == 0 {
		codingProviders = []string{"glm"}
	}
	router.RegisterChain("coding", codingProviders)

	codingAnthropicProviders := []string{}
	if cfg.GLMAPIKey != "" {
		codingAnthropicProviders = append(codingAnthropicProviders, "glm-anthropic")
	}
	if cfg.AliAPIKey != "" {
		codingAnthropicProviders = append(codingAnthropicProviders, "ali-anthropic")
	}
	if len(codingAnthropicProviders) == 0 {
		codingAnthropicProviders = []string{"glm-anthropic"}
	}
	router.RegisterChain("coding-anthropic", codingAnthropicProviders)

	// GLM 核心别名
	if cfg.GLMAPIKey != "" {
		router.RegisterChain("glm-sonnet", []string{"glm"})
		router.RegisterChain("glm-haiku", []string{"glm"})
		router.RegisterChain("glm-opus", []string{"glm"})
		router.RegisterChain("glm-4.7-flash", []string{"glm-free"})
		// Keep the historical alias for existing local clients when providers.yaml
		// is absent; new configuration should use the explicit model ID above.
		router.RegisterChain("glm-flash", []string{"glm-free"})
	}
	// Ali 核心别名
	if cfg.AliAPIKey != "" {
		router.RegisterChain("ali-opus", []string{"ali"})
	}
}

// setupCopilotProviders 设置 GitHub Copilot 提供商
func setupCopilotProviders(router *provider.Router, cfg *config.Config, logger *log.Logger) {
	// 可选：刷新 token
	if cfg.CopilotGithubToken != "" {
		newToken, expiresAt, err := provider.RefreshCopilotToken(cfg.CopilotGithubToken)
		if err != nil {
			logger.Printf("Warning: failed to refresh Copilot token: %v, using existing token", err)
		} else {
			cfg.CopilotToken = newToken
			logger.Printf("Copilot token refreshed, expires at %d", expiresAt)
		}
	}

	copilotConfig := &provider.Config{
		Name:   "copilot",
		URL:    "", // 会从 token 解析
		APIKey: cfg.CopilotToken,
	}

	router.RegisterProvider("copilot", provider.NewCopilotProvider(copilotConfig, cfg.CopilotGithubToken))
	router.RegisterChain("copilot", []string{"copilot"})
	router.RegisterChain("copilot-auto", []string{"copilot"})
	router.RegisterChain("auto", []string{"copilot"})
	router.RegisterChain("copilot-opus", []string{"copilot"})
	router.RegisterChain("copilot-sonnet", []string{"copilot"})
	router.RegisterChain("copilot-haiku", []string{"copilot"})

	logger.Printf("GitHub Copilot enabled")
}

// setupChatGPTProvider 设置 ChatGPT Codex 提供商（使用 OAuth token 走代理）
func setupChatGPTProvider(router *provider.Router, proxyURL string, logger *log.Logger) {
	chatgptProvider := provider.NewChatGPTProvider(proxyURL)
	router.RegisterProvider("chatgpt", chatgptProvider)

	// GPT 模型别名
	router.RegisterChain("gpt-5.5", []string{"chatgpt"})
	router.RegisterChain("gpt-5.5-pro", []string{"chatgpt"})
	router.RegisterChain("gpt-5.4-mini", []string{"chatgpt"})
	router.RegisterChain("gpt-5.4", []string{"chatgpt"})
	router.RegisterChain("gpt-5", []string{"chatgpt"})
	router.RegisterChain("o4-mini", []string{"chatgpt"})

	logger.Printf("ChatGPT Codex provider enabled (proxy: %s)", proxyURL)
}

// startCleanupTask 启动后台清理任务
func startCleanupTask(store *storage.SQLiteStore, logger *log.Logger) {
	// 首次启动时清理
	if err := store.Cleanup(30); err != nil {
		logger.Printf("Cleanup error: %v", err)
	}

	// 每天凌晨 3 点清理
	for {
		now := time.Now()
		tomorrow := time.Date(now.Year(), now.Month(), now.Day()+1, 3, 0, 0, 0, now.Location())
		duration := tomorrow.Sub(now)
		logger.Printf("Next cleanup in %v", duration)
		time.Sleep(duration)

		if err := store.Cleanup(30); err != nil {
			logger.Printf("Cleanup error: %v", err)
		}
	}
}
