package channel

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// LobsterAI（有道）账号渠道。
//
// 与 minimax / cline 的设备码流程不同，lobster 是「授权码 + 回环回调」：网关在本机
// 起一个回调服务（channel.callbackServer），把门户登录页拼上 redirect_uri 交给用户；
// 用户在浏览器里完成登录后，门户把 ?code=&state= 回调到这个本地服务，网关再用 code
// 换令牌（POST /api/auth/exchange）。
//
// 推理走 OpenAI Chat Completions 协议（/api/proxy/v1/chat/completions），因此复用
// provider.OpenAIProvider 的协议转换；渠道只提供令牌、复刻头与续期。上游强制
// stream:true（stream:false 返回 500），网关侧由 provider 统一使用流式路径。
const (
	lobsterChannel     = "lobster"
	lobsterAPIBase     = "https://lobsterai-server.youdao.com"
	lobsterPortalBase  = "https://lobsterai.youdao.com"
	lobsterVersionAPI  = "https://api-overmind.youdao.com/openapi/get/luna/hardware/lobsterai/prod/update"
	lobsterUserAgent   = "LobsterAI/0.1.0"
	lobsterClientCaps  = "kimi-k3-agentic-v1,thinking-level-control-v1"
	lobsterFallbackVer = "2026.9.4"

	lobsterCallbackPath = "/auth/callback"
	lobsterExchangePath = "/api/auth/exchange"
	lobsterRefreshPath  = "/api/auth/refresh"
	lobsterChatPath     = "/api/proxy/v1/chat/completions"
)

// lobsterLoginTimeout 是回调登录的最长等待时间。用户在门户页可能停留很久
// （要登录、可能还要注册），但仍需要一个上限回收本地端口。
const lobsterLoginTimeout = 10 * time.Minute

// lobsterVersionTTL 是客户端版本号的缓存时长：该值几乎不变，取一次用半天。
const lobsterVersionTTL = 12 * time.Hour

// lobsterVersionTimeout 限制版本接口的等待：它只影响一个请求头，绝不该拖慢
// provider 装配或首个请求。超时即回退到常量版本号。
const lobsterVersionTimeout = 5 * time.Second

// lobsterClientVersionRe 匹配门户版本号（形如 2026.9.4，可带 -beta 后缀）。
var lobsterClientVersionRe = regexp.MustCompile(`^(\d+(?:\.\d+)*)(?:-[0-9A-Za-z.-]+)?$`)

// LobsterChannel 持有 lobster 的账号池与登录逻辑。
type LobsterChannel struct {
	accounts *Accounts
	client   *oauthClient
	// 端点主机可覆盖，测试时指向本地假上游；生产用常量默认值。
	apiHost    string
	portalHost string
	versionURL string
	// 客户端版本号带 12h 缓存，供请求头与 exchange 使用。
	versionMu      sync.Mutex
	version        string
	versionFetched time.Time
}

// NewLobsterChannel 在 dir 下构造 lobster 渠道（凭据文件 <dir>/lobster.json）。
func NewLobsterChannel(dir string) *LobsterChannel {
	c := &LobsterChannel{
		client:     newOAuthClient(),
		apiHost:    lobsterAPIBase,
		portalHost: lobsterPortalBase,
		versionURL: lobsterVersionAPI,
	}
	c.accounts = NewAccounts(lobsterChannel, NewStore(dir), c.client, c.refreshCredential)
	return c
}

// Accounts 供 provider 层取用令牌源。
func (c *LobsterChannel) Accounts() *Accounts { return c.accounts }

// Name 实现 Channel。
func (c *LobsterChannel) Name() string { return lobsterChannel }

// ChatURL 是 lobster 的 OpenAI Chat Completions 推理端点。
func (c *LobsterChannel) ChatURL() string { return c.apiHost + lobsterChatPath }

// BeginLogin 实现 Channel：起本地回调服务，返回带登录地址的会话。
func (c *LobsterChannel) BeginLogin(ctx context.Context) (LoginSession, error) {
	server, err := newCallbackServer(lobsterCallbackPath)
	if err != nil {
		return nil, err
	}
	session := &lobsterLogin{
		channel:  c,
		server:   server,
		timeout:  lobsterLoginTimeout,
		uuid:     mustRandomHex(16),
		firstKey: strconv.FormatInt(time.Now().UnixMilli(), 10),
	}
	return session, nil
}

// LobsterClientHeaders 是复刻 LobsterAI 客户端所需的固定请求头。
//
// X-LobsterAI-Client-Version 必须是当前门户版本号（见 clientVersion）；该头缺失
// 或过旧会被上游按非官方客户端拒绝。
func (c *LobsterChannel) ClientHeaders() map[string]string {
	return map[string]string{
		"User-Agent":                      lobsterUserAgent,
		"X-LobsterAI-Client-Capabilities": lobsterClientCaps,
		"X-LobsterAI-Client-Version":      c.clientVersion(context.Background()),
	}
}

// lobsterLogin 是一次进行中的回调登录。
type lobsterLogin struct {
	channel  *LobsterChannel
	server   *callbackServer
	timeout  time.Duration
	uuid     string
	firstKey string

	closeOnce sync.Once
}

// Channel 实现 LoginSession。
func (l *lobsterLogin) Channel() string { return lobsterChannel }

// DeviceCode 实现 LoginSession：回调登录没有设备码，这里用 DeviceCode 承载登录
// 地址。VerificationURIComplete 即用户应打开的完整登录 URL；Admin API 的
// login_url 字段就是它。
func (l *lobsterLogin) DeviceCode() DeviceCode {
	return DeviceCode{
		VerificationURI:         l.loginURL(),
		VerificationURIComplete: l.loginURL(),
		ExpiresIn:               l.timeout,
	}
}

// loginURL 拼门户登录地址。state 用回调服务生成的防伪值，避免旧页面串号。
func (l *lobsterLogin) loginURL() string {
	query := url.Values{}
	query.Set("source", "electron")
	query.Set("redirect_uri", l.server.callbackURL())
	query.Set("state", l.server.stateValue())
	return l.channel.portalHost + "/portal#/login?" + query.Encode()
}

// Wait 实现 LoginSession：等用户在浏览器里完成登录，拿到 code 后换令牌并落盘。
// 整个等待受 l.timeout 限制；state 不匹配或用户取消返回 ErrLoginCancelled。
func (l *lobsterLogin) Wait(ctx context.Context) (*Credential, error) {
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
			return nil, l.waitError(waitCtx, err)
		}
		// 门户把拒绝原因（如用户点了取消）带回 error 参数。
		if req.params.Error != "" {
			req.reply <- callbackReply{status: http.StatusBadRequest, message: "登录被拒绝：" + firstNonEmpty(req.params.ErrorDescription, req.params.Error)}
			return nil, fmt.Errorf("%w: lobster 登录被拒绝：%s", ErrLoginCancelled, firstNonEmpty(req.params.ErrorDescription, req.params.Error))
		}
		if req.params.Code == "" {
			req.reply <- callbackReply{status: http.StatusBadRequest, message: "回调缺少授权码"}
			continue // 让用户刷新页面重试
		}

		cred, err := l.channel.exchange(req.ctx, req.params.Code, l.uuid, l.firstKey)
		if err != nil {
			// 换码失败大多是瞬时问题，回 5xx 让用户刷新重试，会话继续。
			req.reply <- callbackReply{status: http.StatusBadGateway, message: "登录换取凭据失败，请刷新页面重试"}
			continue
		}
		if err := l.channel.accounts.add(*cred); err != nil {
			req.reply <- callbackReply{status: http.StatusInternalServerError, message: "保存凭据失败"}
			return nil, err
		}
		req.reply <- callbackReply{status: http.StatusOK, html: lobsterCallbackHTML}
		return cred, nil
	}
}

// waitError 区分「用户级取消（超时）」与「上层取消（ctx 被撤）」。
func (l *lobsterLogin) waitError(waitCtx context.Context, err error) error {
	if waitCtx.Err() != nil && waitCtx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("%w: lobster 登录超时，请重新发起", ErrLoginCancelled)
	}
	return err
}

func (l *lobsterLogin) close() {
	l.closeOnce.Do(func() { l.server.close() })
}

// exchange 用回调带回来的授权码换令牌。响应是 {code:0, data:{...}} 信封。
func (c *LobsterChannel) exchange(ctx context.Context, code, uuid, firstKey string) (*Credential, error) {
	now := time.Now()
	body := map[string]string{
		"authCode":      code,
		"firstKeyfrom":  firstKey,
		"latestKeyfrom": strconv.FormatInt(now.UnixMilli(), 10),
		"uuid":          uuid,
		"version":       c.clientVersion(ctx),
	}
	parsed, _, err := c.client.postJSON(ctx, c.apiHost+lobsterExchangePath, body, lobsterAnonymousHeaders())
	if err != nil {
		return nil, fmt.Errorf("lobster 换取凭据失败: %w", err)
	}
	data, err := parseLobsterEnvelope(parsed)
	if err != nil {
		return nil, err
	}
	payload, err := parseLobsterTokenPayload(data)
	if err != nil {
		return nil, err
	}
	return buildLobsterCredential(payload, uuid, firstKey, body["latestKeyfrom"], now), nil
}

// refreshCredential 用 refresh_token 换新令牌。请求体带上原始 keyfrom 序列
// （服务端据此绑定客户端会话），沿用 credential 里存的值而不是当前时间。
func (c *LobsterChannel) refreshCredential(ctx context.Context, cred Credential) (Credential, error) {
	if cred.RefreshToken == "" {
		return Credential{}, fmt.Errorf("lobster 凭据缺少 refresh_token，需重新登录")
	}
	now := time.Now()
	body := map[string]string{
		"firstKeyfrom":  cred.Extra["first_keyfrom"],
		"latestKeyfrom": cred.Extra["latest_keyfrom"],
		"version":       c.clientVersion(ctx),
		"refreshToken":  cred.RefreshToken,
	}
	if uuid := cred.Extra["uuid"]; uuid != "" {
		body["uuid"] = uuid
	}
	if userID := cred.Extra["user_id"]; userID != "" {
		body["userId"] = userID
	}
	parsed, _, err := c.client.postJSON(ctx, c.apiHost+lobsterRefreshPath, body, lobsterAnonymousHeaders())
	if err != nil {
		var httpErr *oauthHTTPError
		if errors.As(err, &httpErr) && (httpErr.Status == 401 || httpErr.Status == 403) {
			return Credential{}, fmt.Errorf("%w: lobster 续期被拒绝（HTTP %d）", ErrLoginCancelled, httpErr.Status)
		}
		return Credential{}, fmt.Errorf("lobster 续期失败: %w", err)
	}
	data, err := parseLobsterEnvelope(parsed)
	if err != nil {
		return Credential{}, err
	}
	payload, err := parseLobsterTokenPayload(data)
	if err != nil {
		return Credential{}, err
	}
	// 续期响应可能不回传 refresh_token，沿用旧值。
	if payload.RefreshToken == "" {
		payload.RefreshToken = cred.RefreshToken
	}
	next := buildLobsterCredential(payload, cred.Extra["uuid"], cred.Extra["first_keyfrom"], cred.Extra["latest_keyfrom"], now)
	if next.AccountID == "" {
		next.AccountID = cred.AccountID
	}
	if next.Nickname == "" {
		next.Nickname = cred.Nickname
	}
	return *next, nil
}

// clientVersion 返回当前 LobsterAI 客户端版本号：优先取门户版本接口，失败或超时
// 时回退到常量。结果缓存 12 小时，避免每次请求都去问一次版本。
func (c *LobsterChannel) clientVersion(ctx context.Context) string {
	c.versionMu.Lock()
	if c.version != "" && time.Since(c.versionFetched) < lobsterVersionTTL {
		v := c.version
		c.versionMu.Unlock()
		return v
	}
	c.versionMu.Unlock()

	version := c.fetchClientVersion(ctx)
	c.versionMu.Lock()
	c.version = version
	c.versionFetched = time.Now()
	c.versionMu.Unlock()
	return version
}

func (c *LobsterChannel) fetchClientVersion(ctx context.Context) string {
	fetchCtx, cancel := context.WithTimeout(ctx, lobsterVersionTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(fetchCtx, http.MethodGet, c.versionURL, nil)
	if err != nil {
		return lobsterFallbackVer
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", lobsterUserAgent)
	resp, err := c.client.http.Do(req)
	if err != nil {
		return lobsterFallbackVer
	}
	defer resp.Body.Close()
	var body struct {
		Data struct {
			Value struct {
				Version string `json:"version"`
			} `json:"value"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return lobsterFallbackVer
	}
	if v := strings.TrimSpace(body.Data.Value.Version); lobsterClientVersionRe.MatchString(v) {
		return v
	}
	return lobsterFallbackVer
}

// lobsterAnonymousHeaders 是登录/续期这类匿名请求用的固定头（还不持有令牌）。
func lobsterAnonymousHeaders() map[string]string {
	return map[string]string{
		"Accept":     "application/json",
		"User-Agent": lobsterUserAgent,
	}
}

// lobsterEnvelopeError 承载 {code, message} 信封里的业务错误。
type lobsterEnvelopeError struct {
	Code    int
	Message string
}

func (e *lobsterEnvelopeError) Error() string {
	return fmt.Sprintf("lobster 响应业务错误（code=%d）：%s", e.Code, e.Message)
}

// parseLobsterEnvelope 解出 {code:0, data:{...}} 信封，非 0 视为业务失败。
func parseLobsterEnvelope(body map[string]any) (map[string]any, error) {
	code := -1
	switch v := body["code"].(type) {
	case float64:
		code = int(v)
	case json.Number:
		f, _ := v.Int64()
		code = int(f)
	case string:
		if n, err := strconv.Atoi(v); err == nil {
			code = n
		}
	}
	message := firstNonEmpty(stringField(body, "message"), stringField(body, "msg"))
	if code != 0 {
		return nil, &lobsterEnvelopeError{Code: code, Message: message}
	}
	data, ok := body["data"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("lobster 响应缺少 data 对象")
	}
	return data, nil
}

// lobsterTokenPayload 是 exchange/refresh 响应 data 里的凭据字段。
type lobsterTokenPayload struct {
	AccessToken   string
	RefreshToken  string
	ExpiresIn     float64
	UserID        string
	YID           string
	AccountUserID string
	Nickname      string
}

// parseLobsterTokenPayload 从 data 里抽凭据，至少要有 accessToken。
func parseLobsterTokenPayload(data map[string]any) (*lobsterTokenPayload, error) {
	access := stringField(data, "accessToken")
	if access == "" {
		return nil, fmt.Errorf("lobster 令牌响应缺少 accessToken")
	}
	user, _ := data["user"].(map[string]any)
	payload := &lobsterTokenPayload{
		AccessToken:   access,
		RefreshToken:  stringField(data, "refreshToken"),
		ExpiresIn:     numberField(data, "expiresIn"),
		UserID:        stringField(user, "id"),
		YID:           stringField(user, "yid"),
		AccountUserID: stringField(user, "userId"),
		Nickname:      stringField(user, "nickname"),
	}
	return payload, nil
}

// resolveLobsterUID 选一个稳定的账号标识：优先服务端给的 id，都没有时退回令牌哈希。
func resolveLobsterUID(p *lobsterTokenPayload) string {
	if v := firstNonEmpty(p.UserID, p.AccountUserID, p.YID); v != "" {
		return v
	}
	sum := sha256.Sum256([]byte(p.AccessToken))
	return hex.EncodeToString(sum[:])[:16]
}

// buildLobsterCredential 把令牌响应装配成落盘凭据。uid/user_id/uuid/keyfrom 都
// 存进 Extra：续期请求体要用它们，账号判重也靠 AccountID(uid)。
func buildLobsterCredential(p *lobsterTokenPayload, uuid, firstKey, latestKey string, now time.Time) *Credential {
	expiresAt := time.Time{}
	if p.ExpiresIn > 0 {
		expiresAt = now.Add(time.Duration(p.ExpiresIn) * time.Second)
	} else {
		expiresAt = decodeJWTExpiresAt(p.AccessToken)
	}
	uid := resolveLobsterUID(p)
	return &Credential{
		AccessToken:  p.AccessToken,
		RefreshToken: p.RefreshToken,
		ExpiresAt:    expiresAt,
		AccountID:    uid,
		Nickname:     firstNonEmpty(p.Nickname, p.AccountUserID),
		Extra: map[string]string{
			"uid":            uid,
			"user_id":        firstNonEmpty(p.AccountUserID, p.YID),
			"uuid":           uuid,
			"first_keyfrom":  firstKey,
			"latest_keyfrom": latestKey,
		},
	}
}

const lobsterCallbackHTML = `<!doctype html><html><head><meta charset="utf-8"><title>登录成功</title></head>` +
	`<body style="font-family:system-ui;text-align:center;padding-top:80px">` +
	`<h2>登录成功</h2><p>可以关闭此窗口了。</p></body></html>`
