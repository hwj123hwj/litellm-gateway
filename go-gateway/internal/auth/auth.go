package auth

import (
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// extractToken 从请求中提取 token
func extractToken(c *gin.Context) string {
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
	return token
}

// BearerAuth 创建 Bearer token 认证中间件
func BearerAuth(masterKey string, logger *log.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		// /health 和 OPTIONS 请求不需要认证
		if c.Request.URL.Path == "/health" || c.Request.Method == "OPTIONS" {
			c.Next()
			return
		}

		token := extractToken(c)

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

// AdminAuth 创建管理端点认证中间件
// 如果设置了 adminToken，则管理端点需要 adminToken
// 如果未设置 adminToken，则管理端点使用 masterKey
func AdminAuth(masterKey string, adminToken string, logger *log.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := extractToken(c)

		if token == "" {
			logger.Printf("Missing Authorization header for admin endpoint from %s", c.ClientIP())
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Missing Authorization header"})
			c.Abort()
			return
		}

		// 如果设置了 adminToken，优先验证 adminToken
		if adminToken != "" {
			if token == adminToken {
				c.Next()
				return
			}
			// adminToken 不匹配，也允许 masterKey
			if token == masterKey {
				c.Next()
				return
			}
		} else {
			// 未设置 adminToken，使用 masterKey
			if token == masterKey {
				c.Next()
				return
			}
		}

		logger.Printf("Invalid admin token from %s", c.ClientIP())
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid admin token"})
		c.Abort()
	}
}
