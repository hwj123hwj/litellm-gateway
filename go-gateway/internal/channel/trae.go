package channel

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"
)

// Trae（字节）账号渠道。
//
// 登录是「授权页 + 固定端口回环回调」：网关在 127.0.0.1:18080 起回调服务（端口被
// 占用时退回系统分配端口），把 https://www.trae.cn/authorization?... 交给用户在浏览器
// 打开；浏览器登录后带上 userInfo/userJwt/refreshToken 回调 /authorize，网关据此换
// 取正式令牌（ExchangeToken），再用 GetUserInfo 补 uid/手机号/邮箱。
//
// 与其它渠道的两处结构性差异，决定了这里不能复用 channel.callbackServer：
//   - 回调端口是**固定**的 18080（Trae 授权页要求 localhost 回调，且官方客户端固定用
//     这个端口），callbackServer 只会绑 127.0.0.1:0；
//   - 回调路径是**前缀**匹配 /authorize，且不带 state 参数。
//
// 凭据续期走 ExchangeToken 换新（POST /cloudide/api/v3/trae/oauth/ExchangeToken），
// 401/403 或响应体命中 session-dead 视为 refresh_token 终态失效。
//
// Trae 的推理协议既不是 OpenAI 也不是 Anthropic：请求体是 SOLO 形态（function /
// config_name / messages），鉴权头是 Authorization: Cloud-IDE-JWT <token>，响应是
// 私有 SSE（event: output/token_usage/done/error）。这些都收敛在 provider.TraeProvider 里，
// 本渠道只负责登录、令牌、续期与客户端身份字段。
//
// ⚠️ 与 minimax/cline/lobster/gemini/codebuddy 一样，这是复刻官方客户端的非官方用法，
// 可能违反服务条款并导致账号被封。仅用于本机个人测试，不要对外分发。
const (
	traeChannel = "trae"

	traeAgentHost   = "https://trae-api-cn.mchost.guru"
	traeOAuthHost   = "https://api.trae.com.cn"
	traeConsoleHost = "https://www.trae.cn"

	traeChatPath     = "/api/agent/v3/llm_utils_chat"
	traeExchangePath = "/cloudide/api/v3/trae/oauth/ExchangeToken"
	traeUserInfoPath = "/cloudide/api/v3/trae/GetUserInfo"

	// traeCallbackPath 用前缀匹配：授权页会把查询串直接拼在这个路径后面。
	traeCallbackPath = "/authorize"
	// traeCallbackPort 是官方客户端固定使用的回调端口；被占用时退回系统分配端口。
	traeCallbackPort = 18080

	traeIDEVersion     = "0.1.52"
	traeIDEVersionCode = "20260811"
	traePluginVersion  = "2.3.62834"
	traeOSVersion      = "macOS 15.7.4"
	traeDeviceBrand    = "Apple"
	traeUserAgent      = "Trae/0.1.52"

	// traeFunction 是 SOLO 请求体的 function 字段（官方默认 work_lite 通道）。
	traeFunction = "solo_work_lite"

	// traeRequestTimeout 与官方客户端 TRAE_REQUEST_TIMEOUT_MS 一致（换 token / 取用户信息）。
	traeRequestTimeout = 30 * time.Second
	// traeLoginTimeout 与 TRAE_LOGIN_TIMEOUT_MS 一致（用户可能在授权页停留很久）。
	traeLoginTimeout = 10 * time.Minute

	// traeClientCredentialFile 是本地存放 Trae 客户端标识的文件名，位于 <gatewayHome>
	// （channel 凭据目录的上一级）。内容 {"client_id":..,"app_id":..}。
	//
	// 客户端标识刻意不进仓库：本仓库是公开仓库，硬编码第三方应用标识会被 GitHub 密钥
	// 扫描拦截，也等于把别人的客户端标识公开出去。因此改为「本地文件或环境变量」二选一，
	// 未配置时登录直接给出可操作的报错（见 BeginLogin）。
	traeClientCredentialFile = "trae-client.json"
)

// TraeChannel 持有 trae 的账号池与登录逻辑。
type TraeChannel struct {
	accounts *Accounts
	auth     *traeAuth
	client   *oauthClient
	// home 是 <gatewayHome>，用于定位本地客户端标识文件（可选）。
	home string

	// 端点可覆盖，测试时指向本地假上游。
	agentHost   string
	oauthHost   string
	consoleHost string

	clientID string
	appID    string

	callbackPort int
	loginTimeout time.Duration
}

// NewTraeChannel 在 dir 下构造 trae 渠道（凭据文件 <dir>/trae.json）。
// 客户端标识（client_id / app_id）从本地解析，见 resolveClientCredential。
func NewTraeChannel(dir string) *TraeChannel {
	c := &TraeChannel{
		client:       newOAuthClient(),
		home:         filepath.Dir(dir),
		agentHost:    traeAgentHost,
		oauthHost:    traeOAuthHost,
		consoleHost:  traeConsoleHost,
		callbackPort: traeCallbackPort,
		loginTimeout: traeLoginTimeout,
	}
	c.clientID, c.appID = c.resolveClientCredential()
	c.accounts = NewAccounts(traeChannel, NewStore(dir), c.client, c.refreshCredential)
	c.auth = &traeAuth{Accounts: c.accounts, channel: c}
	return c
}

// resolveClientCredential 解析 Trae 客户端标识，优先级：环境变量 > 本地文件。
// 两者都没有时返回空串，登录时由 BeginLogin 给出明确指引。
func (c *TraeChannel) resolveClientCredential() (string, string) {
	id := os.Getenv("TRAE_CHANNEL_CLIENT_ID")
	app := os.Getenv("TRAE_CHANNEL_APP_ID")
	if id != "" && app != "" {
		return id, app
	}
	fileID, fileApp := c.loadClientCredentialFile()
	return firstNonEmpty(id, fileID), firstNonEmpty(app, fileApp)
}

// loadClientCredentialFile 读取 <home>/trae-client.json（best-effort）。
func (c *TraeChannel) loadClientCredentialFile() (string, string) {
	if c.home == "" {
		return "", ""
	}
	data, err := os.ReadFile(filepath.Join(c.home, traeClientCredentialFile))
	if err != nil {
		return "", ""
	}
	var payload struct {
		ClientID string `json:"client_id"`
		AppID    string `json:"app_id"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return "", ""
	}
	return payload.ClientID, payload.AppID
}

// configured 报告是否已配置客户端标识。
func (c *TraeChannel) configured() bool {
	return c.clientID != "" && c.appID != ""
}

// Accounts 供 Admin API 查看账号状态/登出。
func (c *TraeChannel) Accounts() *Accounts { return c.accounts }

// AuthSource 是交给 provider 的令牌源：在账号池之上多提供复刻客户端所需的身份头。
func (c *TraeChannel) AuthSource() *traeAuth { return c.auth }

// Name 实现 Channel。
func (c *TraeChannel) Name() string { return traeChannel }

// ChatURL 是 trae 的 SOLO 推理端点。
func (c *TraeChannel) ChatURL() string { return c.agentHost + traeChatPath }

// Namespace 是本渠道对外模型名的前缀。trae 的模型名（glm-5.2、kimi-k3、minimax-m3…）
// 与既有供应商大量重名，裸名注册会覆盖掉既有 provider 的路由；加前缀后两路并存。
// 前缀在 provider 组装请求体时剥掉（见 provider.traeModelName）。
func (c *TraeChannel) Namespace() string { return traeChannel + "/" }

// traeAuth 在账号池之上补「复刻 Trae 客户端所需的动态身份头」。
// 嵌入 *Accounts 后自动满足 provider.AuthSource（BearerToken / Refresh）。
//
// 这些头依赖当前账号的凭据字段（uid / machine_id / device_id）与渠道解析出的
// client_id / app_id，provider 自己拿不到，故由渠道注入。
type traeAuth struct {
	*Accounts
	channel *TraeChannel
}

// TraeIdentityHeaders 实现 provider.traeIdentitySource。
//
// machine_id 的轮换（官方客户端 DSH_TRAE_ROTATE_MACHINE_ID）默认关闭，这里不做，
// 因此 X-Machine-Id 恒为登录时生成的那个 32 位十六进制串。
func (t *traeAuth) TraeIdentityHeaders() map[string]string {
	headers := map[string]string{}
	if token := t.BearerToken(); token != "" {
		// Trae 的鉴权方案不是 Bearer：applyAuth 表达不了 Cloud-IDE-JWT，故由
		// provider 原样写入这三个头。
		headers["Authorization"] = "Cloud-IDE-JWT " + token
		headers["X-Cloudide-Token"] = token
		headers["X-Ide-Token"] = token
	}
	extra := t.CurrentExtra()
	if uid := extra["uid"]; uid != "" {
		headers["X-Uid"] = uid
	}
	if machine := extra["machine_id"]; machine != "" {
		headers["X-Machine-Id"] = machine
	}
	if device := extra["device_id"]; device != "" {
		headers["X-Device-Id"] = device
	}
	if appID := t.channel.appID; appID != "" {
		headers["X-App-Id"] = appID
	}
	return headers
}

// BeginLogin 实现 Channel：起固定端口回环回调服务，返回带授权地址的会话。
// 未配置客户端标识时直接失败并给出可操作指引（Admin API 会把它作为 502 的 error 文本）。
func (c *TraeChannel) BeginLogin(ctx context.Context) (LoginSession, error) {
	if !c.configured() {
		return nil, fmt.Errorf(
			"trae 渠道缺少客户端标识：请设置 TRAE_CHANNEL_CLIENT_ID / TRAE_CHANNEL_APP_ID 环境变量，或写入 %s",
			filepath.Join(c.home, traeClientCredentialFile),
		)
	}
	server, err := newTraeCallbackServer(c.callbackPort)
	if err != nil {
		return nil, err
	}
	machineID := mustRandomHex(16)
	deviceID := mustRandomHex(16)
	loginURL := buildTraeLoginURL(c.consoleHost, c.clientID, machineID, deviceID, server.callbackURL())
	return &traeLogin{
		channel:   c,
		server:    server,
		machineID: machineID,
		deviceID:  deviceID,
		dc: DeviceCode{
			VerificationURI:         loginURL,
			VerificationURIComplete: loginURL,
			ExpiresIn:               c.loginTimeout,
		},
		timeout: c.loginTimeout,
	}, nil
}

// traeLogin 是一次进行中的 Trae 登录：授权页没有设备码，用 DeviceCode 承载授权地址。
type traeLogin struct {
	channel   *TraeChannel
	server    *traeCallbackServer
	machineID string
	deviceID  string
	dc        DeviceCode
	timeout   time.Duration

	closeOnce sync.Once
}

func (l *traeLogin) Channel() string        { return traeChannel }
func (l *traeLogin) DeviceCode() DeviceCode { return l.dc }

// Wait 实现 LoginSession：等浏览器回调，换令牌后落盘。
//
// 与官方的单次回调不同，这里在换令牌失败时回 5xx 并让会话继续等下一次回调：用户刷新
// 授权页即可重试，不必重新发起登录（与 lobster/gemini 的回环回调保持一致）。
func (l *traeLogin) Wait(ctx context.Context) (*Credential, error) {
	defer l.close()

	waitCtx, cancel := context.WithTimeout(ctx, l.timeout)
	defer cancel()
	go func() {
		<-waitCtx.Done()
		l.close()
	}()

	for {
		ev, err := l.server.next(waitCtx)
		if err != nil {
			if waitCtx.Err() == context.DeadlineExceeded {
				return nil, fmt.Errorf("%w: TRAE 登录超时，请重新发起", ErrLoginCancelled)
			}
			return nil, err
		}
		cred, err := l.channel.exchangeCallback(ev.ctx, ev.info, l.machineID, l.deviceID)
		if err != nil {
			ev.reply <- callbackReply{status: http.StatusBadGateway, message: "换取凭据失败，请刷新页面重试"}
			continue
		}
		if err := l.channel.accounts.add(*cred); err != nil {
			ev.reply <- callbackReply{status: http.StatusInternalServerError, message: "保存凭据失败"}
			return nil, err
		}
		ev.reply <- callbackReply{status: http.StatusOK, html: traeCallbackHTML}
		return cred, nil
	}
}

func (l *traeLogin) close() {
	l.closeOnce.Do(func() { l.server.close() })
}

// ─── 回调服务 ──────────────────────────────────────────────────────────────

// traeCallbackServer 是 trae 登录专用的回环监听器。
//
// 与 callbackServer 的三点差异：端口固定 18080（占用则退回系统分配）、路径前缀匹配、
// 无 state 校验（Trae 授权页不回调 state）。
type traeCallbackServer struct {
	ln  net.Listener
	srv *http.Server

	events    chan traeCallbackEvent
	done      chan struct{}
	closeOnce sync.Once
}

// traeCallbackEvent 是一次授权回调：业务方必须回复 reply，浏览器在等结果。
type traeCallbackEvent struct {
	ctx   context.Context
	info  *traeCallbackInfo
	reply chan callbackReply
}

// newTraeCallbackServer 在 127.0.0.1:preferredPort 起回调服务；端口被占用或无权限时
// 退回由系统分配端口（对齐官方客户端的 listenWithFallback）。
func newTraeCallbackServer(preferredPort int) (*traeCallbackServer, error) {
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(preferredPort)))
	if err != nil {
		// 官方实现靠 Node 的 error 事件捕获 EADDRINUSE/EACCES；Go 的 net.Listen 直接
		// 返回错误，按错误类型判断即可，其余错误不该被静默降级。
		if !errors.Is(err, syscall.EADDRINUSE) && !errors.Is(err, syscall.EACCES) {
			return nil, fmt.Errorf("监听本地回调端口失败: %w", err)
		}
		ln, err = net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return nil, fmt.Errorf("监听本地回调端口失败: %w", err)
		}
	}
	s := &traeCallbackServer{
		ln:     ln,
		events: make(chan traeCallbackEvent),
		done:   make(chan struct{}),
	}
	s.srv = &http.Server{Handler: http.HandlerFunc(s.handle)}
	go func() { _ = s.srv.Serve(ln) }()
	return s, nil
}

func (s *traeCallbackServer) port() int { return s.ln.Addr().(*net.TCPAddr).Port }

// callbackURL 是授权页需要回调的完整地址（回环，固定路径 /authorize）。
func (s *traeCallbackServer) callbackURL() string {
	return fmt.Sprintf("http://127.0.0.1:%d%s", s.port(), traeCallbackPath)
}

func (s *traeCallbackServer) handle(w http.ResponseWriter, r *http.Request) {
	// 官方按「原始 request-target 前缀」判断，这里用 RequestURI（path+query）保持一致。
	raw := r.URL.RequestURI()
	if !strings.HasPrefix(raw, traeCallbackPath) {
		http.NotFound(w, r)
		return
	}
	info, err := parseTraeCallback(raw)
	if err != nil {
		http.Error(w, "TRAE 登录回调无效："+err.Error(), http.StatusBadRequest)
		return
	}

	ev := traeCallbackEvent{ctx: r.Context(), info: info, reply: make(chan callbackReply, 1)}
	select {
	case s.events <- ev:
	case <-s.done:
		http.Error(w, "登录会话已结束", http.StatusGone)
		return
	}

	select {
	case reply := <-ev.reply:
		if reply.html != "" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(reply.status)
			_, _ = w.Write([]byte(reply.html))
			return
		}
		http.Error(w, reply.message, reply.status)
	case <-s.done:
		http.Error(w, "登录会话已结束", http.StatusGone)
	}
}

// next 等待下一次回调事件。ctx 结束时返回 ctx 的错误。
func (s *traeCallbackServer) next(ctx context.Context) (traeCallbackEvent, error) {
	select {
	case ev := <-s.events:
		return ev, nil
	case <-ctx.Done():
		return traeCallbackEvent{}, ctx.Err()
	}
}

func (s *traeCallbackServer) close() {
	s.closeOnce.Do(func() {
		close(s.done)
		shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = s.srv.Shutdown(shutdownCtx)
	})
}

// ─── 登录地址 ──────────────────────────────────────────────────────────────

// machineTraceID 复刻官方实现：machineId+deviceId 拼接后取末 16 位（不足则左侧补 0）。
func machineTraceID(machineID, deviceID string) string {
	joined := machineID + deviceID
	if len(joined) >= 16 {
		return joined[len(joined)-16:]
	}
	return strings.Repeat("0", 16-len(joined)) + joined
}

// buildTraeLoginURL 拼 Trae 授权地址。
//
// 参数顺序按官方 URLSearchParams 的插入顺序逐字复刻（url.Values.Encode 会按字典序
// 重排，这里手工拼接）。注意授权页要求的参数名是 auth_callback_url，写成 callback_url
// 会拿不到回调。
func buildTraeLoginURL(consoleHost, clientID, machineID, deviceID, callbackURL string) string {
	params := [][2]string{
		{"login_version", "1"},
		{"auth_from", "solo"},
		{"login_channel", "native_ide"},
		{"plugin_version", traePluginVersion},
		{"auth_type", "local"},
		{"client_id", clientID},
		{"redirect", "0"},
		{"login_trace_id", machineTraceID(machineID, deviceID)},
		{"auth_callback_url", callbackURL},
		{"machine_id", machineID},
		{"device_id", deviceID},
		{"x_device_id", deviceID},
		{"x_machine_id", machineID},
		{"x_device_brand", "PC"},
		{"x_device_type", "PC"},
		{"x_os_version", "1.0"},
		{"x_app_version", traeIDEVersion},
		{"x_app_type", "stable"},
	}
	var b strings.Builder
	for i, kv := range params {
		if i > 0 {
			b.WriteByte('&')
		}
		b.WriteString(url.QueryEscape(kv[0]))
		b.WriteByte('=')
		b.WriteString(url.QueryEscape(kv[1]))
	}
	return consoleHost + "/authorization?" + b.String()
}

// ─── 回调解析 ──────────────────────────────────────────────────────────────

// traeCallbackInfo 是回调里能拿到的登录信息。
type traeCallbackInfo struct {
	RefreshToken string
	AccessToken  string
	UID          string
	Nickname     string
	EnterpriseID string
	// AuthCode 只在授权页切到 PKCE 流程时出现；本实现不支持该流程（见 parseTraeCallback）。
	AuthCode string
}

// parseTraeCallback 解析回调 URL（可以是完整 URL，也可以是 /authorize?... 这样的
// request-target）。
//
// 明确不支持 PKCE 授权码流程：官方实现同样不支持，回调里出现 code/authCodeInfo 且没有
// refreshToken/userJwt.Token 时直接报错，而不是拿 code 去换（Trae 没有 PKCE 生成逻辑）。
func parseTraeCallback(rawURL string) (*traeCallbackInfo, error) {
	target := rawURL
	if !strings.HasPrefix(rawURL, "http") {
		target = "http://127.0.0.1" + rawURL
	}
	parsed, err := url.Parse(target)
	if err != nil {
		return nil, errors.New("回调 URL 无法解析")
	}
	query := parsed.Query()

	userInfo := parseTraeJSONParam(query.Get("userInfo"))
	userJWT := parseTraeJSONParam(query.Get("userJwt"))

	refreshToken := query.Get("refreshToken")
	uid := traeJSONString(userInfo, "UserID")
	nicknameRaw := traeJSONString(userInfo, "ScreenName")
	enterpriseID := traeJSONString(userInfo, "TenantID")
	jwtToken := traeJSONString(userJWT, "Token")
	jwtRefresh := traeJSONString(userJWT, "RefreshToken")
	if refreshToken == "" {
		refreshToken = jwtRefresh
	}

	authCodeInfo := parseTraeJSONParam(query.Get("authCodeInfo"))
	for _, candidate := range []string{
		query.Get("code"),
		query.Get("authCode"),
		traeJSONString(authCodeInfo, "code"),
		traeJSONString(authCodeInfo, "authCode"),
		// authCodeInfo 为纯 code 字符串（非 JSON）时上面解不出，故把原始值兜底。
		query.Get("authCodeInfo"),
	} {
		if trimmed := strings.TrimSpace(candidate); trimmed != "" {
			info := &traeCallbackInfo{
				RefreshToken: refreshToken,
				// 仅在无 refreshToken 时才用 userJwt.Token 兜底（与官方一致）。
				UID:          uid,
				Nickname:     fixTraeNicknameMojibake(nicknameRaw, uid),
				EnterpriseID: enterpriseID,
				AuthCode:     trimmed,
			}
			if refreshToken == "" {
				info.AccessToken = jwtToken
			}
			if info.RefreshToken == "" && info.AccessToken == "" {
				return nil, errors.New("上游返回了 PKCE 授权码（code/authCodeInfo），本实现暂不支持该流程；请确认 TRAE 授权页是否已切换到新流程")
			}
			return info, nil
		}
	}

	if refreshToken == "" && jwtToken == "" {
		return nil, errors.New("回调未携带 refreshToken / userJwt.Token / code")
	}
	return &traeCallbackInfo{
		RefreshToken: refreshToken,
		AccessToken:  jwtToken,
		UID:          uid,
		Nickname:     fixTraeNicknameMojibake(nicknameRaw, uid),
		EnterpriseID: enterpriseID,
	}, nil
}

// parseTraeJSONParam 解析查询参数里的 JSON 对象；参数本身可能又被百分号编码过一次。
// 非对象（数组、标量、解析失败）一律当缺失。
func parseTraeJSONParam(raw string) map[string]any {
	if raw == "" {
		return nil
	}
	candidates := []string{raw}
	if unescaped, err := url.QueryUnescape(raw); err == nil && unescaped != raw {
		candidates = append(candidates, unescaped)
	}
	for _, candidate := range candidates {
		var parsed map[string]any
		if err := json.Unmarshal([]byte(candidate), &parsed); err == nil && parsed != nil {
			return parsed
		}
	}
	return nil
}

// traeJSONString 读取 JSON 对象里的字符串字段；数字按十进制字符串返回。
func traeJSONString(source map[string]any, key string) string {
	if source == nil {
		return ""
	}
	switch value := source[key].(type) {
	case string:
		return value
	case float64:
		return strconv.FormatFloat(value, 'f', -1, 64)
	case json.Number:
		return value.String()
	}
	return ""
}

// fixTraeNicknameMojibake 修复回调昵称的编码错乱（服务端按 latin1 解释 UTF-8 字节）。
// 修不好且不含中文时退回「用户+uid 后四位」，避免界面出现乱码。
func fixTraeNicknameMojibake(raw, uid string) string {
	if raw == "" {
		return raw
	}
	// 等价于 Node 的 Buffer.from(raw, "latin1").toString("utf8")：把每个字符的低字节
	// 重新拼成字节串再按 UTF-8 解读。
	bytes := make([]byte, 0, len(raw))
	for _, r := range raw {
		bytes = append(bytes, byte(r))
	}
	if utf8.Valid(bytes) {
		fixed := string(bytes)
		if fixed != "" && !strings.ContainsRune(fixed, utf8.RuneError) && traePrintable(fixed) {
			return fixed
		}
	}
	if !traeHasCJK(raw) {
		return "用户" + traeTail(uid, 4)
	}
	return raw
}

// traePrintable 报告字符串是否全部由可打印字符（码点 >= 32）组成。
func traePrintable(s string) bool {
	for _, r := range s {
		if r < 32 {
			return false
		}
	}
	return true
}

// traeHasCJK 报告字符串是否含中日韩统一表意文字（U+4E00–U+9FFF）。
func traeHasCJK(s string) bool {
	for _, r := range s {
		if r >= 0x4e00 && r <= 0x9fff {
			return true
		}
	}
	return false
}

// traeTail 返回字符串末尾至多 n 个字符（按 rune 计）。
func traeTail(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return string(runes)
	}
	return string(runes[len(runes)-n:])
}

// ─── 换令牌 / 取用户信息 / 续期 ────────────────────────────────────────────

// traeExchange 是 ExchangeToken 的响应。
type traeExchange struct {
	AccessToken         string
	RefreshToken        string
	TokenExpireAt       float64
	TokenExpireDuration float64
	RefreshExpireAt     float64
}

// traeUserInfo 是登录期间确定的用户身份。
type traeUserInfo struct {
	UID          string
	ScreenName   string
	EnterpriseID string
	Phone        string
	Email        string
}

// traeOAuthHeaders 复刻官方客户端的 OAuth 请求头。
func traeOAuthHeaders() map[string]string {
	return map[string]string{
		"Content-Type": "application/json",
		"Accept":       "application/json",
		"User-Agent":   traeUserAgent,
	}
}

// traePostJSON 发一次 Trae OAuth JSON 请求，返回解析后的对象、原始响应文本与状态码。
//
// 与 oauthClient.postJSON 的差别是同时保留原文：Trae 的错误分类依赖原文里的业务码片段
// （如 "code":4008），且成功响应也可能"不是对象/缺 Token"，仅凭解析后的 map 无法区分。
// 解析失败时 ok=false，调用方按状态码与原文处理。
func (c *TraeChannel) traePostJSON(ctx context.Context, urlStr string, body any, headers map[string]string) (map[string]any, string, int, bool, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, "", 0, false, fmt.Errorf("marshal trae request: %w", err)
	}
	callCtx, cancel := context.WithTimeout(ctx, traeRequestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(callCtx, http.MethodPost, urlStr, strings.NewReader(string(payload)))
	if err != nil {
		return nil, "", 0, false, fmt.Errorf("build trae request: %w", err)
	}
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	resp, err := c.client.http.Do(req)
	if err != nil {
		return nil, "", 0, false, err
	}
	defer resp.Body.Close()

	raw, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if readErr != nil {
		return nil, "", resp.StatusCode, false, readErr
	}
	text := strings.TrimSpace(string(raw))
	var parsed map[string]any
	ok := false
	if text != "" {
		if json.Unmarshal([]byte(text), &parsed) == nil && parsed != nil {
			ok = true
		}
	}
	return parsed, text, resp.StatusCode, ok, nil
}

// parseTraeExchangeResponse 解析 ExchangeToken 响应（Result/result 两种大小写）。
func parseTraeExchangeResponse(data map[string]any) *traeExchange {
	result := traeResultObject(data)
	if result == nil {
		return nil
	}
	access := firstNonEmpty(
		buddyStringField(result, "Token"),
		buddyStringField(result, "token"),
		buddyStringField(result, "accessToken"),
	)
	if access == "" {
		return nil
	}
	expireAt := numberField(result, "TokenExpireAt")
	if expireAt == 0 {
		expireAt = numberField(result, "tokenExpireAt")
	}
	expireDuration := numberField(result, "TokenExpireDuration")
	if expireDuration == 0 {
		expireDuration = numberField(result, "tokenExpireDuration")
	}
	refreshExpireAt := numberField(result, "RefreshExpireAt")
	if refreshExpireAt == 0 {
		refreshExpireAt = numberField(result, "refreshExpireAt")
	}
	return &traeExchange{
		AccessToken:         access,
		RefreshToken:        firstNonEmpty(buddyStringField(result, "RefreshToken"), buddyStringField(result, "refreshToken")),
		TokenExpireAt:       expireAt,
		TokenExpireDuration: expireDuration,
		RefreshExpireAt:     refreshExpireAt,
	}
}

// parseTraeUserInfoResponse 解析 GetUserInfo 响应。uid 为空视为解析失败。
//
// 手机号/邮箱字段名是 NonPlainTextMobile / NonPlainTextEmail（只有脱敏形态）。
func parseTraeUserInfoResponse(data map[string]any) *traeUserInfo {
	result := traeResultObject(data)
	if result == nil {
		return nil
	}
	uid := firstNonEmpty(
		buddyStringField(result, "UserID"),
		buddyStringField(result, "userId"),
		buddyStringField(result, "uid"),
	)
	if uid == "" {
		return nil
	}
	return &traeUserInfo{
		UID:          uid,
		ScreenName:   firstNonEmpty(buddyStringField(result, "ScreenName"), buddyStringField(result, "screenName"), uid),
		EnterpriseID: firstNonEmpty(buddyStringField(result, "EnterpriseID"), buddyStringField(result, "enterpriseId")),
		Phone:        firstNonEmpty(buddyStringField(result, "NonPlainTextMobile"), buddyStringField(result, "nonPlainTextMobile")),
		Email:        firstNonEmpty(buddyStringField(result, "NonPlainTextEmail"), buddyStringField(result, "nonPlainTextEmail")),
	}
}

// traeResultObject 取信封里的 Result/result 对象。
func traeResultObject(data map[string]any) map[string]any {
	if data == nil {
		return nil
	}
	if result, ok := data["Result"].(map[string]any); ok && result != nil {
		return result
	}
	if result, ok := data["result"].(map[string]any); ok && result != nil {
		return result
	}
	return nil
}

// exchangeCallback 实现官方 exchangeTraeCallback：换令牌 + 补用户信息 + 组装凭据。
func (c *TraeChannel) exchangeCallback(ctx context.Context, info *traeCallbackInfo, machineID, deviceID string) (*Credential, error) {
	var exchange *traeExchange
	if info.RefreshToken != "" {
		body := map[string]any{
			"ClientID":     c.clientID,
			"RefreshToken": info.RefreshToken,
			"ClientSecret": "-",
			"UserID":       "",
		}
		parsed, text, status, ok, err := c.traePostJSON(ctx, c.oauthHost+traeExchangePath, body, traeOAuthHeaders())
		if err != nil {
			return nil, fmt.Errorf("TRAE ExchangeToken 请求失败: %w", err)
		}
		if status < 200 || status >= 300 {
			return nil, fmt.Errorf("TRAE ExchangeToken 失败（HTTP %d）：%s", status, traeTruncate(text, 200))
		}
		if !ok {
			return nil, errors.New("TRAE ExchangeToken 响应不是 JSON 对象")
		}
		exchange = parseTraeExchangeResponse(parsed)
		if exchange == nil {
			return nil, errors.New("TRAE ExchangeToken 响应缺少 Token")
		}
	} else {
		exchange = &traeExchange{AccessToken: info.AccessToken}
	}

	user := &traeUserInfo{
		UID:          info.UID,
		ScreenName:   info.Nickname,
		EnterpriseID: info.EnterpriseID,
	}
	if fetched := c.fetchUserInfo(ctx, exchange.AccessToken); fetched != nil {
		user = &traeUserInfo{
			UID:          fetched.UID,
			ScreenName:   firstNonEmpty(fetched.ScreenName, user.ScreenName),
			EnterpriseID: firstNonEmpty(fetched.EnterpriseID, user.EnterpriseID),
			Phone:        fetched.Phone,
			Email:        fetched.Email,
		}
	}
	if user.UID == "" {
		return nil, errors.New("TRAE 未能确定 uid（回调 userInfo 与 GetUserInfo 均为空）")
	}
	if exchange.AccessToken == "" {
		return nil, errors.New("TRAE 换 token 后没有 accessToken")
	}
	return c.buildCredential(exchange, user, machineID, deviceID), nil
}

// fetchUserInfo 用 access_token 调 GetUserInfo（best-effort，失败返回 nil）。
func (c *TraeChannel) fetchUserInfo(ctx context.Context, accessToken string) *traeUserInfo {
	headers := traeOAuthHeaders()
	headers["X-Cloudide-Token"] = accessToken
	body := map[string]any{"ReqSource": "IDE", "IDEVersion": traeIDEVersion}
	parsed, _, status, ok, err := c.traePostJSON(ctx, c.oauthHost+traeUserInfoPath, body, headers)
	if err != nil || status < 200 || status >= 300 || !ok {
		return nil
	}
	return parseTraeUserInfoResponse(parsed)
}

// buildCredential 把换令牌结果与用户信息装配成落盘凭据。
//
// Nickname 用 traeDisplayNickname（手机号优先）而不是服务端的 ScreenName：官方记录
// ScreenName 是「按 uid 自动生成的默认名」，多账号无法区分（用户报障 2026-09-27）。
// 原始 ScreenName 仍保留在 Extra["screen_name"] 里。
func (c *TraeChannel) buildCredential(exchange *traeExchange, user *traeUserInfo, machineID, deviceID string) *Credential {
	extra := map[string]string{
		"uid":           user.UID,
		"screen_name":   user.ScreenName,
		"machine_id":    machineID,
		"device_id":     deviceID,
		"enterprise_id": user.EnterpriseID,
	}
	if user.Phone != "" {
		extra["phone"] = user.Phone
	}
	if user.Email != "" {
		extra["email"] = user.Email
	}
	cred := &Credential{
		AccessToken:  exchange.AccessToken,
		RefreshToken: exchange.RefreshToken,
		ExpiresAt:    traeExpiresAt(exchange.TokenExpireAt, exchange.TokenExpireDuration, exchange.AccessToken, time.Now()),
		AccountID:    user.UID,
		Extra:        extra,
	}
	cred.Nickname = traeDisplayNickname(extra, user.UID)
	return cred
}

// traeDisplayNickname 复刻官方展示名：手机号 → 邮箱 → 昵称 → uid。
func traeDisplayNickname(extra map[string]string, fallbackID string) string {
	for _, key := range []string{"phone", "email", "screen_name"} {
		if value := strings.TrimSpace(extra[key]); value != "" {
			return value
		}
	}
	if uid := strings.TrimSpace(extra["uid"]); uid != "" {
		return uid
	}
	return fallbackID
}

// traeExpiresAt 复刻官方的过期时间推导：
// 绝对毫秒（>1e12）→ 绝对秒 → 相对秒数（基于当前时间，×1e3 转毫秒）→ JWT 的 exp。
func traeExpiresAt(tokenExpireAt, tokenExpireDuration float64, accessToken string, now time.Time) time.Time {
	switch {
	case tokenExpireAt > 1e12:
		return time.UnixMilli(int64(tokenExpireAt))
	case tokenExpireAt > 0:
		return time.UnixMilli(int64(tokenExpireAt * 1e3))
	case tokenExpireDuration > 0:
		return time.UnixMilli(now.UnixMilli() + int64(tokenExpireDuration*1e3))
	default:
		return traeJWTExpiresAt(accessToken)
	}
}

// traeJWTExpiresAt 解出 JWT 的 exp 并转成时间。
//
// 这里不复用 oauth.go 的 decodeJWTExpiresAt：那个要求 JWT 恰好三段，而官方只要求
// 「至少两段」并直接解第二段（与 buddyJWTClaims 同理）。
func traeJWTExpiresAt(token string) time.Time {
	segments := strings.Split(token, ".")
	if len(segments) < 2 {
		return time.Time{}
	}
	payload, err := base64.RawURLEncoding.DecodeString(segments[1])
	if err != nil {
		if payload, err = base64.URLEncoding.DecodeString(segments[1]); err != nil {
			return time.Time{}
		}
	}
	var claims struct {
		Exp float64 `json:"exp"`
	}
	if json.Unmarshal(payload, &claims) != nil || claims.Exp <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(int64(claims.Exp * 1e3))
}

// refreshCredential 用 refresh_token 换新令牌（Accounts 的 refreshFn）。
//
// 终态判定与官方一致：HTTP 401/403、命中 session-dead 的业务码、或「2xx 却没换来 Token」
// 都视为 refresh_token 已失效，包成 ErrLoginCancelled 交给账号池换号/提示重新登录；
// 其余错误（网络抖动、5xx、429）按可重试处理。
func (c *TraeChannel) refreshCredential(ctx context.Context, cred Credential) (Credential, error) {
	if cred.RefreshToken == "" {
		return Credential{}, fmt.Errorf("%w: TRAE 凭据缺少 refresh_token，请重新登录", ErrLoginCancelled)
	}
	host := firstNonEmpty(cred.Extra["api_host"], c.oauthHost)
	body := map[string]any{
		"ClientID":     c.clientID,
		"RefreshToken": cred.RefreshToken,
		"ClientSecret": "-",
		"UserID":       "",
	}
	parsed, text, status, ok, err := c.traePostJSON(ctx, host+traeExchangePath, body, traeOAuthHeaders())
	if err != nil {
		return Credential{}, fmt.Errorf("TRAE 续期网络失败：%w", err)
	}
	var exchange *traeExchange
	if ok {
		exchange = parseTraeExchangeResponse(parsed)
	}
	if exchange == nil || exchange.AccessToken == "" {
		kind := classifyTraeError(status, text)
		noTokenInSuccess := status >= 200 && status < 300 && ok
		if status == http.StatusUnauthorized || status == http.StatusForbidden ||
			isTraeTerminalError(kind) || noTokenInSuccess {
			return Credential{}, fmt.Errorf("%w: TRAE refresh_token 已失效，请重新登录", ErrLoginCancelled)
		}
		return Credential{}, fmt.Errorf("TRAE 续期失败（HTTP %d）：%s", status, traeTruncate(text, 200))
	}
	return applyTraeRefresh(cred, exchange), nil
}

// applyTraeRefresh 把换来的新令牌写回旧凭据（refresh_token 轮换时以新值为准）。
func applyTraeRefresh(previous Credential, exchange *traeExchange) Credential {
	next := previous
	next.AccessToken = exchange.AccessToken
	if exchange.RefreshToken != "" {
		next.RefreshToken = exchange.RefreshToken
	}
	next.ExpiresAt = traeExpiresAt(exchange.TokenExpireAt, exchange.TokenExpireDuration, exchange.AccessToken, time.Now())
	return next
}

// ─── 错误分类 ──────────────────────────────────────────────────────────────

// Trae 的错误分类标记（与官方 trae-errors.ts 一致）。
var (
	traeSessionDeadMarkers  = []string{"login", "token 失效", "token invalid", "session", "unauthorized", "401"}
	traePlanLimitMarkers    = []string{`"code":1005`, "1005"}
	traeQuotaExceededMarker = []string{"4008", `"code":4008`, "quota", "exceeded the quota"}
)

// classifyTraeError 把 HTTP 状态与响应体归类成 Trae 的错误种类。
// 判定顺序与官方一致：套餐限制（需同时含 plan）→ 配额用尽 → 软限流 → 401 会话失效 →
// 429 → 404 → 5xx → 4xx → 其它。
func classifyTraeError(status int, body string) string {
	lower := strings.ToLower(body)
	for _, marker := range traePlanLimitMarkers {
		if strings.Contains(body, marker) && (strings.Contains(body, "plan") || strings.Contains(lower, "plan")) {
			return "hard-plan"
		}
	}
	for _, marker := range traeQuotaExceededMarker {
		if strings.Contains(body, marker) {
			return "quota-exceeded"
		}
	}
	if strings.Contains(body, "4011") || strings.Contains(body, `"code":4011`) {
		return "soft-rate"
	}
	if status == http.StatusUnauthorized {
		// 401 一律按会话失效处理（官方在此处无条件返回 session-dead）。
		return "session-dead"
	}
	switch {
	case status == http.StatusTooManyRequests:
		return "soft-rate"
	case status == http.StatusNotFound:
		return "not-found"
	case status >= 500:
		return "server"
	case status >= 400:
		return "client"
	}
	return "none"
}

// isTraeTerminalError 报告错误种类是否为终态（重试无意义，需重新登录）。
func isTraeTerminalError(kind string) bool { return kind == "session-dead" }

// traeTruncate 截断错误文本，避免把整段响应塞进错误消息。
func traeTruncate(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit])
}

const traeCallbackHTML = `<!doctype html><html><head><meta charset="utf-8"><title>TRAE 登录成功</title></head>` +
	`<body style="font-family:system-ui;text-align:center;padding-top:80px">` +
	`<h2>TRAE 登录成功</h2><p>可以关闭此页面返回面板。</p></body></html>`
