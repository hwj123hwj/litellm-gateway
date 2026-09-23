package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/gin-gonic/gin"
	"github.com/weijian/go-llm-gateway/internal/piconfig"
)

// PiConfigHandler 把 Pi 客户端的模型清单同步搬进控制面板：
// 看板里一键完成原本需要 `llm-gateway setup pi` 手动执行的动作。
type PiConfigHandler struct {
	gatewayHome string
	piHome      string
	logger      *log.Logger
}

// NewPiConfigHandler 创建 Pi 配置面板 handler
func NewPiConfigHandler(gatewayHome, piHome string, logger *log.Logger) *PiConfigHandler {
	return &PiConfigHandler{gatewayHome: gatewayHome, piHome: piHome, logger: logger}
}

func (h *PiConfigHandler) modelsPath() string {
	return filepath.Join(h.piHome, "agent", "models.json")
}

// piStatus 汇总看板需要展示的状态。currentIDs 为空表示 Pi 配置还不存在。
func (h *PiConfigHandler) piStatus() gin.H {
	desired := piconfig.DesiredModels()
	desiredIDs := make([]string, 0, len(desired))
	for _, m := range desired {
		desiredIDs = append(desiredIDs, m["id"].(string))
	}

	path := h.modelsPath()
	currentIDs := []string{}
	fileExists := false
	inSync := false
	raw, err := os.ReadFile(path)
	if err == nil {
		fileExists = true
		currentIDs = currentModelIDs(raw)
		inSync = equalStringSets(desiredIDs, currentIDs)
	} else if !os.IsNotExist(err) {
		h.logger.Printf("read Pi models.json: %v", err)
	}

	return gin.H{
		"path":         path,
		"file_exists":  fileExists,
		"in_sync":      inSync,
		"desired":      desired,
		"current_ids":  currentIDs,
		"desired_ids":  desiredIDs,
		"missing_ids":  difference(desiredIDs, currentIDs),
		"stale_ids":    difference(currentIDs, desiredIDs),
		"gateway_home": h.gatewayHome,
	}
}

// HandleStatus GET /admin/pi
func (h *PiConfigHandler) HandleStatus(c *gin.Context) {
	c.JSON(http.StatusOK, h.piStatus())
}

// HandleSync POST /admin/pi/sync
// 与 `llm-gateway setup pi` 完全同一合并逻辑；写入前把现有文件轮转备份一份。
func (h *PiConfigHandler) HandleSync(c *gin.Context) {
	path := h.modelsPath()
	if raw, err := os.ReadFile(path); err == nil {
		backup := path + ".pre-sync.bak"
		if err := os.WriteFile(backup, raw, 0o600); err != nil {
			h.logger.Printf("backup Pi models.json failed: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("备份现有配置失败: %v", err)})
			return
		}
	}

	merged, _, err := piconfig.Setup(piconfig.SetupOptions{
		GatewayHome: h.gatewayHome,
		PiHome:      h.piHome,
	})
	if err != nil {
		h.logger.Printf("sync Pi models.json failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	status := h.piStatus()
	status["synced"] = true
	status["bytes"] = len(merged)
	h.logger.Printf("Pi models.json synced from dashboard: %s", path)
	c.JSON(http.StatusOK, status)
}

// currentModelIDs 从现有 models.json 里取 llm-gateway 组的模型 ID 列表。
func currentModelIDs(raw []byte) []string {
	ids := []string{}
	var document struct {
		Providers map[string]struct {
			Models []struct {
				ID string `json:"id"`
			} `json:"models"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(raw, &document); err != nil {
		return ids
	}
	provider, ok := document.Providers[piconfig.ProviderID]
	if !ok {
		return ids
	}
	for _, m := range provider.Models {
		ids = append(ids, m.ID)
	}
	return ids
}

func equalStringSets(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := make(map[string]int, len(a))
	for _, v := range a {
		seen[v]++
	}
	for _, v := range b {
		if seen[v] == 0 {
			return false
		}
		seen[v]--
	}
	return true
}

func difference(a, b []string) []string {
	inB := make(map[string]bool, len(b))
	for _, v := range b {
		inB[v] = true
	}
	missing := []string{}
	for _, v := range a {
		if !inB[v] {
			missing = append(missing, v)
		}
	}
	return missing
}
