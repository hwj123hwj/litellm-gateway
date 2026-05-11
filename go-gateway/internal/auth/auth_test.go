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
