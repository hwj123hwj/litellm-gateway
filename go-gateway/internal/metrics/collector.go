package metrics

import (
	"fmt"
	"sync"
	"time"

	"github.com/weijian/go-llm-gateway/internal/requestmeta"
)

// RequestRecord 单条请求记录
type RequestRecord struct {
	RequestID        string                        `json:"request_id,omitempty"`
	ProviderAttempts []requestmeta.ProviderAttempt `json:"provider_attempts,omitempty"`
	Timestamp        time.Time                     `json:"timestamp"`
	Method           string                        `json:"method"`
	Path             string                        `json:"path"`
	Model            string                        `json:"model"`
	Provider         string                        `json:"provider"`
	StatusCode       int                           `json:"status_code"`
	Latency          float64                       `json:"latency_ms"` // 毫秒
	InputTokens      int                           `json:"input_tokens"`
	OutputTokens     int                           `json:"output_tokens"`
	IsStream         bool                          `json:"is_stream"`
	Error            string                        `json:"error,omitempty"`
}

// ModelStats 模型维度统计
type ModelStats struct {
	Model       string  `json:"model"`
	Requests    int     `json:"requests"`
	Successes   int     `json:"successes"`
	Errors      int     `json:"errors"`
	TotalTokens int     `json:"total_tokens"`
	AvgLatency  float64 `json:"avg_latency_ms"`
	Provider    string  `json:"provider"`
}

// ProviderStats 提供商维度统计
type ProviderStats struct {
	Provider   string    `json:"provider"`
	Requests   int       `json:"requests"`
	Successes  int       `json:"successes"`
	Errors     int       `json:"errors"`
	AvgLatency float64   `json:"avg_latency_ms"`
	Status     string    `json:"status"` // "online", "degraded", "offline"
	LastCheck  time.Time `json:"last_check"`
}

// DashboardSummary 仪表盘概览
type DashboardSummary struct {
	TodayRequests int     `json:"today_requests"`
	SuccessRate   float64 `json:"success_rate"`
	ActiveModels  int     `json:"active_models"`
	AvgLatency    float64 `json:"avg_latency_ms"`
	Uptime        string  `json:"uptime"`
}

// Store 持久化存储接口
type Store interface {
	SaveRecord(r RequestRecord) error
	GetRecentLogs(limit int) ([]RequestRecord, error)
}

// Collector 内存指标收集器
type Collector struct {
	mu         sync.RWMutex
	records    []RequestRecord // 环形缓冲
	maxRecords int
	startTime  time.Time
	todayStart time.Time
	store      Store // 可选的持久化存储

	// 按天重置的计数器
	todayTotal      int
	todaySuccess    int
	todayErrors     int
	todayLatencySum float64

	// 按模型聚合
	modelStats map[string]*ModelStats

	// 按提供商聚合
	providerStats map[string]*ProviderStats

	// 活跃模型集合（今日有请求的）
	activeModels map[string]bool
}

// NewCollector 创建新的指标收集器
func NewCollector() *Collector {
	now := time.Now()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	return &Collector{
		records:       make([]RequestRecord, 0, 2000),
		maxRecords:    2000,
		startTime:     now,
		todayStart:    todayStart,
		modelStats:    make(map[string]*ModelStats),
		providerStats: make(map[string]*ProviderStats),
		activeModels:  make(map[string]bool),
	}
}

// SetStore 设置持久化存储
func (c *Collector) SetStore(store Store) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.store = store
	if store == nil || len(c.records) > 0 {
		return
	}
	// Restore the recent lightweight log window after a restart. Aggregates are
	// intentionally kept in memory; the persisted request log is enough for the
	// Dashboard activity view and avoids replaying records into today's stats.
	persisted, err := store.GetRecentLogs(c.maxRecords)
	if err != nil {
		return
	}
	for i := len(persisted) - 1; i >= 0; i-- {
		c.records = append(c.records, persisted[i])
	}
}

// Record 记录一条请求
func (c *Collector) Record(r RequestRecord) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// 检查是否需要重置每日计数器
	now := r.Timestamp
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	if todayStart.After(c.todayStart) {
		c.resetDaily(todayStart)
	}

	// 追加到环形缓冲
	if len(c.records) >= c.maxRecords {
		// 移除最旧的记录
		copy(c.records, c.records[1:])
		c.records[len(c.records)-1] = r
	} else {
		c.records = append(c.records, r)
	}

	// 持久化到 SQLite
	if c.store != nil {
		go c.store.SaveRecord(r)
	}

	// 更新每日计数
	c.todayTotal++
	if r.StatusCode >= 200 && r.StatusCode < 400 {
		c.todaySuccess++
	} else {
		c.todayErrors++
	}
	c.todayLatencySum += r.Latency

	// 更新模型统计
	if r.Model != "" {
		c.activeModels[r.Model] = true
		ms, ok := c.modelStats[r.Model]
		if !ok {
			ms = &ModelStats{Model: r.Model, Provider: r.Provider}
			c.modelStats[r.Model] = ms
		}
		ms.Requests++
		ms.TotalTokens += r.InputTokens + r.OutputTokens
		if r.StatusCode >= 200 && r.StatusCode < 400 {
			ms.Successes++
		} else {
			ms.Errors++
		}
		// 增量平均延迟
		ms.AvgLatency = (ms.AvgLatency*float64(ms.Requests-1) + r.Latency) / float64(ms.Requests)
	}

	// 更新提供商统计
	if r.Provider != "" {
		ps, ok := c.providerStats[r.Provider]
		if !ok {
			ps = &ProviderStats{Provider: r.Provider, Status: "online"}
			c.providerStats[r.Provider] = ps
		}
		ps.Requests++
		ps.LastCheck = r.Timestamp
		if r.StatusCode >= 200 && r.StatusCode < 400 {
			ps.Successes++
		} else {
			ps.Errors++
			// 连续错误超过阈值则标记为 degraded
			if ps.Errors > 0 && float64(ps.Errors)/float64(ps.Requests) > 0.3 {
				ps.Status = "degraded"
			}
		}
		ps.AvgLatency = (ps.AvgLatency*float64(ps.Requests-1) + r.Latency) / float64(ps.Requests)
	}
}

// resetDaily 重置每日计数器
func (c *Collector) resetDaily(todayStart time.Time) {
	c.todayStart = todayStart
	c.todayTotal = 0
	c.todaySuccess = 0
	c.todayErrors = 0
	c.todayLatencySum = 0
	c.activeModels = make(map[string]bool)
	// 注意：不清除 modelStats 和 providerStats，保留累计数据
}

// GetDashboard 获取仪表盘概览
func (c *Collector) GetDashboard() DashboardSummary {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var successRate float64
	if c.todayTotal > 0 {
		successRate = float64(c.todaySuccess) / float64(c.todayTotal) * 100
	}

	var avgLatency float64
	if c.todayTotal > 0 {
		avgLatency = c.todayLatencySum / float64(c.todayTotal)
	}

	uptime := time.Since(c.startTime)
	uptimeStr := formatDuration(uptime)

	return DashboardSummary{
		TodayRequests: c.todayTotal,
		SuccessRate:   successRate,
		ActiveModels:  len(c.activeModels),
		AvgLatency:    avgLatency,
		Uptime:        uptimeStr,
	}
}

// GetModelStats 获取所有模型统计
func (c *Collector) GetModelStats() []ModelStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	result := make([]ModelStats, 0, len(c.modelStats))
	for _, ms := range c.modelStats {
		result = append(result, *ms)
	}
	return result
}

// GetProviderStats 获取所有提供商统计
func (c *Collector) GetProviderStats() []ProviderStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	result := make([]ProviderStats, 0, len(c.providerStats))
	for _, ps := range c.providerStats {
		result = append(result, *ps)
	}
	return result
}

// GetRecentLogs 获取最近的请求日志
func (c *Collector) GetRecentLogs(limit int) []RequestRecord {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if limit <= 0 || limit > len(c.records) {
		limit = len(c.records)
	}

	// 返回最新的 limit 条
	start := len(c.records) - limit
	if start < 0 {
		start = 0
	}
	result := make([]RequestRecord, limit)
	copy(result, c.records[start:])

	// 反转，最新的在前
	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}
	return result
}

// GetUptime 返回服务运行时长
func (c *Collector) GetUptime() time.Duration {
	return time.Since(c.startTime)
}

// formatDuration 格式化时长为人类可读字符串
func formatDuration(d time.Duration) string {
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	if h > 0 {
		return fmt.Sprintf("%dh%dm", h, m)
	}
	if m > 0 {
		return fmt.Sprintf("%dm", m)
	}
	return fmt.Sprintf("%ds", int(d.Seconds()))
}
