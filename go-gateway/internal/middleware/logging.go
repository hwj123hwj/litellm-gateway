package middleware

import (
	"log"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/weijian/go-llm-gateway/internal/metrics"
)

// Logging 创建请求日志中间件，同时记录指标
func Logging(logger *log.Logger, collector *metrics.Collector) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		latency := time.Since(start)

		logger.Printf("%s %s -> %d (%v)",
			c.Request.Method,
			c.Request.URL.Path,
			c.Writer.Status(),
			latency,
		)

		// 记录指标（排除 admin 和 health 端点）
		path := c.Request.URL.Path
		if collector != nil && !isAdminPath(path) {
			model := c.GetString("request_model")
			provider := c.GetString("request_provider")
			collector.Record(metrics.RequestRecord{
				Timestamp:  start,
				Method:     c.Request.Method,
				Path:       path,
				Model:      model,
				Provider:   provider,
				StatusCode: c.Writer.Status(),
				Latency:    float64(latency.Milliseconds()),
				IsStream:   c.GetBool("request_is_stream"),
				InputTokens:  c.GetInt("request_input_tokens"),
				OutputTokens: c.GetInt("request_output_tokens"),
			})
		}
	}
}

// isAdminPath 判断是否为管理端点路径
func isAdminPath(path string) bool {
	return len(path) >= 6 && path[:6] == "/admin"
}
