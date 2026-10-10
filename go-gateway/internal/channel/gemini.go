package channel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Gemini（Google CloudCode / Antigravity）账号渠道。
//
// 登录是标准 OAuth 2.0 授权码 + 回环回调：网关起本地回调服务，用户浏览器完成
// Google 授权后回调 ?code=，网关用 code 换 access/refresh token。与 lobster 共用
// channel.callbackServer，区别只是 redirect_uri 主机必须是 localhost（Google 会
// 逐字比对，127.0.0.1 不在允许列表）。
//
// 推理端点是 CloudCode 私有协议（v1internal:streamGenerateContent），既不是
// OpenAI 也不是 Anthropic，因此由 provider.CloudCodeProvider 负责协议翻译；本渠道
// 只提供令牌、OAuth 续期与 CloudCode 项目 ID 缓存。
//
// ⚠️ 与 minimax/cline 一样，这是复刻官方客户端的非官方用法，可能违反服务条款。
const (
	geminiChannel          = "gemini"
	geminiAuthEndpoint     = "https://accounts.google.com/o/oauth2/v2/auth"
	geminiTokenEndpoint    = "https://oauth2.googleapis.com/token"
	geminiUserinfoEndpoint = "https://www.googleapis.com/oauth2/v2/userinfo"
	geminiCallbackPath     = "/oauth-callback"
	geminiRedirectHost     = "localhost"

	// geminiClientCredentialFile 是本地存放 OAuth 客户端凭据的文件名，位于
	// <gatewayHome>（channel 凭据目录的上一级）。内容 {"client_id":..,"client_secret":..}。
	//
	// 客户端凭据刻意不进仓库：本仓库是公开仓库，硬编码第三方 OAuth 客户端密钥会被
	// GitHub 密钥扫描拦截，也等于把别人的客户端凭据公开出去。因此改为「本地文件或
	// 环境变量」二选一，未配置时登录直接给出可操作的报错（见 BeginLogin）。
	// 用户需自行注册 Google OAuth 客户端（类型选桌面应用，回调地址
	// http://localhost:<port>/oauth-callback）后填入。
	geminiClientCredentialFile = "gemini-client.json"
)

// geminiLoginTimeout 是授权流程的最长等待时间（要留够用户在 Google 页面登录的工夫）。
const geminiLoginTimeout = 6 * time.Minute

// geminiScopes 是复刻 Antigravity 客户端所需的 OAuth 授权范围。
var geminiScopes = []string{
	"openid",
	"https://www.googleapis.com/auth/cloud-platform",
	"https://www.googleapis.com/auth/userinfo.email",
	"https://www.googleapis.com/auth/userinfo.profile",
	"https://www.googleapis.com/auth/cclog",
	"https://www.googleapis.com/auth/experimentsandconfigs",
}

// GeminiChannel 持有 gemini 的账号池与登录逻辑。
type GeminiChannel struct {
	accounts *Accounts
	auth     *geminiAuth
	client   *oauthClient
	// home 是 <gatewayHome>，用于定位本地 OAuth 客户端凭据文件（可选）。
	home string
	// 端点与客户端凭据可覆盖，测试时指向本地假上游。
	authEndpoint     string
	tokenEndpoint    string
	userinfoEndpoint string
	clientID         string
	clientSecret     string
	loginTimeout     time.Duration
}

// NewGeminiChannel 在 dir 下构造 gemini 渠道（凭据文件 <dir>/gemini.json）。
// OAuth 客户端凭据（client_id/client_secret）从本地解析，见 resolveCredentials。
func NewGeminiChannel(dir string) *GeminiChannel {
	c := &GeminiChannel{
		client:           newOAuthClient(),
		home:             filepath.Dir(dir),
		authEndpoint:     geminiAuthEndpoint,
		tokenEndpoint:    geminiTokenEndpoint,
		userinfoEndpoint: geminiUserinfoEndpoint,
		loginTimeout:     geminiLoginTimeout,
	}
	c.clientID, c.clientSecret = c.resolveCredentials()
	c.accounts = NewAccounts(geminiChannel, NewStore(dir), c.client, c.refreshCredential)
	c.auth = &geminiAuth{Accounts: c.accounts}
	return c
}

// resolveCredentials 解析 OAuth 客户端凭据，优先级：环境变量 > 本地凭据文件。
// 两者都没有时返回空串，登录时由 BeginLogin 给出明确指引。
//
// 不用硬编码默认值：客户端凭据属于第三方应用的机密，写进公开仓库会泄露。用户自行
// 注册 Google OAuth 客户端后，用环境变量或 <gatewayHome>/gemini-client.json 提供。
func (c *GeminiChannel) resolveCredentials() (string, string) {
	id := firstNonEmpty(
		os.Getenv("CMDC_PAK_GOOGLE_CLIENT_ID"),
		os.Getenv("GEMINI_CHANNEL_CLIENT_ID"),
	)
	secret := firstNonEmpty(
		os.Getenv("CMDC_PAK_GOOGLE_CLIENT_SECRET"),
		os.Getenv("GEMINI_CHANNEL_CLIENT_SECRET"),
	)
	if id != "" && secret != "" {
		return id, secret
	}
	fileID, fileSecret := c.loadCredentialFile()
	return firstNonEmpty(id, fileID), firstNonEmpty(secret, fileSecret)
}

// loadCredentialFile 读取 <home>/gemini-client.json（best-effort）。
func (c *GeminiChannel) loadCredentialFile() (string, string) {
	if c.home == "" {
		return "", ""
	}
	data, err := os.ReadFile(filepath.Join(c.home, geminiClientCredentialFile))
	if err != nil {
		return "", ""
	}
	var payload struct {
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return "", ""
	}
	return payload.ClientID, payload.ClientSecret
}

// configured 报告是否已配置客户端凭据。
func (c *GeminiChannel) configured() bool {
	return c.clientID != "" && c.clientSecret != ""
}

// Accounts 供 Admin API 查看账号状态/登出。
func (c *GeminiChannel) Accounts() *Accounts { return c.accounts }

// AuthSource 是交给 provider 的令牌源：在账号池之上多实现 CloudCode 项目缓存。
func (c *GeminiChannel) AuthSource() *geminiAuth { return c.auth }

// Name 实现 Channel。
func (c *GeminiChannel) Name() string { return geminiChannel }

// geminiAuth 在账号池之上补 CloudCode 项目 ID 的缓存接口，供
// provider.CloudCodeProvider 通过可选接口读写（loadCodeAssist 的结果只查一次）。
// 嵌入 *Accounts 后自动满足 provider.AuthSource（BearerToken / Refresh）。
type geminiAuth struct {
	*Accounts
	mu      sync.RWMutex
	project string
}

// CloudCodeProject 返回缓存的 CloudCode 项目 ID（空串表示未缓存）。
func (g *geminiAuth) CloudCodeProject() string {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.project
}

// SetCloudCodeProject 缓存 CloudCode 项目 ID。
func (g *geminiAuth) SetCloudCodeProject(project string) {
	if project == "" {
		return
	}
	g.mu.Lock()
	g.project = project
	g.mu.Unlock()
}

// BeginLogin 实现 Channel：起本地回调服务，返回带授权地址的会话。
// 未配置 OAuth 客户端凭据时直接失败，并给出可操作的指引（Admin API 会把它
// 作为 502 的 error 文本暴露出来）。
func (c *GeminiChannel) BeginLogin(ctx context.Context) (LoginSession, error) {
	if !c.configured() {
		return nil, fmt.Errorf(
			"gemini 渠道缺少 OAuth 客户端凭据：请注册 Google OAuth 桌面应用客户端后，设置 GEMINI_CHANNEL_CLIENT_ID / GEMINI_CHANNEL_CLIENT_SECRET 环境变量，或写入 %s",
			filepath.Join(c.home, geminiClientCredentialFile),
		)
	}
	server, err := newCallbackServerOnHost(geminiCallbackPath, geminiRedirectHost)
	if err != nil {
		return nil, err
	}
	return &geminiLogin{
		channel: c,
		server:  server,
		timeout: c.loginTimeout,
	}, nil
}

// geminiLogin 是一次进行中的 OAuth 授权码登录。
type geminiLogin struct {
	channel *GeminiChannel
	server  *callbackServer
	timeout time.Duration

	closeOnce sync.Once
}

// Channel 实现 LoginSession。
func (l *geminiLogin) Channel() string { return geminiChannel }

// DeviceCode 实现 LoginSession：授权码流程没有设备码，用 DeviceCode 承载授权地址。
func (l *geminiLogin) DeviceCode() DeviceCode {
	uri := l.authURL()
	return DeviceCode{
		VerificationURI:         uri,
		VerificationURIComplete: uri,
		ExpiresIn:               l.timeout,
	}
}

// authURL 拼 Google 授权地址。
func (l *geminiLogin) authURL() string {
	query := url.Values{}
	query.Set("client_id", l.channel.clientID)
	query.Set("response_type", "code")
	query.Set("redirect_uri", l.redirectURI())
	query.Set("scope", strings.Join(geminiScopes, " "))
	query.Set("state", l.server.stateValue())
	query.Set("access_type", "offline")
	query.Set("include_granted_scopes", "true")
	query.Set("prompt", "consent")
	return l.channel.authEndpoint + "?" + query.Encode()
}

func (l *geminiLogin) redirectURI() string { return l.server.callbackURL() }

// Wait 实现 LoginSession：等浏览器回调，拿 code 换令牌并落盘。
func (l *geminiLogin) Wait(ctx context.Context) (*Credential, error) {
	defer l.close()

	waitCtx, cancel := context.WithTimeout(ctx, l.timeout)
	defer cancel()
	go func() {
		<-waitCtx.Done()
		l.close()
	}()

	for {
		req, err := l.server.next(waitCtx)
		if err != nil {
			if waitCtx.Err() == context.DeadlineExceeded {
				return nil, fmt.Errorf("%w: gemini 授权超时，请重新发起", ErrLoginCancelled)
			}
			return nil, err
		}
		if req.params.Error != "" {
			detail := firstNonEmpty(req.params.ErrorDescription, req.params.Error)
			req.reply <- callbackReply{status: http.StatusBadRequest, message: "授权被拒绝：" + detail}
			return nil, fmt.Errorf("%w: gemini 授权被拒绝：%s", ErrLoginCancelled, detail)
		}
		if req.params.Code == "" {
			req.reply <- callbackReply{status: http.StatusBadRequest, message: "回调缺少授权码"}
			continue // 让用户刷新页面重试
		}

		cred, err := l.channel.exchange(req.ctx, req.params.Code, l.redirectURI())
		if err != nil {
			// 换码失败大多是瞬时问题，回 5xx 让用户刷新重试，会话继续。
			req.reply <- callbackReply{status: http.StatusBadGateway, message: "换取凭据失败，请刷新页面重试"}
			continue
		}
		// 先补身份信息再落盘：AccountID 决定账号判重，避免同账号出现两条记录。
		l.channel.fillUserInfo(req.ctx, cred)
		if err := l.channel.accounts.add(*cred); err != nil {
			req.reply <- callbackReply{status: http.StatusInternalServerError, message: "保存凭据失败"}
			return nil, err
		}
		req.reply <- callbackReply{status: http.StatusOK, html: geminiCallbackHTML}
		return cred, nil
	}
}

func (l *geminiLogin) close() {
	l.closeOnce.Do(func() { l.server.close() })
}

// exchange 用授权码换令牌。
func (c *GeminiChannel) exchange(ctx context.Context, code, redirectURI string) (*Credential, error) {
	form := url.Values{
		"client_id":     {c.clientID},
		"client_secret": {c.clientSecret},
		"code":          {code},
		"grant_type":    {"authorization_code"},
		"redirect_uri":  {redirectURI},
	}
	body, _, err := c.client.postForm(ctx, c.tokenEndpoint, form)
	if err != nil {
		return nil, geminiGrantError(err)
	}
	return parseGeminiGrant(body, time.Now())
}

// refreshCredential 用 refresh_token 换新令牌。
func (c *GeminiChannel) refreshCredential(ctx context.Context, cred Credential) (Credential, error) {
	if cred.RefreshToken == "" {
		return Credential{}, fmt.Errorf("gemini 凭据缺少 refresh_token，需重新登录")
	}
	form := url.Values{
		"client_id":     {c.clientID},
		"client_secret": {c.clientSecret},
		"refresh_token": {cred.RefreshToken},
		"grant_type":    {"refresh_token"},
	}
	body, _, err := c.client.postForm(ctx, c.tokenEndpoint, form)
	if err != nil {
		var httpErr *oauthHTTPError
		if errors.As(err, &httpErr) && (httpErr.Status == 401 || httpErr.Status == 403) {
			return Credential{}, fmt.Errorf("%w: gemini 续期被拒绝（HTTP %d）", ErrLoginCancelled, httpErr.Status)
		}
		return Credential{}, geminiGrantError(err)
	}
	next, err := parseGeminiGrant(body, time.Now())
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

// fillUserInfo 用 access_token 查 userinfo 补 sub/email（best-effort，失败不影响登录）。
func (c *GeminiChannel) fillUserInfo(ctx context.Context, cred *Credential) {
	if c.userinfoEndpoint == "" {
		return
	}
	fetchCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(fetchCtx, http.MethodGet, c.userinfoEndpoint, nil)
	if err != nil {
		return
	}
	req.Header.Set("Authorization", "Bearer "+cred.AccessToken)
	req.Header.Set("Accept", "application/json")
	resp, err := c.client.http.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return
	}
	var info struct {
		ID    string `json:"id"`
		Email string `json:"email"`
	}
	if json.NewDecoder(resp.Body).Decode(&info) != nil {
		return
	}
	if cred.AccountID == "" {
		cred.AccountID = info.ID
	}
	if cred.Nickname == "" {
		cred.Nickname = info.Email
	}
}

// parseGeminiGrant 解析 Google 令牌响应；至少要有 access_token。
// 身份字段优先从 id_token 的 JWT 声明取（Google 一定返回 id_token）。
func parseGeminiGrant(body map[string]any, now time.Time) (*Credential, error) {
	access := stringField(body, "access_token")
	if access == "" {
		detail := firstNonEmpty(stringField(body, "error_description"), stringField(body, "error"))
		return nil, fmt.Errorf("gemini 令牌响应缺少 access_token: %s", detail)
	}
	expiresIn := numberField(body, "expires_in")
	if expiresIn <= 0 {
		expiresIn = 3600
	}
	idToken := stringField(body, "id_token")
	return &Credential{
		AccessToken:  access,
		RefreshToken: stringField(body, "refresh_token"),
		ExpiresAt:    now.Add(time.Duration(expiresIn) * time.Second),
		AccountID:    firstNonEmpty(decodeJWTClaim(idToken, "sub"), decodeJWTClaim(access, "sub")),
		Nickname:     decodeJWTClaim(idToken, "email"),
	}, nil
}

// geminiGrantError 把 Google 令牌端点的错误归一成可读错误（含 error_description）。
func geminiGrantError(err error) error {
	var httpErr *oauthHTTPError
	if errors.As(err, &httpErr) {
		detail := firstNonEmpty(stringField(httpErr.Body, "error_description"), stringField(httpErr.Body, "error"))
		return fmt.Errorf("gemini 令牌请求失败（HTTP %d）：%s", httpErr.Status, detail)
	}
	return fmt.Errorf("gemini 令牌请求失败: %w", err)
}

const geminiCallbackHTML = `<!doctype html><html><head><meta charset="utf-8"><title>授权完成</title></head>` +
	`<body style="font-family:system-ui;text-align:center;padding-top:80px">` +
	`<h2>授权已完成</h2><p>可以关闭此页面。</p></body></html>`
