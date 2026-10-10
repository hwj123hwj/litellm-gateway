package channel

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// trae 渠道测试：覆盖授权地址参数、回环回调解析（含 PKCE 拒绝与昵称乱码修复）、
// ExchangeToken 换令牌与续期、凭据落盘、身份头。
// SOLO 请求体与私有 SSE 的翻译由 internal/provider/trae_test.go 覆盖。

// clearTraeCredentialEnv 清空可能影响客户端标识解析的环境变量。
func clearTraeCredentialEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{"TRAE_CHANNEL_CLIENT_ID", "TRAE_CHANNEL_APP_ID"} {
		t.Setenv(key, "")
	}
}

// traeTestChannel 造一个已配置客户端标识、回调端口交给系统分配的渠道。
// oauthHost 由调用方指向假上游。
func traeTestChannel(t *testing.T) *TraeChannel {
	t.Helper()
	clearTraeCredentialEnv(t)
	c := NewTraeChannel(t.TempDir())
	c.clientID, c.appID = "test-client-id", "test-app-id"
	// 固定端口 18080 在并行测试里可能互相抢，测试统一让系统分配。
	c.callbackPort = 0
	c.loginTimeout = 5 * time.Second
	return c
}

// traeCallbackURL 从授权地址里取回 auth_callback_url（也就是回环回调地址）。
func traeCallbackURL(t *testing.T, loginURL string) string {
	t.Helper()
	parsed, err := url.Parse(loginURL)
	if err != nil {
		t.Fatalf("解析授权地址失败: %v", err)
	}
	callback := parsed.Query().Get("auth_callback_url")
	if callback == "" {
		t.Fatalf("授权地址缺少 auth_callback_url: %s", loginURL)
	}
	return callback
}

// TestTraeLoginURLParams 锁住授权地址的参数名与顺序。
// 顺序按官方 URLSearchParams 的插入顺序，写成 callback_url（而不是 auth_callback_url）
// 会拿不到回调，是这里最需要防的回归。
func TestTraeLoginURLParams(t *testing.T) {
	loginURL := buildTraeLoginURL("https://www.trae.cn", "cid", "m1", "d1", "http://127.0.0.1:18080/authorize")

	if !strings.HasPrefix(loginURL, "https://www.trae.cn/authorization?") {
		t.Fatalf("授权地址基址不对: %s", loginURL)
	}
	raw := loginURL[strings.Index(loginURL, "?")+1:]
	keys := make([]string, 0, 20)
	for _, pair := range strings.Split(raw, "&") {
		keys = append(keys, strings.SplitN(pair, "=", 2)[0])
	}
	want := []string{
		"login_version", "auth_from", "login_channel", "plugin_version", "auth_type",
		"client_id", "redirect", "login_trace_id", "auth_callback_url",
		"machine_id", "device_id", "x_device_id", "x_machine_id",
		"x_device_brand", "x_device_type", "x_os_version", "x_app_version", "x_app_type",
	}
	if strings.Join(keys, ",") != strings.Join(want, ",") {
		t.Fatalf("授权地址参数顺序不符\n实际: %v\n期望: %v", keys, want)
	}

	values, err := url.ParseQuery(raw)
	if err != nil {
		t.Fatalf("解析授权地址失败: %v", err)
	}
	if values.Get("auth_callback_url") != "http://127.0.0.1:18080/authorize" {
		t.Fatalf("auth_callback_url 不正确: %q", values.Get("auth_callback_url"))
	}
	if values.Get("client_id") != "cid" {
		t.Fatalf("client_id 不正确: %q", values.Get("client_id"))
	}
	if values.Get("auth_from") != "solo" || values.Get("auth_type") != "local" {
		t.Fatalf("auth_from/auth_type 不正确: %v", values)
	}
	// 授权页要求 machine_id/device_id 同时以 x_ 前缀再带一份。
	if values.Get("machine_id") != "m1" || values.Get("x_machine_id") != "m1" {
		t.Fatalf("machine_id 应出现两次: %v", values)
	}
}

// TestMachineTraceID 复刻官方：拼接后取末 16 位，不足左侧补 0。
func TestMachineTraceID(t *testing.T) {
	if got := machineTraceID("0123456789abcdef", "ffff"); got != "456789abcdefffff" {
		t.Fatalf("末 16 位应为 456789abcdefffff，得到 %q", got)
	}
	if got := machineTraceID("m", "d"); got != "00000000000000md" {
		t.Fatalf("不足应左侧补 0，得到 %q", got)
	}
}

// TestTraeBeginLoginRequiresCredentials 验证未配置客户端标识时登录明确失败，
// 且报错带出可操作的配置位置（Admin API 会把它作为 502 文本暴露）。
func TestTraeBeginLoginRequiresCredentials(t *testing.T) {
	clearTraeCredentialEnv(t)
	c := NewTraeChannel(t.TempDir())
	if c.configured() {
		t.Fatalf("无环境变量与文件时 configured 应为 false")
	}
	_, err := c.BeginLogin(context.Background())
	if err == nil {
		t.Fatalf("缺少客户端标识时 BeginLogin 应报错")
	}
	if !strings.Contains(err.Error(), "TRAE_CHANNEL_CLIENT_ID") ||
		!strings.Contains(err.Error(), traeClientCredentialFile) {
		t.Fatalf("报错应给出配置指引，得到 %v", err)
	}
}

// TestTraeCredentialsFromEnv 验证环境变量优先，且解析结果落到渠道上。
func TestTraeCredentialsFromEnv(t *testing.T) {
	clearTraeCredentialEnv(t)
	t.Setenv("TRAE_CHANNEL_CLIENT_ID", "env-id")
	t.Setenv("TRAE_CHANNEL_APP_ID", "env-app")

	c := NewTraeChannel(t.TempDir())
	if c.clientID != "env-id" || c.appID != "env-app" {
		t.Fatalf("应从环境变量取标识，得到 %q/%q", c.clientID, c.appID)
	}
}

// TestTraeCredentialsFromFile 验证环境变量缺省时回退到 <gatewayHome>/trae-client.json。
// dir 是 <home>/channels，标识文件放在其上一级。
func TestTraeCredentialsFromFile(t *testing.T) {
	clearTraeCredentialEnv(t)
	home := t.TempDir()
	dir := filepath.Join(home, "channels")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatalf("建目录: %v", err)
	}
	payload := []byte(`{"client_id":"file-id","app_id":"file-app"}`)
	if err := os.WriteFile(filepath.Join(home, traeClientCredentialFile), payload, 0600); err != nil {
		t.Fatalf("写标识文件: %v", err)
	}

	c := NewTraeChannel(dir)
	if c.clientID != "file-id" || c.appID != "file-app" {
		t.Fatalf("应从本地文件取标识，得到 %q/%q", c.clientID, c.appID)
	}
}

// TestTraeCallbackServerFallsBackOnBusyPort 验证首选端口被占用时退回系统分配端口。
// 用系统分配的端口占位，避免依赖 18080 在测试机上空闲。
func TestTraeCallbackServerFallsBackOnBusyPort(t *testing.T) {
	busy, err := newTraeCallbackServer(0)
	if err != nil {
		t.Fatalf("占位监听失败: %v", err)
	}
	defer busy.close()

	server, err := newTraeCallbackServer(busy.port())
	if err != nil {
		t.Fatalf("端口占用时应退回系统分配，得到 %v", err)
	}
	defer server.close()

	if server.port() == busy.port() {
		t.Fatalf("应换用其它端口，实际仍是 %d", server.port())
	}
	if !strings.HasPrefix(server.callbackURL(), "http://127.0.0.1:") {
		t.Fatalf("回调地址应是回环地址: %s", server.callbackURL())
	}
}

// TestTraeLoginLoopbackPersists 跑完整登录：授权地址 → 回环回调 → ExchangeToken →
// GetUserInfo → 凭据落盘。
func TestTraeLoginLoopbackPersists(t *testing.T) {
	var exchangeBody map[string]any
	var userInfoToken string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cloudide/api/v3/trae/oauth/ExchangeToken":
			_ = json.NewDecoder(r.Body).Decode(&exchangeBody)
			writeJSON(w, map[string]any{"Result": map[string]any{
				"Token":               "at-1",
				"RefreshToken":        "rt-1",
				"TokenExpireDuration": 3600,
			}})
		case "/cloudide/api/v3/trae/GetUserInfo":
			userInfoToken = r.Header.Get("X-Cloudide-Token")
			writeJSON(w, map[string]any{"Result": map[string]any{
				"UserID":             "u-9",
				"ScreenName":         "张三",
				"TenantID":           "t-1",
				"NonPlainTextMobile": "138****8888",
				"NonPlainTextEmail":  "a***@example.com",
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	c := traeTestChannel(t)
	c.oauthHost = server.URL

	session, err := c.BeginLogin(context.Background())
	if err != nil {
		t.Fatalf("BeginLogin: %v", err)
	}
	callback := traeCallbackURL(t, session.DeviceCode().VerificationURIComplete)
	if !strings.HasSuffix(callback, traeCallbackPath) {
		t.Fatalf("回调地址路径应为 %s，得到 %s", traeCallbackPath, callback)
	}

	type loginResult struct {
		cred *Credential
		err  error
	}
	resultCh := make(chan loginResult, 1)
	go func() {
		cred, err := session.Wait(context.Background())
		resultCh <- loginResult{cred, err}
	}()
	time.Sleep(50 * time.Millisecond)

	// 回调携带 refreshToken + userInfo + userJwt（官方授权页的形态）。
	userInfo := url.QueryEscape(`{"UserID":"u-9","ScreenName":"张三","TenantID":"t-1"}`)
	userJWT := url.QueryEscape(`{"Token":"jwt-token","RefreshToken":"jwt-refresh"}`)
	resp, err := http.Get(callback + "?refreshToken=rt-0&userInfo=" + userInfo + "&userJwt=" + userJWT)
	if err != nil {
		t.Fatalf("回调请求失败: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("回调应返回 200，得到 %d", resp.StatusCode)
	}

	res := <-resultCh
	if res.err != nil {
		t.Fatalf("Wait: %v", res.err)
	}
	if res.cred.AccessToken != "at-1" || res.cred.RefreshToken != "rt-1" {
		t.Fatalf("换令牌结果不正确: %+v", res.cred)
	}
	if res.cred.AccountID != "u-9" {
		t.Fatalf("AccountID 应为 GetUserInfo 的 UserID，得到 %q", res.cred.AccountID)
	}
	if exchangeBody["ClientID"] != "test-client-id" || exchangeBody["RefreshToken"] != "rt-0" {
		t.Fatalf("ExchangeToken 请求体不正确: %v", exchangeBody)
	}
	if userInfoToken != "at-1" {
		t.Fatalf("GetUserInfo 应带新令牌，得到 %q", userInfoToken)
	}
	if res.cred.Extra["machine_id"] == "" || res.cred.Extra["device_id"] == "" {
		t.Fatalf("凭据应记录 machine_id/device_id: %v", res.cred.Extra)
	}

	creds := c.Accounts().Credentials()
	if len(creds) != 1 {
		t.Fatalf("应有 1 条凭据，得到 %d", len(creds))
	}
	// 展示名手机号优先（服务端 ScreenName 是自动生成的默认名，多账号无法区分）。
	if creds[0].Nickname != "138****8888" {
		t.Fatalf("展示名应优先取手机号，得到 %q", creds[0].Nickname)
	}
	if creds[0].Expired {
		t.Fatalf("凭据不应立即过期: %+v", creds[0])
	}
}

// TestTraeLoginRetriesAfterExchangeFailure 验证换令牌失败时回 502 并继续等下一次
// 回调（用户刷新授权页即可重试，不必重新发起登录）。
func TestTraeLoginRetriesAfterExchangeFailure(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cloudide/api/v3/trae/oauth/ExchangeToken":
			calls++
			if calls == 1 {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"message":"boom"}`))
				return
			}
			writeJSON(w, map[string]any{"Result": map[string]any{"Token": "at-2", "RefreshToken": "rt-2"}})
		case "/cloudide/api/v3/trae/GetUserInfo":
			writeJSON(w, map[string]any{"Result": map[string]any{"UserID": "u-2"}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	c := traeTestChannel(t)
	c.oauthHost = server.URL

	session, err := c.BeginLogin(context.Background())
	if err != nil {
		t.Fatalf("BeginLogin: %v", err)
	}
	callback := traeCallbackURL(t, session.DeviceCode().VerificationURIComplete)

	resultCh := make(chan error, 1)
	go func() {
		_, err := session.Wait(context.Background())
		resultCh <- err
	}()
	time.Sleep(50 * time.Millisecond)

	first, err := http.Get(callback + "?refreshToken=rt-0")
	if err != nil {
		t.Fatalf("首次回调失败: %v", err)
	}
	first.Body.Close()
	if first.StatusCode != http.StatusBadGateway {
		t.Fatalf("换令牌失败应回 502，得到 %d", first.StatusCode)
	}

	second, err := http.Get(callback + "?refreshToken=rt-0")
	if err != nil {
		t.Fatalf("二次回调失败: %v", err)
	}
	second.Body.Close()
	if second.StatusCode != http.StatusOK {
		t.Fatalf("重试应成功并回 200，得到 %d", second.StatusCode)
	}
	if err := <-resultCh; err != nil {
		t.Fatalf("Wait: %v", err)
	}
}

// TestParseTraeCallbackRejectsPKCE 验证回调里只有 PKCE 授权码时明确报错，
// 而不是拿 code 去换（Trae 没有 PKCE 生成逻辑，该流程本实现不支持）。
func TestParseTraeCallbackRejectsPKCE(t *testing.T) {
	_, err := parseTraeCallback("/authorize?code=abc&state=")
	if err == nil || !strings.Contains(err.Error(), "PKCE") {
		t.Fatalf("应报不支持 PKCE 流程，得到 %v", err)
	}
	_, err = parseTraeCallback("/authorize?authCodeInfo=" + url.QueryEscape(`{"code":"abc"}`))
	if err == nil || !strings.Contains(err.Error(), "PKCE") {
		t.Fatalf("authCodeInfo 形态也应被拒，得到 %v", err)
	}
}

// TestParseTraeCallbackPrefersRefreshToken 验证同时带 refreshToken 与 code 时
// refreshToken 优先（不触发 PKCE 拒绝）。
func TestParseTraeCallbackPrefersRefreshToken(t *testing.T) {
	info, err := parseTraeCallback("/authorize?refreshToken=rt-1&code=abc")
	if err != nil {
		t.Fatalf("parseTraeCallback: %v", err)
	}
	if info.RefreshToken != "rt-1" {
		t.Fatalf("应优先取 refreshToken，得到 %q", info.RefreshToken)
	}
}

// TestParseTraeCallbackFallsBackToJWTToken 验证无 refreshToken 时用 userJwt.Token 兜底。
func TestParseTraeCallbackFallsBackToJWTToken(t *testing.T) {
	userJWT := url.QueryEscape(`{"Token":"jwt-at"}`)
	info, err := parseTraeCallback("/authorize?userJwt=" + userJWT)
	if err != nil {
		t.Fatalf("parseTraeCallback: %v", err)
	}
	if info.AccessToken != "jwt-at" || info.RefreshToken != "" {
		t.Fatalf("应以 userJwt.Token 作为访问令牌，得到 %+v", info)
	}
}

// TestParseTraeCallbackRequiresSomething 验证三种凭据都没有时明确报错。
func TestParseTraeCallbackRequiresSomething(t *testing.T) {
	if _, err := parseTraeCallback("/authorize?foo=1"); err == nil {
		t.Fatalf("没有任何凭据时应报错")
	}
}

// TestFixTraeNicknameMojibake 覆盖昵称乱码修复：latin1 误解的 UTF-8 还原，
// 还原不出且不含中文时退回到「用户+uid 后四位」。
func TestFixTraeNicknameMojibake(t *testing.T) {
	// "张三" 的 UTF-8 字节被按 latin1 解读后的形态。
	mangled := string([]rune{0xE5, 0xBC, 0xA0, 0xE4, 0xB8, 0x89})
	if got := fixTraeNicknameMojibake(mangled, "u-1"); got != "张三" {
		t.Fatalf("应还原成「张三」，得到 %q", got)
	}
	// 无法还原且不含中文 → 退回占位名。
	if got := fixTraeNicknameMojibake("\u00ff\u00fe", "12345678"); got != "用户5678" {
		t.Fatalf("应退回「用户+后四位」，得到 %q", got)
	}
	if got := fixTraeNicknameMojibake("", "u-1"); got != "" {
		t.Fatalf("空值应原样返回，得到 %q", got)
	}
}

// TestParseTraeExchangeResponse 锁住 ExchangeToken 响应的大小写兼容与缺 Token 判定。
func TestParseTraeExchangeResponse(t *testing.T) {
	lower := parseTraeExchangeResponse(map[string]any{"result": map[string]any{
		"accessToken":         "at",
		"refreshToken":        "rt",
		"tokenExpireDuration": 60,
	}})
	if lower == nil || lower.AccessToken != "at" || lower.RefreshToken != "rt" {
		t.Fatalf("小写字段应被识别: %+v", lower)
	}
	upper := parseTraeExchangeResponse(map[string]any{"Result": map[string]any{
		"Token": "at2", "TokenExpireAt": 1.7e12,
	}})
	if upper == nil || upper.AccessToken != "at2" || upper.TokenExpireAt != 1.7e12 {
		t.Fatalf("大写字段应被识别: %+v", upper)
	}
	if parseTraeExchangeResponse(map[string]any{"Result": map[string]any{"foo": "bar"}}) != nil {
		t.Fatalf("缺 Token 时应返回 nil")
	}
	if parseTraeExchangeResponse(nil) != nil {
		t.Fatalf("空响应应返回 nil")
	}
}

// TestParseTraeUserInfoResponse 验证脱敏手机号/邮箱字段被解析，uid 缺失时判失败。
func TestParseTraeUserInfoResponse(t *testing.T) {
	info := parseTraeUserInfoResponse(map[string]any{"Result": map[string]any{
		"UserID":             "u-1",
		"ScreenName":         "名字",
		"NonPlainTextMobile": "138****8888",
		"NonPlainTextEmail":  "a***@example.com",
	}})
	if info == nil || info.UID != "u-1" || info.Phone != "138****8888" || info.Email != "a***@example.com" {
		t.Fatalf("脱敏字段应被解析: %+v", info)
	}
	if parseTraeUserInfoResponse(map[string]any{"Result": map[string]any{"ScreenName": "x"}}) != nil {
		t.Fatalf("缺 uid 时应返回 nil")
	}
}

// TestTraeRefreshCredential 验证续期成功时令牌与过期时间被更新，refresh_token
// 轮换时以新值为准。
func TestTraeRefreshCredential(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/cloudide/api/v3/trae/oauth/ExchangeToken" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		writeJSON(w, map[string]any{"Result": map[string]any{
			"Token": "at-new", "RefreshToken": "rt-new", "TokenExpireDuration": 7200,
		}})
	}))
	defer server.Close()

	c := traeTestChannel(t)
	c.oauthHost = server.URL

	next, err := c.refreshCredential(context.Background(), Credential{
		AccessToken:  "at-old",
		RefreshToken: "rt-old",
		AccountID:    "u-1",
		Nickname:     "名字",
		Extra:        map[string]string{"uid": "u-1"},
	})
	if err != nil {
		t.Fatalf("refreshCredential: %v", err)
	}
	if next.AccessToken != "at-new" || next.RefreshToken != "rt-new" {
		t.Fatalf("应换到新令牌: %+v", next)
	}
	if next.AccountID != "u-1" || next.Nickname != "名字" || next.Extra["uid"] != "u-1" {
		t.Fatalf("身份字段应沿用旧值: %+v", next)
	}
	if !next.ExpiresAt.After(time.Now().Add(time.Hour)) {
		t.Fatalf("过期时间应按 TokenExpireDuration 前推: %v", next.ExpiresAt)
	}
	if body["RefreshToken"] != "rt-old" {
		t.Fatalf("续期请求应带旧 refresh_token: %v", body)
	}
}

// TestTraeRefreshCredentialTerminal 验证续期在 401/403、命中会话失效、以及
// 「2xx 却没换来 Token」时判定为终态失效，包成 ErrLoginCancelled 让账号池换号。
func TestTraeRefreshCredentialTerminal(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
	}{
		{"unauthorized", http.StatusUnauthorized, `{"message":"unauthorized"}`},
		{"forbidden", http.StatusForbidden, `{"message":"forbidden"}`},
		{"success-without-token", http.StatusOK, `{"Result":{}}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(c.status)
				_, _ = w.Write([]byte(c.body))
			}))
			defer server.Close()

			ch := traeTestChannel(t)
			ch.oauthHost = server.URL
			_, err := ch.refreshCredential(context.Background(), Credential{
				AccessToken:  "at-old",
				RefreshToken: "rt-old",
			})
			if !errors.Is(err, ErrLoginCancelled) {
				t.Fatalf("应判为终态失效，得到 %v", err)
			}
		})
	}
}

// TestTraeRefreshCredentialRetryable 验证网络/服务端错误不判终态（交由上层重试）。
func TestTraeRefreshCredentialRetryable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"message":"upstream boom"}`))
	}))
	defer server.Close()

	c := traeTestChannel(t)
	c.oauthHost = server.URL
	_, err := c.refreshCredential(context.Background(), Credential{RefreshToken: "rt"})
	if err == nil || errors.Is(err, ErrLoginCancelled) {
		t.Fatalf("5xx 应可重试，得到 %v", err)
	}
}

// TestTraeRefreshCredentialWithoutRefreshToken 验证缺 refresh_token 时直接判终态。
func TestTraeRefreshCredentialWithoutRefreshToken(t *testing.T) {
	c := traeTestChannel(t)
	_, err := c.refreshCredential(context.Background(), Credential{AccessToken: "at"})
	if !errors.Is(err, ErrLoginCancelled) {
		t.Fatalf("缺 refresh_token 应判终态，得到 %v", err)
	}
}

// TestTraeExpiresAt 覆盖过期时间的四种推导：绝对毫秒、绝对秒、相对秒数、JWT exp。
func TestTraeExpiresAt(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)

	if got := traeExpiresAt(1.7e12, 0, "", now); got.UnixMilli() != 1_700_000_000_000 {
		t.Fatalf("绝对毫秒解析错误: %v", got)
	}
	if got := traeExpiresAt(1_700_000_000, 0, "", now); got.UnixMilli() != 1_700_000_000_000 {
		t.Fatalf("绝对秒解析错误: %v", got)
	}
	// TokenExpireDuration 的单位是秒（官方 nowMs + duration * 1e3）。
	if got := traeExpiresAt(0, 3600, "", now); got.UnixMilli() != now.Add(time.Hour).UnixMilli() {
		t.Fatalf("相对秒数应基于当前时间前推 1 小时: %v", got)
	}
	// 两段式 JWT 也要能解出 exp（官方只要求「至少两段」）。
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"exp":1700000000}`))
	if got := traeExpiresAt(0, 0, "header."+payload, now); got.UnixMilli() != 1_700_000_000_000 {
		t.Fatalf("JWT exp 解析错误: %v", got)
	}
	if got := traeExpiresAt(0, 0, "", now); !got.IsZero() {
		t.Fatalf("无从推导时应返回零值: %v", got)
	}
}

// TestClassifyTraeError 锁住错误分类的判定顺序。
func TestClassifyTraeError(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{"plan-limit", 200, `{"code":1005,"message":"plan limit reached"}`, "hard-plan"},
		{"quota", 200, `{"code":4008,"message":"quota exceeded"}`, "quota-exceeded"},
		{"soft-rate-code", 200, `{"code":4011,"message":"slow down"}`, "soft-rate"},
		{"unauthorized", 401, `{"message":"unauthorized"}`, "session-dead"},
		{"too-many", 429, `{}`, "soft-rate"},
		{"not-found", 404, `{}`, "not-found"},
		{"server", 503, `{}`, "server"},
		{"client", 400, `{}`, "client"},
		{"none", 200, `{}`, "none"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := classifyTraeError(c.status, c.body); got != c.want {
				t.Fatalf("classifyTraeError(%d, %s) = %q，期望 %q", c.status, c.body, got, c.want)
			}
		})
	}
}

// TestTraeIdentityHeaders 验证交给 provider 的身份头：鉴权必须是
// Authorization: Cloud-IDE-JWT（不是 Bearer），且带上账号级字段。
func TestTraeIdentityHeaders(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "channels")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatalf("建目录: %v", err)
	}
	writeCreds(t, NewStore(dir), traeChannel, Credential{
		AccessToken: "tok-1",
		AccountID:   "u-1",
		Extra: map[string]string{
			"uid":        "u-1",
			"machine_id": "machine-1",
			"device_id":  "device-1",
		},
	})

	clearTraeCredentialEnv(t)
	c := NewTraeChannel(dir)
	c.appID = "app-1"

	headers := c.AuthSource().TraeIdentityHeaders()
	if headers["Authorization"] != "Cloud-IDE-JWT tok-1" {
		t.Fatalf("鉴权头应为 Cloud-IDE-JWT，得到 %q", headers["Authorization"])
	}
	for name, want := range map[string]string{
		"X-Cloudide-Token": "tok-1",
		"X-Ide-Token":      "tok-1",
		"X-Uid":            "u-1",
		"X-Machine-Id":     "machine-1",
		"X-Device-Id":      "device-1",
		"X-App-Id":         "app-1",
	} {
		if headers[name] != want {
			t.Fatalf("身份头 %s 应为 %q，得到 %q", name, want, headers[name])
		}
	}
	// 未登录时不应产出鉴权头。
	empty := NewTraeChannel(t.TempDir())
	if _, ok := empty.AuthSource().TraeIdentityHeaders()["Authorization"]; ok {
		t.Fatalf("未登录时不应有 Authorization 头")
	}
}
