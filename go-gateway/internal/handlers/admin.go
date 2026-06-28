package handlers

import (
	"log"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/weijian/go-llm-gateway/internal/metrics"
	"github.com/weijian/go-llm-gateway/internal/provider"
)

// AdminHandler 处理管理面板 API
type AdminHandler struct {
	router    *provider.Router
	collector *metrics.Collector
	logger    *log.Logger
}

// NewAdminHandler 创建管理面板 handler
func NewAdminHandler(router *provider.Router, collector *metrics.Collector, logger *log.Logger) *AdminHandler {
	return &AdminHandler{
		router:    router,
		collector: collector,
		logger:    logger,
	}
}

// HandleDashboard GET /admin/dashboard
// 返回仪表盘 KPI 概览
func (h *AdminHandler) HandleDashboard(c *gin.Context) {
	summary := h.collector.GetDashboard()

	// 补充提供商状态信息
	providerStats := h.collector.GetProviderStats()
	providers := make([]gin.H, 0, len(providerStats))
	for _, ps := range providerStats {
		providers = append(providers, gin.H{
			"name":        ps.Provider,
			"status":      ps.Status,
			"requests":    ps.Requests,
			"avg_latency": ps.AvgLatency,
		})
	}

	// 补充模型排行
	modelStats := h.collector.GetModelStats()
	models := make([]gin.H, 0, len(modelStats))
	for _, ms := range modelStats {
		models = append(models, gin.H{
			"model":       ms.Model,
			"provider":    ms.Provider,
			"requests":    ms.Requests,
			"total_tokens": ms.TotalTokens,
			"avg_latency": ms.AvgLatency,
			"successes":   ms.Successes,
			"errors":      ms.Errors,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"summary": gin.H{
			"today_requests": summary.TodayRequests,
			"success_rate":   summary.SuccessRate,
			"active_models":  summary.ActiveModels,
			"avg_latency_ms": summary.AvgLatency,
			"uptime":         summary.Uptime,
		},
		"providers": providers,
		"models":    models,
	})
}

// HandleProviders GET /admin/providers
// 返回所有提供商状态
func (h *AdminHandler) HandleProviders(c *gin.Context) {
	providerStats := h.collector.GetProviderStats()
	registeredProviders := h.router.ListProviders()

	// 合并已注册但还没有请求记录的提供商
	seen := make(map[string]bool)
	for _, ps := range providerStats {
		seen[ps.Provider] = true
	}

	result := make([]gin.H, 0, len(registeredProviders))
	for _, name := range registeredProviders {
		if seen[name] {
			continue
		}
		result = append(result, gin.H{
			"name":        name,
			"status":      "unknown",
			"requests":    0,
			"avg_latency": 0,
		})
	}
	for _, ps := range providerStats {
		result = append(result, gin.H{
			"name":        ps.Provider,
			"status":      ps.Status,
			"requests":    ps.Requests,
			"successes":   ps.Successes,
			"errors":      ps.Errors,
			"avg_latency": ps.AvgLatency,
			"last_check":  ps.LastCheck,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"providers": result,
		"total":     len(result),
	})
}

// HandleModels GET /admin/models
// 返回所有模型统计
func (h *AdminHandler) HandleModels(c *gin.Context) {
	modelStats := h.collector.GetModelStats()
	chains := h.router.ListChains()

	// 合并已注册但还没有请求记录的模型
	seen := make(map[string]bool)
	for _, ms := range modelStats {
		seen[ms.Model] = true
	}

	result := make([]gin.H, 0, len(chains))
	for _, name := range chains {
		if seen[name] {
			continue
		}
		result = append(result, gin.H{
			"model":       name,
			"status":      "idle",
			"requests":    0,
			"total_tokens": 0,
			"avg_latency": 0,
		})
	}
	for _, ms := range modelStats {
		status := "online"
		if ms.Errors > 0 && float64(ms.Errors)/float64(ms.Requests) > 0.3 {
			status = "degraded"
		}
		result = append(result, gin.H{
			"model":        ms.Model,
			"provider":     ms.Provider,
			"status":       status,
			"requests":     ms.Requests,
			"total_tokens": ms.TotalTokens,
			"avg_latency":  ms.AvgLatency,
			"successes":    ms.Successes,
			"errors":       ms.Errors,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"models": result,
		"total":  len(result),
	})
}

// HandleLogs GET /admin/logs
// 返回最近的请求日志
func (h *AdminHandler) HandleLogs(c *gin.Context) {
	limit := 50
	if v := c.Query("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}

	logs := h.collector.GetRecentLogs(limit)
	result := make([]gin.H, 0, len(logs))
	for _, r := range logs {
		entry := gin.H{
			"timestamp":    r.Timestamp,
			"method":       r.Method,
			"path":         r.Path,
			"model":        r.Model,
			"provider":     r.Provider,
			"status_code":  r.StatusCode,
			"latency_ms":   r.Latency,
			"input_tokens": r.InputTokens,
			"output_tokens": r.OutputTokens,
			"is_stream":    r.IsStream,
		}
		if r.Error != "" {
			entry["error"] = r.Error
		}
		result = append(result, entry)
	}

	c.JSON(http.StatusOK, gin.H{
		"logs":  result,
		"total": len(result),
	})
}

// HandleHealth GET /admin/health
// 返回详细的系统健康状态
func (h *AdminHandler) HandleHealth(c *gin.Context) {
	dashboard := h.collector.GetDashboard()
	providerStats := h.collector.GetProviderStats()

	providerStatuses := make([]gin.H, 0, len(providerStats))
	onlineCount := 0
	for _, ps := range providerStats {
		if ps.Status == "online" {
			onlineCount++
		}
		providerStatuses = append(providerStatuses, gin.H{
			"provider": ps.Provider,
			"status":   ps.Status,
		})
	}

	overallStatus := "ok"
	if dashboard.SuccessRate < 95 && dashboard.TodayRequests > 10 {
		overallStatus = "degraded"
	}

	c.JSON(http.StatusOK, gin.H{
		"status":           overallStatus,
		"uptime":           dashboard.Uptime,
		"today_requests":   dashboard.TodayRequests,
		"success_rate":     dashboard.SuccessRate,
		"active_models":    dashboard.ActiveModels,
		"avg_latency_ms":   dashboard.AvgLatency,
		"providers_online": onlineCount,
		"providers_total":  len(providerStats),
		"providers":        providerStatuses,
	})
}

// HandleConfig GET /admin/config
// 返回当前配置信息
func (h *AdminHandler) HandleConfig(c *gin.Context) {
	providers := h.router.ListProviders()
	chains := h.router.ListChains()

	c.JSON(http.StatusOK, gin.H{
		"providers": providers,
		"chains":    chains,
		"total_providers": len(providers),
		"total_chains":    len(chains),
	})
}

// HandleStats GET /admin/stats
// 返回详细统计信息
func (h *AdminHandler) HandleStats(c *gin.Context) {
	dashboard := h.collector.GetDashboard()
	modelStats := h.collector.GetModelStats()
	providerStats := h.collector.GetProviderStats()

	// 计算总 token 数
	totalTokens := 0
	for _, ms := range modelStats {
		totalTokens += ms.TotalTokens
	}

	c.JSON(http.StatusOK, gin.H{
		"summary": gin.H{
			"today_requests": dashboard.TodayRequests,
			"success_rate":   dashboard.SuccessRate,
			"active_models":  dashboard.ActiveModels,
			"avg_latency_ms": dashboard.AvgLatency,
			"uptime":         dashboard.Uptime,
			"total_tokens":   totalTokens,
		},
		"models":    modelStats,
		"providers": providerStats,
	})
}
