package middleware

import (
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/weijian/go-llm-gateway/internal/metrics"
	"github.com/weijian/go-llm-gateway/internal/requestmeta"
)

// Logging 创建请求日志中间件，同时记录指标
func Logging(logger *log.Logger, collector *metrics.Collector) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		latency := time.Since(start)
		status := c.Writer.Status()
		requestID := c.GetString(requestmeta.RequestIDKey)

		logger.Printf("request_id=%s %s %s -> %d (%v)",
			requestID,
			c.Request.Method,
			c.Request.URL.Path,
			status,
			latency,
		)

		// 记录指标（排除 admin 和 health 端点）
		path := c.Request.URL.Path
		if collector != nil && !isMetricsExcludedPath(path) {
			model := c.GetString(requestmeta.ModelKey)
			provider := c.GetString(requestmeta.ProviderKey)
			requestError := c.GetString(requestmeta.RequestErrorKey)
			if requestError == "" && status >= http.StatusBadRequest {
				requestError = http.StatusText(status)
			}
			var attempts []requestmeta.ProviderAttempt
			if value, ok := c.Get(requestmeta.ProviderAttemptsKey); ok {
				attempts, _ = value.([]requestmeta.ProviderAttempt)
			}
			collector.Record(metrics.RequestRecord{
				Timestamp:        start,
				RequestID:        requestID,
				Method:           c.Request.Method,
				Path:             path,
				Model:            model,
				Provider:         provider,
				ProviderAttempts: attempts,
				StatusCode:       status,
				Latency:          float64(latency.Milliseconds()),
				IsStream:         c.GetBool(requestmeta.RequestIsStreamKey),
				InputTokens:      c.GetInt(requestmeta.InputTokensKey),
				OutputTokens:     c.GetInt(requestmeta.OutputTokensKey),
				Error:            requestError,
			})
		}
	}
}

// isMetricsExcludedPath keeps operational probes and control-plane traffic
// out of user request metrics.
func isMetricsExcludedPath(path string) bool {
	return (len(path) >= 6 && path[:6] == "/admin") || path == "/health"
}
