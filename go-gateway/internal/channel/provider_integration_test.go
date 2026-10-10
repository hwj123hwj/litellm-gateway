package channel

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/weijian/go-llm-gateway/internal/provider"
)

// 这组测试把「渠道账号池 → provider 实例 → 上游请求」整条链接起来跑，是单测里
// 唯一能覆盖鉴权头形态的地方：channel 包自身的测试只到登录/解析为止，看不出
// provider 到底发的是 x-api-key 还是 Authorization: Bearer。
//
// 为什么放 channel 内部包：NewAccounts 需要未导出的 *oauthClient 与 refreshFn，
// 外部测试包构造不出来；provider 不 import channel（靠 AuthSource 结构化对接），
// 反向依赖不会成环，所以这里可以同时引用两边。

// channelAccounts 造一个已登录的账号池（存储里直接塞一条有效凭据）。
func channelAccounts(t *testing.T, name, token string) *Accounts {
	t.Helper()
	store := NewStore(t.TempDir())
	if err := store.Save(name, []Credential{{
		AccessToken:  token,
		RefreshToken: "refresh-not-used",
		ExpiresAt:    time.Now().Add(2 * time.Hour),
	}}); err != nil {
		t.Fatalf("seed credentials: %v", err)
	}
	return NewAccounts(name, store, newOAuthClient(), func(context.Context, Credential) (Credential, error) {
		return Credential{}, errors.New("test stub: refresh not configured")
	})
}

// minimaxProvider 复刻 channels.go 里的装配，用来断言真实装配产生的请求形态。
// 与 main 里的工厂保持一致：这是刻意的重复，一旦 channels.go 改错，这里会失败。
func minimaxProvider(accounts *Accounts, url string) *provider.AnthropicProvider {
	return provider.NewAnthropicProvider(&provider.Config{
		Name:      "MiniMax-M3",
		URL:       url,
		Auth:      accounts,
		UseBearer: true,
		Transform: MiniMaxTransformRequest,
	})
}

func clineProvider(accounts *Accounts, url string) *provider.OpenAIProvider {
	return provider.NewOpenAIProvider(&provider.Config{
		Name:         "cline-free/mimo-v2.6-flash",
		URL:          url,
		Auth:         accounts,
		ExtraHeaders: ClineClientHeaders(),
	})
}

// TestMiniMaxProviderSendsBearerAuth 是回归测试：minimax 上游要求
// Authorization: Bearer <token>，绝不能发 Anthropic 官方的 x-api-key。
// 之前 channels.go 漏设 UseBearer 时会发 x-api-key，对真实上游必然 401。
func TestMiniMaxProviderSendsBearerAuth(t *testing.T) {
	var gotAuth, gotAPIKey, gotVersion string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotAPIKey = r.Header.Get("x-api-key")
		gotVersion = r.Header.Get("anthropic-version")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","model":"MiniMax-M3","content":[{"type":"text","text":"hi"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer server.Close()

	accounts := channelAccounts(t, "minimax", "mini-token")
	p := minimaxProvider(accounts, server.URL)

	req := mustRequest(t, `{"model":"MiniMax-M3","max_tokens":16,"messages":[{"role":"user","content":"hi"}]}`)
	if _, err := p.ForwardRequest(context.Background(), req); err != nil {
		t.Fatalf("ForwardRequest failed: %v", err)
	}

	if gotAuth != "Bearer mini-token" {
		t.Fatalf("minimax 必须用 Authorization: Bearer，得到 %q", gotAuth)
	}
	if gotAPIKey != "" {
		t.Fatalf("minimax 不应发送 x-api-key，得到 %q", gotAPIKey)
	}
	// Anthropic Messages 协议版本头仍需保留。
	if gotVersion != "2023-06-01" {
		t.Fatalf("anthropic-version 头缺失: %q", gotVersion)
	}
}

// TestMiniMaxProviderAppliesAdaptiveTransform 验证 M3.1 的请求体改写确实作用在
// 发往上游的 body 上（单测只覆盖了纯函数，这里覆盖真实发送路径）。
func TestMiniMaxProviderAppliesAdaptiveTransform(t *testing.T) {
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"m","type":"message","role":"assistant","model":"MiniMax-M3.1-Flash-Preview","content":[],"stop_reason":"end_turn","usage":{"input_tokens":0,"output_tokens":0}}`))
	}))
	defer server.Close()

	accounts := channelAccounts(t, "minimax", "tok")
	p := minimaxProvider(accounts, server.URL)

	req := mustRequest(t, `{"model":"MiniMax-M3.1-Flash-Preview","max_tokens":16,"thinking":{"type":"disabled"},"messages":[{"role":"user","content":"hi"}]}`)
	if _, err := p.ForwardRequest(context.Background(), req); err != nil {
		t.Fatalf("ForwardRequest failed: %v", err)
	}

	thinking, _ := gotBody["thinking"].(map[string]any)
	if thinking == nil || thinking["type"] != "adaptive" {
		t.Fatalf("发往上游的 thinking 应被改写为 adaptive，实际 %v", gotBody["thinking"])
	}
}

// TestClineProviderSendsWorkOSBearerAndHeaders 验证 cline 的鉴权与复刻头：
// Authorization 必须是 `Bearer workos:<token>`，四个客户端头必须齐全。
func TestClineProviderSendsWorkOSBearerAndHeaders(t *testing.T) {
	var gotAuth string
	gotHeaders := map[string]string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		for name := range ClineClientHeaders() {
			gotHeaders[name] = r.Header.Get(name)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"c1","object":"chat.completion","model":"cline-free/mimo-v2.6-flash","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer server.Close()

	accounts := channelAccounts(t, "cline", "workos:cline-token")
	p := clineProvider(accounts, server.URL)

	req := mustRequest(t, `{"model":"cline-free/mimo-v2.6-flash","max_tokens":16,"messages":[{"role":"user","content":"hi"}]}`)
	if _, err := p.ForwardRequest(context.Background(), req); err != nil {
		t.Fatalf("ForwardRequest failed: %v", err)
	}

	if gotAuth != "Bearer workos:cline-token" {
		t.Fatalf("cline Authorization 应为 Bearer workos:<token>，得到 %q", gotAuth)
	}
	for name, want := range ClineClientHeaders() {
		if gotHeaders[name] != want {
			t.Fatalf("cline 客户端头 %s 应为 %q，得到 %q", name, want, gotHeaders[name])
		}
	}
}

// TestClineProviderPrefixesBareToken 验证账号池里存的是裸 token 时，渠道层补上
// workos: 前缀（parseClineToken 负责），provider 只做 Bearer 拼接。
func TestClineProviderPrefixesBareToken(t *testing.T) {
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"index":0,"message":{"role":"assistant","content":"x"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer server.Close()

	// 模拟真实登录产物：凭据里已经是带前缀的值。
	accounts := channelAccounts(t, "cline", "workos:abc")
	p := clineProvider(accounts, server.URL)
	req := mustRequest(t, `{"model":"cline-free/mimo-v2.6-flash","max_tokens":8,"messages":[{"role":"user","content":"hi"}]}`)
	if _, err := p.ForwardRequest(context.Background(), req); err != nil {
		t.Fatalf("ForwardRequest failed: %v", err)
	}
	if gotAuth != "Bearer workos:abc" {
		t.Fatalf("前缀应保持单份，得到 %q", gotAuth)
	}
}

// TestChannelProviderUnavailableWhenLoggedOut 验证未登录时 provider 报告不可用，
// 且不发请求——路由层据此跳过，不必先撞一次注定失败的请求。
func TestChannelProviderUnavailableWhenLoggedOut(t *testing.T) {
	var hits int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	empty := NewAccounts("minimax", NewStore(t.TempDir()), newOAuthClient(), nil)
	p := minimaxProvider(empty, server.URL)

	if p.Available() {
		t.Fatalf("未登录时 Available 应为 false")
	}
	req := mustRequest(t, `{"model":"MiniMax-M3","max_tokens":8,"messages":[{"role":"user","content":"hi"}]}`)
	if _, err := p.ForwardRequest(context.Background(), req); err == nil {
		t.Fatalf("未登录时应直接报错，不发请求")
	}
	if hits != 0 {
		t.Fatalf("未登录时不应触达上游，实际 %d 次", hits)
	}
}

// TestChannelProviderRefreshOn401 验证上游返回 401 后 provider 会用账号池的
// refresh 续期并重试一次；续期成功则第二次带新令牌。
func TestChannelProviderRefreshOn401(t *testing.T) {
	var auths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auths = append(auths, r.Header.Get("Authorization"))
		if len(auths) == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"expired"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"m","type":"message","role":"assistant","model":"MiniMax-M3","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer server.Close()

	store := NewStore(t.TempDir())
	// 凭据已过期（保证 401 后 refresh 会被触发），并在假上游上验证新令牌生效。
	if err := store.Save("minimax", []Credential{{
		AccessToken:  "old-token",
		RefreshToken: "r",
		ExpiresAt:    time.Now().Add(-time.Hour),
	}}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// 用可注入的续期函数把令牌换成 new-token（真实续期需网络，这里只测调度）。
	accounts := NewAccounts("minimax", store, newOAuthClient(), func(_ context.Context, cred Credential) (Credential, error) {
		next := cred
		next.AccessToken = "new-token"
		next.ExpiresAt = time.Now().Add(2 * time.Hour)
		return next, nil
	})

	p := minimaxProvider(accounts, server.URL)
	req := mustRequest(t, `{"model":"MiniMax-M3","max_tokens":8,"messages":[{"role":"user","content":"hi"}]}`)
	if _, err := p.ForwardRequest(context.Background(), req); err != nil {
		t.Fatalf("续期后应成功，得到 %v", err)
	}

	if len(auths) != 2 {
		t.Fatalf("应重试一次（共 2 次请求），实际 %d 次: %v", len(auths), auths)
	}
	if auths[0] != "Bearer old-token" || auths[1] != "Bearer new-token" {
		t.Fatalf("续期前后令牌不正确: %v", auths)
	}
}

func mustRequest(t *testing.T, body string) *provider.Request {
	t.Helper()
	var req provider.Request
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	return &req
}
