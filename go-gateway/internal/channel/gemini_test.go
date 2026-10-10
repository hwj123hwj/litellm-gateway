package channel

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// gemini 渠道测试：授权码 + localhost 回环回调，令牌端点是 Google 的
// application/x-www-form-urlencoded 接口，身份从 id_token JWT 里取。
// CloudCode 信封与 SSE 翻译由 internal/provider/cloudcode_test.go 覆盖。

// newGeminiForTest 造一个端点全部指向假上游的 gemini 渠道。OAuth 客户端凭据在
// 生产环境来自本地配置（环境变量或 <gatewayHome>/gemini-client.json），测试里
// 直接用固定值，避免依赖运行环境。
func newGeminiForTest(t *testing.T, token, userinfo string) *GeminiChannel {
	t.Helper()
	c := NewGeminiChannel(t.TempDir())
	c.tokenEndpoint = token
	c.userinfoEndpoint = userinfo
	c.clientID = "test-client-id"
	c.clientSecret = "test-client-secret"
	c.loginTimeout = 5 * time.Second
	return c
}

// TestGeminiLoginExchangesCodeAndPersists 跑完整授权码登录：换码 → 补 userinfo →
// 落盘，并校验换码请求是用 form 提交且带 PKCE 之外的必需字段。
func TestGeminiLoginExchangesCodeAndPersists(t *testing.T) {
	var form url.Values
	token := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		form = r.PostForm
		writeJSON(w, map[string]any{
			"access_token":  "at-1",
			"refresh_token": "rt-1",
			"expires_in":    3600,
			"id_token":      makeJWT(t, map[string]any{"sub": "sub-7", "email": "me@example.com"}),
		})
	}))
	defer token.Close()
	userinfo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"id": "sub-7", "email": "me@example.com"})
	}))
	defer userinfo.Close()

	c := newGeminiForTest(t, token.URL, userinfo.URL)
	session, err := c.BeginLogin(context.Background())
	if err != nil {
		t.Fatalf("BeginLogin: %v", err)
	}
	dc := session.DeviceCode()
	// Google 桌面流程要求 redirect_uri 主机是 localhost。
	if !strings.Contains(dc.VerificationURIComplete, "redirect_uri=http%3A%2F%2Flocalhost%3A") {
		t.Fatalf("redirect_uri 应指向 localhost 回环，实际 %s", dc.VerificationURIComplete)
	}

	values, err := url.ParseQuery(dc.VerificationURIComplete[strings.Index(dc.VerificationURIComplete, "?")+1:])
	if err != nil {
		t.Fatalf("解析 auth url: %v", err)
	}
	redirect, state := values.Get("redirect_uri"), values.Get("state")
	if redirect == "" || state == "" {
		t.Fatalf("auth url 缺少 redirect_uri/state")
	}

	done := make(chan error, 1)
	go func() {
		_, err := session.Wait(context.Background())
		done <- err
	}()
	time.Sleep(50 * time.Millisecond)

	resp, err := http.Get(redirect + "?code=code-1&state=" + url.QueryEscape(state))
	if err != nil {
		t.Fatalf("回调请求失败: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("回调应返回 200，得到 %d", resp.StatusCode)
	}
	if err := <-done; err != nil {
		t.Fatalf("Wait: %v", err)
	}

	if form.Get("grant_type") != "authorization_code" || form.Get("code") != "code-1" {
		t.Fatalf("换码请求的 form 不正确: %v", form)
	}
	if form.Get("client_secret") == "" {
		t.Fatalf("换码请求应带 client_secret: %v", form)
	}

	creds := c.Accounts().Credentials()
	if len(creds) != 1 {
		t.Fatalf("应有 1 条凭据，得到 %d", len(creds))
	}
	if creds[0].AccountID != "sub-7" {
		t.Fatalf("AccountID 应从 id_token sub 取，得到 %q", creds[0].AccountID)
	}
}

// TestGeminiLoginRejectsBadState 验证 state 不匹配的回调被拒。
func TestGeminiLoginRejectsBadState(t *testing.T) {
	var served int
	token := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		served++
		writeJSON(w, map[string]any{"access_token": "x"})
	}))
	defer token.Close()

	c := newGeminiForTest(t, token.URL, "")
	session, err := c.BeginLogin(context.Background())
	if err != nil {
		t.Fatalf("BeginLogin: %v", err)
	}
	dc := session.DeviceCode()
	values, _ := url.ParseQuery(dc.VerificationURIComplete[strings.Index(dc.VerificationURIComplete, "?")+1:])
	redirect := values.Get("redirect_uri")

	resp, err := http.Get(redirect + "?code=x&state=nope")
	if err != nil {
		t.Fatalf("回调请求失败: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("state 不匹配应返回 400，得到 %d", resp.StatusCode)
	}
	if served != 0 {
		t.Fatalf("state 校验失败时不应换码，实际 %d 次", served)
	}
}

// TestGeminiRefreshFillsIdentity 验证续期在响应缺少身份字段时沿用旧凭据的身份。
func TestGeminiRefreshFillsIdentity(t *testing.T) {
	token := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.PostForm.Get("grant_type") != "refresh_token" {
			t.Errorf("续期应以 refresh_token 授权，得到 %v", r.PostForm)
		}
		// 刻意不回传 refresh_token / id_token。
		writeJSON(w, map[string]any{"access_token": "at-2", "expires_in": 1800})
	}))
	defer token.Close()

	c := newGeminiForTest(t, token.URL, "")
	next, err := c.refreshCredential(context.Background(), Credential{
		AccessToken:  "at-1",
		RefreshToken: "rt-1",
		AccountID:    "sub-7",
		Nickname:     "me@example.com",
	})
	if err != nil {
		t.Fatalf("refreshCredential: %v", err)
	}
	if next.AccessToken != "at-2" {
		t.Fatalf("应拿到新令牌，得到 %q", next.AccessToken)
	}
	if next.RefreshToken != "rt-1" || next.AccountID != "sub-7" || next.Nickname != "me@example.com" {
		t.Fatalf("缺失字段应沿用旧值: %+v", next)
	}
}

// TestGeminiAuthSourceCachesProject 验证 geminiAuth 作为 AuthSource 暴露的
// CloudCode 项目缓存：写入后读回同一值，空串不覆盖。
func TestGeminiAuthSourceCachesProject(t *testing.T) {
	c := NewGeminiChannel(t.TempDir())
	auth := c.AuthSource()
	if auth.CloudCodeProject() != "" {
		t.Fatalf("初始应为空")
	}
	auth.SetCloudCodeProject("")
	if auth.CloudCodeProject() != "" {
		t.Fatalf("空串不应写入缓存")
	}
	auth.SetCloudCodeProject("proj-1")
	if auth.CloudCodeProject() != "proj-1" {
		t.Fatalf("应读到缓存值，得到 %q", auth.CloudCodeProject())
	}
	// 未登录时 BearerToken 为空 → AuthSource 可用性判定为 false。
	if auth.BearerToken() != "" {
		t.Fatalf("未登录时不应有令牌")
	}
}

// TestGeminiGrantErrorIncludesDescription 验证令牌端点错误会把 error_description 带出来。
func TestGeminiGrantErrorIncludesDescription(t *testing.T) {
	err := geminiGrantError(&oauthHTTPError{Status: 400, Body: map[string]any{"error": "invalid_grant", "error_description": "code expired"}})
	if err == nil || !strings.Contains(err.Error(), "code expired") {
		t.Fatalf("错误应包含 error_description，得到 %v", err)
	}
}

// TestParseGeminiGrantRequiresAccessToken 验证缺少 access_token 时解析失败并带出细节。
func TestParseGeminiGrantRequiresAccessToken(t *testing.T) {
	_, err := parseGeminiGrant(map[string]any{"error": "invalid_grant"}, time.Now())
	if err == nil || !strings.Contains(err.Error(), "invalid_grant") {
		t.Fatalf("应报缺少 access_token 并带出 error，得到 %v", err)
	}
}

// ensure json import is used (writeJSON helper is in helpers_test, this keeps parity).
var _ = json.Marshal

// clearGeminiCredentialEnv 清空可能影响凭据解析的环境变量。
func clearGeminiCredentialEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"CMDC_PAK_GOOGLE_CLIENT_ID", "CMDC_PAK_GOOGLE_CLIENT_SECRET",
		"GEMINI_CHANNEL_CLIENT_ID", "GEMINI_CHANNEL_CLIENT_SECRET",
	} {
		t.Setenv(key, "")
	}
}

// TestGeminiCredentialsFromEnv 验证环境变量优先于本地文件，且解析结果落到渠道上。
func TestGeminiCredentialsFromEnv(t *testing.T) {
	clearGeminiCredentialEnv(t)
	t.Setenv("GEMINI_CHANNEL_CLIENT_ID", "env-id")
	t.Setenv("GEMINI_CHANNEL_CLIENT_SECRET", "env-secret")

	c := NewGeminiChannel(t.TempDir())
	if c.clientID != "env-id" || c.clientSecret != "env-secret" {
		t.Fatalf("应从环境变量取凭据，得到 %q/%q", c.clientID, c.clientSecret)
	}
	if !c.configured() {
		t.Fatalf("配置了环境变量后 configured 应为 true")
	}
}

// TestGeminiCredentialsFromFile 验证环境变量缺省时回退到 <gatewayHome>/gemini-client.json。
// dir 是 <home>/channels，凭据文件放在其上一级。
func TestGeminiCredentialsFromFile(t *testing.T) {
	clearGeminiCredentialEnv(t)
	home := t.TempDir()
	dir := filepath.Join(home, "channels")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatalf("建目录: %v", err)
	}
	payload := []byte(`{"client_id":"file-id","client_secret":"file-secret"}`)
	if err := os.WriteFile(filepath.Join(home, geminiClientCredentialFile), payload, 0600); err != nil {
		t.Fatalf("写凭据文件: %v", err)
	}

	c := NewGeminiChannel(dir)
	if c.clientID != "file-id" || c.clientSecret != "file-secret" {
		t.Fatalf("应从本地文件取凭据，得到 %q/%q", c.clientID, c.clientSecret)
	}
}

// TestGeminiBeginLoginRequiresCredentials 验证未配置客户端凭据时登录明确失败，
// 且报错里带出可操作的配置位置（Admin API 会把它作为 502 文本暴露）。
func TestGeminiBeginLoginRequiresCredentials(t *testing.T) {
	clearGeminiCredentialEnv(t)

	c := NewGeminiChannel(t.TempDir())
	if c.configured() {
		t.Fatalf("无环境变量与文件时 configured 应为 false")
	}
	_, err := c.BeginLogin(context.Background())
	if err == nil {
		t.Fatalf("缺少凭据时 BeginLogin 应报错")
	}
	if !strings.Contains(err.Error(), geminiClientCredentialFile) ||
		!strings.Contains(err.Error(), "GEMINI_CHANNEL_CLIENT_ID") {
		t.Fatalf("报错应给出配置指引，得到 %v", err)
	}
}
