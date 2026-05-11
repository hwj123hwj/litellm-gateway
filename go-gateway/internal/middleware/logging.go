package middleware

import (
	"log"
	"time"

	"github.com/gin-gonic/gin"
)

// Logging 创建请求日志中间件
func Logging(logger *log.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		logger.Printf("%s %s -> %d (%v)",
			c.Request.Method,
			c.Request.URL.Path,
			c.Writer.Status(),
			time.Since(start),
		)
	}
}
