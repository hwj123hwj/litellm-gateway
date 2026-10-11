package provider

import (
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"
)

// stubAuth 模拟渠道账号池：空令牌即未登录。
type stubAuth struct{ token string }

func (s *stubAuth) BearerToken() string          { return s.token }
func (s *stubAuth) Refresh(context.Context) bool { return false }

func TestModelUsable(t *testing.T) {
	logger := log.New(io.Discard, "", 0)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{}`))
	}))
	defer upstream.Close()

	router := NewRouter(logger)
	router.RegisterProvider("keyed", NewOpenAIProvider(&Config{Name: "keyed", URL: upstream.URL, APIKey: "k"}))
	router.RegisterProvider("channel-out", NewOpenAIProvider(&Config{Name: "channel-out", URL: upstream.URL, Auth: &stubAuth{}}))
	router.RegisterProvider("channel-in", NewOpenAIProvider(&Config{Name: "channel-in", URL: upstream.URL, Auth: &stubAuth{token: "t"}}))
	router.RegisterChain("usable-model", []string{"keyed"})
	router.RegisterChain("logged-out-model", []string{"channel-out"})
	router.RegisterChain("channel-model", []string{"channel-out", "channel-in"})
	router.RegisterChain("missing-provider", []string{"ghost"})
	router.RegisterModel(ModelInfo{ID: "usable-model", Capabilities: []string{"text"}})
	router.RegisterModel(ModelInfo{ID: "logged-out-model", Capabilities: []string{"text"}})
	router.RegisterModel(ModelInfo{ID: "channel-model", Capabilities: []string{"text"}})

	if !router.ModelUsable("usable-model") {
		t.Fatal("keyed provider model should be usable")
	}
	if router.ModelUsable("logged-out-model") {
		t.Fatal("logged-out channel model must not be usable")
	}
	if !router.ModelUsable("channel-model") {
		t.Fatal("chain with one logged-in provider should be usable")
	}
	if router.ModelUsable("missing-provider") {
		t.Fatal("chain pointing at an unregistered provider must not be usable")
	}
	if router.ModelUsable("unknown-model") {
		t.Fatal("unknown model must not be usable")
	}

	// 停用链上唯一可用 provider 后不可用；重新启用恢复。
	if err := router.SetProviderEnabled("channel-in", false); err != nil {
		t.Fatal(err)
	}
	if router.ModelUsable("channel-model") {
		t.Fatal("disabled provider must make the model unusable")
	}
	if err := router.SetProviderEnabled("channel-in", true); err != nil {
		t.Fatal(err)
	}
	if !router.ModelUsable("channel-model") {
		t.Fatal("re-enabled provider should restore usability")
	}
}
