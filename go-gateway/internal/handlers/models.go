package handlers

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/weijian/go-llm-gateway/internal/provider"
)

// ModelHandler 处理 /v1/models 端点
type ModelHandler struct {
	router *provider.Router
	logger *log.Logger
}

func NewModelHandler(router *provider.Router, logger *log.Logger) *ModelHandler {
	return &ModelHandler{router: router, logger: logger}
}

func (h *ModelHandler) Handle(c *gin.Context) {
	models := []map[string]string{
		{"id": "glm-haiku", "object": "model"},
		{"id": "glm-sonnet", "object": "model"},
		{"id": "glm-opus", "object": "model"},
		{"id": "mimo-haiku", "object": "model"},
		{"id": "mimo-sonnet", "object": "model"},
		{"id": "mimo-opus", "object": "model"},
		{"id": "longcat-sonnet", "object": "model"},
		{"id": "longcat-opus", "object": "model"},
		{"id": "coding", "object": "model"},
	}
	c.JSON(http.StatusOK, gin.H{"object": "list", "data": models})
}
