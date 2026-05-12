package auth

import (
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// BearerAuth 创建 Bearer token 认证中间件
func BearerAuth(masterKey string, logger *log.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		// /health 不需要认证
		if c.Request.URL.Path == "/health" {
			c.Next()
			return
		}

		// 支持多种 auth 方式：Authorization: Bearer xxx 或 x-api-key: xxx
		token := ""
		authHeader := c.GetHeader("Authorization")
		if authHeader != "" {
			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) == 2 && parts[0] == "Bearer" {
				token = parts[1]
			}
		}
		if token == "" {
			token = c.GetHeader("x-api-key")
		}

		if token == "" {
			logger.Printf("Missing Authorization header from %s", c.ClientIP())
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Missing Authorization header"})
			c.Abort()
			return
		}

		if token != masterKey {
			logger.Printf("Invalid token from %s", c.ClientIP())
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid token"})
			c.Abort()
			return
		}

		c.Next()
	}
}
