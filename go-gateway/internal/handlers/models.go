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
	chains := h.router.ListChains()
	models := make([]map[string]string, 0, len(chains))
	for _, id := range chains {
		models = append(models, map[string]string{"id": id, "object": "model"})
	}
	c.JSON(http.StatusOK, gin.H{"object": "list", "data": models})
}
