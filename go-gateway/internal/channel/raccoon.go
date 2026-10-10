package channel

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Raccoon（商汤 / Senser「小浣熊」）账号渠道。
//
// 登录有两条路径，都在**本机回环页**上完成（网关在 127.0.0.1 随机端口起一个小服务）：
//   - 微信扫码：网关本地生成一个随机 code，把它拼成 https://xiaohuanxiong.com/login/mp
//     的地址并**本地渲染成二维码**（服务端不提供二维码图片，见 raccoon_qr.go），
//     页面每 2 秒轮询一次 /raccoon/poll；扫到并确认后轮询直接取回 access_token。
//   - 短信：手机号经 AES-128-CFB 加密（密钥是客户端内置的固定十六进制串，见
//     raccoonPhoneCipherSecret）后调 send_sms / login_with_sms。下发短信前需要先过
//     阿里云滑块验证码，故页面里嵌了官方的 AliyunCaptcha 组件；组件加载失败（离线/
//     被拦截）时退化为直接提交，由服务端报明确的错误码。
//
// 与 loomy 的区别：raccoon 没有第三方应用密钥要配，AES 密钥与滑块 SceneId 都是客户端
// 内置的公开常量，登录开箱可用；但它的二维码是**本地渲染**而不是下载图片，因此多了一份
// QR 编码器（raccoon_qr.go）。
//
// 推理走标准 OpenAI Chat Completions 协议（POST /api/web/llm/v2/chat/completions），
// 复用 provider.OpenAIProvider。渠道特有逻辑有两处，缺一上游会忽略或拒绝：
//   - 请求头 X-Org-Code 必须是**当前账号**的 office_identity（个人账号为空），因此走
//     DynamicHeaders 而非静态 ExtraHeaders；
//   - 思考档位必须写在 extra_body.thinking 里（放顶层会被静默忽略），故由
//     RaccoonTransformRequest 把 inbound 的 reasoning_effort / thinking 折叠进去。
//
// 模型 ID 用上游原生的 `sn-` 前缀（与既有供应商无重名，无需再套命名空间）。
//
// ⚠️ 同其它账号渠道：这是复刻官方客户端的非官方用法，可能违反服务条款并导致账号被封。
// 仅用于本机个人测试，不要对外分发。
const (
	raccoonChannel = "raccoon"

	raccoonAPIBase = "https://xiaohuanxiong.com"

	raccoonAuthPrefix     = "/api/web/auth/v1"
	raccoonLLMPrefix      = "/api/web/llm/v2"
	raccoonDesktopPrefix  = "/api/web/desktop/v1"
	raccoonChatPath       = raccoonLLMPrefix + "/chat/completions"
	raccoonQrPollPath     = raccoonAuthPrefix + "/login_with_qrcode_code"
	raccoonSmsSendPath    = raccoonAuthPrefix + "/send_sms"
	raccoonSmsLoginPath   = raccoonAuthPrefix + "/login_with_sms"
	raccoonRefreshPath    = raccoonAuthPrefix + "/refresh"
	raccoonUserInfoPath   = raccoonAuthPrefix + "/user_info"
	raccoonLoginRewardURI = raccoonDesktopPrefix + "/login/points/grant"

	raccoonClientPlatform = "desktop-windows"
	raccoonClientVersion  = "v1.0.35"
	raccoonLanguage       = "zh"

	// raccoonOKCode 是业务信封的成功码；其余非零一律视为业务失败。
	raccoonOKCode = 0
	// raccoonCaptchaFailedCode 是「图形验证码校验失败」的业务码（短信下发）。
	raccoonCaptchaFailedCode = 100006
	// raccoonSessionExpiredCode 是「登录态已过期」的业务码（续期端点，HTTP 401 同义）。
	raccoonSessionExpiredCode = 200003

	// 本机回环页路径。与官方客户端一致：页面 → 轮询 → 短信下发 / 短信验证。
	raccoonPageLoginPath  = "/raccoon/login"
	raccoonPagePollPath   = "/raccoon/poll"
	raccoonPageSmsSend    = "/raccoon/sms/send"
	raccoonPageSmsVerify  = "/raccoon/sms/verify"
	raccoonMaxBodyBytes   = 64 * 1024
	raccoonRequestTimeout = 60 * time.Second
	raccoonLoginTimeout   = 5 * time.Minute
	// raccoonQrCodeBytes 是二维码 code 的随机字节数（官方为 randomBytes(16).toString("hex")，
	// 即 32 个十六进制字符）。
	raccoonQrCodeBytes = 16
)

// raccoonPhoneCipherSecret 是加密手机号的 AES-128 密钥。
//
// 它来自官方客户端内置的固定常量，**不是凭据**：这是把手机号发给「自己服务端」用的
// 对称密钥，服务端要求逐字节相同的值，知道它并不能取得任何访问权限（没有可解密的
// 机密数据，官方包里也只有一个加密函数、没有解密路径）。因此这里给出默认值以保证
// 登录开箱可用，同时允许环境变量覆盖以便联调。
const raccoonPhoneCipherSecret = "senseraccoon2023"

// raccoonPhoneCipherEnv 允许覆盖内置 AES 密钥（联调/服务端换钥时使用）。
const raccoonPhoneCipherEnv = "RACCOON_PHONE_CIPHER_SECRET"

// 阿里云滑块验证码的公开标识（前端 SceneId 与资源前缀）。它们出现在页面的网络请求里，
// 不是密钥；短信下发要求携带由它换来的 captcha_param。
const (
	raccoonCaptchaSceneID = "1pkmy0x3"
	raccoonCaptchaPrefix  = "hk1r5l"
)

// RaccoonChannel 持有 raccoon 的账号池与登录逻辑。
type RaccoonChannel struct {
	accounts *Accounts
	client   *oauthClient
	// http 用于登录页上游调用：超时比 oauthClient 短（登录端点都是小请求）。
	http *http.Client
	// apiBase 可覆盖，测试时指向本地假上游。
	apiBase string
	// cipherSecret 是加密手机号用的 AES 密钥（默认内置常量，可被环境变量覆盖）。
	cipherSecret string

	loginTimeout time.Duration
}

// NewRaccoonChannel 在 dir 下构造 raccoon 渠道（凭据文件 <dir>/raccoon.json）。
func NewRaccoonChannel(dir string) *RaccoonChannel {
	c := &RaccoonChannel{
		client:       newOAuthClient(),
		http:         &http.Client{Timeout: raccoonRequestTimeout},
		apiBase:      raccoonAPIBase,
		cipherSecret: raccoonPhoneCipherSecret,
		loginTimeout: raccoonLoginTimeout,
	}
	if override := strings.TrimSpace(os.Getenv(raccoonPhoneCipherEnv)); override != "" {
		c.cipherSecret = override
	}
	c.accounts = NewAccounts(raccoonChannel, NewStore(dir), c.client, c.refreshCredential)
	return c
}

// Accounts 供 provider 层取用令牌源，也是 Admin API 查看状态/登出的入口。
func (c *RaccoonChannel) Accounts() *Accounts { return c.accounts }

// Name 实现 Channel。
func (c *RaccoonChannel) Name() string { return raccoonChannel }

// ChatURL 是 raccoon 的 OpenAI Chat Completions 推理端点。
func (c *RaccoonChannel) ChatURL() string { return c.apiBase + raccoonChatPath }

// ClientHeaders 是复刻 raccoon 客户端所需的固定请求头。
//
// Accept: text/event-stream 必需（上游只提供流式响应，与 lobster 同理）；Authorization
// 由 provider.applyAuth 写入；随账号变化的 X-Org-Code 见 DynamicHeaders。
func (c *RaccoonChannel) ClientHeaders() map[string]string {
	return map[string]string{
		"Accept":             "text/event-stream",
		"X-Raccoon-Language": raccoonLanguage,
		"X-Client-Platform":  raccoonClientPlatform,
	}
}

// DynamicHeaders 返回「每次请求都重新求值」的请求头：X-Org-Code 取当前账号的
// office_identity（个人账号为空串，此时不写该头，与上游对缺失头的容忍一致）。
//
// 上游要求该头反映请求所用账号的身份，静态 ExtraHeaders 无法表达，故必须动态求值。
func (c *RaccoonChannel) DynamicHeaders() func() map[string]string {
	return func() map[string]string {
		extra := c.accounts.CurrentExtra()
		if extra == nil {
			return nil
		}
		org := extra["office_identity"]
		if org == "" {
			return nil
		}
		return map[string]string{"X-Org-Code": org}
	}
}

// RaccoonTransformRequest 把思考档位折叠进 extra_body.thinking。
//
// 上游只认 `extra_body.thinking.type`（`enabled` / `disabled`），顶层放 reasoning_effort
// 或 thinking 都会被静默忽略（连非法值都不报错）。而网关的 inbound 协议（Anthropic /
// OpenAI）把两者放在顶层，因此这里做一次迁移：
//   - 取 reasoning_effort（优先）或 thinking.type；
//   - "off" / "disabled" → disabled，其余非空取值 → enabled；
//   - 写进 extra_body.thinking，并删掉顶层的两个字段；
//   - 无档位信息时原样透传，保持服务端默认（= 开）。
//
// 与 loomy/buddy 的改写同理，provider 在序列化后、发送前调用本函数。
func RaccoonTransformRequest(body []byte) ([]byte, error) {
	payload := map[string]json.RawMessage{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return body, nil // 结构异常时原样透传，交给上游报错
	}

	effort := ""
	if raw, ok := payload["reasoning_effort"]; ok {
		var value string
		if json.Unmarshal(raw, &value) == nil {
			effort = value
		}
		delete(payload, "reasoning_effort")
	}
	if raw, ok := payload["thinking"]; ok {
		if effort == "" {
			var thinking struct {
				Type string `json:"type"`
			}
			if json.Unmarshal(raw, &thinking) == nil {
				effort = thinking.Type
			}
		}
		// 顶层 thinking 上游不认，无论是否取到档位都删掉。
		delete(payload, "thinking")
	}
	if effort == "" {
		return json.Marshal(payload)
	}

	thinkingType := "enabled"
	if effort == "off" || effort == "disabled" {
		thinkingType = "disabled"
	}
	// 合并进已有的 extra_body（inbound 可能已经带了其它扩展字段），只覆盖 thinking。
	extra := map[string]json.RawMessage{}
	if raw, ok := payload["extra_body"]; ok {
		_ = json.Unmarshal(raw, &extra)
	}
	thinking, err := json.Marshal(map[string]string{"type": thinkingType})
	if err != nil {
		return body, nil
	}
	extra["thinking"] = thinking
	encoded, err := json.Marshal(extra)
	if err != nil {
		return body, nil
	}
	payload["extra_body"] = encoded
	return json.Marshal(payload)
}

// ── 登录流程 ───────────────────────────────────────────────────────────────

// raccoonLoginResult 是登录成功后要落盘的字段。
type raccoonLoginResult struct {
	AccessToken    string
	RefreshToken   string
	ExpiresAt      time.Time
	OfficeIdentity string
}

// raccoonLoginState 是回环页面的共享状态。浏览器会并发请求轮询与表单路由，读写都要持锁。
type raccoonLoginState struct {
	mu            sync.Mutex
	qrCode        string
	phone         string
	lastExpiredAt string
	settled       bool
	result        raccoonLoginResult
	err           error
}

// raccoonLogin 是一次进行中的登录。对上层暴露成 LoginSession。
type raccoonLogin struct {
	channel  *RaccoonChannel
	server   *http.Server
	listener net.Listener
	port     int
	state    *raccoonLoginState
	timeout  time.Duration

	resultCh  chan struct{}
	pollMu    sync.Mutex // 串行化页面轮询，避免并发请求重复消费上游状态
	closeOnce sync.Once
}

// BeginLogin 实现 Channel：起本机回环页服务并立即返回登录地址。
//
// 与其余渠道一致，登录地址放进 DeviceCode.VerificationURIComplete，Admin API 原样
// 回成 login_url，前端据此打开弹窗。
func (c *RaccoonChannel) BeginLogin(ctx context.Context) (LoginSession, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("raccoon 登录服务启动失败: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port

	login := &raccoonLogin{
		channel: c,
		state: &raccoonLoginState{
			qrCode: mustRandomHex(raccoonQrCodeBytes),
		},
		timeout:  c.loginTimeout,
		resultCh: make(chan struct{}),
	}
	login.listener = listener
	login.port = port
	login.server = &http.Server{Handler: http.HandlerFunc(login.handle)}
	go func() { _ = login.server.Serve(listener) }()
	return login, nil
}

// Channel 实现 LoginSession。
func (l *raccoonLogin) Channel() string { return raccoonChannel }

// DeviceCode 实现 LoginSession：扫码登录没有设备码，用 DeviceCode 承载登录地址。
func (l *raccoonLogin) DeviceCode() DeviceCode {
	loginURL := fmt.Sprintf("http://127.0.0.1:%d%s", l.port, raccoonPageLoginPath)
	return DeviceCode{
		VerificationURI:         loginURL,
		VerificationURIComplete: loginURL,
		ExpiresIn:               l.timeout,
	}
}

// Wait 实现 LoginSession：等浏览器里完成扫码或短信登录，补全用户信息后落盘。
func (l *raccoonLogin) Wait(ctx context.Context) (*Credential, error) {
	defer l.close()

	waitCtx, cancel := context.WithTimeout(ctx, l.timeout)
	defer cancel()
	go func() {
		<-waitCtx.Done()
		l.close()
	}()

	select {
	case <-l.resultCh:
	case <-waitCtx.Done():
		if waitCtx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("%w: raccoon 登录超时（%s 内未完成）", ErrLoginCancelled, l.timeout)
		}
		return nil, waitCtx.Err()
	}

	l.state.mu.Lock()
	err := l.state.err
	result := l.state.result
	l.state.mu.Unlock()
	if err != nil {
		return nil, err
	}

	cred := raccoonCredential(result)
	// 补全用户信息（昵称/手机号/user_id/office_identity）：失败不阻断登录，
	// 只少几个展示字段。
	if info, infoErr := l.channel.fetchUserInfo(ctx, cred.AccessToken, cred.Extra["office_identity"]); infoErr == nil {
		applyRaccoonUserInfo(&cred, info)
	}
	if err := l.channel.accounts.add(cred); err != nil {
		return nil, err
	}
	// 领取一次性登录奖励：与官方客户端一致，失败只忽略。
	_ = l.channel.claimLoginReward(ctx, cred)
	return &cred, nil
}

func (l *raccoonLogin) close() {
	l.closeOnce.Do(func() {
		if l.server != nil {
			_ = l.server.Close()
		}
	})
}

// settle 记录登录结果并唤醒 Wait；只生效一次。
func (l *raccoonLogin) settle(result raccoonLoginResult, err error) {
	l.state.mu.Lock()
	if l.state.settled {
		l.state.mu.Unlock()
		return
	}
	l.state.settled = true
	l.state.result = result
	l.state.err = err
	l.state.mu.Unlock()
	close(l.resultCh)
}

// handle 是本机登录页的四个路由。
func (l *raccoonLogin) handle(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == raccoonPageLoginPath:
		l.state.mu.Lock()
		code := l.state.qrCode
		l.state.mu.Unlock()
		svg, err := renderRaccoonQrSVG(l.channel.qrImageURL(code), 200, 4)
		if err != nil {
			http.Error(w, "二维码渲染失败："+err.Error(), http.StatusInternalServerError)
			return
		}
		writeRaccoonHTML(w, raccoonLoginPageHTML(svg))
	case r.URL.Path == raccoonPagePollPath:
		l.handlePoll(r.Context(), w)
	case r.URL.Path == raccoonPageSmsSend && r.Method == http.MethodPost:
		l.handleSmsSend(r, w)
	case r.URL.Path == raccoonPageSmsVerify && r.Method == http.MethodPost:
		l.handleSmsVerify(r, w)
	default:
		http.NotFound(w, r)
	}
}

// handlePoll 处理页面轮询：把上游扫码状态映射成页面文案，成功后结算登录。
//
// 用互斥锁串行化：页面每 2 秒轮询一次，网络慢时可能并发，重复消费会打乱状态机。
func (l *raccoonLogin) handlePoll(ctx context.Context, w http.ResponseWriter) {
	l.pollMu.Lock()
	defer l.pollMu.Unlock()

	l.state.mu.Lock()
	code := l.state.qrCode
	l.state.mu.Unlock()

	poll, err := l.channel.pollQrLogin(ctx, code)
	if err != nil {
		// 轮询偶发失败不结算，让页面下一次继续（与官方客户端一致）。
		writeRaccoonJSON(w, map[string]any{"status": raccoonQRPending, "message": err.Error()})
		return
	}

	if poll.ExpiredAt != "" {
		l.state.mu.Lock()
		l.state.lastExpiredAt = poll.ExpiredAt
		l.state.mu.Unlock()
	}

	switch poll.Status {
	case raccoonQRSuccess:
		l.settle(raccoonLoginResult{
			AccessToken:  poll.AccessToken,
			RefreshToken: poll.RefreshToken,
			ExpiresAt:    poll.ExpiresAt,
		}, nil)
		writeRaccoonJSON(w, map[string]any{"status": raccoonQRSuccess})
	case raccoonQRCanceled:
		// 二维码被取消：换一个新 code，并把新二维码给页面（页面据此换图）。
		next := mustRandomHex(raccoonQrCodeBytes)
		l.state.mu.Lock()
		l.state.qrCode = next
		l.state.mu.Unlock()
		payload := map[string]any{"status": raccoonQRCanceled}
		if svg, err := renderRaccoonQrSVG(l.channel.qrImageURL(next), 200, 4); err == nil {
			payload["qr"] = svg
		}
		writeRaccoonJSON(w, payload)
	default:
		payload := map[string]any{"status": poll.Status}
		if poll.ExpiredAt != "" {
			payload["expiredAt"] = poll.ExpiredAt
		}
		writeRaccoonJSON(w, payload)
	}
}

// handleSmsSend 处理「获取验证码」：校验手机号 → 加密 → 调上游下发。
func (l *raccoonLogin) handleSmsSend(r *http.Request, w http.ResponseWriter) {
	body, err := readRaccoonJSONBody(r)
	if err != nil {
		writeRaccoonJSON(w, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	phone := strings.TrimSpace(stringField(body, "phone"))
	captchaParam := stringField(body, "captchaParam")
	if !raccoonPhoneRe.MatchString(phone) {
		writeRaccoonJSON(w, map[string]any{"ok": false, "message": "请输入有效的 11 位手机号"})
		return
	}
	if err := l.channel.sendSmsCode(r.Context(), phone, captchaParam); err != nil {
		writeRaccoonJSON(w, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	l.state.mu.Lock()
	l.state.phone = phone
	l.state.mu.Unlock()
	writeRaccoonJSON(w, map[string]any{"ok": true})
}

// handleSmsVerify 处理「登录」：用手机号 + 短信验证码换令牌。
func (l *raccoonLogin) handleSmsVerify(r *http.Request, w http.ResponseWriter) {
	body, err := readRaccoonJSONBody(r)
	if err != nil {
		writeRaccoonJSON(w, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	l.state.mu.Lock()
	phone := l.state.phone
	l.state.mu.Unlock()
	if phone == "" {
		writeRaccoonJSON(w, map[string]any{"ok": false, "message": "请先获取手机验证码"})
		return
	}
	smsCode := strings.TrimSpace(stringField(body, "smsCode"))
	if smsCode == "" {
		writeRaccoonJSON(w, map[string]any{"ok": false, "message": "请输入验证码"})
		return
	}
	result, err := l.channel.loginWithSmsCode(r.Context(), phone, smsCode)
	if err != nil {
		writeRaccoonJSON(w, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	l.settle(result, nil)
	writeRaccoonJSON(w, map[string]any{"ok": true})
}

// ── 上游登录端点 ───────────────────────────────────────────────────────────

// raccoonEnvelope 是业务信封 {code, message?, details?, data?}。
type raccoonEnvelope struct {
	Code    int
	Message string
	Details string
	Data    map[string]any
}

// parseRaccoonEnvelope 解析信封。code 缺失时回退到 HTTP 状态码（≥400 视为失败）。
func parseRaccoonEnvelope(payload map[string]any, status int) raccoonEnvelope {
	code := 0
	switch value := payload["code"].(type) {
	case float64:
		code = int(value)
	case json.Number:
		if parsed, err := value.Int64(); err == nil {
			code = int(parsed)
		} else {
			code = status
		}
	default:
		if status >= 400 {
			code = status
		}
	}
	envelope := raccoonEnvelope{
		Code:    code,
		Message: stringField(payload, "message"),
		Details: stringField(payload, "details"),
	}
	if data, ok := payload["data"].(map[string]any); ok {
		envelope.Data = data
	}
	return envelope
}

// OK 报告信封是否表示成功。
func (e raccoonEnvelope) OK() bool { return e.Code == raccoonOKCode }

// Error 把 message 与 details 拼成错误文案；都为空时用 fallback。
func (e raccoonEnvelope) Error(fallback string) error {
	parts := make([]string, 0, 2)
	if e.Message != "" {
		parts = append(parts, e.Message)
	}
	if e.Details != "" {
		parts = append(parts, e.Details)
	}
	if len(parts) == 0 {
		return errors.New(fallback)
	}
	return errors.New(strings.Join(parts, ": "))
}

// postRaccoonJSON 向登录端点发一个 JSON POST，返回信封与 HTTP 状态码。
func (c *RaccoonChannel) postRaccoonJSON(ctx context.Context, path string, body any) (raccoonEnvelope, int, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return raccoonEnvelope{}, 0, fmt.Errorf("raccoon 请求体构造失败: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiBase+path, strings.NewReader(string(payload)))
	if err != nil {
		return raccoonEnvelope{}, 0, fmt.Errorf("raccoon 请求构造失败（%s）: %w", path, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "raccoon-client")

	resp, err := c.http.Do(req)
	if err != nil {
		return raccoonEnvelope{}, 0, fmt.Errorf("raccoon 请求失败（%s）: %w", path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return raccoonEnvelope{}, resp.StatusCode, fmt.Errorf("raccoon 响应读取失败（%s）: %w", path, err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return raccoonEnvelope{}, resp.StatusCode, fmt.Errorf("raccoon 响应不是 JSON（%s，HTTP %d）", path, resp.StatusCode)
	}
	return parseRaccoonEnvelope(parsed, resp.StatusCode), resp.StatusCode, nil
}

// qrImageURL 拼二维码内容：{apiBase}/login/mp?code=<hex>&appname=<商汤小浣熊官网>。
func (c *RaccoonChannel) qrImageURL(code string) string {
	params := url.Values{}
	params.Set("code", code)
	params.Set("appname", "商汤小浣熊官网")
	return c.apiBase + "/login/mp?" + params.Encode()
}

// raccoonQrPoll 是一次二维码轮询的结果。
type raccoonQrPoll struct {
	Status       string
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
	ExpiredAt    string
}

const (
	raccoonQRPending  = "pending"
	raccoonQRLogging  = "logging"
	raccoonQRCanceled = "canceled"
	raccoonQRSuccess  = "success"
)

// pollQrLogin 轮询一次扫码状态。
//
// 上游把「还没扫」「扫了没确认」都表达在 data.status 里；任何传输失败/非零 code/缺
// data 一律按 pending 处理（官方客户端如此），避免把瞬时抖动当成终止态。
func (c *RaccoonChannel) pollQrLogin(ctx context.Context, code string) (raccoonQrPoll, error) {
	envelope, _, err := c.postRaccoonJSON(ctx, raccoonQrPollPath, map[string]any{"qrcode_code": code})
	if err != nil {
		return raccoonQrPoll{Status: raccoonQRPending}, nil
	}
	if !envelope.OK() || envelope.Data == nil {
		return raccoonQrPoll{Status: raccoonQRPending}, nil
	}
	status := stringField(envelope.Data, "status")
	expiredAt := stringField(envelope.Data, "expired_at")

	switch status {
	case raccoonQRCanceled:
		return raccoonQrPoll{Status: raccoonQRCanceled}, nil
	case raccoonQRLogging:
		return raccoonQrPoll{Status: raccoonQRLogging, ExpiredAt: expiredAt}, nil
	case raccoonQRSuccess:
		accessToken := stringField(envelope.Data, "access_token")
		if accessToken == "" {
			return raccoonQrPoll{Status: raccoonQRPending}, nil
		}
		return raccoonQrPoll{
			Status:       raccoonQRSuccess,
			AccessToken:  accessToken,
			RefreshToken: stringField(envelope.Data, "refresh_token"),
			ExpiresAt:    raccoonExpiryFromToken(accessToken),
		}, nil
	}
	return raccoonQrPoll{Status: raccoonQRPending}, nil
}

// sendSmsCode 下发短信验证码。手机号先做 AES-128-CFB 加密（密钥见 cipherSecret）。
func (c *RaccoonChannel) sendSmsCode(ctx context.Context, phone, captchaParam string) error {
	encrypted, err := c.encryptPhone(phone)
	if err != nil {
		return err
	}
	envelope, _, err := c.postRaccoonJSON(ctx, raccoonSmsSendPath, map[string]any{
		"captcha_param": captchaParam,
		"nation_code":   "86",
		"phone":         encrypted,
	})
	if err != nil {
		return err
	}
	if !envelope.OK() {
		if envelope.Code == raccoonCaptchaFailedCode {
			return errors.New("图形验证码校验失败，请重新完成滑块验证")
		}
		return envelope.Error("下发短信验证码失败")
	}
	return nil
}

// loginWithSmsCode 用手机号 + 短信验证码换令牌。
func (c *RaccoonChannel) loginWithSmsCode(ctx context.Context, phone, smsCode string) (raccoonLoginResult, error) {
	encrypted, err := c.encryptPhone(phone)
	if err != nil {
		return raccoonLoginResult{}, err
	}
	envelope, _, err := c.postRaccoonJSON(ctx, raccoonSmsLoginPath, map[string]any{
		"nation_code": "86",
		"phone":       encrypted,
		"sms_code":    smsCode,
	})
	if err != nil {
		return raccoonLoginResult{}, err
	}
	if !envelope.OK() {
		return raccoonLoginResult{}, envelope.Error("短信登录失败")
	}
	return raccoonResultFromData(envelope.Data)
}

// raccoonResultFromData 从登录响应的 data 里取令牌。
func raccoonResultFromData(data map[string]any) (raccoonLoginResult, error) {
	accessToken := stringField(data, "access_token")
	if accessToken == "" {
		return raccoonLoginResult{}, errors.New("登录响应缺少 access_token")
	}
	return raccoonLoginResult{
		AccessToken:    accessToken,
		RefreshToken:   stringField(data, "refresh_token"),
		ExpiresAt:      raccoonExpiryFromToken(accessToken),
		OfficeIdentity: stringField(data, "office_identity"),
	}, nil
}

// encryptPhone 用 AES-128-CFB 加密手机号：IV（16 随机字节）前置在密文之前，整体 base64。
//
// 与官方客户端逐字节等价：密钥 = cipherSecret 的 UTF-8 字节，setAutoPadding(false)
// （CFB 是流模式，无填充）。服务端无解密函数，且这是唯一的加密调用点。
func (c *RaccoonChannel) encryptPhone(phone string) (string, error) {
	key := []byte(c.cipherSecret)
	if len(key) != 16 {
		return "", fmt.Errorf("raccoon 手机号加密密钥长度非法（需 16 字节，当前 %d）", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("raccoon 手机号加密初始化失败: %w", err)
	}
	nonce := make([]byte, aes.BlockSize)
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("raccoon 生成加密随机数失败: %w", err)
	}
	ciphertext := make([]byte, len(phone))
	cipher.NewCFBEncrypter(block, nonce).XORKeyStream(ciphertext, []byte(phone))
	return base64.StdEncoding.EncodeToString(append(nonce, ciphertext...)), nil
}

// raccoonExpiryFromToken 从 JWT 的 exp 推出到期时间（解析失败返回零值，由调用方在
// 401 时触发刷新）。
func raccoonExpiryFromToken(token string) time.Time {
	return decodeJWTExpiresAt(token)
}

// raccoonCredential 把登录结果装配成落盘凭据。
//
// Extra 里存 office_identity（聊天请求头 X-Org-Code 要用）、user_id、phone，
// 供多账号消歧与请求头动态取值。
func raccoonCredential(result raccoonLoginResult) Credential {
	extra := map[string]string{}
	if result.OfficeIdentity != "" {
		extra["office_identity"] = result.OfficeIdentity
	}
	return Credential{
		AccessToken:  result.AccessToken,
		RefreshToken: result.RefreshToken,
		ExpiresAt:    result.ExpiresAt,
		Nickname:     result.OfficeIdentity,
		Extra:        extra,
	}
}

// raccoonUserInfo 是 user_info 端点取到的用户信息。
type raccoonUserInfo struct {
	UserID         string
	Nickname       string
	OfficeIdentity string
	Phone          string
}

// fetchUserInfo 拉取用户信息（只读，失败由调用方忽略）。
func (c *RaccoonChannel) fetchUserInfo(ctx context.Context, accessToken, officeIdentity string) (raccoonUserInfo, error) {
	if accessToken == "" {
		return raccoonUserInfo{}, errors.New("缺少令牌")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.apiBase+raccoonUserInfoPath, nil)
	if err != nil {
		return raccoonUserInfo{}, err
	}
	c.setCredentialHeaders(req, accessToken, officeIdentity)
	resp, err := c.http.Do(req)
	if err != nil {
		return raccoonUserInfo{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return raccoonUserInfo{}, fmt.Errorf("user_info 返回 HTTP %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return raccoonUserInfo{}, err
	}
	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return raccoonUserInfo{}, err
	}
	envelope := parseRaccoonEnvelope(parsed, resp.StatusCode)
	if !envelope.OK() || envelope.Data == nil {
		return raccoonUserInfo{}, errors.New("user_info 业务失败")
	}
	return raccoonUserInfo{
		UserID:         stringField(envelope.Data, "id"),
		Nickname:       stringField(envelope.Data, "name"),
		OfficeIdentity: stringField(envelope.Data, "office_identity"),
		Phone:          stringField(envelope.Data, "phone"),
	}, nil
}

// applyRaccoonUserInfo 把用户信息合并进凭据（已存在的值不覆盖）。
func applyRaccoonUserInfo(cred *Credential, info raccoonUserInfo) {
	if cred.Extra == nil {
		cred.Extra = map[string]string{}
	}
	if info.UserID != "" {
		cred.Extra["user_id"] = info.UserID
		if cred.AccountID == "" {
			cred.AccountID = info.UserID
		}
	}
	if info.Phone != "" {
		cred.Extra["phone"] = info.Phone
	}
	if info.OfficeIdentity != "" {
		cred.Extra["office_identity"] = info.OfficeIdentity
	}
	// 昵称优先用服务端 name；服务端的 name 是自动生成的默认名（可能重名），
	// 故手机号在消歧上更可靠，作为次选。
	if info.Nickname != "" {
		cred.Nickname = info.Nickname
	} else if cred.Nickname == "" {
		cred.Nickname = firstNonEmpty(info.Phone, info.UserID)
	} else if info.Phone != "" && cred.Nickname == info.Phone {
		cred.Nickname = info.Phone
	}
}

// ── 续期与奖励 ─────────────────────────────────────────────────────────────

// refreshCredential 实现 Accounts 的刷新函数：调 /refresh 换新令牌。
//
// 缺 refresh_token 或上游报告登录态过期时返回 ErrLoginCancelled 包装的错误，
// Accounts.Refresh 会据此换下一个账号；全部失效即报告未登录，UI 提示重新登录。
func (c *RaccoonChannel) refreshCredential(ctx context.Context, cred Credential) (Credential, error) {
	if cred.RefreshToken == "" {
		return Credential{}, fmt.Errorf("%w: raccoon 凭据缺少 refresh_token，请重新登录", ErrLoginCancelled)
	}
	envelope, status, err := c.postRaccoonJSON(ctx, raccoonRefreshPath, map[string]any{
		"refresh_token": cred.RefreshToken,
	})
	if err != nil {
		return Credential{}, err
	}
	if status == http.StatusUnauthorized || envelope.Code == raccoonSessionExpiredCode {
		return Credential{}, fmt.Errorf("%w: 登录态已过期，请重新登录", ErrLoginCancelled)
	}
	if !envelope.OK() {
		return Credential{}, envelope.Error("raccoon 续期失败")
	}
	accessToken := stringField(envelope.Data, "access_token")
	if accessToken == "" {
		return Credential{}, errors.New("raccoon 续期响应缺少 access_token")
	}
	next := cred
	next.AccessToken = accessToken
	if refresh := stringField(envelope.Data, "refresh_token"); refresh != "" {
		next.RefreshToken = refresh
	}
	if expires := raccoonExpiryFromToken(accessToken); !expires.IsZero() {
		next.ExpiresAt = expires
	}
	return next, nil
}

// claimLoginReward 领取一次性登录奖励（best-effort：失败不影响登录结果）。
func (c *RaccoonChannel) claimLoginReward(ctx context.Context, cred Credential) error {
	if cred.AccessToken == "" {
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiBase+raccoonLoginRewardURI, strings.NewReader("{}"))
	if err != nil {
		return err
	}
	c.setCredentialHeaders(req, cred.AccessToken, cred.Extra["office_identity"])
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	return nil
}

// setCredentialHeaders 写入带账号身份的请求头（业务端点与聊天端点的公共形状）。
func (c *RaccoonChannel) setCredentialHeaders(req *http.Request, accessToken, officeIdentity string) {
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("X-Org-Code", officeIdentity)
	req.Header.Set("X-Raccoon-Language", raccoonLanguage)
	req.Header.Set("X-Client-Platform", raccoonClientPlatform)
	req.Header.Set("X-Client-Version", raccoonClientVersion)
}

// ── 本机页面辅助 ───────────────────────────────────────────────────────────

// raccoonPhoneRe 校验中国大陆手机号（与官方客户端页面的正则一致）。
var raccoonPhoneRe = regexp.MustCompile(`^1[3-9]\d{9}$`)

// readRaccoonJSONBody 读取本机页面提交的 JSON 体，超过上限报错。
func readRaccoonJSONBody(r *http.Request) (map[string]any, error) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, raccoonMaxBodyBytes+1))
	if err != nil {
		return nil, errors.New("请求体读取失败")
	}
	if len(raw) > raccoonMaxBodyBytes {
		return nil, errors.New("请求体过大")
	}
	if len(raw) == 0 {
		return map[string]any{}, nil
	}
	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil || parsed == nil {
		return nil, errors.New("请求体不是合法 JSON")
	}
	return parsed, nil
}

// writeRaccoonJSON 输出本机页面用的 JSON 响应。
func writeRaccoonJSON(w http.ResponseWriter, payload map[string]any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(payload)
}

// writeRaccoonHTML 输出本机登录页。
func writeRaccoonHTML(w http.ResponseWriter, html string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = io.WriteString(w, html)
}

// ── 登录页 ────────────────────────────────────────────────────────────────

// raccoonLoginPageTemplate 是登录页模板；__RACCOON_QR_SVG__ 由服务器渲染的二维码 SVG 替换。
//
// 页面结构照抄官方客户端：左侧二维码 + 右侧短信表单两个 tab，每 2 秒轮询一次状态。
// 短信下发前先过阿里云滑块：官方组件加载失败（离线/被拦截）时降级为直接提交，
// 由服务端返回明确的验证码失败文案。
const raccoonLoginPageTemplate = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Raccoon 登录</title>
<style>
  :root { color-scheme: light dark; }
  body { margin: 0; min-height: 100vh; display: flex; align-items: center; justify-content: center;
         font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", "Microsoft YaHei", sans-serif;
         background: #f4f5f7; color: #1f2329; }
  .card { background: #fff; border-radius: 12px; padding: 26px 30px 30px; width: 340px;
          box-shadow: 0 4px 24px rgba(0,0,0,.08); }
  h1 { font-size: 17px; margin: 0 0 2px; font-weight: 600; text-align: center; }
  .sub { font-size: 12px; color: #8a9099; text-align: center; margin-bottom: 16px; }
  .tabs { display: flex; border-bottom: 1px solid #eceef1; margin-bottom: 18px; }
  .tab { flex: 1; text-align: center; padding: 9px 0; font-size: 14px; color: #8a9099; cursor: pointer;
         border-bottom: 2px solid transparent; }
  .tab[data-active="1"] { color: #1f2329; font-weight: 600; border-bottom-color: #07c160; }
  .pane { display: none; text-align: center; }
  .pane[data-show="1"] { display: block; }
  .qr { width: 200px; height: 200px; display: block; margin: 0 auto; border: 1px solid #eceef1;
        border-radius: 8px; }
  .status { margin-top: 14px; font-size: 13px; color: #4e5969; min-height: 20px; }
  .status[data-tone="ok"] { color: #0f9d58; font-weight: 600; }
  .status[data-tone="err"] { color: #d93026; }
  label { display: block; font-size: 12px; color: #8a9099; margin: 0 0 4px; text-align: left; }
  input { width: 100%; box-sizing: border-box; padding: 9px 10px; font-size: 14px; margin-bottom: 12px;
          border: 1px solid #d9dde3; border-radius: 6px; }
  button { width: 100%; padding: 10px; font-size: 14px; border: 0; border-radius: 6px;
           background: #07c160; color: #fff; cursor: pointer; }
  button:disabled { background: #c9cdd4; cursor: not-allowed; }
  .row { display: flex; gap: 8px; }
  .row input { margin-bottom: 12px; }
  .row button { width: auto; white-space: nowrap; padding: 9px 12px; }
  #captcha-element { margin-bottom: 12px; }
</style>
</head>
<body>
  <div class="card">
    <h1>Raccoon（商汤）账号登录</h1>
    <div class="sub">登录后令牌只保存于本机文件</div>

    <div class="tabs">
      <div class="tab" id="tabQr" data-active="1">微信扫码</div>
      <div class="tab" id="tabSms" data-active="0">短信登录</div>
    </div>

    <div class="pane" id="paneQr" data-show="1">
      <div id="qrHolder">{{RACCOON_QR_SVG}}</div>
      <div class="status" id="qrStatus">等待扫码…</div>
    </div>

    <div class="pane" id="paneSms" data-show="0">
      <label for="phone">手机号</label>
      <div class="row">
        <input id="phone" type="tel" inputmode="numeric" maxlength="11" placeholder="11 位手机号">
        <button id="sendBtn" type="button">获取验证码</button>
      </div>
      <label for="code">短信验证码</label>
      <input id="code" type="text" inputmode="numeric" maxlength="6" placeholder="请输入验证码">
      <div id="captcha-element"></div>
      <button id="verifyBtn" type="button">登录</button>
      <div class="status" id="smsStatus"></div>
    </div>
  </div>

<script>
(function () {
  var qrHolder = document.getElementById('qrHolder');
  var qrStatus = document.getElementById('qrStatus');
  var smsStatus = document.getElementById('smsStatus');
  var tabQr = document.getElementById('tabQr');
  var tabSms = document.getElementById('tabSms');
  var paneQr = document.getElementById('paneQr');
  var paneSms = document.getElementById('paneSms');
  var phoneEl = document.getElementById('phone');
  var codeEl = document.getElementById('code');
  var sendBtn = document.getElementById('sendBtn');
  var verifyBtn = document.getElementById('verifyBtn');
  var captchaParam = '';
  var qrStopped = false;

  function setQrStatus(text, tone) {
    qrStatus.textContent = text;
    if (tone) qrStatus.setAttribute('data-tone', tone); else qrStatus.removeAttribute('data-tone');
  }
  function setSmsStatus(text, tone) {
    smsStatus.textContent = text || '';
    if (tone) smsStatus.setAttribute('data-tone', tone); else smsStatus.removeAttribute('data-tone');
  }

  tabQr.addEventListener('click', function () {
    tabQr.setAttribute('data-active', '1'); tabSms.setAttribute('data-active', '0');
    paneQr.setAttribute('data-show', '1'); paneSms.setAttribute('data-show', '0');
  });
  tabSms.addEventListener('click', function () {
    tabQr.setAttribute('data-active', '0'); tabSms.setAttribute('data-active', '1');
    paneQr.setAttribute('data-show', '0'); paneSms.setAttribute('data-show', '1');
  });

  function done(message) {
    var text = message || '登录成功，可以关闭此窗口了';
    setQrStatus(text, 'ok');
    setSmsStatus(text, 'ok');
    setTimeout(function () { window.close(); }, 1200);
  }

  function poll() {
    if (qrStopped) return;
    fetch('/raccoon/poll')
      .then(function (r) { return r.json(); })
      .then(function (res) {
        if (res.status === 'success') { qrStopped = true; done(); return; }
        if (res.status === 'logging') { setQrStatus('已扫码，请在微信中确认…'); }
        else if (res.status === 'canceled') {
          if (res.qr) qrHolder.innerHTML = res.qr;
          setQrStatus('二维码已刷新，请重新扫码');
        } else { setQrStatus('等待扫码…'); }
        setTimeout(poll, 2000);
      })
      .catch(function () { setTimeout(poll, 2000); });
  }
  poll();

  function post(path, payload) {
    return fetch(path, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload)
    }).then(function (r) { return r.json(); });
  }

  // 阿里云滑块：加载官方脚本初始化；若不可用（离线/被拦截）则退化为直接提交，
  // 由服务端给出明确错误（后端会返回「图形验证码校验失败」）。
  window.__raccoonCaptchaReady = false;
  (function loadCaptcha() {
    var script = document.createElement('script');
    script.src = 'https://o.alicdn.com/captcha-frontend/aliyunCaptcha/AliyunCaptcha.js';
    script.onload = function () {
      try {
        window.initAliyunCaptcha({
          SceneId: '{{RACCOON_CAPTCHA_SCENE_ID}}',
          prefix: '{{RACCOON_CAPTCHA_PREFIX}}',
          mode: 'popup',
          element: '#captcha-element',
          button: '#sendBtn',
          slideStyle: { width: 320, height: 40 },
          language: 'cn',
          captchaVerifyCallback: function (param) {
            captchaParam = (param && param.captchaVerifyParam) || '';
            window.__raccoonCaptchaReady = true;
            doSend();
            return { captchaResult: true };
          },
          onBizResultCallback: function () {}
        });
      } catch (e) { /* 忽略，走降级 */ }
    };
    script.onerror = function () { /* 忽略，走降级 */ };
    document.head.appendChild(script);
  })();

  function doSend() {
    var phone = (phoneEl.value || '').replace(/\D/g, '');
    if (!/^1[3-9]\d{9}$/.test(phone)) { setSmsStatus('请输入有效的 11 位手机号', 'err'); return; }
    sendBtn.disabled = true;
    setSmsStatus('正在发送验证码…');
    post('/raccoon/sms/send', { phone: phone, captchaParam: captchaParam })
      .then(function (res) {
        if (res.ok) setSmsStatus('验证码已发送至 ' + phone);
        else setSmsStatus(res.message || '发送失败', 'err');
      })
      .catch(function () { setSmsStatus('发送失败，请重试', 'err'); })
      .finally(function () { sendBtn.disabled = false; });
  }

  sendBtn.addEventListener('click', function () {
    // 如果滑块组件还没就绪（脚本未加载/初始化失败），直接提交，让服务端报错。
    if (!window.__raccoonCaptchaReady) { doSend(); }
    // 已就绪时由组件的按钮接管点击（button: '#sendBtn'），不重复触发。
  });

  verifyBtn.addEventListener('click', function () {
    var code = (codeEl.value || '').replace(/\D/g, '');
    if (!code) { setSmsStatus('请输入验证码', 'err'); return; }
    verifyBtn.disabled = true;
    setSmsStatus('正在登录…');
    post('/raccoon/sms/verify', { smsCode: code })
      .then(function (res) {
        if (res.ok) done();
        else setSmsStatus(res.message || '登录失败', 'err');
      })
      .catch(function () { setSmsStatus('登录失败，请重试', 'err'); })
      .finally(function () { verifyBtn.disabled = false; });
  });
})();
</script>
</body>
</html>`

// raccoonLoginPageHTML 把二维码 SVG 与滑块常量填进页面模板。
//
// 滑块常量从 Go 常量注入（而不是在页面里再抄一份），保证与 raccoonCaptchaSceneID /
// raccoonCaptchaPrefix 只有一个来源。
func raccoonLoginPageHTML(qrSVG string) string {
	page := strings.Replace(raccoonLoginPageTemplate, "{{RACCOON_QR_SVG}}", qrSVG, 1)
	page = strings.Replace(page, "{{RACCOON_CAPTCHA_SCENE_ID}}", raccoonCaptchaSceneID, 1)
	return strings.Replace(page, "{{RACCOON_CAPTCHA_PREFIX}}", raccoonCaptchaPrefix, 1)
}
