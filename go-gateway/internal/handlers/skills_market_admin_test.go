package handlers

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"
)

// newMarketStore 起一个模拟技能商城的 httptest 服务：目录、详情、下载
// 三个端点，包内容为单个 SKILL.md，详情带真实的包大小与摘要；
// sha256Hex 非空时覆盖摘要（模拟包被篡改）。
func newMarketStore(t *testing.T, sha256Hex string) *httptest.Server {
	t.Helper()
	zipBytes := marketDemoZip(t)
	digest := zipDigest(t, zipBytes)
	if sha256Hex != "" {
		digest = sha256Hex
	}
	metadata := `"tarball_size":` + strconv.Itoa(len(zipBytes)) + `,"sha256":"` + digest + `"`
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/skills", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("q") != "" {
			w.Write([]byte(`{"skills":[],"total":0,"page":1}`))
			return
		}
		w.Write([]byte(`{"skills":[{"id":7,"name":"demo-skill","display_name":"示例技能","description":"演示用","version":"1.2.0","install_count":3,` + metadata + `}],"total":1,"page":1}`))
	})
	mux.HandleFunc("GET /api/skills/7", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"id":7,"name":"demo-skill","display_name":"示例技能","description":"演示用","version":"1.2.0",` + metadata + `}`))
	})
	mux.HandleFunc("GET /api/skills/7/download", func(w http.ResponseWriter, r *http.Request) {
		w.Write(zipBytes)
	})
	mux.HandleFunc("GET /api/sections", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"sections":[{"id":1,"name":"开发工具","order_index":0}]}`))
	})
	return httptest.NewServer(mux)
}

// marketDemoZip 构造一个只含 SKILL.md 的技能包。
func marketDemoZip(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	entry, err := zw.Create("SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write([]byte("---\nname: demo-skill\n---\nbody")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func marketTestRouter(t *testing.T, handler *SkillsMarketHandler) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.GET("/admin/skills/market/browse", handler.HandleBrowse)
	engine.GET("/admin/skills/market/sections", handler.HandleSections)
	engine.GET("/admin/skills/market/installed", handler.HandleInstalled)
	engine.POST("/admin/skills/market/install", handler.HandleInstall)
	engine.POST("/admin/skills/market/uninstall", handler.HandleUninstall)
	return engine
}

func zipDigest(t *testing.T, data []byte) string {
	t.Helper()
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func TestSkillsMarketDisabledReturnsHint(t *testing.T) {
	engine := marketTestRouter(t, NewSkillsMarketHandler(t.TempDir(), "", false, log.New(io.Discard, "", 0)))
	w := doJSON(t, engine, http.MethodGet, "/admin/skills/market/browse", nil)
	var body struct {
		Enabled bool   `json:"enabled"`
		Hint    string `json:"hint"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusOK || body.Enabled || body.Hint == "" {
		t.Fatalf("未启用应返回 200 + enabled=false + 提示: %d %+v", w.Code, body)
	}
}

func TestSkillsMarketBrowseProxiesStore(t *testing.T) {
	store := newMarketStore(t, "")
	defer store.Close()
	engine := marketTestRouter(t, NewSkillsMarketHandler(t.TempDir(), store.URL, true, log.New(io.Discard, "", 0)))
	w := doJSON(t, engine, http.MethodGet, "/admin/skills/market/browse?sort=installs&page=1", nil)
	var body struct {
		Enabled bool `json:"enabled"`
		Total   int  `json:"total"`
		Skills  []struct {
			ID          int    `json:"id"`
			Name        string `json:"name"`
			DisplayName string `json:"display_name"`
		} `json:"skills"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !body.Enabled || body.Total != 1 || len(body.Skills) != 1 || body.Skills[0].Name != "demo-skill" || body.Skills[0].DisplayName != "示例技能" {
		t.Fatalf("目录代理结果不符: %+v", body)
	}
}

func TestSkillsMarketInstallUninstallLifecycle(t *testing.T) {
	store := newMarketStore(t, "")
	defer store.Close()
	skillsDir := t.TempDir()
	engine := marketTestRouter(t, NewSkillsMarketHandler(skillsDir, store.URL, true, log.New(io.Discard, "", 0)))

	// 安装：zip 落盘、清单标记写入。
	w := doJSON(t, engine, http.MethodPost, "/admin/skills/market/install", map[string]int{"id": 7})
	var installed struct {
		Error string `json:"error"`
		Skill struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"skill"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &installed); err != nil {
		t.Fatal(err)
	}
	if installed.Error != "" || installed.Skill.Name != "demo-skill" {
		t.Fatalf("安装失败: %d %+v", w.Code, installed)
	}
	dir := filepath.Join(skillsDir, "demo-skill")
	if raw, err := os.ReadFile(filepath.Join(dir, "SKILL.md")); err != nil || len(raw) == 0 {
		t.Fatalf("SKILL.md 未解包: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".easyagent-market.json")); err != nil {
		t.Fatalf("安装清单缺失: %v", err)
	}

	// 重复安装：返回带内错误，不破坏已有目录。
	w = doJSON(t, engine, http.MethodPost, "/admin/skills/market/install", map[string]int{"id": 7})
	var conflict struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &conflict); err != nil {
		t.Fatal(err)
	}
	if conflict.Error == "" {
		t.Fatal("重复安装应报目录已存在")
	}

	// 已安装列表能扫到这条记录。
	w = doJSON(t, engine, http.MethodGet, "/admin/skills/market/installed", nil)
	var list struct {
		SkillsDir string `json:"skills_dir"`
		Installed []struct {
			Name string `json:"name"`
		} `json:"installed"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Installed) != 1 || list.Installed[0].Name != "demo-skill" || list.SkillsDir != skillsDir {
		t.Fatalf("已安装列表不符: %+v", list)
	}

	// 手写技能不可经市场路径卸载。
	handwritten := filepath.Join(skillsDir, "handwritten")
	if err := os.MkdirAll(handwritten, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(handwritten, "SKILL.md"), []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	w = doJSON(t, engine, http.MethodPost, "/admin/skills/market/uninstall", map[string]string{"name": "handwritten"})
	var refused struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &refused); err != nil {
		t.Fatal(err)
	}
	if refused.Error == "" {
		t.Fatal("手写技能应拒绝卸载")
	}
	if _, err := os.Stat(handwritten); err != nil {
		t.Fatal("手写技能目录被误删")
	}

	// 卸载市场技能：目录整体移除。
	w = doJSON(t, engine, http.MethodPost, "/admin/skills/market/uninstall", map[string]string{"name": "demo-skill"})
	if w.Code != http.StatusOK {
		t.Fatalf("卸载失败: %d %s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("市场技能目录未移除: %v", err)
	}
}

func TestSkillsMarketInstallRejectsInvalidBody(t *testing.T) {
	store := newMarketStore(t, "")
	defer store.Close()
	engine := marketTestRouter(t, NewSkillsMarketHandler(t.TempDir(), store.URL, true, log.New(io.Discard, "", 0)))
	w := doJSON(t, engine, http.MethodPost, "/admin/skills/market/install", map[string]int{"id": 0})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("非法 ID 应返回 400: %d", w.Code)
	}
}

// TestSkillsMarketInstallRejectsTamperedPackage 校验失败的包必须被拒绝且不落盘。
func TestSkillsMarketInstallRejectsTamperedPackage(t *testing.T) {
	store := newMarketStore(t, "0000000000000000000000000000000000000000000000000000000000000000")
	defer store.Close()
	skillsDir := t.TempDir()
	engine := marketTestRouter(t, NewSkillsMarketHandler(skillsDir, store.URL, true, log.New(io.Discard, "", 0)))
	w := doJSON(t, engine, http.MethodPost, "/admin/skills/market/install", map[string]int{"id": 7})
	var body struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error == "" {
		t.Fatalf("摘要不匹配应报错: %d %s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(filepath.Join(skillsDir, "demo-skill")); !os.IsNotExist(err) {
		t.Fatal("校验失败的包不应落盘")
	}
}
