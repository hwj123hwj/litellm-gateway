package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
)

func fsTestEngine(t *testing.T) (*gin.Engine, string) {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "projects", "demo"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "linked"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "projects"), filepath.Join(root, "projects-link")); err != nil {
		t.Fatal(err)
	}
	engine := gin.New()
	engine.GET("/admin/fs/dirs", NewFSHandler().HandleDirs)
	return engine, root
}

func getDirs(t *testing.T, engine *gin.Engine, query string) (int, map[string]any) {
	t.Helper()
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/admin/fs/dirs"+query, nil))
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("响应不是 JSON: %v", w.Body.String())
	}
	return w.Code, body
}

func TestFSDirsListsOnlyDirectories(t *testing.T) {
	engine, root := fsTestEngine(t)
	code, body := getDirs(t, engine, "?path="+root)
	if code != http.StatusOK {
		t.Fatalf("status %d: %v", code, body)
	}
	if body["path"] != root {
		t.Fatalf("path %v", body["path"])
	}
	if body["parent"] != filepath.Dir(root) {
		t.Fatalf("parent %v", body["parent"])
	}
	dirs := body["dirs"].([]any)
	if len(dirs) != 3 {
		t.Fatalf("期望 3 个目录（含符号链接），得到 %v", dirs)
	}
	for _, name := range dirs {
		if name == "notes.txt" {
			t.Fatal("文件不应出现在目录列表中")
		}
	}
}

func TestFSDirsDefaultsToHomeAndRejectsRelative(t *testing.T) {
	engine, _ := fsTestEngine(t)
	code, body := getDirs(t, engine, "")
	if code != http.StatusOK {
		t.Fatalf("缺省 path 应返回主目录，status %d: %v", code, body)
	}
	if path, _ := body["path"].(string); !filepath.IsAbs(path) {
		t.Fatalf("缺省 path 应为绝对路径: %v", path)
	}
	code, body = getDirs(t, engine, "?path=relative/path")
	if code != http.StatusBadRequest {
		t.Fatalf("相对路径应被拒绝，status %d: %v", code, body)
	}
	if body["error"] == "" {
		t.Fatal("缺少错误说明")
	}
}

func TestFSDirsRejectsMissingDirectory(t *testing.T) {
	engine, root := fsTestEngine(t)
	code, body := getDirs(t, engine, "?path="+filepath.Join(root, "nope"))
	if code != http.StatusBadRequest {
		t.Fatalf("status %d: %v", code, body)
	}
	if body["error"] == "" {
		t.Fatal("缺少错误说明")
	}
}
