package channel

import (
	"context"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

// OpenCode（opencode.ai Zen）账号渠道。
//
// 与其它渠道最大的不同：免费模型**不需要登录**。上游允许匿名访问（Bearer 常量
// "public"），但只对「从 OpenCode 客户端发出」的请求放行——判定依据是请求形态，
// 三条硬性要求缺一即 403 FreeTierError：
//
//	(a) 请求体 stream:true（免费通道只有流式端点）；
//	(b) 请求体带 tools，且同时含 bash 与 read 两项（客户端运行时的占位工具）；
//	(c) User-Agent 形如 opencode/<version>，且 x-opencode-session 为 ses_ + 26 位 base62。
//
// (a)(b) 由 OpenCodeTransformRequest 补齐，(c) 由 ClientHeaders / DynamicHeaders 提供。
//
// 推理走标准 OpenAI Chat Completions（POST /zen/v1/chat/completions），因此复用
// provider.OpenAIProvider 的协议转换；由于强制流式，provider 侧用 OpenCodeProvider
// 把「非流式」请求也走流式端点再累积回完整响应。
//
// 登录是标准设备码流程（POST /console/auth/device/code 取码 → 轮询
// /console/auth/device/token），登录拿到的令牌即 Zen 的 Bearer key，可用于非免费模型；
// 免费模型则始终可用，无需登录。
const (
	opencodeChannel  = "opencode"
	opencodeSiteBase = "https://opencode.ai"
	opencodeAPIBase  = "https://opencode.ai/zen"
	opencodeConsole  = "https://opencode.ai/console"

	opencodeChatPath         = "/v1/chat/completions"
	opencodeDeviceCodePath   = "/auth/device/code"
	opencodeDeviceTokenPath  = "/auth/device/token"
	opencodeUserAPIPath      = "/api/user"
	opencodeOrgsAPIPath      = "/api/orgs"
	opencodeDeviceGrantType  = "urn:ietf:params:oauth:grant-type:device_code"
	opencodeDefaultUserAgent = "opencode/1.18.22"

	// 匿名通道的固定 Bearer。免费模型以此为凭据，无需任何账号。
	opencodeAnonymousKey = "public"

	// opencode-cli 是官方 CLI 的公开 client_id（非密钥，与 cline 的 WorkOS client
	// 同理），设备码流程必须原样带上。
	opencodeClientID = "opencode-cli"

	// 会话 ID 形如 ses_ + 12 位十六进制（6 字节时间戳）+ 14 位 base62，共 30 字符。
	opencodeSessionTailLen   = 26
	opencodeSessionRandomLen = opencodeSessionTailLen - 12 // = 14
	opencodeBase62           = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

	opencodeMaxBodyBytes = 64 * 1024

	opencodeLoginTimeout = 5 * time.Minute
)

// opencodeGateTools 是免费通道要求请求体必须携带的占位工具名（顺序即补入顺序）。
var opencodeGateTools = []string{"bash", "read"}

// OpenCodeChannel 持有 opencode 的账号池与登录逻辑。
type OpenCodeChannel struct {
	accounts *Accounts
	client   *oauthClient
	// 端点可覆盖，测试指向本地假上游；生产用常量默认值。
	siteBase  string
	apiBase   string
	console   string
	userAgent string
}

// NewOpenCodeChannel 在 dir 下构造 opencode 渠道（凭据文件 <dir>/opencode.json）。
func NewOpenCodeChannel(dir string) *OpenCodeChannel {
	c := &OpenCodeChannel{
		client:    newOAuthClient(),
		siteBase:  opencodeSiteBase,
		apiBase:   opencodeAPIBase,
		console:   opencodeConsole,
		userAgent: opencodeUserAgent(),
	}
	c.accounts = NewAccounts(opencodeChannel, NewStore(dir), c.client, c.refreshCredential)
	return c
}

// opencodeUserAgent 返回复刻用的 User-Agent：允许用 OPENCODE_USER_AGENT 覆盖
// （上游会升级版本号，硬编码的默认值需要能跟上）。
func opencodeUserAgent() string {
	if override := strings.TrimSpace(os.Getenv("OPENCODE_USER_AGENT")); override != "" {
		return override
	}
	return opencodeDefaultUserAgent
}

// Accounts 供 provider 层取用令牌源。
func (c *OpenCodeChannel) Accounts() *Accounts { return c.accounts }

// AuthSource 返回注入 provider 的鉴权源：未登录时退化为匿名 Bearer "public"，
// 保证免费通道在「没有任何账号」时也能工作。
func (c *OpenCodeChannel) AuthSource() *openCodeAuth { return &openCodeAuth{Accounts: c.accounts} }

// Name 实现 Channel。
func (c *OpenCodeChannel) Name() string { return opencodeChannel }

// ChatURL 是 opencode Zen 的 OpenAI Chat Completions 推理端点。
func (c *OpenCodeChannel) ChatURL() string { return c.apiBase + opencodeChatPath }

// ClientHeaders 是复刻 opencode 客户端所需的**固定**请求头。
//
// User-Agent 必须是 opencode/<version>：缺失或形如 curl / go-llm-gateway 一律 403。
// Authorization 由 provider.applyAuth 写入；随时变化的会话头见 DynamicHeaders。
func (c *OpenCodeChannel) ClientHeaders() map[string]string {
	return map[string]string{
		"Accept":            "text/event-stream",
		"User-Agent":        c.userAgent,
		"x-opencode-client": "cli",
	}
}

// DynamicHeaders 返回每个请求重新求值的会话头。
//
// x-opencode-session 是唯一被上游校验的动态头（必须 ses_ + 26 位 base62），
// project/request 不被校验但一并发送以保持形态一致（project 由当前令牌派生，
// 与官方「身份 → 项目」的派生方式相同）。
func (c *OpenCodeChannel) DynamicHeaders() func() map[string]string {
	return func() map[string]string {
		identity := c.accounts.BearerToken()
		if identity == "" {
			identity = opencodeAnonymousKey
		}
		return map[string]string{
			"x-opencode-project": deriveOpenCodeProjectID(identity),
			"x-opencode-session": deriveOpenCodeSessionID(),
			"x-opencode-request": "req_" + mustRandomHex(16),
		}
	}
}

// openCodeAuth 包装账号池，把「未登录」映射成匿名常量而非空串。
//
// 关键点：provider.applyAuth 在 BearerToken() 为空时不写任何头并让调用方短路。
// 免费通道恰恰要求未登录也能发请求，因此这里必须返回 "public"。
type openCodeAuth struct {
	*Accounts
}

// BearerToken 优先返回真实账号令牌，未登录时回退到匿名常量。
func (a *openCodeAuth) BearerToken() string {
	if token := a.Accounts.BearerToken(); token != "" {
		return token
	}
	return opencodeAnonymousKey
}

// OpenCodeTransformRequest 复刻官方客户端的免费通道请求形态：
//  1. 强制 stream:true；
//  2. tools 里补齐 bash / read 两个占位工具（去重，不改动已有同名工具）；
//  3. 仅当调用方原本没给任何工具时，补 tool_choice:"none"（避免占位工具被模型真的调用）。
//
// 解析失败时原样返回（这类请求会由上游给出更准确的错误）。
func OpenCodeTransformRequest(body []byte) ([]byte, error) {
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil {
		return body, nil
	}
	payload["stream"] = json.RawMessage("true")

	var tools []json.RawMessage
	if raw, ok := payload["tools"]; ok {
		_ = json.Unmarshal(raw, &tools)
	}

	names := make(map[string]bool, len(tools))
	for _, tool := range tools {
		var probe struct {
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
		}
		if json.Unmarshal(tool, &probe) == nil && probe.Function.Name != "" {
			names[probe.Function.Name] = true
		}
	}

	missing := make([]string, 0, len(opencodeGateTools))
	for _, gate := range opencodeGateTools {
		if !names[gate] {
			missing = append(missing, gate)
		}
	}
	if len(missing) == 0 {
		return json.Marshal(payload)
	}

	hadCallerTools := len(tools) > 0
	for _, name := range missing {
		tools = append(tools, opencodeGateTool(name))
	}
	encoded, err := json.Marshal(tools)
	if err != nil {
		return body, nil
	}
	payload["tools"] = encoded
	if !hadCallerTools {
		payload["tool_choice"] = json.RawMessage(`"none"`)
	}
	return json.Marshal(payload)
}

// opencodeGateTool 构造一个占位工具定义（描述明确提示勿调用）。
func opencodeGateTool(name string) json.RawMessage {
	tool, _ := json.Marshal(map[string]any{
		"type": "function",
		"function": map[string]any{
			"name":        name,
			"description": "Reserved for the host runtime; do not call it.",
			"parameters":  map[string]any{"type": "object", "properties": map[string]any{}},
		},
	})
	return tool
}

// BeginLogin 实现 Channel：申请设备码并返回可轮询的登录会话。
func (c *OpenCodeChannel) BeginLogin(ctx context.Context) (LoginSession, error) {
	dc, err := c.beginDeviceLogin(ctx)
	if err != nil {
		return nil, err
	}
	return &deviceLogin{
		channel: opencodeChannel,
		dc:      dc,
		wait: func(waitCtx context.Context) (*Credential, error) {
			return c.waitDeviceLogin(waitCtx, dc)
		},
	}, nil
}

// beginDeviceLogin 向 console 申请设备码。verification_uri 是相对路径，这里绝对化，
// 让 Admin API 能把 login_url 直接给用户。
func (c *OpenCodeChannel) beginDeviceLogin(ctx context.Context) (DeviceCode, error) {
	payload := map[string]string{"client_id": opencodeClientID}
	body, _, err := c.client.postJSON(ctx, c.console+opencodeDeviceCodePath, payload, nil)
	if err != nil {
		return DeviceCode{}, fmt.Errorf("opencode 设备码申请失败: %w", err)
	}
	dc := DeviceCode{
		DeviceCode:              stringField(body, "device_code"),
		UserCode:                stringField(body, "user_code"),
		VerificationURI:         c.absoluteConsoleURL(stringField(body, "verification_uri")),
		VerificationURIComplete: c.absoluteConsoleURL(stringField(body, "verification_uri_complete")),
		ExpiresIn:               time.Duration(numberField(body, "expires_in")) * time.Second,
		Interval:                time.Duration(numberField(body, "interval")) * time.Second,
	}
	if dc.DeviceCode == "" || dc.UserCode == "" || dc.VerificationURI == "" {
		return DeviceCode{}, fmt.Errorf("opencode 设备码响应缺少必要字段")
	}
	if dc.ExpiresIn <= 0 {
		dc.ExpiresIn = opencodeLoginTimeout
	}
	return dc, nil
}

// absoluteConsoleURL 把 console 返回的相对路径解析成绝对地址。
func (c *OpenCodeChannel) absoluteConsoleURL(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") {
		return value
	}
	if !strings.HasPrefix(value, "/") {
		value = "/" + value
	}
	return c.siteBase + value
}

// waitDeviceLogin 轮询设备码授权，成功后落盘。
func (c *OpenCodeChannel) waitDeviceLogin(ctx context.Context, dc DeviceCode) (*Credential, error) {
	cred, err := pollDeviceToken(ctx, dc, func(ctx context.Context) (pollOutcome, *Credential, error) {
		return c.pollDeviceToken(ctx, dc)
	})
	if err != nil {
		return nil, err
	}
	c.enrich(ctx, cred)
	if err := c.accounts.add(*cred); err != nil {
		return nil, err
	}
	return cred, nil
}

// pollDeviceToken 发一次令牌轮询。pending 在上游是 HTTP 400 + error=authorization_pending。
func (c *OpenCodeChannel) pollDeviceToken(ctx context.Context, dc DeviceCode) (pollOutcome, *Credential, error) {
	payload := map[string]string{
		"grant_type":  opencodeDeviceGrantType,
		"device_code": dc.DeviceCode,
		"client_id":   opencodeClientID,
	}
	body, status, err := c.client.postJSON(ctx, c.console+opencodeDeviceTokenPath, payload, nil)
	if err != nil {
		var httpErr *oauthHTTPError
		if !errors.As(err, &httpErr) {
			return 0, nil, err
		}
		body, status = httpErr.Body, httpErr.Status
	}
	if status >= 200 && status < 300 {
		cred, err := credentialFromDeviceToken(body)
		if err != nil {
			return 0, nil, err
		}
		return pollSuccess, cred, nil
	}
	switch stringField(body, "error") {
	case "authorization_pending":
		return pollPending, nil, nil
	case "slow_down":
		return pollSlowDown, nil, nil
	case "access_denied":
		return pollDenied, nil, nil
	case "expired_token", "invalid_grant":
		return pollExpired, nil, nil
	}
	return 0, nil, openCodeOAuthFailure("OpenCode 授权", body, status)
}

// credentialFromDeviceToken 解析令牌响应为 Credential。
func credentialFromDeviceToken(body map[string]any) (*Credential, error) {
	access := firstNonEmpty(stringField(body, "access_token"), stringField(body, "access"))
	if access == "" {
		return nil, fmt.Errorf("opencode 令牌响应缺少 access_token")
	}
	cred := &Credential{
		AccessToken:  access,
		RefreshToken: stringField(body, "refresh_token"),
	}
	if secs := numberField(body, "expires_in"); secs > 0 {
		cred.ExpiresAt = time.Now().Add(time.Duration(secs) * time.Second)
	}
	return cred, nil
}

// enrich 尽力拉取账号信息填充 AccountID / Nickname。失败不影响登录（令牌已可用）。
func (c *OpenCodeChannel) enrich(ctx context.Context, cred *Credential) {
	if cred == nil {
		return
	}
	if user, _, err := c.getJSON(ctx, c.console+opencodeUserAPIPath, cred.AccessToken); err == nil {
		if cred.AccountID == "" {
			cred.AccountID = firstNonEmpty(stringField(user, "id"), stringField(user, "account_id"), stringField(user, "sub"))
		}
		if cred.Nickname == "" {
			cred.Nickname = firstNonEmpty(
				stringField(user, "name"),
				stringField(user, "email"),
				stringField(user, "username"),
			)
		}
	}
	if cred.Nickname == "" {
		if orgs, _, err := c.getJSON(ctx, c.console+opencodeOrgsAPIPath, cred.AccessToken); err == nil {
			cred.Nickname = openCodeFirstOrgName(orgs)
		}
	}
	if cred.Nickname == "" {
		cred.Nickname = cred.AccountID
	}
}

// openCodeFirstOrgName 从 orgs 响应里取第一个组织名（结构未知，尽力解析）。
func openCodeFirstOrgName(body map[string]any) string {
	candidates := []any{body["data"], body["orgs"], body["organizations"]}
	for _, candidate := range candidates {
		list, ok := candidate.([]any)
		if !ok || len(list) == 0 {
			continue
		}
		first, ok := list[0].(map[string]any)
		if !ok {
			continue
		}
		if name := firstNonEmpty(stringField(first, "name"), stringField(first, "slug"), stringField(first, "id")); name != "" {
			return name
		}
	}
	return ""
}

// getJSON 发一次带 Bearer 的 GET 并解析 JSON（oauthClient 只提供 POST，这里补上）。
func (c *OpenCodeChannel) getJSON(ctx context.Context, urlStr, bearer string) (map[string]any, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("build opencode get request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	return c.client.do(req)
}

// refreshCredential 用 refresh_token 换新令牌（同一 token 端点，grant_type 不同）。
func (c *OpenCodeChannel) refreshCredential(ctx context.Context, cred Credential) (Credential, error) {
	if cred.RefreshToken == "" {
		return Credential{}, fmt.Errorf("opencode 凭据缺少 refresh_token，需重新登录")
	}
	payload := map[string]string{
		"grant_type":    "refresh_token",
		"refresh_token": cred.RefreshToken,
		"client_id":     opencodeClientID,
	}
	body, status, err := c.client.postJSON(ctx, c.console+opencodeDeviceTokenPath, payload, nil)
	if err != nil {
		// 401/403 表示 refresh_token 已失效，是终态：重试无意义。
		var httpErr *oauthHTTPError
		if errors.As(err, &httpErr) && (httpErr.Status == 401 || httpErr.Status == 403) {
			return Credential{}, fmt.Errorf("%w: opencode 续期被拒绝（HTTP %d）", ErrLoginCancelled, httpErr.Status)
		}
		return Credential{}, fmt.Errorf("opencode 续期失败: %w", err)
	}
	if status < 200 || status >= 300 {
		return Credential{}, openCodeOAuthFailure("opencode 续期", body, status)
	}
	next, err := credentialFromDeviceToken(body)
	if err != nil {
		return Credential{}, err
	}
	if next.RefreshToken == "" {
		next.RefreshToken = cred.RefreshToken
	}
	if next.AccountID == "" {
		next.AccountID = cred.AccountID
	}
	if next.Nickname == "" {
		next.Nickname = cred.Nickname
	}
	return *next, nil
}

// openCodeOAuthFailure 把上游错误体归一成可读错误，保留 error_description。
func openCodeOAuthFailure(what string, body map[string]any, status int) error {
	detail := firstNonEmpty(stringField(body, "error_description"), stringField(body, "error"))
	if detail == "" {
		return fmt.Errorf("%s失败（HTTP %d）", what, status)
	}
	return fmt.Errorf("%s失败（HTTP %d）：%s", what, status, detail)
}

// deriveOpenCodeSessionID 生成 ses_ + 12 位十六进制时间戳 + 14 位 base62（共 30 字符）。
func deriveOpenCodeSessionID() string {
	var stamp [6]byte
	now := uint64(time.Now().UnixMilli())
	for i := 0; i < len(stamp); i++ {
		stamp[i] = byte(now & 0xff)
		now >>= 8
	}
	return "ses_" + hex.EncodeToString(stamp[:]) + randomBase62(opencodeSessionRandomLen)
}

// deriveOpenCodeProjectID 复刻官方的项目标识派生：
// sha1("git-remote:opencode/" + sha256(identity + " " + generation).hex)。
func deriveOpenCodeProjectID(identity string) string {
	inner := sha256.Sum256([]byte(identity + " 0"))
	innerHex := hex.EncodeToString(inner[:])
	outer := sha1.Sum([]byte("git-remote:opencode/" + innerHex))
	return hex.EncodeToString(outer[:])
}

// randomBase62 生成 n 个 base62 字符（与官方实现同构：随机字节对 62 取模）。
func randomBase62(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		// 退化：随机源故障时用时间戳派生，保证请求形态仍然合法。
		fallback := fmt.Sprintf("%0*x", n*2, time.Now().UnixNano())
		for i := range buf {
			buf[i] = fallback[i%len(fallback)]
		}
	}
	out := make([]byte, n)
	for i, b := range buf {
		out[i] = opencodeBase62[int(b)%len(opencodeBase62)]
	}
	return string(out)
}
