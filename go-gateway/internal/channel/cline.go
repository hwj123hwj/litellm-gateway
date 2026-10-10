package channel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// Cline 账号渠道。
//
// 登录分两跳，都是标准的设备码流程，网关可以完整跑完，不需要浏览器回调：
//  1. WorkOS 设备授权（/user_management/authorize/device + /user_management/authenticate）
//     拿到 WorkOS 的 access/refresh token；
//  2. 把 WorkOS 令牌注册进 Cline（/api/v1/auth/register），换回 Cline 自己的令牌。
//
// 推理走 OpenAI Chat Completions 协议（/api/v1/chat/completions），因此复用
// provider.OpenAIProvider 的协议转换；渠道只提供令牌、固定请求头与续期。
//
// 注意 Authorization 的值必须带 `workos:` 前缀（服务端按这个前缀识别令牌类型），
// 这里把前缀连同令牌一起存进 Credential.AccessToken，provider 只做
// `Bearer <AccessToken>` 拼接。
const (
	clineChannel       = "cline"
	clineAPIHost       = "https://api.cline.bot"
	clineWorkOSHost    = "https://api.workos.com"
	clineWorkOSClient  = "client_01K3A541FN8TA3EPPHTD2325AR"
	clineTokenPrefix   = "workos:"
	clineDeviceCodeURL = "/user_management/authorize/device"
	clineAuthenticate  = "/user_management/authenticate"
	clineRegisterPath  = "/api/v1/auth/register"
	clineRefreshPath   = "/api/v1/auth/refresh"
	clineChatPath      = "/api/v1/chat/completions"
)

// ClineClientHeaders 是复刻 Cline 客户端所需的固定请求头。注册、续期、推理三处
// 都必须带上，否则上游按非官方客户端处理。
func ClineClientHeaders() map[string]string {
	return map[string]string{
		"HTTP-Referer":   "https://cline.bot",
		"X-Title":        "Cline",
		"X-IS-MULTIROOT": "false",
		"X-CLIENT-TYPE":  "cline-sdk",
	}
}

// ClineChannel 持有 cline 的账号池与登录逻辑。
type ClineChannel struct {
	accounts *Accounts
	client   *oauthClient
	// 端点主机可覆盖，测试时指向本地假上游；生产用常量默认值。
	apiHost    string
	workosHost string
}

// NewClineChannel 在 dir 下构造 cline 渠道（凭据文件 <dir>/cline.json）。
func NewClineChannel(dir string) *ClineChannel {
	c := &ClineChannel{
		client:     newOAuthClient(),
		apiHost:    clineAPIHost,
		workosHost: clineWorkOSHost,
	}
	c.accounts = NewAccounts(clineChannel, NewStore(dir), c.client, c.refreshCredential)
	return c
}

// Accounts 供 provider 层取用令牌源。
func (c *ClineChannel) Accounts() *Accounts { return c.accounts }

// Name 实现 Channel。
func (c *ClineChannel) Name() string { return clineChannel }

// ChatURL 是 cline 的 OpenAI Chat Completions 推理端点。
func (c *ClineChannel) ChatURL() string { return c.apiHost + clineChatPath }

// BeginLogin 实现 Channel：申请 WorkOS 设备码并返回可轮询的登录会话。
func (c *ClineChannel) BeginLogin(ctx context.Context) (LoginSession, error) {
	dc, err := c.beginDeviceLogin(ctx)
	if err != nil {
		return nil, err
	}
	return &deviceLogin{
		channel: clineChannel,
		dc:      dc,
		wait: func(waitCtx context.Context) (*Credential, error) {
			return c.waitDeviceLogin(waitCtx, dc)
		},
	}, nil
}

// beginDeviceLogin 申请 WorkOS 设备码。cline 不使用 PKCE。
func (c *ClineChannel) beginDeviceLogin(ctx context.Context) (DeviceCode, error) {
	form := url.Values{"client_id": {clineWorkOSClient}}
	body, _, err := c.client.postForm(ctx, c.workosHost+clineDeviceCodeURL, form)
	if err != nil {
		return DeviceCode{}, fmt.Errorf("cline 设备码申请失败: %w", err)
	}
	dc := DeviceCode{
		DeviceCode:              stringField(body, "device_code"),
		UserCode:                stringField(body, "user_code"),
		VerificationURI:         firstNonEmpty(stringField(body, "verification_uri"), stringField(body, "verification_url")),
		VerificationURIComplete: stringField(body, "verification_uri_complete"),
		ExpiresIn:               time.Duration(numberField(body, "expires_in")) * time.Second,
		Interval:                time.Duration(numberField(body, "interval")) * time.Second,
	}
	if dc.DeviceCode == "" || dc.UserCode == "" || dc.VerificationURI == "" {
		return DeviceCode{}, fmt.Errorf("cline 设备码响应缺少必要字段")
	}
	if dc.ExpiresIn <= 0 {
		dc.ExpiresIn = 5 * time.Minute
	}
	return dc, nil
}

// waitDeviceLogin 轮询 WorkOS 授权，成功后注册到 Cline 并落盘。
func (c *ClineChannel) waitDeviceLogin(ctx context.Context, dc DeviceCode) (*Credential, error) {
	workos, err := pollDeviceToken(ctx, dc, func(ctx context.Context) (pollOutcome, *Credential, error) {
		return c.pollWorkOS(ctx, dc)
	})
	if err != nil {
		return nil, err
	}
	cred, err := c.register(ctx, workos)
	if err != nil {
		return nil, err
	}
	if err := c.accounts.add(*cred); err != nil {
		return nil, err
	}
	return cred, nil
}

// pollWorkOS 发一次 WorkOS 令牌轮询。成功时把结果暂存进 Credential，供 register 使用。
func (c *ClineChannel) pollWorkOS(ctx context.Context, dc DeviceCode) (pollOutcome, *Credential, error) {
	form := url.Values{
		"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
		"device_code": {dc.DeviceCode},
		"client_id":   {clineWorkOSClient},
	}
	body, status, err := c.client.postForm(ctx, c.workosHost+clineAuthenticate, form)
	if err != nil {
		var httpErr *oauthHTTPError
		if !errors.As(err, &httpErr) {
			return 0, nil, err
		}
		body, status = httpErr.Body, httpErr.Status
	}
	if status >= 200 && status < 300 {
		access := stringField(body, "access_token")
		refresh := stringField(body, "refresh_token")
		if access == "" || refresh == "" {
			return 0, nil, fmt.Errorf("cline WorkOS 令牌响应缺少必要字段")
		}
		// 此处只暂存 WorkOS 令牌；真正入池的是 register 换回的 Cline 令牌。
		return pollSuccess, &Credential{AccessToken: access, RefreshToken: refresh}, nil
	}
	switch stringField(body, "error") {
	case "authorization_pending":
		return pollPending, nil, nil
	case "slow_down":
		return pollSlowDown, nil, nil
	case "access_denied", "expired_token", "invalid_grant":
		return pollDenied, nil, nil
	}
	return 0, nil, c.oauthFailure("WorkOS 授权", body, status)
}

// register 把 WorkOS 令牌注册进 Cline，换回 Cline 自己的令牌。
func (c *ClineChannel) register(ctx context.Context, workos *Credential) (*Credential, error) {
	payload := map[string]string{
		"accessToken":  workos.AccessToken,
		"refreshToken": workos.RefreshToken,
	}
	body, _, err := c.client.postJSON(ctx, c.apiHost+clineRegisterPath, payload, ClineClientHeaders())
	if err != nil {
		return nil, fmt.Errorf("cline token 注册失败: %w", err)
	}
	cred, err := parseClineToken(body)
	if err != nil {
		return nil, err
	}
	return cred, nil
}

// refreshCredential 用 Cline 的 refresh_token 换新令牌。字段名是驼峰，
// 与 OAuth 标准的 snake_case 不同（上游实现如此）。
func (c *ClineChannel) refreshCredential(ctx context.Context, cred Credential) (Credential, error) {
	if cred.RefreshToken == "" {
		return Credential{}, fmt.Errorf("cline 凭据缺少 refresh_token，需重新登录")
	}
	payload := map[string]string{
		"refreshToken": cred.RefreshToken,
		"grantType":    "refresh_token",
	}
	body, _, err := c.client.postJSON(ctx, c.apiHost+clineRefreshPath, payload, ClineClientHeaders())
	if err != nil {
		// 401/403 表示 refresh_token 已失效，是终态：换号或重新登录，重试无意义。
		var httpErr *oauthHTTPError
		if errors.As(err, &httpErr) && (httpErr.Status == 401 || httpErr.Status == 403) {
			return Credential{}, fmt.Errorf("%w: cline 续期被拒绝（HTTP %d）", ErrLoginCancelled, httpErr.Status)
		}
		return Credential{}, fmt.Errorf("cline 续期失败: %w", err)
	}
	next, err := parseClineToken(body)
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

// parseClineToken 归一化 Cline 令牌响应。响应可能是 {data:{...}} 信封，也可能
// 直接就是凭据对象，字段名同时兼容驼峰与下划线。
func parseClineToken(body map[string]any) (*Credential, error) {
	inner := body
	if data, ok := body["data"].(map[string]any); ok {
		inner = data
	}
	access := firstNonEmpty(
		stringField(inner, "accessToken"),
		stringField(inner, "access_token"),
	)
	if access == "" {
		return nil, fmt.Errorf("cline 令牌响应缺少访问令牌")
	}
	refresh := firstNonEmpty(
		stringField(inner, "refreshToken"),
		stringField(inner, "refresh_token"),
	)

	userInfo, _ := inner["userInfo"].(map[string]any)
	accountID := ""
	if userInfo != nil {
		accountID = firstNonEmpty(
			stringField(userInfo, "clineUserId"),
			stringField(userInfo, "accountId"),
			stringField(userInfo, "account_id"),
		)
	}
	if accountID == "" {
		accountID = firstNonEmpty(stringField(inner, "accountId"), stringField(inner, "account_id"))
	}
	email := stringField(inner, "email")
	if email == "" && userInfo != nil {
		email = stringField(userInfo, "email")
	}
	nickname := email
	if nickname == "" && userInfo != nil {
		nickname = strings.TrimSpace(stringField(userInfo, "firstName") + " " + stringField(userInfo, "lastName"))
	}
	if nickname == "" {
		nickname = accountID
	}

	expiresAt := parseClineTimestamp(inner["expiresAt"])
	if expiresAt.IsZero() {
		expiresAt = parseClineTimestamp(inner["expires_at"])
	}
	if expiresAt.IsZero() {
		expiresAt = parseClineTimestamp(inner["expire_time"])
	}

	return &Credential{
		AccessToken:  clineBearerValue(access),
		RefreshToken: refresh,
		ExpiresAt:    expiresAt,
		AccountID:    accountID,
		Nickname:     nickname,
	}, nil
}

// clineBearerValue 给令牌补上 workos: 前缀（已有则不重复加）。
func clineBearerValue(token string) string {
	token = strings.TrimSpace(token)
	if token == "" || strings.HasPrefix(token, clineTokenPrefix) {
		return token
	}
	return clineTokenPrefix + token
}

// parseClineTimestamp 兼容毫秒时间戳、秒时间戳与 ISO 字符串。
func parseClineTimestamp(value any) time.Time {
	switch v := value.(type) {
	case float64:
		if v <= 0 {
			return time.Time{}
		}
		if v < 1e12 {
			v *= 1000 // 秒 → 毫秒
		}
		return time.UnixMilli(int64(v))
	case json.Number:
		f, err := v.Float64()
		if err != nil {
			return time.Time{}
		}
		return parseClineTimestamp(f)
	case string:
		if v == "" {
			return time.Time{}
		}
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			return t
		}
		var f float64
		if _, err := fmt.Sscanf(v, "%g", &f); err == nil {
			return parseClineTimestamp(f)
		}
	}
	return time.Time{}
}

// oauthFailure 把上游错误体归一成可读错误，保留 error_description。
func (c *ClineChannel) oauthFailure(what string, body map[string]any, status int) error {
	detail := firstNonEmpty(stringField(body, "error_description"), stringField(body, "error"))
	if detail == "" {
		return fmt.Errorf("%s失败（HTTP %d）", what, status)
	}
	return fmt.Errorf("%s失败（HTTP %d）：%s", what, status, detail)
}
