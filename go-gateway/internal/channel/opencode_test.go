package channel

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"
)

// opencode 渠道测试：覆盖免费通道的请求形态（Transform 补工具 + 强制 stream）、
// 复刻头与动态会话头（含上游唯一校验的 x-opencode-session 形状）、设备码登录全流程
// （含 authorization_pending → 成功）、续期与凭据映射。

// opencodeTestChannel 造一个指向假上游、超时很短的渠道。
func opencodeTestChannel(t *testing.T, serverURL string) *OpenCodeChannel {
	t.Helper()
	c := NewOpenCodeChannel(t.TempDir())
	c.siteBase = serverURL
	c.apiBase = serverURL
	c.console = serverURL
	return c
}

// TestOpenCodeChatURLAndClientHeaders 锁定 chat 端点与固定头。User-Agent 必须是
// opencode/<version>（缺失或形如 curl/go-llm-gateway 上游直接 403 FreeTierError）；
// Authorization 由 provider.applyAuth 写入，不应出现在固定头里。
func TestOpenCodeChatURLAndClientHeaders(t *testing.T) {
	c := NewOpenCodeChannel(t.TempDir())
	if c.ChatURL() != "https://opencode.ai/zen/v1/chat/completions" {
		t.Fatalf("chat 端点不正确: %s", c.ChatURL())
	}
	if c.Name() != opencodeChannel {
		t.Fatalf("Name 应为 %s，得到 %s", opencodeChannel, c.Name())
	}
	headers := c.ClientHeaders()
	if headers["User-Agent"] != "opencode/1.18.22" {
		t.Fatalf("User-Agent 应为复刻值 opencode/1.18.22，得到 %q", headers["User-Agent"])
	}
	if !strings.HasPrefix(headers["User-Agent"], "opencode/") {
		t.Fatalf("User-Agent 必须以 opencode/ 开头，否则上游 403，得到 %q", headers["User-Agent"])
	}
	if headers["x-opencode-client"] != "cli" {
		t.Fatalf("x-opencode-client 应为 cli，得到 %q", headers["x-opencode-client"])
	}
	if headers["Accept"] != "text/event-stream" {
		t.Fatalf("Accept 应为 text/event-stream，得到 %q", headers["Accept"])
	}
	if headers["Authorization"] != "" {
		t.Fatalf("Authorization 由 provider.applyAuth 写入，不应出现在固定头里")
	}
}

// TestOpenCodeUserAgentOverride 验证版本号可覆盖：上游升级后硬编码默认值会失效，
// 用 OPENCODE_USER_AGENT 跟上，不必改代码。
func TestOpenCodeUserAgentOverride(t *testing.T) {
	t.Setenv("OPENCODE_USER_AGENT", "opencode/9.9.9")
	c := NewOpenCodeChannel(t.TempDir())
	if got := c.ClientHeaders()["User-Agent"]; got != "opencode/9.9.9" {
		t.Fatalf("User-Agent 应被环境变量覆盖，得到 %q", got)
	}

	t.Setenv("OPENCODE_USER_AGENT", "   ")
	if got := NewOpenCodeChannel(t.TempDir()).ClientHeaders()["User-Agent"]; got != "opencode/1.18.22" {
		t.Fatalf("空白覆盖值应回落到默认值，得到 %q", got)
	}
}

// TestOpenCodeSessionIDShape 钉死 x-opencode-session 的形状：上游对它是**硬校验**
// （缺一个字符即 403），所以这里逐位断言。
func TestOpenCodeSessionIDShape(t *testing.T) {
	pattern := regexp.MustCompile(`^ses_[0-9a-f]{12}[0-9A-Za-z]{14}$`)
	seen := make(map[string]bool)
	for i := 0; i < 20; i++ {
		id := deriveOpenCodeSessionID()
		if !pattern.MatchString(id) {
			t.Fatalf("会话 ID 形状不对（应 ses_ + 12 位十六进制 + 14 位 base62）：%q", id)
		}
		if len(id) != 30 {
			t.Fatalf("会话 ID 应为 30 字符，得到 %d：%q", len(id), id)
		}
		seen[id] = true
	}
	if len(seen) != 20 {
		t.Fatalf("会话 ID 应每次不同（曾出现重复），得到 %d 个不同值", len(seen))
	}
}

// TestOpenCodeProjectIDMatchesReference 是派生实现的钉死测试：项目标识由官方算法
// sha1("git-remote:opencode/" + sha256(identity + " 0").hex) 得出。
// 参考值由 node crypto 算出；实现换成别的哈希或别的拼接顺序都会立刻失败。
func TestOpenCodeProjectIDMatchesReference(t *testing.T) {
	const want = "9e2c575aff0ab741017172ce36629f03016821b1"
	if got := deriveOpenCodeProjectID("public"); got != want {
		t.Fatalf("项目标识与参考实现不一致：\n got %s\nwant %s", got, want)
	}
}

// TestOpenCodeDynamicHeadersAnonymous 验证匿名请求的动态头：每个请求生成新的
// 会话 ID，令牌缺失时项目标识按匿名身份派生。
func TestOpenCodeDynamicHeadersAnonymous(t *testing.T) {
	c := NewOpenCodeChannel(t.TempDir())
	dyn := c.DynamicHeaders()

	first := dyn()
	if first["x-opencode-session"] == "" || first["x-opencode-project"] == "" || first["x-opencode-request"] == "" {
		t.Fatalf("动态头不完整: %+v", first)
	}
	if !strings.HasPrefix(first["x-opencode-request"], "req_") {
		t.Fatalf("请求标识应形如 req_<hex>，得到 %q", first["x-opencode-request"])
	}
	if len(first["x-opencode-request"]) != len("req_")+32 {
		t.Fatalf("请求标识应为 req_ + 32 位十六进制，得到 %q", first["x-opencode-request"])
	}
	if want := deriveOpenCodeProjectID("public"); first["x-opencode-project"] != want {
		t.Fatalf("匿名身份的项目标识不正确：%q", first["x-opencode-project"])
	}
	if second := dyn(); second["x-opencode-session"] == first["x-opencode-session"] {
		t.Fatalf("每个请求都应换新会话 ID")
	}
}

// TestOpenCodeDynamicHeadersUsesAccountIdentity 验证登录后项目标识改由账号令牌派生，
// 与官方「身份 → 项目」的派生方式一致。
func TestOpenCodeDynamicHeadersUsesAccountIdentity(t *testing.T) {
	store := NewStore(t.TempDir())
	if err := store.Save(opencodeChannel, []Credential{{
		AccessToken: "sk-abcdefghijklmnopqrstuvwx",
		ExpiresAt:   time.Now().Add(time.Hour),
	}}); err != nil {
		t.Fatalf("seed credentials: %v", err)
	}
	c := NewOpenCodeChannel(t.TempDir())
	c.accounts = NewAccounts(opencodeChannel, store, newOAuthClient(), c.refreshCredential)

	headers := c.DynamicHeaders()()
	if want := deriveOpenCodeProjectID("sk-abcdefghijklmnopqrstuvwx"); headers["x-opencode-project"] != want {
		t.Fatalf("登录后项目标识应由账号令牌派生，得到 %q", headers["x-opencode-project"])
	}
}

// TestOpenCodeTransformAddsGateTools 是免费通道的核心回归测试：上游要求请求体
// 同时含 bash 与 read 两个占位工具，缺一即 403 FreeTierError。
func TestOpenCodeTransformAddsGateTools(t *testing.T) {
	out, err := OpenCodeTransformRequest([]byte(`{"model":"big-pickle","messages":[]}`))
	if err != nil {
		t.Fatalf("OpenCodeTransformRequest: %v", err)
	}
	var payload struct {
		Stream     bool              `json:"stream"`
		ToolChoice string            `json:"tool_choice"`
		Tools      []json.RawMessage `json:"tools"`
	}
	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatalf("输出不是合法 JSON: %v", err)
	}
	if !payload.Stream {
		t.Fatalf("必须强制 stream:true（免费通道只有流式端点）")
	}
	if payload.ToolChoice != "none" {
		t.Fatalf("调用方原本没有工具时应补 tool_choice:none，得到 %q", payload.ToolChoice)
	}
	if names := gateToolNames(t, payload.Tools); !names["bash"] || !names["read"] {
		t.Fatalf("必须补齐 bash 与 read 两个占位工具，得到 %v", names)
	}
}

// TestOpenCodeTransformKeepsCallerTools 验证补齐是**追加**而非覆盖：调用方自己的工具
// 必须原样保留，且只在调用方一个工具都没给时才补 tool_choice:none。
func TestOpenCodeTransformKeepsCallerTools(t *testing.T) {
	body := `{"model":"big-pickle","messages":[],"tools":[{"type":"function","function":{"name":"lookup","description":"find","parameters":{"type":"object"}}}]}`
	out, err := OpenCodeTransformRequest([]byte(body))
	if err != nil {
		t.Fatalf("OpenCodeTransformRequest: %v", err)
	}
	var payload struct {
		ToolChoice string            `json:"tool_choice"`
		Tools      []json.RawMessage `json:"tools"`
	}
	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatalf("输出不是合法 JSON: %v", err)
	}
	names := gateToolNames(t, payload.Tools)
	for _, want := range []string{"lookup", "bash", "read"} {
		if !names[want] {
			t.Fatalf("工具 %s 应保留/补齐，得到 %v", want, names)
		}
	}
	if len(payload.Tools) != 3 {
		t.Fatalf("应恰好 3 个工具（调用方 1 个 + 占位 2 个），得到 %d", len(payload.Tools))
	}
	if payload.ToolChoice != "" {
		t.Fatalf("调用方有工具时不应写 tool_choice，得到 %q", payload.ToolChoice)
	}
}

// TestOpenCodeTransformDoesNotDuplicateGateTools 验证已有同名占位工具时不重复补入。
func TestOpenCodeTransformDoesNotDuplicateGateTools(t *testing.T) {
	body := `{"model":"big-pickle","tools":[{"type":"function","function":{"name":"bash"}},{"type":"function","function":{"name":"read"}}]}`
	out, err := OpenCodeTransformRequest([]byte(body))
	if err != nil {
		t.Fatalf("OpenCodeTransformRequest: %v", err)
	}
	var payload struct {
		Tools []json.RawMessage `json:"tools"`
	}
	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatalf("输出不是合法 JSON: %v", err)
	}
	if len(payload.Tools) != 2 {
		t.Fatalf("已有 bash/read 时不应再补，得到 %d 个工具", len(payload.Tools))
	}
}

// TestOpenCodeTransformInvalidJSONPassesThrough 验证非法 JSON 原样透传（交给上游报错）。
func TestOpenCodeTransformInvalidJSONPassesThrough(t *testing.T) {
	raw := []byte(`not json`)
	got, err := OpenCodeTransformRequest(raw)
	if err != nil || string(got) != string(raw) {
		t.Fatalf("非法 JSON 应原样返回，得到 %s (%v)", got, err)
	}
}

// gateToolNames 收集工具数组里的函数名。
func gateToolNames(t *testing.T, tools []json.RawMessage) map[string]bool {
	t.Helper()
	names := make(map[string]bool, len(tools))
	for _, tool := range tools {
		var probe struct {
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
		}
		if err := json.Unmarshal(tool, &probe); err != nil {
			t.Fatalf("工具不是合法 JSON: %v", err)
		}
		names[probe.Function.Name] = true
	}
	return names
}

// TestOpenCodeAuthSourceFallsBackToAnonymous 是关键的集成回归：未登录时
// provider.applyAuth 若拿到空令牌会**什么都不写**（连 ExtraHeaders 一起跳过），
// 免费通道必须有 Bearer "public" 才放行。
func TestOpenCodeAuthSourceFallsBackToAnonymous(t *testing.T) {
	c := NewOpenCodeChannel(t.TempDir())
	if got := c.AuthSource().BearerToken(); got != opencodeAnonymousKey {
		t.Fatalf("未登录应回退到匿名常量 %q，得到 %q", opencodeAnonymousKey, got)
	}
}

// ── 登录流程 ──────────────────────────────────────────────────────────────

// TestOpenCodeDeviceLoginFlow 走完设备码登录：取码 → pending → 成功 → 拉取账号信息 → 落盘。
func TestOpenCodeDeviceLoginFlow(t *testing.T) {
	pollCount := 0
	mux := http.NewServeMux()
	mux.HandleFunc(opencodeDeviceCodePath, func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]string
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode device code body: %v", err)
		}
		if payload["client_id"] != opencodeClientID {
			t.Errorf("client_id 错误: %s", payload["client_id"])
		}
		writeJSON(w, map[string]any{
			"device_code": "dev-code-1",
			"user_code":   "ABCD-EFGH",
			// 上游返回的是相对路径，网关需要绝对化后给用户。
			"verification_uri":          "/console/device",
			"verification_uri_complete": "/console/device?user_code=ABCD-EFGH&client_id=opencode-cli",
			"expires_in":                600,
			"interval":                  1,
		})
	})
	mux.HandleFunc(opencodeDeviceTokenPath, func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]string
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode token body: %v", err)
		}
		if payload["grant_type"] != opencodeDeviceGrantType {
			t.Errorf("grant_type 错误: %s", payload["grant_type"])
		}
		if payload["device_code"] != "dev-code-1" {
			t.Errorf("device_code 错误: %s", payload["device_code"])
		}
		pollCount++
		if pollCount == 1 {
			w.WriteHeader(http.StatusBadRequest)
			writeJSON(w, map[string]any{
				"_tag":              "DeviceTokenError",
				"error":             "authorization_pending",
				"error_description": "The authorization request is still pending",
			})
			return
		}
		writeJSON(w, map[string]any{
			"access_token":  "zen-access",
			"refresh_token": "zen-refresh",
			"expires_in":    3600,
		})
	})
	mux.HandleFunc(opencodeUserAPIPath, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer zen-access" {
			t.Errorf("账号信息请求应带登录令牌，得到 %q", got)
		}
		writeJSON(w, map[string]any{"id": "user-1", "email": "u@example.com"})
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	ch := opencodeTestChannel(t, server.URL)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := ch.BeginLogin(ctx)
	if err != nil {
		t.Fatalf("BeginLogin failed: %v", err)
	}
	if session.Channel() != opencodeChannel {
		t.Fatalf("channel 名错误: %s", session.Channel())
	}
	dc := session.DeviceCode()
	if dc.UserCode != "ABCD-EFGH" {
		t.Fatalf("设备码展示信息错误: %+v", dc)
	}
	if dc.VerificationURI != server.URL+"/console/device" {
		t.Fatalf("verification_uri 应绝对化，得到 %q", dc.VerificationURI)
	}
	if want := server.URL + "/console/device?user_code=ABCD-EFGH&client_id=opencode-cli"; dc.LoginURL() != want {
		t.Fatalf("login_url 应优先用 verification_uri_complete，得到 %q", dc.LoginURL())
	}

	cred, err := session.Wait(ctx)
	if err != nil {
		t.Fatalf("Wait failed: %v", err)
	}
	if cred.AccessToken != "zen-access" || cred.RefreshToken != "zen-refresh" {
		t.Fatalf("登录结果错误: %+v", cred)
	}
	if cred.AccountID != "user-1" || cred.Nickname != "u@example.com" {
		t.Fatalf("账号信息未填充: %+v", cred)
	}
	if cred.ExpiresAt.IsZero() {
		t.Fatalf("expires_in 应换算成过期时间")
	}
	if !ch.Accounts().LoggedIn() {
		t.Fatalf("凭据未进入账号池")
	}
	// 登录后动态头改由真实令牌派生。
	if got := ch.DynamicHeaders()()["x-opencode-project"]; got != deriveOpenCodeProjectID("zen-access") {
		t.Fatalf("登录后项目标识应由账号令牌派生，得到 %q", got)
	}
}

// TestOpenCodeDeviceLoginAccessDenied 验证用户拒绝被映射成终态错误（需重新登录）。
func TestOpenCodeDeviceLoginAccessDenied(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc(opencodeDeviceCodePath, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"device_code":      "dev-2",
			"user_code":        "ZZZZ-9999",
			"verification_uri": "/console/device",
			"expires_in":       600,
			"interval":         1,
		})
	})
	mux.HandleFunc(opencodeDeviceTokenPath, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]any{"error": "access_denied"})
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	ch := opencodeTestChannel(t, server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := ch.BeginLogin(ctx)
	if err != nil {
		t.Fatalf("BeginLogin failed: %v", err)
	}
	if _, err := session.Wait(ctx); err == nil {
		t.Fatalf("用户拒绝后 Wait 应失败")
	}
}

// TestOpenCodeRefreshCredential 验证续期：grant_type=refresh_token，返回新令牌并保留
// 未回传的字段。
func TestOpenCodeRefreshCredential(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc(opencodeDeviceTokenPath, func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]string
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode refresh body: %v", err)
		}
		if payload["grant_type"] != "refresh_token" || payload["refresh_token"] != "old-refresh" {
			t.Errorf("续期请求错误: %+v", payload)
		}
		writeJSON(w, map[string]any{
			"access_token":  "new-access",
			"refresh_token": "new-refresh",
			"expires_in":    3600,
		})
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	ch := opencodeTestChannel(t, server.URL)
	next, err := ch.refreshCredential(context.Background(), Credential{
		AccessToken:  "old-access",
		RefreshToken: "old-refresh",
		AccountID:    "user-1",
		Nickname:     "小明",
	})
	if err != nil {
		t.Fatalf("refreshCredential failed: %v", err)
	}
	if next.AccessToken != "new-access" || next.RefreshToken != "new-refresh" {
		t.Fatalf("令牌未更新: %+v", next)
	}
	if next.AccountID != "user-1" || next.Nickname != "小明" {
		t.Fatalf("未回传的字段应保留: %+v", next)
	}
}

// TestOpenCodeRefreshWithoutTokenIsTerminal 验证无 refresh_token 时给出可操作错误。
func TestOpenCodeRefreshWithoutTokenIsTerminal(t *testing.T) {
	ch := NewOpenCodeChannel(t.TempDir())
	if _, err := ch.refreshCredential(context.Background(), Credential{AccessToken: "only-access"}); err == nil {
		t.Fatalf("缺少 refresh_token 时应报错并提示重新登录")
	}
}
