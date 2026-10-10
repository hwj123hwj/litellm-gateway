package channel

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// CodeBuddy（腾讯）/ WorkBuddy（国际版）账号渠道。
//
// 两个产品共用同一套私有登录与推理协议（下称 buddy 协议），差别只在产品常量：
// 端点、X-Product-Code、客户端版本号，以及 WorkBuddy 按模型家族切换 User-Agent。
// 因此这里用一个渠道实现 + 两张产品配置表，而不是复制两份代码。
//
// 登录不是设备码，也没有回环回调：网关先向 /v2/plugin/auth/state 申请一个 state
// 与登录地址，把地址交给用户在浏览器打开；随后用同一个 state 轮询
// /v2/plugin/auth/token（未就绪时上游回 code=11217）拿到令牌，再轮询
// /v2/plugin/login/account（未就绪回 code=12151）补上账号信息。
//
// 推理走 OpenAI Chat Completions 协议（/v2/chat/completions），因此复用
// provider.OpenAIProvider 的协议转换；渠道只负责令牌、复刻头与续期。
//
// ⚠️ 与 minimax/cline/lobster/gemini 一样，这是复刻官方客户端的非官方用法，
// 可能违反服务条款并导致账号被封。仅用于本机个人测试，不要对外分发。
const (
	buddyChannelCodeBuddy = "codebuddy"
	buddyChannelWorkBuddy = "workbuddy"

	buddyAuthStatePath    = "/v2/plugin/auth/state"
	buddyAuthTokenPath    = "/v2/plugin/auth/token"
	buddyLoginAccountPath = "/v2/plugin/login/account"
	buddyAuthRefreshPath  = "/v2/plugin/auth/token/refresh"
	buddyChatPath         = "/v2/chat/completions"

	// buddyRefreshSource 是续期请求的 X-Auth-Refresh-Source 取值：官方 IDE 客户端
	// 用它告诉服务端这次续期来自哪条产品线。
	buddyRefreshSource = "ide-main"

	// buddyTokenNotReadyCode / buddyAccountNotReadyCode 是轮询期间「还没好」的
	// 业务码：上游用 HTTP 非 2xx + 该 code 表示继续等，而不是失败。
	buddyTokenNotReadyCode   = 11217
	buddyAccountNotReadyCode = 12151

	buddyWorkBuddyUAIntl = "WorkBuddy/5.5.2 WorkBuddy AI/5.5.2 CLI/5.5.2"
	buddyWorkBuddyUACN   = "WorkBuddy/5.5.2 WorkBuddy/5.5.2 CLI/5.5.2"
)

// buddyStepTimeout 是登录单个轮询步骤（取 token / 取账号）的最长等待时间，
// 与官方客户端 LOGIN_TIMEOUT_MS 一致。
const buddyStepTimeout = 5 * time.Minute

// buddyLoginTimeout 是整个登录会话的上限：取 token 与取账号各留一份预算。
const buddyLoginTimeout = 2 * buddyStepTimeout

// buddyPollInterval 是轮询间隔。作为变量是为了让测试不必真的每秒等一次。
var buddyPollInterval = time.Second

// buddy 协议的请求头名。
const (
	buddyHeaderDomain        = "X-Domain"
	buddyHeaderNoAuth        = "X-No-Authorization"
	buddyHeaderNoUserID      = "X-No-User-Id"
	buddyHeaderNoEnterprise  = "X-No-Enterprise-Id"
	buddyHeaderNoDepartment  = "X-No-Department-Info"
	buddyHeaderEnterpriseID  = "X-Enterprise-Id"
	buddyHeaderTenantID      = "X-Tenant-Id"
	buddyHeaderProduct       = "X-Product"
	buddyHeaderProductCode   = "X-Product-Code"
	buddyHeaderRefreshToken  = "X-Refresh-Token"
	buddyHeaderRefreshSource = "X-Auth-Refresh-Source"
)

// buddyUAMatch 是「模型名前缀 → User-Agent」的一条规则，首个命中者生效。
type buddyUAMatch struct {
	prefix string
	ua     string
}

// buddyProduct 是一个 buddy 产品的常量表。
type buddyProduct struct {
	id              string
	platform        string
	endpoint        string
	apiDomain       string
	productCode     string
	userAgent       string
	uaByModelFamily []buddyUAMatch
	attributionName string
	clientVersion   string
	// appendSessionParams 为真时，登录地址要补 version 与 loginSessionId 两个
	// 查询参数（国际版插件形态需要，中国版不需要）。
	appendSessionParams bool
	pluginVersion       string
}

// buddyProducts 是两个产品的配置表，键为渠道名（= providers.yaml 的 provider 名）。
var buddyProducts = map[string]buddyProduct{
	buddyChannelCodeBuddy: {
		id:              buddyChannelCodeBuddy,
		platform:        "ide",
		endpoint:        "https://copilot.tencent.com",
		apiDomain:       "copilot.tencent.com",
		productCode:     "codebuddy",
		userAgent:       "CodeBuddyIDE/1.106.1",
		attributionName: "CodeBuddy",
		clientVersion:   "1.106.1",
		// 中国版只有一条产品线，无需按模型分档：全部模型沿用 IDE UA。
		appendSessionParams: false,
	},
	buddyChannelWorkBuddy: {
		id:              buddyChannelWorkBuddy,
		platform:        "workbuddy-ai",
		endpoint:        "https://www.workbuddy.ai",
		apiDomain:       "www.workbuddy.ai",
		productCode:     "workbuddy",
		userAgent:       buddyWorkBuddyUAIntl,
		attributionName: "WorkBuddy",
		clientVersion:   "5.5.2",
		// 国际版独有模型线（GPT / Gemini / Claude 系）→ 国际版形态；
		// 国内系模型（glm / hy / kimi / minimax）→ 国内客户端形态。
		uaByModelFamily: []buddyUAMatch{
			{prefix: "gpt-", ua: buddyWorkBuddyUAIntl},
			{prefix: "gemini-", ua: buddyWorkBuddyUAIntl},
			{prefix: "claude-", ua: buddyWorkBuddyUAIntl},
			{prefix: "glm-", ua: buddyWorkBuddyUACN},
			{prefix: "hy", ua: buddyWorkBuddyUACN},
			{prefix: "kimi-", ua: buddyWorkBuddyUACN},
			{prefix: "minimax-", ua: buddyWorkBuddyUACN},
		},
		appendSessionParams: true,
		pluginVersion:       "5.5.2",
	},
}

// BuddyChannel 持有某个 buddy 产品的账号池与登录逻辑。
type BuddyChannel struct {
	accounts *Accounts
	client   *oauthClient
	product  buddyProduct
	// endpoint 可覆盖（测试指向本地假上游）；生产用 product.endpoint。
	endpoint string
}

// NewCodeBuddyChannel 在 dir 下构造 CodeBuddy 渠道（凭据文件 <dir>/codebuddy.json）。
func NewCodeBuddyChannel(dir string) *BuddyChannel {
	return newBuddyChannel(dir, buddyChannelCodeBuddy)
}

// NewWorkBuddyChannel 在 dir 下构造 WorkBuddy 渠道（凭据文件 <dir>/workbuddy.json）。
func NewWorkBuddyChannel(dir string) *BuddyChannel {
	return newBuddyChannel(dir, buddyChannelWorkBuddy)
}

func newBuddyChannel(dir, name string) *BuddyChannel {
	product := buddyProducts[name]
	c := &BuddyChannel{
		client:   newOAuthClient(),
		product:  product,
		endpoint: product.endpoint,
	}
	c.accounts = NewAccounts(name, NewStore(dir), c.client, c.refreshCredential)
	return c
}

// Accounts 供 provider 层取用令牌源。
func (c *BuddyChannel) Accounts() *Accounts { return c.accounts }

// Name 实现 Channel。
func (c *BuddyChannel) Name() string { return c.product.id }

// ChatURL 是 buddy 的 OpenAI Chat Completions 推理端点。
func (c *BuddyChannel) ChatURL() string { return c.endpoint + buddyChatPath }

// Namespace 是本渠道对外模型名的前缀。buddy 的产品与既有供应商大量重名
// （glm-5.3、glm-5.2、kimi-k2.6、deepseek-v4-flash…），裸名注册会覆盖掉既有
// provider 的路由。加前缀后两路并存，前缀在发往上游前由 TransformRequest 剥掉。
func (c *BuddyChannel) Namespace() string { return c.product.id + "/" }

// ClientHeaders 是复刻对应产品客户端所需的固定请求头。User-Agent 按模型家族
// 解析（WorkBuddy 国际/国内两套形态），因此需要模型名。
func (c *BuddyChannel) ClientHeaders(model string) map[string]string {
	return map[string]string{
		"Accept":               "text/event-stream",
		"Content-Type":         "application/json",
		buddyHeaderDomain:      c.product.apiDomain,
		buddyHeaderProductCode: c.product.productCode,
		"X-Agent-Purpose":      "conversation",
		"X-IDE-Name":           c.product.attributionName,
		"X-IDE-Type":           c.product.attributionName,
		"X-IDE-Version":        c.product.clientVersion,
		buddyHeaderProduct:     c.product.attributionName,
		"User-Agent":           c.resolveUserAgent(model),
	}
}

// resolveUserAgent 按模型家族选 User-Agent，首个命中的规则生效，否则用产品默认值。
//
// 入参可能带本渠道的命名空间前缀（装配时传的是 providers.yaml 里的对外模型 ID，形如
// workbuddy/gpt-5.6-sol），前缀会让 "gpt-" 这类匹配失手，故先剥掉再比对。
func (c *BuddyChannel) resolveUserAgent(model string) string {
	model = strings.TrimPrefix(model, c.Namespace())
	for _, rule := range c.product.uaByModelFamily {
		if strings.HasPrefix(model, rule.prefix) {
			return rule.ua
		}
	}
	return c.product.userAgent
}

// TransformRequest 把对外模型名剥掉本渠道的命名空间前缀，再发往上游。
func (c *BuddyChannel) TransformRequest(body []byte) ([]byte, error) {
	payload := map[string]json.RawMessage{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return body, nil // 结构异常时原样透传，交给上游报错
	}
	raw, ok := payload["model"]
	if !ok {
		return body, nil
	}
	var model string
	if err := json.Unmarshal(raw, &model); err != nil {
		return body, nil
	}
	stripped := strings.TrimPrefix(model, c.Namespace())
	if stripped == model {
		return body, nil
	}
	encoded, err := json.Marshal(stripped)
	if err != nil {
		return body, nil
	}
	payload["model"] = encoded
	return json.Marshal(payload)
}

// BeginLogin 实现 Channel：申请 state 与登录地址，返回可轮询的登录会话。
func (c *BuddyChannel) BeginLogin(ctx context.Context) (LoginSession, error) {
	state, authURL, err := c.fetchAuthState(ctx)
	if err != nil {
		return nil, err
	}
	loginURL := c.decorateLoginURL(authURL)
	return &buddyLogin{
		channel: c,
		state:   state,
		dc: DeviceCode{
			VerificationURI:         loginURL,
			VerificationURIComplete: loginURL,
			ExpiresIn:               buddyLoginTimeout,
			Interval:                buddyPollInterval,
		},
		timeout: buddyLoginTimeout,
	}, nil
}

// fetchAuthState 申请一次登录的 state 与登录地址（POST /v2/plugin/auth/state）。
func (c *BuddyChannel) fetchAuthState(ctx context.Context) (string, string, error) {
	rawURL := c.endpoint + buddyAuthStatePath + "?platform=" + url.QueryEscape(c.product.platform)
	headers := map[string]string{
		buddyHeaderDomain:       c.product.apiDomain,
		buddyHeaderNoAuth:       "true",
		buddyHeaderNoUserID:     "true",
		buddyHeaderNoEnterprise: "true",
		buddyHeaderNoDepartment: "true",
		"User-Agent":            c.product.userAgent,
	}
	body, _, err := c.doJSON(ctx, http.MethodPost, rawURL, headers, nil)
	if err != nil {
		var httpErr *oauthHTTPError
		if errors.As(err, &httpErr) {
			return "", "", fmt.Errorf("buddy 申请登录状态失败（HTTP %d）：%s", httpErr.Status, httpErr.Error())
		}
		return "", "", fmt.Errorf("buddy 申请登录状态失败: %w", err)
	}
	data, ok := buddyData(body)
	if !ok {
		return "", "", fmt.Errorf("buddy 登录状态响应缺少 data 字段")
	}
	state := buddyStringField(data, "state")
	authURL := buddyStringField(data, "authUrl")
	if state == "" {
		return "", "", fmt.Errorf("buddy 登录状态响应缺少 state 字段")
	}
	if authURL == "" {
		return "", "", fmt.Errorf("buddy 登录状态响应缺少 authUrl 字段")
	}
	return state, authURL, nil
}

// decorateLoginURL 按产品要求给登录地址补查询参数（带 loginSessionId 便于服务端
// 关联本次会话）。失败时返回原地址，不影响登录能否进行。
func (c *BuddyChannel) decorateLoginURL(authURL string) string {
	if !c.product.appendSessionParams {
		return authURL
	}
	parsed, err := url.Parse(authURL)
	if err != nil {
		return authURL
	}
	query := parsed.Query()
	if c.product.pluginVersion != "" {
		query.Set("version", c.product.pluginVersion)
	}
	query.Set("loginSessionId", mustRandomHex(16))
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

// buddyLogin 是一次进行中的 buddy 登录：拿 state 轮询令牌与账号信息。
type buddyLogin struct {
	channel *BuddyChannel
	state   string
	dc      DeviceCode
	timeout time.Duration
}

func (l *buddyLogin) Channel() string        { return l.channel.Name() }
func (l *buddyLogin) DeviceCode() DeviceCode { return l.dc }

// Wait 实现 LoginSession：先轮询令牌，再轮询账号信息，最后落盘。
func (l *buddyLogin) Wait(ctx context.Context) (*Credential, error) {
	waitCtx, cancel := context.WithTimeout(ctx, l.timeout)
	defer cancel()

	token, err := l.channel.pollToken(waitCtx, l.state)
	if err != nil {
		return nil, err
	}
	account, err := l.channel.fetchAccount(waitCtx, l.state, token)
	if err != nil {
		return nil, err
	}
	cred := buildBuddyCredential(token, account)
	if err := l.channel.accounts.add(*cred); err != nil {
		return nil, err
	}
	return cred, nil
}

// pollToken 轮询令牌直到就绪或超时。未就绪时上游回 code=11217。
func (c *BuddyChannel) pollToken(ctx context.Context, state string) (*buddyToken, error) {
	rawURL := c.endpoint + buddyAuthTokenPath + "?state=" + url.QueryEscape(state)
	headers := map[string]string{
		buddyHeaderNoAuth: "true",
		"User-Agent":      c.product.userAgent,
	}
	data, err := c.pollData(ctx, "auth/token", buddyTokenNotReadyCode, func(ctx context.Context) (map[string]any, int, error) {
		return c.doJSON(ctx, http.MethodGet, rawURL, headers, nil)
	})
	if err != nil {
		return nil, err
	}
	token, err := parseBuddyToken(data, time.Now())
	if err != nil {
		return nil, err
	}
	return token, nil
}

// fetchAccount 轮询账号信息直到就绪或超时。未就绪时上游回 code=12151。
func (c *BuddyChannel) fetchAccount(ctx context.Context, state string, token *buddyToken) (*buddyAccount, error) {
	rawURL := c.endpoint + buddyLoginAccountPath + "?state=" + url.QueryEscape(state)
	// X-Domain 优先用服务端刚下发的 token.domain（本次登录的权威值），
	// 为空时回退到产品域名——不能裸用 token.domain，空值会让请求以空 X-Domain 发出。
	headers := map[string]string{
		buddyHeaderDomain:       firstNonEmpty(token.Domain, c.product.apiDomain),
		"Authorization":         "Bearer " + token.AccessToken,
		buddyHeaderNoUserID:     "true",
		buddyHeaderNoEnterprise: "true",
		"User-Agent":            c.product.userAgent,
	}
	data, err := c.pollData(ctx, "login/account", buddyAccountNotReadyCode, func(ctx context.Context) (map[string]any, int, error) {
		return c.doJSON(ctx, http.MethodGet, rawURL, headers, nil)
	})
	if err != nil {
		return nil, err
	}
	return parseBuddyAccount(data), nil
}

// pollData 是 buddy 两个轮询步骤的共用循环：fetch 返回 HTTP 200 且带 data 即就绪；
// 非 2xx 且业务码等于 notReadyCode 视为「继续等」；其余按终态失败返回。
// 传输层错误同样按「继续等」处理（官方客户端在轮询里也是吞掉网络错误重试）。
func (c *BuddyChannel) pollData(ctx context.Context, step string, notReadyCode int, fetch func(context.Context) (map[string]any, int, error)) (map[string]any, error) {
	deadline := time.Now().Add(buddyStepTimeout)
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		body, status, err := fetch(ctx)
		var httpErr *oauthHTTPError
		if errors.As(err, &httpErr) {
			body, status, err = httpErr.Body, httpErr.Status, nil
		}
		if err == nil {
			if status == http.StatusOK {
				if data, ok := buddyData(body); ok {
					return data, nil
				}
			} else if code := int(numberField(body, "code")); code != notReadyCode {
				return nil, fmt.Errorf("buddy %s HTTP %d code=%d: %s", step, status, code, stringField(body, "message"))
			}
		}

		if time.Now().After(deadline) {
			return nil, fmt.Errorf("%w: buddy %s 轮询超时，请重新登录", ErrLoginCancelled, step)
		}
		if err := sleepCtx(ctx, buddyPollInterval); err != nil {
			return nil, err
		}
	}
}

// refreshCredential 用 refresh_token 换新令牌（POST /v2/plugin/auth/token/refresh）。
func (c *BuddyChannel) refreshCredential(ctx context.Context, cred Credential) (Credential, error) {
	if cred.RefreshToken == "" {
		return Credential{}, fmt.Errorf("buddy 凭据缺少 refresh_token，需重新登录")
	}
	headers := map[string]string{
		buddyHeaderDomain:        c.product.apiDomain,
		buddyHeaderRefreshToken:  cred.RefreshToken,
		buddyHeaderRefreshSource: buddyRefreshSource,
		"Authorization":          "Bearer " + cred.AccessToken,
		"User-Agent":             c.product.userAgent,
	}
	// 企业账号的续期也要带上企业/租户标识，否则会被当个人账号处理。
	if enterprise := cred.Extra["enterprise_id"]; enterprise != "" {
		headers[buddyHeaderEnterpriseID] = enterprise
		headers[buddyHeaderTenantID] = enterprise
	}

	body, status, err := c.doJSON(ctx, http.MethodPost, c.endpoint+buddyAuthRefreshPath, headers, map[string]any{})
	if err != nil {
		var httpErr *oauthHTTPError
		if !errors.As(err, &httpErr) {
			return Credential{}, fmt.Errorf("buddy 续期失败: %w", err)
		}
		body, status = httpErr.Body, httpErr.Status
	}
	if status != http.StatusOK {
		code := int(numberField(body, "code"))
		message := stringField(body, "message")
		if buddyRefreshTerminal(status, code, message) {
			return Credential{}, fmt.Errorf("%w: buddy 续期被拒绝（HTTP %d %s）", ErrLoginCancelled, status, message)
		}
		return Credential{}, fmt.Errorf("buddy 续期失败（HTTP %d code=%d）：%s", status, code, message)
	}
	data, ok := buddyData(body)
	if !ok {
		return Credential{}, fmt.Errorf("buddy 续期响应缺少 data 字段")
	}
	token, err := parseBuddyToken(data, time.Now())
	if err != nil {
		return Credential{}, err
	}
	// 续期响应不回传 refresh_token 与账号信息，全部沿用旧值。
	if token.RefreshToken == "" {
		token.RefreshToken = cred.RefreshToken
	}
	next := buildBuddyCredential(token, &buddyAccount{
		UID:          cred.AccountID,
		Nickname:     cred.Nickname,
		EnterpriseID: cred.Extra["enterprise_id"],
		AccountType:  cred.Extra["account_type"],
	})
	return *next, nil
}

// buddyRefreshTerminal 判定续期失败是否为终态（refresh_token 已失效，重试无意义）。
func buddyRefreshTerminal(status, code int, message string) bool {
	if status == http.StatusUnauthorized || status == http.StatusForbidden || code == 401 || code == 403 {
		return true
	}
	return strings.Contains(message, "expired") || strings.Contains(message, "invalid")
}

// doJSON 发一次 buddy 协议请求。payload 为 nil 时不带请求体（登录申请与轮询
// 都是无体请求）；非 2xx 时返回 *oauthHTTPError，错误体已解析好。
func (c *BuddyChannel) doJSON(ctx context.Context, method, rawURL string, headers map[string]string, payload any) (map[string]any, int, error) {
	var reader io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return nil, 0, fmt.Errorf("marshal buddy request: %w", err)
		}
		reader = strings.NewReader(string(encoded))
	}
	req, err := http.NewRequestWithContext(ctx, method, rawURL, reader)
	if err != nil {
		return nil, 0, fmt.Errorf("build buddy request: %w", err)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	for name, value := range headers {
		if value != "" {
			req.Header.Set(name, value)
		}
	}
	return c.client.do(req)
}

// ─── 响应解析 ────────────────────────────────────────────────────────────────

// buddyToken 是 parseTokenData 的 Go 对应物。
type buddyToken struct {
	AccessToken      string
	RefreshToken     string
	ExpiresAt        time.Time
	RefreshExpiresAt time.Time
	TokenType        string
	Scope            string
	Domain           string
}

// buddyAccount 是 parseAccountData 的 Go 对应物。
type buddyAccount struct {
	UID          string
	Nickname     string
	EnterpriseID string
	AccountType  string
}

// parseBuddyToken 解析令牌响应；accessToken 为空视为登录未真正完成。
func parseBuddyToken(data map[string]any, now time.Time) (*buddyToken, error) {
	access := buddyStringField(data, "accessToken")
	if access == "" {
		return nil, fmt.Errorf("buddy 令牌响应缺少 accessToken")
	}
	tokenType := buddyStringField(data, "tokenType")
	if tokenType == "" {
		tokenType = "Bearer"
	}
	expiresAt, _ := buddyExpiry(data, "expiresAt", "expiresIn", access, now)
	if expiresAt.IsZero() {
		// expires_at 缺失或不可解析时按 JWT exp 兜底（与官方客户端一致）。
		expiresAt = decodeJWTExpiresAt(access)
	}
	refreshExpiresAt, _ := buddyExpiry(data, "refreshExpiresAt", "refreshExpiresIn", access, now)
	return &buddyToken{
		AccessToken:      access,
		RefreshToken:     buddyStringField(data, "refreshToken"),
		ExpiresAt:        expiresAt,
		RefreshExpiresAt: refreshExpiresAt,
		TokenType:        tokenType,
		Scope:            buddyStringField(data, "scope"),
		Domain:           buddyStringField(data, "domain"),
	}, nil
}

// parseBuddyAccount 解析账号信息；所有字段都可缺省（上游按需下发）。
func parseBuddyAccount(data map[string]any) *buddyAccount {
	accountType := buddyStringField(data, "type")
	if accountType == "" {
		accountType = "personal"
	}
	return &buddyAccount{
		UID:          buddyStringField(data, "uid"),
		Nickname:     buddyStringField(data, "nickname"),
		EnterpriseID: buddyStringField(data, "enterpriseId"),
		AccountType:  accountType,
	}
}

// buildBuddyCredential 把令牌与账号信息装配成落盘凭据。
// account.uid 与 token 里的 JWT 声明都可能缺失，故逐级回退。
func buildBuddyCredential(token *buddyToken, account *buddyAccount) *Credential {
	extra := map[string]string{
		"domain":        token.Domain,
		"token_type":    token.TokenType,
		"scope":         token.Scope,
		"enterprise_id": account.EnterpriseID,
		"account_type":  account.AccountType,
	}
	if !token.RefreshExpiresAt.IsZero() {
		extra["refresh_expires_at"] = token.RefreshExpiresAt.UTC().Format(time.RFC3339)
	}
	return &Credential{
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		ExpiresAt:    token.ExpiresAt,
		AccountID:    firstNonEmpty(account.UID, buddyJWTClaim(token.AccessToken, "sub")),
		Nickname:     firstNonEmpty(account.Nickname, buddyJWTNickname(token.AccessToken)),
		Extra:        extra,
	}
}

// buddyExpiry 复刻官方客户端的过期时间推导：
//   - 优先取绝对字段（纯数字按秒/毫秒判断，否则按日期字符串解析）；
//   - 都没有时用相对字段（秒）加基准时间，基准优先取 JWT 的 iat（不是当前时间，
//     这样重放同一个令牌得到的过期时刻稳定），无 iat 才用 now。
//
// 返回 ok=false 表示字段缺失或不可解析，由调用方决定兜底方式。
func buddyExpiry(record map[string]any, absoluteKey, relativeKey, accessToken string, now time.Time) (time.Time, bool) {
	if absolute := buddyStringField(record, absoluteKey); absolute != "" {
		if t, ok := parseBuddyAbsolute(absolute); ok {
			return t, true
		}
		return time.Time{}, false
	}
	relative := numberField(record, relativeKey)
	if relative <= 0 {
		return time.Time{}, false
	}
	base := buddyJWTIat(accessToken)
	if base.IsZero() {
		base = now
	}
	return base.Add(time.Duration(relative * float64(time.Second))), true
}

// parseBuddyAbsolute 解析绝对过期值：纯数字按毫秒（>1e12）或秒处理，否则按
// ISO 8601 日期解析。
func parseBuddyAbsolute(value string) (time.Time, bool) {
	if isAllDigits(value) {
		number, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return time.Time{}, false
		}
		if number <= 1e12 {
			number *= 1000
		}
		return time.UnixMilli(number), true
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if t, err := time.Parse(layout, value); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

func isAllDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// buddyStringField 读取字符串字段：数字按十进制字符串处理，其余类型返回空串
// （对应官方客户端的 readStringField）。
func buddyStringField(obj map[string]any, key string) string {
	if obj == nil {
		return ""
	}
	switch value := obj[key].(type) {
	case string:
		return strings.TrimSpace(value)
	case float64:
		return strconv.FormatFloat(value, 'f', -1, 64)
	case json.Number:
		return value.String()
	}
	return ""
}

// buddyData 取出信封里的 data 对象；缺失或为 null 时 ok=false。
func buddyData(body map[string]any) (map[string]any, bool) {
	if body == nil {
		return nil, false
	}
	data, ok := body["data"].(map[string]any)
	if !ok || data == nil {
		return nil, false
	}
	return data, true
}

// ─── JWT 声明 ──────────────────────────────────────────────────────────────
//
// 这里不复用 oauth.go 的 decodeJWTClaim/decodeJWTExpiresAt：那两个要求 JWT 恰好
// 三段，而官方客户端只要求「至少两段」并直接解第二段，宽松一些才能与它对齐。

func buddyJWTClaims(token string) map[string]any {
	segments := strings.Split(token, ".")
	if len(segments) < 2 {
		return nil
	}
	payload, err := base64.RawURLEncoding.DecodeString(segments[1])
	if err != nil {
		return nil
	}
	var claims map[string]any
	if json.Unmarshal(payload, &claims) != nil {
		return nil
	}
	return claims
}

func buddyJWTClaim(token, claim string) string {
	return stringField(buddyJWTClaims(token), claim)
}

// buddyJWTNickname 从 JWT 里取昵称，字段优先级与官方客户端一致。
func buddyJWTNickname(token string) string {
	claims := buddyJWTClaims(token)
	return firstNonEmpty(
		stringField(claims, "nickname"),
		stringField(claims, "preferred_username"),
		stringField(claims, "name"),
	)
}

// buddyJWTIat 取 JWT 的签发时间（iat），缺失返回零值。
func buddyJWTIat(token string) time.Time {
	seconds := numberField(buddyJWTClaims(token), "iat")
	if seconds <= 0 {
		return time.Time{}
	}
	return time.Unix(int64(seconds), 0)
}
