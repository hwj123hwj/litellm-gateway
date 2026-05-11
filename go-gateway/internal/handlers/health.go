package handlers

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/weijian/go-llm-gateway/internal/provider"
)

// HealthHandler 处理 /health 端点
type HealthHandler struct {
	router *provider.Router
	logger *log.Logger
}

func NewHealthHandler(router *provider.Router, logger *log.Logger) *HealthHandler {
	return &HealthHandler{router: router, logger: logger}
}

func (h *HealthHandler) Handle(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}
