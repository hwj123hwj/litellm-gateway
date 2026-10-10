package channel

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// lobster 渠道测试：覆盖授权码回调登录（本地回调服务 → 换码 → 落盘）、令牌解析与
// 续期请求体形态。推理链路的鉴权头/客户端头由 provider_integration_test.go 覆盖。

// newLobsterForTest 造一个端点全部指向假上游的 lobster 渠道。
func newLobsterForTest(t *testing.T, api, portal string) *LobsterChannel {
	t.Helper()
	c := NewLobsterChannel(t.TempDir())
	c.apiHost = api
	c.portalHost = portal
	// 版本接口指向一个必然失败的地址，回退到常量版本号，避免测试触网。
	c.versionURL = "http://127.0.0.1:1/update"
	return c
}

// lobsterLoginParams 从登录地址里取回调地址与 state。门户地址把查询参数放在
// fragment（#/login?...）里，所以要按 fragment 解析而不是 URL.Query。
func lobsterLoginParams(t *testing.T, loginURL string) (redirect, state string) {
	t.Helper()
	idx := strings.Index(loginURL, "?")
	if idx < 0 {
		t.Fatalf("login_url 缺少查询参数: %s", loginURL)
	}
	values, err := url.ParseQuery(loginURL[idx+1:])
	if err != nil {
		t.Fatalf("解析 login_url 查询参数失败: %v", err)
	}
	redirect = values.Get("redirect_uri")
	state = values.Get("state")
	if redirect == "" || state == "" {
		t.Fatalf("login_url 缺少 redirect_uri/state: %s", loginURL)
	}
	return redirect, state
}

// TestLobsterLoginExchangesCodeAndPersists 跑完整回调登录：起会话 → 浏览器带着
// state 回调 → 网关换码 → 凭据落盘。
func TestLobsterLoginExchangesCodeAndPersists(t *testing.T) {
	var exchanged map[string]string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != lobsterExchangePath {
			http.NotFound(w, r)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&exchanged)
		writeJSON(w, map[string]any{
			"code": 0,
			"data": map[string]any{
				"accessToken":  "access-1",
				"refreshToken": "refresh-1",
				"expiresIn":    3600,
				"user":         map[string]any{"id": "u-9", "nickname": "小明"},
			},
		})
	}))
	defer api.Close()

	c := newLobsterForTest(t, api.URL, "https://lobsterai.youdao.com")
	session, err := c.BeginLogin(context.Background())
	if err != nil {
		t.Fatalf("BeginLogin: %v", err)
	}
	dc := session.DeviceCode()
	if dc.VerificationURIComplete == "" {
		t.Fatalf("登录会话应提供 login_url")
	}

	redirect, state := lobsterLoginParams(t, dc.VerificationURIComplete)

	type waitResult struct {
		cred *Credential
		err  error
	}
	done := make(chan waitResult, 1)
	go func() {
		cred, err := session.Wait(context.Background())
		done <- waitResult{cred, err}
	}()

	// 稍微等一下，确保 Wait 已在 server.next 上等待。
	time.Sleep(50 * time.Millisecond)
	cbURL := redirect + "?code=the-code&state=" + url.QueryEscape(state)
	resp, err := http.Get(cbURL)
	if err != nil {
		t.Fatalf("回调请求失败: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("回调应返回 200，得到 %d", resp.StatusCode)
	}

	res := <-done
	if res.err != nil {
		t.Fatalf("Wait: %v", res.err)
	}
	if res.cred == nil || res.cred.AccessToken != "access-1" {
		t.Fatalf("凭据不正确: %+v", res.cred)
	}
	if exchanged["authCode"] != "the-code" {
		t.Fatalf("换码请求应带 authCode=the-code，得到 %v", exchanged)
	}
	// 落盘校验。
	creds := c.Accounts().Credentials()
	if len(creds) != 1 {
		t.Fatalf("应有 1 条已保存凭据，得到 %d", len(creds))
	}
}

// TestLobsterLoginRejectsBadState 验证 state 不匹配的回调被拒，不会换码。
func TestLobsterLoginRejectsBadState(t *testing.T) {
	var exchanged int
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		exchanged++
		writeJSON(w, map[string]any{"code": 0, "data": map[string]any{}})
	}))
	defer api.Close()

	c := newLobsterForTest(t, api.URL, "")
	session, err := c.BeginLogin(context.Background())
	if err != nil {
		t.Fatalf("BeginLogin: %v", err)
	}
	dc := session.DeviceCode()
	redirect, _ := lobsterLoginParams(t, dc.VerificationURIComplete)

	resp, err := http.Get(redirect + "?code=x&state=wrong")
	if err != nil {
		t.Fatalf("回调请求失败: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("state 不匹配应返回 400，得到 %d", resp.StatusCode)
	}
	if exchanged != 0 {
		t.Fatalf("state 校验失败时不应换码，实际 %d 次", exchanged)
	}
}

// TestLobsterRefreshBodyShape 验证续期请求体沿用凭据里的 keyfrom/uuid，并带 refreshToken。
func TestLobsterRefreshBodyShape(t *testing.T) {
	var body map[string]string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != lobsterRefreshPath {
			http.NotFound(w, r)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		writeJSON(w, map[string]any{"code": 0, "data": map[string]any{"accessToken": "new-access", "expiresIn": 3600}})
	}))
	defer api.Close()

	c := newLobsterForTest(t, api.URL, "")
	next, err := c.refreshCredential(context.Background(), Credential{
		AccessToken:  "old",
		RefreshToken: "r-old",
		Extra: map[string]string{
			"first_keyfrom":  "100",
			"latest_keyfrom": "200",
			"uuid":           "uuid-x",
			"user_id":        "u-1",
		},
	})
	if err != nil {
		t.Fatalf("refreshCredential: %v", err)
	}
	if body["refreshToken"] != "r-old" || body["firstKeyfrom"] != "100" || body["latestKeyfrom"] != "200" || body["uuid"] != "uuid-x" {
		t.Fatalf("续期请求体不正确: %v", body)
	}
	if next.AccessToken != "new-access" {
		t.Fatalf("续期后应拿到新令牌，得到 %q", next.AccessToken)
	}
	// 续期响应未回传 refresh_token 时应沿用旧值。
	if next.RefreshToken != "r-old" {
		t.Fatalf("refresh_token 应沿用旧值，得到 %q", next.RefreshToken)
	}
}

// TestLobsterClientHeaders 验证复刻头齐全，且版本号回退到常量（版本接口不可达时）。
func TestLobsterClientHeaders(t *testing.T) {
	c := newLobsterForTest(t, "", "")
	headers := c.ClientHeaders()
	if headers["User-Agent"] != lobsterUserAgent {
		t.Fatalf("User-Agent 应为 %q，得到 %q", lobsterUserAgent, headers["User-Agent"])
	}
	if headers["X-LobsterAI-Client-Capabilities"] != lobsterClientCaps {
		t.Fatalf("能力头不正确: %q", headers["X-LobsterAI-Client-Capabilities"])
	}
	if headers["X-LobsterAI-Client-Version"] != lobsterFallbackVer {
		t.Fatalf("版本号应回退到常量 %q，得到 %q", lobsterFallbackVer, headers["X-LobsterAI-Client-Version"])
	}
}

// TestParseLobsterEnvelope 验证 {code:0,...} 信封解析：非 0 视为业务失败并带出 message。
func TestParseLobsterEnvelope(t *testing.T) {
	if _, err := parseLobsterEnvelope(map[string]any{"code": float64(0), "data": map[string]any{"x": 1}}); err != nil {
		t.Fatalf("code=0 应成功: %v", err)
	}
	_, err := parseLobsterEnvelope(map[string]any{"code": float64(1001), "message": "令牌失效"})
	if err == nil || !strings.Contains(err.Error(), "令牌失效") {
		t.Fatalf("非 0 code 应报业务错误并带 message，得到 %v", err)
	}
}
