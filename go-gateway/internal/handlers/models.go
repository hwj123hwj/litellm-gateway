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
	infos := h.router.ListModelInfos()
	models := make([]map[string]any, 0, len(infos))
	for _, info := range infos {
		model := map[string]any{
			"id":           info.ID,
			"object":       "model",
			"slug":         info.ID,
			"display_name": info.ID,
			// Keep the historical value for client compatibility; provider is
			// exposed separately as additive metadata below.
			"owned_by":     "custom",
			"capabilities": info.Capabilities,
		}
		if info.Provider != "" {
			model["provider"] = info.Provider
		}
		if info.Protocol != "" {
			model["protocol"] = info.Protocol
		}
		if len(info.InputModalities) > 0 {
			model["input_modalities"] = info.InputModalities
		}
		if info.MaxInputTokens > 0 {
			model["max_input_tokens"] = info.MaxInputTokens
		}
		if info.MaxOutputTokens > 0 {
			model["max_output_tokens"] = info.MaxOutputTokens
		}
		models = append(models, model)
	}
	c.JSON(http.StatusOK, gin.H{"object": "list", "data": models, "models": models})
}
