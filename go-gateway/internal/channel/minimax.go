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

// MiniMax Code 账号渠道。
//
// 登录：OAuth 2.0 设备码 + PKCE（S256），全程无需固定客户端密钥；
// 推理：上游是 **Anthropic Messages** 协议（/mavis/api/v1/llm/v1/messages），
// 因此复用 provider.AnthropicProvider 的传输与 SSE 逻辑，渠道只负责令牌与
// 请求体形态两件事。
const (
	miniMaxChannel     = "minimax"
	miniMaxAccountHost = "https://account.minimax.cn"
	miniMaxAPIHost     = "https://agent.minimax.cn"
	miniMaxClientID    = "mcode-public"
	miniMaxAudience    = "agent-backend"
	miniMaxScope       = "agent.default"

	miniMaxDeviceCodePath = "/oauth2/device/code"
	miniMaxTokenPath      = "/oauth2/token"
	miniMaxInferPath      = "/mavis/api/v1/llm/v1/messages"
)

// miniMaxAdaptiveOnlyPrefix 下的模型强制思考：送 thinking:{type:"disabled"} 会被
// 上游以 400（code 2013）硬拒。其余模型把请求原样透传。
const miniMaxAdaptiveOnlyPrefix = "MiniMax-M3.1"

// MiniMaxChannel 持有 minimax 的账号池与登录逻辑。
type MiniMaxChannel struct {
	accounts *Accounts
	client   *oauthClient
	// 端点主机可覆盖，测试时指向本地假上游；生产用常量默认值。
	accountHost string
	apiHost     string
}

// NewMiniMaxChannel 在 dir 下构造 minimax 渠道（凭据文件 <dir>/minimax.json）。
func NewMiniMaxChannel(dir string) *MiniMaxChannel {
	c := &MiniMaxChannel{
		client:      newOAuthClient(),
		accountHost: miniMaxAccountHost,
		apiHost:     miniMaxAPIHost,
	}
	c.accounts = NewAccounts(miniMaxChannel, NewStore(dir), c.client, c.refreshCredential)
	return c
}

// Accounts 供 provider 层取用令牌源。
func (c *MiniMaxChannel) Accounts() *Accounts { return c.accounts }

// Name 实现 Channel。
func (c *MiniMaxChannel) Name() string { return miniMaxChannel }

// MessagesURL 是 minimax 的 Anthropic Messages 推理端点。
func (c *MiniMaxChannel) MessagesURL() string { return c.apiHost + miniMaxInferPath }

// BeginLogin 实现 Channel：申请设备码并返回可轮询的登录会话。
func (c *MiniMaxChannel) BeginLogin(ctx context.Context) (LoginSession, error) {
	dc, verifier, err := c.beginDeviceLogin(ctx)
	if err != nil {
		return nil, err
	}
	return &deviceLogin{
		channel: miniMaxChannel,
		dc:      dc,
		wait: func(waitCtx context.Context) (*Credential, error) {
			return c.waitDeviceLogin(waitCtx, dc, verifier)
		},
	}, nil
}

// beginDeviceLogin 申请设备码并返回展示信息与本次登录的 PKCE verifier。
func (c *MiniMaxChannel) beginDeviceLogin(ctx context.Context) (DeviceCode, string, error) {
	pkce, err := newPKCE()
	if err != nil {
		return DeviceCode{}, "", err
	}
	form := url.Values{
		"client_id":             {miniMaxClientID},
		"scope":                 {miniMaxScope},
		"audience":              {miniMaxAudience},
		"code_challenge":        {pkce.Challenge},
		"code_challenge_method": {"S256"},
	}
	body, _, err := c.client.postForm(ctx, c.accountHost+miniMaxDeviceCodePath, form)
	if err != nil {
		return DeviceCode{}, "", fmt.Errorf("minimax 设备码申请失败: %w", err)
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
		return DeviceCode{}, "", fmt.Errorf("minimax 设备码响应缺少必要字段")
	}
	if dc.ExpiresIn <= 0 {
		dc.ExpiresIn = 5 * time.Minute
	}
	return dc, pkce.Verifier, nil
}

// waitDeviceLogin 轮询令牌直到用户完成授权、拒绝或设备码过期。
func (c *MiniMaxChannel) waitDeviceLogin(ctx context.Context, dc DeviceCode, verifier string) (*Credential, error) {
	cred, err := pollDeviceToken(ctx, dc, func(ctx context.Context) (pollOutcome, *Credential, error) {
		return c.pollOnce(ctx, dc, verifier)
	})
	if err != nil {
		return nil, err
	}
	if err := c.accounts.add(*cred); err != nil {
		return nil, err
	}
	return cred, nil
}

func (c *MiniMaxChannel) pollOnce(ctx context.Context, dc DeviceCode, verifier string) (pollOutcome, *Credential, error) {
	form := url.Values{
		"grant_type":    {"urn:ietf:params:oauth:grant-type:device_code"},
		"device_code":   {dc.DeviceCode},
		"client_id":     {miniMaxClientID},
		"code_verifier": {verifier},
	}
	body, status, err := c.client.postForm(ctx, c.accountHost+miniMaxTokenPath, form)
	if err != nil {
		// 设备码流程在「尚未授权」时也返回 4xx，这里按状态码归类，其余才算真错误。
		var httpErr *oauthHTTPError
		if !errors.As(err, &httpErr) {
			return 0, nil, err
		}
		body, status = httpErr.Body, httpErr.Status
	}

	state := stringField(body, "status")
	code := stringField(body, "error")
	switch {
	case status >= 200 && status < 300 && state == "pending":
		return pollPending, nil, nil
	case status >= 200 && status < 300 && state == "slow_down":
		return pollSlowDown, nil, nil
	case status >= 200 && status < 300 && (state == "denied" || state == "access_denied"):
		return pollDenied, nil, nil
	case status >= 200 && status < 300 && (state == "expired" || state == "expired_token"):
		return pollExpired, nil, nil
	case code == "authorization_pending":
		return pollPending, nil, nil
	case code == "slow_down":
		return pollSlowDown, nil, nil
	case status >= 200 && status < 300:
		cred, err := parseMiniMaxToken(body, "")
		if err != nil {
			return 0, nil, err
		}
		return pollSuccess, cred, nil
	}
	return 0, nil, fmt.Errorf("minimax 授权失败: %s", firstNonEmpty(code, fmt.Sprintf("HTTP %d", status)))
}

// refreshCredential 用 refresh_token 换新令牌。
func (c *MiniMaxChannel) refreshCredential(ctx context.Context, cred Credential) (Credential, error) {
	if cred.RefreshToken == "" {
		return Credential{}, fmt.Errorf("minimax 凭据缺少 refresh_token，需重新登录")
	}
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {cred.RefreshToken},
		"client_id":     {miniMaxClientID},
		"scope":         {miniMaxScope},
		"audience":      {miniMaxAudience},
	}
	body, _, err := c.client.postForm(ctx, c.accountHost+miniMaxTokenPath, form)
	if err != nil {
		return Credential{}, fmt.Errorf("minimax 续期失败: %w", err)
	}
	next, err := parseMiniMaxToken(body, cred.RefreshToken)
	if err != nil {
		return Credential{}, err
	}
	// 续期响应可能不回传昵称，保留原值。
	if next.Nickname == "" {
		next.Nickname = cred.Nickname
	}
	return *next, nil
}

// parseMiniMaxToken 校验并归一化令牌响应。previousRefresh 是续期时的旧值，上游
// 不回传 refresh_token 时沿用。
func parseMiniMaxToken(body map[string]any, previousRefresh string) (*Credential, error) {
	access := stringField(body, "access_token")
	if access == "" {
		return nil, fmt.Errorf("minimax 令牌响应缺少 access_token")
	}
	refresh := firstNonEmpty(stringField(body, "refresh_token"), previousRefresh)
	if refresh == "" {
		return nil, fmt.Errorf("minimax 令牌响应缺少 refresh_token")
	}
	if tokenType := stringField(body, "token_type"); !strings.EqualFold(tokenType, "bearer") {
		return nil, fmt.Errorf("minimax 令牌的 token_type 不是 Bearer")
	}
	if scope := stringField(body, "scope"); scope != "" && !hasScope(scope, miniMaxScope) {
		return nil, fmt.Errorf("minimax 令牌 scope 不含 %s", miniMaxScope)
	}

	expiresAt := decodeJWTExpiresAt(access)
	if expiresAt.IsZero() {
		if secs := numberField(body, "expires_in"); secs > 0 {
			expiresAt = time.Now().Add(time.Duration(secs) * time.Second)
		}
	}
	return &Credential{
		AccessToken:  access,
		RefreshToken: refresh,
		ExpiresAt:    expiresAt,
		AccountID:    decodeJWTClaim(access, "sub"),
	}, nil
}

// TransformRequest 是 provider 层的请求体改写钩子：MiniMax-M3.1 系列强制思考，
// 必须把 thinking 归一为 adaptive，否则上游直接 400。
func MiniMaxTransformRequest(body []byte) ([]byte, error) {
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil {
		return body, nil // 结构异常时原样透传，交给上游报错
	}
	model := strings.Trim(string(payload["model"]), `"`)
	if !strings.HasPrefix(model, miniMaxAdaptiveOnlyPrefix) {
		return body, nil
	}
	payload["thinking"] = json.RawMessage(`{"type":"adaptive"}`)
	return json.Marshal(payload)
}

func hasScope(scope, want string) bool {
	for _, part := range strings.Fields(scope) {
		if part == want {
			return true
		}
	}
	return false
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
