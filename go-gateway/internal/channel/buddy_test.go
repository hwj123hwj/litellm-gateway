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

// buddy 渠道测试：覆盖 state → 轮询令牌 → 轮询账号的完整登录、两次轮询对
// 11217/12151 这类「未就绪」码的重试、续期请求头/请求体形态、命名空间前缀剥离、
// 以及 WorkBuddy 按模型家族解析 User-Agent。
// 推理链路的鉴权头与复刻头由 provider_integration_test.go 覆盖。

// buddyStep 描述一个轮询步骤的假响应序列：前几次未就绪，之后给出 data。
type buddyStep struct {
	notReadyCode int
	notReady     int
	data         map[string]any
}

// newBuddyUpstream 起一个假上游，按 paths 处理 buddy 协议的三个端点。
// 返回的计数器让测试能断言重试次数。
func newBuddyUpstream(t *testing.T, state string, token, account buddyStep) (*httptest.Server, *map[string]int) {
	t.Helper()
	hits := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case buddyAuthStatePath:
			hits["state"]++
			writeJSON(w, map[string]any{
				"code": 0,
				"data": map[string]any{"state": state, "authUrl": "https://example.com/login?x=1"},
			})
		case buddyAuthTokenPath:
			hits["token"]++
			if hits["token"] <= token.notReady {
				w.WriteHeader(http.StatusBadRequest)
				writeJSON(w, map[string]any{"code": token.notReadyCode, "message": "token not ready"})
				return
			}
			writeJSON(w, map[string]any{"code": 0, "data": token.data})
		case buddyLoginAccountPath:
			hits["account"]++
			if hits["account"] <= account.notReady {
				w.WriteHeader(http.StatusBadRequest)
				writeJSON(w, map[string]any{"code": account.notReadyCode, "message": "account not ready"})
				return
			}
			writeJSON(w, map[string]any{"code": 0, "data": account.data})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server, &hits
}

// newBuddyForTest 造一个端点指向假上游、轮询间隔极短的渠道。
func newBuddyForTest(t *testing.T, endpoint, name string) *BuddyChannel {
	t.Helper()
	c := newBuddyChannel(t.TempDir(), name)
	c.endpoint = endpoint
	return c
}

// TestBuddyLoginPollsUntilReadyAndPersists 跑完整登录：取 state → 令牌轮询（先撞
// 一次 11217 未就绪）→ 账号轮询（先撞一次 12151）→ 凭据落盘。
func TestBuddyLoginPollsUntilReadyAndPersists(t *testing.T) {
	defer restoreBuddyPollInterval()
	buddyPollInterval = time.Millisecond

	server, hits := newBuddyUpstream(t, "st-1",
		buddyStep{notReadyCode: buddyTokenNotReadyCode, notReady: 1, data: map[string]any{
			"accessToken":  "tok-1",
			"refreshToken": "ref-1",
			"expiresIn":    3600,
			"tokenType":    "Bearer",
			"domain":       "copilot.tencent.com",
		}},
		buddyStep{notReadyCode: buddyAccountNotReadyCode, notReady: 1, data: map[string]any{
			"uid": "u-7", "nickname": "小明", "enterpriseId": "ent-9", "type": "enterprise",
		}},
	)

	c := newBuddyForTest(t, server.URL, buddyChannelCodeBuddy)
	session, err := c.BeginLogin(context.Background())
	if err != nil {
		t.Fatalf("BeginLogin: %v", err)
	}
	dc := session.DeviceCode()
	if dc.VerificationURIComplete == "" {
		t.Fatalf("登录会话应提供 login_url")
	}
	if _, err := url.Parse(dc.VerificationURIComplete); err != nil {
		t.Fatalf("login_url 不是合法地址: %v", err)
	}

	cred, err := session.Wait(context.Background())
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if cred.AccessToken != "tok-1" || cred.RefreshToken != "ref-1" {
		t.Fatalf("凭据令牌不正确: %+v", cred)
	}
	if cred.AccountID != "u-7" || cred.Nickname != "小明" {
		t.Fatalf("账号信息未落盘: %+v", cred)
	}
	if cred.Extra["enterprise_id"] != "ent-9" || cred.Extra["account_type"] != "enterprise" {
		t.Fatalf("企业信息未落进 Extra: %+v", cred.Extra)
	}
	if cred.Extra["domain"] != "copilot.tencent.com" {
		t.Fatalf("domain 未落进 Extra: %+v", cred.Extra)
	}
	// 未就绪码必须被当成「继续等」而不是失败：各多打一次才说明重试生效。
	if (*hits)["token"] != 2 || (*hits)["account"] != 2 {
		t.Fatalf("未就绪码应触发重试，令牌 %d 次、账号 %d 次", (*hits)["token"], (*hits)["account"])
	}
	if c.Accounts().Len() != 1 {
		t.Fatalf("凭据应写入账号池，实际 %d 条", c.Accounts().Len())
	}
}

// TestBuddyLoginFailsOnUnexpectedCode 验证非「未就绪」的业务码直接失败，
// 不会白等到超时（否则用户拿到的是一个 5 分钟不报错的登录）。
func TestBuddyLoginFailsOnUnexpectedCode(t *testing.T) {
	defer restoreBuddyPollInterval()
	buddyPollInterval = time.Millisecond

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case buddyAuthStatePath:
			writeJSON(w, map[string]any{"code": 0, "data": map[string]any{"state": "st", "authUrl": "https://example.com/l"}})
		case buddyAuthTokenPath:
			w.WriteHeader(http.StatusForbidden)
			writeJSON(w, map[string]any{"code": 40001, "message": "denied"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	c := newBuddyForTest(t, server.URL, buddyChannelCodeBuddy)
	session, err := c.BeginLogin(context.Background())
	if err != nil {
		t.Fatalf("BeginLogin: %v", err)
	}
	if _, err := session.Wait(context.Background()); err == nil {
		t.Fatalf("非未就绪码应让登录失败")
	}
}

// TestBuddyBeginLoginRequiresStateAndAuthURL 验证登录状态响应缺字段时报错而不是
// 返回一个无法打开的登录地址。
func TestBuddyBeginLoginRequiresStateAndAuthURL(t *testing.T) {
	for _, body := range []map[string]any{
		{"code": 0, "data": map[string]any{"authUrl": "https://example.com/l"}},
		{"code": 0, "data": map[string]any{"state": "st"}},
		{"code": 0},
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, body)
		}))
		c := newBuddyForTest(t, server.URL, buddyChannelCodeBuddy)
		if _, err := c.BeginLogin(context.Background()); err == nil {
			server.Close()
			t.Fatalf("响应 %v 应导致 BeginLogin 报错", body)
		}
		server.Close()
	}
}

// TestBuddyDecorateLoginURLPerProduct 验证只有国际版会给登录地址补
// version/loginSessionId（中国版补了会被服务端当异常请求）。
func TestBuddyDecorateLoginURLPerProduct(t *testing.T) {
	original := "https://example.com/login?x=1"

	code := newBuddyForTest(t, "http://127.0.0.1:1", buddyChannelCodeBuddy)
	if got := code.decorateLoginURL(original); got != original {
		t.Fatalf("CodeBuddy 不应改写登录地址，得到 %q", got)
	}

	work := newBuddyForTest(t, "http://127.0.0.1:1", buddyChannelWorkBuddy)
	got := work.decorateLoginURL(original)
	parsed, err := url.Parse(got)
	if err != nil {
		t.Fatalf("改写后的地址不合法: %v", err)
	}
	if parsed.Query().Get("x") != "1" {
		t.Fatalf("原有查询参数应保留: %q", got)
	}
	if parsed.Query().Get("version") != "5.5.2" {
		t.Fatalf("应补 version=5.5.2: %q", got)
	}
	if parsed.Query().Get("loginSessionId") == "" {
		t.Fatalf("应补 loginSessionId: %q", got)
	}
}

// TestBuddyRefreshCredentialRequestShape 验证续期请求的头与体：必须带
// X-Refresh-Token、X-Auth-Refresh-Source、Bearer 旧令牌，并按产品覆盖 X-Domain 与
// UA（漏掉这几项时 WorkBuddy 会以 CodeBuddy 身份续期而被拒）。
func TestBuddyRefreshCredentialRequestShape(t *testing.T) {
	var gotHeaders http.Header
	var gotBody string
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotHeaders = r.Header.Clone()
		buf := make([]byte, 1024)
		n, _ := r.Body.Read(buf)
		gotBody = string(buf[:n])
		writeJSON(w, map[string]any{"code": 0, "data": map[string]any{
			"accessToken": "new-tok", "expiresIn": 3600, "domain": "www.workbuddy.ai",
		}})
	}))
	defer server.Close()

	c := newBuddyForTest(t, server.URL, buddyChannelWorkBuddy)
	next, err := c.refreshCredential(context.Background(), Credential{
		AccessToken:  "old-tok",
		RefreshToken: "ref-1",
		Extra:        map[string]string{"enterprise_id": "ent-1"},
	})
	if err != nil {
		t.Fatalf("refreshCredential: %v", err)
	}
	if gotPath != buddyAuthRefreshPath {
		t.Fatalf("续期应打到 %s，实际 %s", buddyAuthRefreshPath, gotPath)
	}
	if got := gotHeaders.Get("Authorization"); got != "Bearer old-tok" {
		t.Fatalf("续期应带旧 access_token，得到 %q", got)
	}
	if got := gotHeaders.Get(buddyHeaderRefreshToken); got != "ref-1" {
		t.Fatalf("%s 应为 refresh_token，得到 %q", buddyHeaderRefreshToken, got)
	}
	if got := gotHeaders.Get(buddyHeaderRefreshSource); got != buddyRefreshSource {
		t.Fatalf("%s 应为 %q，得到 %q", buddyHeaderRefreshSource, buddyRefreshSource, got)
	}
	if got := gotHeaders.Get(buddyHeaderDomain); got != "www.workbuddy.ai" {
		t.Fatalf("WorkBuddy 续期的 X-Domain 应按产品覆盖，得到 %q", got)
	}
	if got := gotHeaders.Get("User-Agent"); got != buddyWorkBuddyUAIntl {
		t.Fatalf("WorkBuddy 续期的 UA 应按产品覆盖，得到 %q", got)
	}
	// 企业账号必须带上企业/租户标识，否则会被当个人账号处理。
	if got := gotHeaders.Get(buddyHeaderEnterpriseID); got != "ent-1" {
		t.Fatalf("企业账号续期应带 X-Enterprise-Id，得到 %q", got)
	}
	if got := gotHeaders.Get(buddyHeaderTenantID); got != "ent-1" {
		t.Fatalf("企业账号续期应带 X-Tenant-Id，得到 %q", got)
	}
	if strings.TrimSpace(gotBody) != "{}" {
		t.Fatalf("续期请求体应为空对象，得到 %q", gotBody)
	}
	// 续期响应不回传 refresh_token，应沿用旧值。
	if next.AccessToken != "new-tok" || next.RefreshToken != "ref-1" {
		t.Fatalf("续期后令牌不正确: %+v", next)
	}
	if next.AccountID != "" && next.Extra["enterprise_id"] != "ent-1" {
		t.Fatalf("续期应保留企业标识: %+v", next.Extra)
	}
}

// TestBuddyRefreshCredentialWithoutToken 验证没有 refresh_token 时直接失败，
// 不发无意义的请求。
func TestBuddyRefreshCredentialWithoutToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("无 refresh_token 时不应触达上游")
	}))
	defer server.Close()

	c := newBuddyForTest(t, server.URL, buddyChannelCodeBuddy)
	if _, err := c.refreshCredential(context.Background(), Credential{AccessToken: "x"}); err == nil {
		t.Fatalf("缺 refresh_token 应报错")
	}
}

// TestBuddyRefreshTerminalClassification 验证「终态」判定：401/403 或消息里含
// expired/invalid 视为 refresh_token 已失效（此时账号池应换下一个账号，而不是重试）。
func TestBuddyRefreshTerminalClassification(t *testing.T) {
	cases := []struct {
		status   int
		code     int
		message  string
		terminal bool
	}{
		{http.StatusUnauthorized, 0, "", true},
		{http.StatusForbidden, 0, "", true},
		{http.StatusOK, 401, "", true},
		{http.StatusOK, 403, "", true},
		{http.StatusOK, 1234, "refresh token expired", true},
		{http.StatusOK, 1234, "invalid refresh_token", true},
		{http.StatusOK, 1234, "service busy", false},
		{http.StatusInternalServerError, 1234, "oops", false},
	}
	for _, tc := range cases {
		if got := buddyRefreshTerminal(tc.status, tc.code, tc.message); got != tc.terminal {
			t.Fatalf("buddyRefreshTerminal(%d,%d,%q) 应为 %v，得到 %v", tc.status, tc.code, tc.message, tc.terminal, got)
		}
	}
}

// TestParseBuddyTokenExpirySources 覆盖过期时间的三条来源：绝对秒/毫秒、相对秒
// 加 JWT iat 基准、以及两者都缺时回退到 JWT 的 exp 声明。
func TestParseBuddyTokenExpirySources(t *testing.T) {
	now := time.Now()

	absolute, err := parseBuddyToken(map[string]any{"accessToken": "t", "expiresAt": "1893456000"}, now)
	if err != nil {
		t.Fatalf("parseBuddyToken: %v", err)
	}
	if want := time.Unix(1893456000, 0); !absolute.ExpiresAt.Equal(want) {
		t.Fatalf("绝对秒值解析错误: %v", absolute.ExpiresAt)
	}

	millis, err := parseBuddyToken(map[string]any{"accessToken": "t", "expiresAt": "1893456000000"}, now)
	if err != nil {
		t.Fatalf("parseBuddyToken: %v", err)
	}
	if want := time.UnixMilli(1893456000000); !millis.ExpiresAt.Equal(want) {
		t.Fatalf("绝对毫秒值解析错误: %v", millis.ExpiresAt)
	}

	// 相对值以 JWT 的 iat 为基准，而不是当前时间：重放同一令牌结果稳定。
	iat := time.Unix(1700000000, 0)
	token := makeJWT(t, map[string]any{"iat": float64(1700000000), "exp": float64(1900000000)})
	relative, err := parseBuddyToken(map[string]any{"accessToken": token, "expiresIn": float64(3600)}, now)
	if err != nil {
		t.Fatalf("parseBuddyToken: %v", err)
	}
	if want := iat.Add(time.Hour); !relative.ExpiresAt.Equal(want) {
		t.Fatalf("相对值应以 iat 为基准，want %v got %v", want, relative.ExpiresAt)
	}

	// 绝对与相对都缺时回退到 JWT exp。
	fallback, err := parseBuddyToken(map[string]any{"accessToken": token}, now)
	if err != nil {
		t.Fatalf("parseBuddyToken: %v", err)
	}
	if want := time.Unix(1900000000, 0); !fallback.ExpiresAt.Equal(want) {
		t.Fatalf("应回退到 JWT exp，want %v got %v", want, fallback.ExpiresAt)
	}

	if _, err := parseBuddyToken(map[string]any{"refreshToken": "r"}, now); err == nil {
		t.Fatalf("缺 accessToken 应报错")
	}
}

// TestBuildBuddyCredentialFallsBackToJWT 验证账号响应缺字段时从 JWT 声明兜底
// （上游有时只回令牌不回账号信息）。
func TestBuildBuddyCredentialFallsBackToJWT(t *testing.T) {
	token := makeJWT(t, map[string]any{"sub": "uid-jwt", "nickname": "jwt-user"})
	cred := buildBuddyCredential(&buddyToken{
		AccessToken: token, RefreshToken: "r", ExpiresAt: time.Now().Add(time.Hour),
		TokenType: "Bearer",
	}, &buddyAccount{AccountType: "personal"})

	if cred.AccountID != "uid-jwt" {
		t.Fatalf("AccountID 应从 JWT sub 兜底，得到 %q", cred.AccountID)
	}
	if cred.Nickname != "jwt-user" {
		t.Fatalf("Nickname 应从 JWT 兜底，得到 %q", cred.Nickname)
	}
}

// TestBuddyTransformRequestStripsNamespace 验证对外模型名的渠道前缀在发往上游前
// 被剥掉；不带前缀或结构异常时原样透传。
func TestBuddyTransformRequestStripsNamespace(t *testing.T) {
	work := newBuddyForTest(t, "http://127.0.0.1:1", buddyChannelWorkBuddy)

	out, err := work.TransformRequest([]byte(`{"model":"workbuddy/gpt-5.6-sol","stream":true}`))
	if err != nil {
		t.Fatalf("TransformRequest: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatalf("改写后不是合法 JSON: %v", err)
	}
	if payload["model"] != "gpt-5.6-sol" {
		t.Fatalf("前缀应被剥掉，得到 %v", payload["model"])
	}
	if payload["stream"] != true {
		t.Fatalf("其余字段应原样保留: %v", payload)
	}

	// 不带前缀（例如直连上游调试）时不改写。
	plain := []byte(`{"model":"glm-5.3","stream":true}`)
	out, err = work.TransformRequest(plain)
	if err != nil {
		t.Fatalf("TransformRequest: %v", err)
	}
	if string(out) != string(plain) {
		t.Fatalf("无前缀时不应改写，得到 %s", out)
	}

	// 结构异常时透传，交给上游报错。
	broken := []byte(`{"model":`)
	if out, err := work.TransformRequest(broken); err != nil || string(out) != string(broken) {
		t.Fatalf("结构异常时应原样透传，得到 %s err=%v", out, err)
	}
}

// TestBuddyClientHeadersUserAgentPerModelFamily 验证 WorkBuddy 的 UA 按模型家族
// 切换、CodeBuddy 恒用 IDE UA，且入参带渠道前缀时仍能命中规则。
func TestBuddyClientHeadersUserAgentPerModelFamily(t *testing.T) {
	work := newBuddyForTest(t, "http://127.0.0.1:1", buddyChannelWorkBuddy)
	// deepseek 不在家族规则里，应回退到产品默认 UA（国际版）。
	cases := map[string]string{
		"workbuddy/gpt-5.6-sol":         buddyWorkBuddyUAIntl,
		"workbuddy/gemini-3.5-flash":    buddyWorkBuddyUAIntl,
		"workbuddy/glm-5.3":             buddyWorkBuddyUACN,
		"workbuddy/hy3":                 buddyWorkBuddyUACN,
		"workbuddy/kimi-k2.6":           buddyWorkBuddyUACN,
		"workbuddy/deepseek-v4.1-flash": buddyWorkBuddyUAIntl,
	}
	for model, want := range cases {
		if got := work.ClientHeaders(model)["User-Agent"]; got != want {
			t.Fatalf("模型 %s 的 UA 应为 %q，得到 %q", model, want, got)
		}
	}

	code := newBuddyForTest(t, "http://127.0.0.1:1", buddyChannelCodeBuddy)
	headers := code.ClientHeaders("codebuddy/glm-5.3")
	if headers["User-Agent"] != "CodeBuddyIDE/1.106.1" {
		t.Fatalf("CodeBuddy 恒用 IDE UA，得到 %q", headers["User-Agent"])
	}
	// 复刻头至少要有这些：缺任意一项上游都会按非官方客户端拒绝。
	for name, want := range map[string]string{
		buddyHeaderDomain:      "copilot.tencent.com",
		buddyHeaderProductCode: "codebuddy",
		"X-Agent-Purpose":      "conversation",
		"X-IDE-Name":           "CodeBuddy",
		"X-IDE-Version":        "1.106.1",
		buddyHeaderProduct:     "CodeBuddy",
	} {
		if headers[name] != want {
			t.Fatalf("CodeBuddy 复刻头 %s 应为 %q，得到 %q", name, want, headers[name])
		}
	}
}

// restoreBuddyPollInterval 还原被测试改短的轮询间隔。
func restoreBuddyPollInterval() { buddyPollInterval = time.Second }
