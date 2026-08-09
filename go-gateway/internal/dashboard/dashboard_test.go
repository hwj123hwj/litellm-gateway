package dashboard

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewHandlerServesIndex(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	response := httptest.NewRecorder()

	NewHandler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected index status 200, got %d", response.Code)
	}
	if !strings.Contains(response.Body.String(), "id=\"root\"") {
		t.Fatalf("expected embedded dashboard index, got %q", response.Body.String())
	}
}

func TestNewHandlerFallsBackToIndexForClientRoute(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/models", nil)
	response := httptest.NewRecorder()

	NewHandler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected client route status 200, got %d", response.Code)
	}
	if !strings.Contains(response.Body.String(), "id=\"root\"") {
		t.Fatalf("expected client route to serve index, got %q", response.Body.String())
	}
}

func TestNewHandlerRejectsMutation(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/", nil)
	response := httptest.NewRecorder()

	NewHandler().ServeHTTP(response, request)

	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 for mutation, got %d", response.Code)
	}
}
