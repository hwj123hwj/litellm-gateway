package handlers

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
)

// FSHandler 提供网关主机文件系统的只读目录浏览，供桌面端在技能等
// 场景选择网关主机上的路径。只返回子目录名，不返回文件内容、大小或
// 任何文件正文；挂载在 /admin 下，与其他管理端点共享同一套鉴权。
// maxDirs 限制单次返回的目录数量，防止超大目录拖垮响应。
type FSHandler struct {
	maxDirs int
}

// NewFSHandler 创建目录浏览 handler。
func NewFSHandler() *FSHandler {
	return &FSHandler{maxDirs: 500}
}

// HandleDirs GET /admin/fs/dirs?path=/absolute/path
// path 缺省时返回网关进程用户的主目录；相对路径一律拒绝。
func (h *FSHandler) HandleDirs(c *gin.Context) {
	target := strings.TrimSpace(c.Query("path"))
	if target == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "无法确定网关主机主目录"})
			return
		}
		target = home
	}
	if !filepath.IsAbs(target) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请提供网关主机上的绝对路径"})
		return
	}
	clean := filepath.Clean(target)
	entries, err := os.ReadDir(clean)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("无法读取目录 %s：%v", clean, err)})
		return
	}
	dirs := make([]string, 0, 32)
	for _, entry := range entries {
		if !h.isDir(clean, entry) {
			continue
		}
		dirs = append(dirs, entry.Name())
		if len(dirs) >= h.maxDirs {
			break
		}
	}
	sort.Strings(dirs)
	parent := ""
	if above := filepath.Dir(clean); above != clean {
		parent = above
	}
	c.JSON(http.StatusOK, gin.H{"path": clean, "parent": parent, "dirs": dirs})
}

// isDir 对符号链接做一次 Stat 解析，保证指向目录的链接也能进入。
func (h *FSHandler) isDir(dir string, entry os.DirEntry) bool {
	if entry.IsDir() {
		return true
	}
	if entry.Type()&os.ModeSymlink == 0 {
		return false
	}
	info, err := os.Stat(filepath.Join(dir, entry.Name()))
	return err == nil && info.IsDir()
}
