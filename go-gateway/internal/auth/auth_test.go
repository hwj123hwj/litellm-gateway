package auth

import (
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func newTestRouter(masterKey string) *gin.Engine {
	logger := log.New(os.Stderr, "", 0)
	e := gin.New()
	e.Use(BearerAuth(masterKey, logger))
	e.POST("/test", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) })
	e.GET("/health", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })
	e.GET("/readyz", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ready"}) })
	e.GET("/dashboard", func(c *gin.Context) { c.Status(http.StatusOK) })
	e.GET("/assets/index.js", func(c *gin.Context) { c.Status(http.StatusOK) })
	e.GET("/favicon.ico", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	return e
}

func TestBearerAuthValidToken(t *testing.T) {
	e := newTestRouter("test-key")
	req := httptest.NewRequest(http.MethodPost, "/test", nil)
	req.Header.Set("Authorization", "Bearer test-key")
	w := httptest.NewRecorder()
	e.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", w.Code)
	}
}

func TestBearerAuthInvalidToken(t *testing.T) {
	e := newTestRouter("test-key")
	req := httptest.NewRequest(http.MethodPost, "/test", nil)
	req.Header.Set("Authorization", "Bearer wrong-key")
	w := httptest.NewRecorder()
	e.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected 401, got %d", w.Code)
	}
}

func TestBearerAuthMissingHeader(t *testing.T) {
	e := newTestRouter("test-key")
	req := httptest.NewRequest(http.MethodPost, "/test", nil)
	w := httptest.NewRecorder()
	e.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected 401, got %d", w.Code)
	}
}

func TestBearerAuthHealthPublic(t *testing.T) {
	e := newTestRouter("test-key")
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	// 故意不设置 Authorization header
	w := httptest.NewRecorder()
	e.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("Expected 200 (public endpoint), got %d", w.Code)
	}
}

func TestBearerAuthReadinessPublic(t *testing.T) {
	e := newTestRouter("test-key")
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	w := httptest.NewRecorder()
	e.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("Expected 200 (public endpoint), got %d", w.Code)
	}
}

func TestBearerAuthDashboardPublic(t *testing.T) {
	e := newTestRouter("test-key")
	e.GET("/", func(c *gin.Context) { c.Status(http.StatusOK) })

	for _, test := range []struct {
		path   string
		status int
	}{
		{path: "/", status: http.StatusOK},
		{path: "/assets/index.js", status: http.StatusOK},
		{path: "/dashboard", status: http.StatusOK},
		{path: "/favicon.ico", status: http.StatusNoContent},
	} {
		req := httptest.NewRequest(http.MethodGet, test.path, nil)
		w := httptest.NewRecorder()
		e.ServeHTTP(w, req)
		if w.Code != test.status {
			t.Errorf("expected dashboard path %s to be public with status %d, got %d", test.path, test.status, w.Code)
		}
	}
}

func TestBearerAuthAllowsConfiguredAdminTokenOnAdminPaths(t *testing.T) {
	logger := log.New(os.Stderr, "", 0)
	e := gin.New()
	e.Use(BearerAuthWithAdminToken("master-key", "admin-key", logger))
	e.GET("/admin/providers", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) })

	req := httptest.NewRequest(http.MethodGet, "/admin/providers", nil)
	req.Header.Set("Authorization", "Bearer admin-key")
	w := httptest.NewRecorder()
	e.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected admin token to be accepted, got %d: %s", w.Code, w.Body.String())
	}
}
