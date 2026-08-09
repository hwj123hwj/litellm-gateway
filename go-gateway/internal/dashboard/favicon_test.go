package dashboard

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFaviconHandlerReturnsNoContent(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		req := httptest.NewRequest(method, "/favicon.ico", nil)
		res := httptest.NewRecorder()

		NewFaviconHandler().ServeHTTP(res, req)

		if res.Code != http.StatusNoContent {
			t.Fatalf("%s favicon status = %d, want %d", method, res.Code, http.StatusNoContent)
		}
	}
}

func TestFaviconHandlerRejectsMutation(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/favicon.ico", nil)
	res := httptest.NewRecorder()

	NewFaviconHandler().ServeHTTP(res, req)

	if res.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST favicon status = %d, want %d", res.Code, http.StatusMethodNotAllowed)
	}
}
