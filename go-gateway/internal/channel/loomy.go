package channel

import (
	"context"
	"crypto/hmac"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Loomy（讯飞 / xunfei）账号渠道。
//
// 登录是「微信扫码 + 本机回环页」：网关先向微信开放平台换一个二维码 uuid，再在
// 127.0.0.1 上起一个小服务承载弹窗页（内联二维码 + 长轮询 + 首次使用时的手机号绑定
// 表单）。用户用微信扫码后：
//   - 若该微信号已绑定过 Loomy 账号 → 直接拿到会话；
//   - 若首次使用 → 页面出现手机号表单，短信验证通过后拿到会话。
//
// 与 lobster/gemini 的回环回调不同，这里没有 OAuth 重定向：微信的 `wx_code` 由
// **长轮询** 直接取回（回调页实测 404），所以不复用 channel.callbackServer，而是自己
// 起一个三路由的小服务（/wechat/qr、/wechat/poll、/wechat/complete）。
//
// 推理走标准 OpenAI Chat Completions 协议（/api/v1/chat/completions），因此复用
// provider.OpenAIProvider。两处渠道特有逻辑必须齐全，缺一上游会拒绝：
//   - 聊天请求要同时带 `Authorization: Bearer <token>` 与自定义头 `token: <token>`
//     （业务端点只认 `token`，聊天端点只认 Bearer，官方客户端两个都发）。Bearer 由
//     provider.applyAuth 写入，`token` 是「按请求动态求值」的头，通过
//     Config.DynamicHeaders 提供。
//   - 模型名带 `loomy/` 前缀（8 个模型 ID 与既有供应商重名，不加前缀会与
//     Router.RegisterModel 的模型 ID 冲突），发上游前必须剥掉。
//
// 账号端点的鉴权与聊天完全不同：/login/** 走 HMAC-SHA1 签名
// （Authorization: account <accessKeyId>:<signature>），密钥来自讯飞账号体系的
// accessKey。该 accessKey 属于第三方应用凭据，刻意不进公开仓库（见
// loomyClientCredentialFile / resolveCredentials）。
//
// ⚠️ 与其余账号渠道一样，这是复刻官方客户端的非官方用法，可能违反服务条款并导致
// 账号被封。仅用于本机个人测试，不要对外分发。
const (
	loomyChannel = "loomy"

	loomyAPIBase     = "https://loomyad.xunfei.cn/api/v1"
	loomyAccountBase = "https://account.xfinfr.com"

	// loomyOKCode 是业务信封的成功码。其余取值一律视为业务失败。
	loomyOKCode = "000000"
	// loomyAuthErrorCode 是「登录已失效」的业务码：探测到它就说明凭据必须重新登录。
	loomyAuthErrorCode = "100002"

	// loomySmsCodeTTL 与 loomySessionTTL 对应官方客户端的 expire 参数（秒）。
	// 服务端响应不带到期时间戳，凭据的 expires_at 只能由本地按 loomySessionTTL 推算。
	loomySmsCodeTTL = 300
	loomySessionTTL = 1209600 // 14 天

	loomyAccountModelID = "Web"
	loomyAccountVersion = "1.0.0"
	loomyAccountDevID   = "web"
	// loomyAccountUA 刻意硬编码 macOS：官方客户端在 Windows 上发的也是这个值，
	// 服务端按它做校验，照抄不要改。
	loomyAccountUA = "Loomy|Desktop|Electron|macOS"

	loomyChatPath = "/chat/completions"

	// loomyWeChatRedirectURI 是微信开放平台侧登记的回调地址。取 code 走长轮询、
	// 不碰回调页，但授权地址里仍要带上它（微信会校验 redirect_uri 是否登记过）。
	loomyWeChatRedirectURI = "https://loomy.xunfei.cn/oauth/wechat/callback"

	// loomyWeChatQRPath/loomyWeChatPollPath/loomyWeChatCompletePath 是本机回环
	// 服务承载的三个路由：页面、轮询、表单提交。
	loomyWeChatQRPath       = "/wechat/qr"
	loomyWeChatPollPath     = "/wechat/poll"
	loomyWeChatCompletePath = "/wechat/complete"

	// loomyClientCredentialFile 是本地存放讯飞账号侧应用凭据的文件名，位于
	// <gatewayHome>（channel 凭据目录的上一级）。内容形如
	// {"access_key_id":"..","access_key_secret":"..","app_id":".."}。
	//
	// accessKeyId/accessKeySecret 刻意不进仓库：本仓库是公开仓库，硬编码第三方应用
	// 密钥会被 GitHub 密钥扫描拦截，也等于把别人的凭据公开出去。因此改为「本地文件或
	// 环境变量」二选一，未配置时登录直接给出可操作的报错（见 BeginLogin）。
	loomyClientCredentialFile = "loomy-client.json"
)

// loomyWeChatAppID 是 Loomy 官方客户端的微信开放平台 AppID。
//
// AppID 不是机密：它出现在用户可见的授权 URL 与二维码链接里。因此这里给出默认值
// （与 Cline 的公开 client id 同一处理），仍支持环境变量覆盖以便更换。
const loomyWeChatAppID = "wx18d60be432287cf8"

// loomyAppID 是讯飞账号侧的 appid（账号请求体 base.appid）。
// 同样是公开标识而非密钥，给出默认值并支持覆盖。
const loomyAppID = "GM3LOOMY"

// loomyWeChatUA 是抓取微信授权页/二维码时使用的浏览器 UA：微信开放平台会拒绝
// 非浏览器 UA。轮询接口共用。
const loomyWeChatUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

// loomyWeChatBase 是微信开放平台的网页端主机（授权页与二维码图片）。
const loomyWeChatBase = "https://open.weixin.qq.com"

// loomyWeChatLongBase 是微信扫码状态的长轮询主机。
const loomyWeChatLongBase = "https://long.open.weixin.qq.com"

const (
	loomyRequestTimeout = 60 * time.Second
	loomyFetchTimeout   = 30 * time.Second
	loomyPollTimeout    = 40 * time.Second
	loomyLoginTimeout   = 5 * time.Minute
	loomyQRPollInterval = 1200 * time.Millisecond
	loomyMaxBodyBytes   = 64 * 1024
)

// 微信扫码轮询状态。注意 404 只是「已扫码待确认」，必须继续轮询等 405。
const (
	loomyPollWaiting   = "waiting"
	loomyPollScanned   = "scanned"
	loomyPollConfirmed = "confirmed"
	loomyPollCancelled = "cancelled"
	loomyPollExpired   = "expired"
	loomyPollError     = "error"
)

// loomyUUIDRe 校验从授权页 HTML 里抽出来的二维码 uuid 形状。
var loomyUUIDRe = regexp.MustCompile(`^[A-Za-z0-9_\-=+/]{6,64}$`)

// loomyUUIDFromImgRe / loomyUUIDFromPollRe 是两种页面形态的 uuid 提取模式。
var (
	loomyUUIDFromImgRe  = regexp.MustCompile(`/connect/qrcode/([A-Za-z0-9_\-=+/]+)`)
	loomyUUIDFromPollRe = regexp.MustCompile(`l/qrconnect\?uuid=([A-Za-z0-9_\-=+/]+)`)
)

// LoomyChannel 持有 loomy 的账号池、登录逻辑与账号侧应用凭据。
type LoomyChannel struct {
	accounts *Accounts
	client   *oauthClient
	// fetch 用于抓取微信授权页/二维码/轮询：这些请求要带浏览器 UA 与 Referer，
	// 且是纯 GET 文本/二进制，不复用 oauthClient 的 JSON 往返。
	fetch *http.Client
	// accountClient 用于调用讯飞账号端点：超时 60s（比 oauthClient 的 20s 长），
	// 请求体需要逐字节签名，故单独构造请求而不是走 postJSON。
	accountClient *http.Client
	// home 是 <gatewayHome>，用于定位本地应用凭据文件（可选）。
	home string

	// 端点与凭据可覆盖，测试时指向本地假上游。
	apiBase     string
	accountBase string
	wechatBase  string
	wechatLong  string

	appID           string
	wechatAppID     string
	accessKeyID     string
	accessKeySecret string

	loginTimeout time.Duration
}

// NewLoomyChannel 在 dir 下构造 loomy 渠道（凭据文件 <dir>/loomy.json）。
// 讯飞账号侧应用凭据（accessKeyId/accessKeySecret）从本地解析，见 resolveCredentials。
func NewLoomyChannel(dir string) *LoomyChannel {
	c := &LoomyChannel{
		client:        newOAuthClient(),
		fetch:         &http.Client{Timeout: loomyFetchTimeout},
		accountClient: &http.Client{Timeout: loomyRequestTimeout},
		home:          filepath.Dir(dir),
		apiBase:       loomyAPIBase,
		accountBase:   loomyAccountBase,
		wechatBase:    loomyWeChatBase,
		wechatLong:    loomyWeChatLongBase,
		appID:         loomyAppID,
		wechatAppID:   loomyWeChatAppID,
		loginTimeout:  loomyLoginTimeout,
	}
	c.accessKeyID, c.accessKeySecret, c.appID, c.wechatAppID = c.resolveCredentials()
	c.accounts = NewAccounts(loomyChannel, NewStore(dir), c.client, c.refreshCredential)
	return c
}

// resolveCredentials 解析应用凭据，优先级：环境变量 > 本地凭据文件 > 内置公开默认值。
//
// accessKeyId/accessKeySecret 没有内置默认值——它们是第三方应用密钥，写进公开仓库会
// 泄露。app_id 与微信 AppID 是公开标识，保留默认值。
func (c *LoomyChannel) resolveCredentials() (accessKeyID, accessKeySecret, appID, wechatAppID string) {
	accessKeyID = os.Getenv("LOOMY_ACCESS_KEY_ID")
	accessKeySecret = os.Getenv("LOOMY_ACCESS_KEY_SECRET")
	// 公开标识有内置默认值，但默认值必须最后才兜底：若在这里先套上默认值，
	// 后面的 firstNonEmpty(appID, fileAppID, loomyAppID) 就永远取不到文件里的值。
	appID = os.Getenv("LOOMY_APP_ID")
	wechatAppID = os.Getenv("LOOMY_WECHAT_APP_ID")

	fileID, fileSecret, fileAppID, fileWeChatAppID := c.loadCredentialFile()
	return firstNonEmpty(accessKeyID, fileID),
		firstNonEmpty(accessKeySecret, fileSecret),
		firstNonEmpty(appID, fileAppID, loomyAppID),
		firstNonEmpty(wechatAppID, fileWeChatAppID, loomyWeChatAppID)
}

// loadCredentialFile 读取 <home>/loomy-client.json（best-effort）。
func (c *LoomyChannel) loadCredentialFile() (accessKeyID, accessKeySecret, appID, wechatAppID string) {
	if c.home == "" {
		return "", "", "", ""
	}
	data, err := os.ReadFile(filepath.Join(c.home, loomyClientCredentialFile))
	if err != nil {
		return "", "", "", ""
	}
	var payload struct {
		AccessKeyID     string `json:"access_key_id"`
		AccessKeySecret string `json:"access_key_secret"`
		AppID           string `json:"app_id"`
		WeChatAppID     string `json:"wechat_app_id"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return "", "", "", ""
	}
	return payload.AccessKeyID, payload.AccessKeySecret, payload.AppID, payload.WeChatAppID
}

// configured 报告应用凭据是否齐备（缺一登录无法进行）。
func (c *LoomyChannel) configured() bool {
	return c.accessKeyID != "" && c.accessKeySecret != "" && c.appID != "" && c.wechatAppID != ""
}

// Accounts 供 provider 层取用令牌源，也是 Admin API 查看状态/登出的入口。
func (c *LoomyChannel) Accounts() *Accounts { return c.accounts }

// Name 实现 Channel。
func (c *LoomyChannel) Name() string { return loomyChannel }

// Namespace 是 loomy 对外模型名的前缀。8 个模型 ID 与既有供应商重名，必须加前缀
// 才能与 Router.RegisterModel 的模型 ID 区分。
func (c *LoomyChannel) Namespace() string { return loomyChannel + "/" }

// ChatURL 是 loomy 的 OpenAI Chat Completions 推理端点。
func (c *LoomyChannel) ChatURL() string { return c.apiBase + loomyChatPath }

// ClientHeaders 是复刻 Loomy 客户端所需的固定请求头。
//
// Accept: text/event-stream 是必需的：上游只提供流式响应（与 lobster 同理），
// 不带它会被拒。Authorization 由 provider.applyAuth 写入，动态的 `token` 头见
// DynamicHeaders。
func (c *LoomyChannel) ClientHeaders() map[string]string {
	return map[string]string{
		"Accept":       "text/event-stream",
		"Content-Type": "application/json",
	}
}

// DynamicHeaders 返回「每次请求都重新求值」的请求头：把当前账号令牌再写进自定义头
// `token`。上游的聊天端点只认 Bearer、业务端点只认 token，官方客户端两个都发，因此
// 这里必须与 applyAuth 写的 Authorization 同时存在。
//
// 未登录时返回 nil（provider 侧本就不会发出请求，这里只是保持一致）。
func (c *LoomyChannel) DynamicHeaders() func() map[string]string {
	return func() map[string]string {
		token := c.accounts.BearerToken()
		if token == "" {
			return nil
		}
		return map[string]string{"token": token}
	}
}

// LoomyTransformRequest 把对外模型名的 `loomy/` 前缀剥掉再发上游。
//
// 与 buddy 的改写同理：provider 在序列化后、发送前调用本函数，此时 body 里的 model
// 已是路由层赋的绑定模型 ID（带前缀）。解析失败或无前缀时原样透传，交给上游报错。
func LoomyTransformRequest(body []byte) ([]byte, error) {
	payload := map[string]json.RawMessage{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return body, nil
	}
	raw, ok := payload["model"]
	if !ok {
		return body, nil
	}
	var model string
	if err := json.Unmarshal(raw, &model); err != nil {
		return body, nil
	}
	stripped := strings.TrimPrefix(model, loomyChannel+"/")
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

// BeginLogin 实现 Channel：先向微信换二维码 uuid，再起本机回环服务承载扫码页。
//
// 与其余渠道一致，登录地址放进 DeviceCode.VerificationURIComplete，Admin API 原样
// 回成 login_url，前端据此打开弹窗。未配置应用凭据时直接给出可操作的报错。
func (c *LoomyChannel) BeginLogin(ctx context.Context) (LoginSession, error) {
	if !c.configured() {
		return nil, fmt.Errorf(
			"loomy 登录需要讯飞账号侧的应用凭据（access_key_id / access_key_secret）："+
				"请设置 LOOMY_ACCESS_KEY_ID / LOOMY_ACCESS_KEY_SECRET 环境变量，或提供 %s"+
				"（{\"access_key_id\":\"..\",\"access_key_secret\":\"..\"}）。"+
				"凭据属于第三方应用，不进公开仓库",
			filepath.Join(c.home, loomyClientCredentialFile))
	}

	uuid, err := c.fetchWechatUUID(ctx, mustRandomHex(16))
	if err != nil {
		return nil, err
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("loomy 登录服务启动失败: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port

	login := &loomyLogin{
		channel:  c,
		state:    &loomyWechatState{uuid: uuid},
		resultCh: make(chan struct{}),
		timeout:  c.loginTimeout,
	}
	login.server = &http.Server{Handler: http.HandlerFunc(login.handle)}
	login.listener = listener
	login.port = port

	go func() { _ = login.server.Serve(listener) }()

	return login, nil
}

// loomyLoginResult 是扫码流程成功后要落盘的字段。
type loomyLoginResult struct {
	Session  string
	UserID   string
	Phone    string
	Nickname string
}

// loomyWechatState 是扫码流程的共享状态。浏览器会并发请求轮询与表单路由，
// 每次读写都要持锁。
type loomyWechatState struct {
	mu          sync.Mutex
	uuid        string
	lastErrcode string
	wechatCode  string
	rcode       string
	bind        int
	nickname    string
	bindMsgid   string

	// settled 后不再接受新的 settle 调用（单次登录只结算一次）。
	settled bool
	result  loomyLoginResult
	err     error
}

// loomyLogin 是一次进行中的扫码登录。
type loomyLogin struct {
	channel  *LoomyChannel
	server   *http.Server
	listener net.Listener
	port     int
	state    *loomyWechatState
	timeout  time.Duration
	resultCh chan struct{}

	closeOnce sync.Once
}

// Channel 实现 LoginSession。
func (l *loomyLogin) Channel() string { return loomyChannel }

// DeviceCode 实现 LoginSession：扫码登录没有设备码，用 DeviceCode 承载登录地址，
// 与 lobster/gemini 的做法一致。
func (l *loomyLogin) DeviceCode() DeviceCode {
	loginURL := l.loginURL()
	return DeviceCode{
		VerificationURI:         loginURL,
		VerificationURIComplete: loginURL,
		ExpiresIn:               l.timeout,
	}
}

// loginURL 是本机扫码页地址，也是 Admin API 回给前端的 login_url。
func (l *loomyLogin) loginURL() string {
	return fmt.Sprintf("http://127.0.0.1:%d%s", l.port, loomyWeChatQRPath)
}

// Wait 实现 LoginSession：等浏览器里完成扫码（必要时补绑手机号），拿会话落盘。
func (l *loomyLogin) Wait(ctx context.Context) (*Credential, error) {
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
			return nil, fmt.Errorf("%w: loomy 微信登录超时（%s 内未完成）", ErrLoginCancelled, l.timeout)
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

	cred := buildLoomyCredential(result, time.Now())
	if err := l.channel.accounts.add(cred); err != nil {
		return nil, err
	}
	// 领取每日额度：与官方客户端一致，失败只忽略（不影响本次登录）。
	_ = l.channel.claimDailyQuota(ctx, cred.AccessToken)
	return &cred, nil
}

func (l *loomyLogin) close() {
	l.closeOnce.Do(func() {
		if l.server != nil {
			_ = l.server.Close()
		}
	})
}

// settle 记录登录结果并唤醒 Wait；只生效一次。
func (l *loomyLogin) settle(result loomyLoginResult, err error) {
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

// handle 是本机扫码页的三个路由。逻辑与官方客户端的回环服务一致：
// 页面 → 轮询 → （首次使用时）表单。
func (l *loomyLogin) handle(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == loomyWeChatQRPath:
		dataURL, err := l.channel.fetchWechatQRImage(r.Context(), l.state.getUUID())
		if err != nil {
			http.Error(w, "二维码获取失败："+err.Error(), http.StatusBadGateway)
			return
		}
		writeLoomyHTML(w, loomyQRPageHTML(dataURL))
	case r.URL.Path == loomyWeChatPollPath:
		l.handlePoll(r.Context(), w)
	case r.URL.Path == loomyWeChatCompletePath && r.Method == http.MethodPost:
		l.handleComplete(r, w)
	default:
		http.NotFound(w, r)
	}
}

// getUUID 读取二维码 uuid。
func (s *loomyWechatState) getUUID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.uuid
}

// handlePoll 处理页面轮询：确认后立刻换 rcode，已绑定则直接结算登录，
// 否则提示页面显示手机号绑定表单。
func (l *loomyLogin) handlePoll(ctx context.Context, w http.ResponseWriter) {
	l.state.mu.Lock()
	already := l.state.wechatCode
	uuid := l.state.uuid
	last := l.state.lastErrcode
	l.state.mu.Unlock()

	if already != "" {
		writeLoomyJSON(w, map[string]any{"status": loomyPollConfirmed})
		return
	}

	polled := l.channel.pollWechatOnce(ctx, uuid, last)
	l.state.mu.Lock()
	if polled.Errcode != "" {
		l.state.lastErrcode = polled.Errcode
	}
	l.state.mu.Unlock()

	if polled.Status != loomyPollConfirmed || polled.Code == "" {
		writeLoomyJSON(w, map[string]any{"status": polled.Status, "message": polled.Message})
		return
	}

	auth, err := l.channel.bindWechatAccount(ctx, polled.Code)
	if err != nil {
		l.state.mu.Lock()
		l.state.wechatCode = polled.Code
		l.state.mu.Unlock()
		l.settle(loomyLoginResult{}, err)
		writeLoomyJSON(w, map[string]any{"status": loomyPollError, "message": err.Error()})
		return
	}
	l.state.mu.Lock()
	l.state.wechatCode = polled.Code
	l.state.rcode = auth.Rcode
	l.state.bind = auth.Bind
	l.state.nickname = auth.Nickname
	l.state.mu.Unlock()

	if auth.Bind == 1 {
		login, err := l.channel.bindSkip(ctx, auth.Rcode)
		if err != nil {
			l.settle(loomyLoginResult{}, err)
			writeLoomyJSON(w, map[string]any{"status": loomyPollError, "message": err.Error()})
			return
		}
		l.settle(loomyLoginResult{
			Session:  login.Session,
			UserID:   login.UserID,
			Nickname: auth.Nickname,
		}, nil)
		writeLoomyJSON(w, map[string]any{"status": "done", "message": "登录成功，可以关闭此窗口了"})
		return
	}

	writeLoomyJSON(w, map[string]any{"status": "need_phone"})
}

// handleComplete 处理首次使用时的手机号绑定表单（发短信 / 校验短信）。
func (l *loomyLogin) handleComplete(r *http.Request, w http.ResponseWriter) {
	body, err := readLoomyJSONBody(r)
	if err != nil {
		writeLoomyJSON(w, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	action := stringField(body, "action")

	l.state.mu.Lock()
	rcode := l.state.rcode
	nickname := l.state.nickname
	l.state.mu.Unlock()

	switch action {
	case "send_sms":
		phone := loomyDigits(stringField(body, "phone"))
		msgid, err := l.channel.bindSendMsg(r.Context(), rcode, phone)
		if err != nil {
			writeLoomyJSON(w, map[string]any{"ok": false, "message": err.Error()})
			return
		}
		l.state.mu.Lock()
		l.state.bindMsgid = msgid
		l.state.mu.Unlock()
		writeLoomyJSON(w, map[string]any{"ok": true})
	case "verify_sms":
		phone := loomyDigits(stringField(body, "phone"))
		code := loomyDigits(stringField(body, "code"))
		l.state.mu.Lock()
		msgid := l.state.bindMsgid
		l.state.mu.Unlock()
		login, err := l.channel.bindCheckCode(r.Context(), rcode, code, msgid)
		if err != nil {
			writeLoomyJSON(w, map[string]any{"ok": false, "message": err.Error()})
			return
		}
		if login.Phone == "" {
			login.Phone = phone
		}
		l.settle(loomyLoginResult{
			Session:  login.Session,
			UserID:   login.UserID,
			Phone:    login.Phone,
			Nickname: nickname,
		}, nil)
		writeLoomyJSON(w, map[string]any{"ok": true, "done": true})
	default:
		writeLoomyJSON(w, map[string]any{"ok": false, "message": "未知 action: " + action})
	}
}

// ── 微信扫码原语 ────────────────────────────────────────────────────────────

// wechatAuthURL 拼微信开放平台的授权地址。state 只在抓取授权页时用于取 uuid。
func (c *LoomyChannel) wechatAuthURL(state string) string {
	query := url.Values{}
	query.Set("appid", c.wechatAppID)
	query.Set("redirect_uri", loomyWeChatRedirectURI)
	query.Set("response_type", "code")
	query.Set("scope", "snsapi_login")
	query.Set("state", state)
	return c.wechatBase + "/connect/qrconnect?" + query.Encode() + "#wechat_redirect"
}

// fetchWechatUUID 抓取授权页并从中提取二维码 uuid。
func (c *LoomyChannel) fetchWechatUUID(ctx context.Context, state string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.wechatAuthURL(state), nil)
	if err != nil {
		return "", fmt.Errorf("微信授权页请求构造失败: %w", err)
	}
	c.setWechatHeaders(req, c.wechatBase+"/")
	resp, err := c.fetch.Do(req)
	if err != nil {
		return "", fmt.Errorf("微信授权页拉取失败：%w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("微信授权页返回 HTTP %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("微信授权页读取失败：%w", err)
	}
	uuid := extractLoomyWechatUUID(string(raw))
	if uuid == "" {
		return "", errors.New("微信授权页未包含二维码 uuid（页面结构可能已变化）")
	}
	return uuid, nil
}

// fetchWechatQRImage 下载二维码图片并转成 data URL（内联进页面）。
func (c *LoomyChannel) fetchWechatQRImage(ctx context.Context, uuid string) (string, error) {
	endpoint := c.wechatBase + "/connect/qrcode/" + url.PathEscape(uuid)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", fmt.Errorf("微信二维码请求构造失败: %w", err)
	}
	c.setWechatHeaders(req, c.wechatBase+"/")
	resp, err := c.fetch.Do(req)
	if err != nil {
		return "", fmt.Errorf("微信二维码下载失败：%w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("微信二维码返回 HTTP %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return "", fmt.Errorf("微信二维码读取失败：%w", err)
	}
	mime := detectImageMime(raw)
	if mime == "" || len(raw) < 200 {
		return "", errors.New("微信二维码响应不是图片（可能是错误页）")
	}
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(raw), nil
}

// loomyWechatPoll 是一次轮询的结果。
type loomyWechatPoll struct {
	Status  string
	Code    string
	Errcode string
	Message string
}

// pollWechatOnce 长轮询一次扫码状态。405 才是「已确认」，404 只是「已扫码待确认」，
// 必须继续轮询；网络/解析异常返回 error 状态让调用方继续轮询。
func (c *LoomyChannel) pollWechatOnce(ctx context.Context, uuid, lastErrcode string) loomyWechatPoll {
	query := "uuid=" + url.QueryEscape(uuid)
	if lastErrcode != "" {
		query += "&last=" + url.QueryEscape(lastErrcode)
	}
	query += "&_=" + strconv.FormatInt(time.Now().UnixMilli(), 10)
	endpoint := c.wechatLong + "/connect/l/qrconnect?" + query

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return loomyWechatPoll{Status: loomyPollError, Message: err.Error()}
	}
	c.setWechatHeaders(req, c.wechatAuthURL(""))
	resp, err := c.fetch.Do(req)
	if err != nil {
		return loomyWechatPoll{Status: loomyPollError, Message: "网络异常"}
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return loomyWechatPoll{Status: loomyPollError, Message: "读取失败"}
	}
	body := string(raw)

	errcode := loomyErrcodeRe.FindStringSubmatch(body)
	code := loomyWxCodeRe.FindStringSubmatch(body)
	errcodeValue := ""
	if len(errcode) > 1 {
		errcodeValue = errcode[1]
	}
	codeValue := ""
	if len(code) > 1 {
		codeValue = code[1]
	}

	switch errcodeValue {
	case "405":
		if codeValue != "" {
			return loomyWechatPoll{Status: loomyPollConfirmed, Code: codeValue, Errcode: errcodeValue}
		}
		return loomyWechatPoll{Status: loomyPollScanned, Errcode: errcodeValue}
	case "404":
		return loomyWechatPoll{Status: loomyPollScanned, Errcode: errcodeValue}
	case "403":
		return loomyWechatPoll{Status: loomyPollCancelled, Errcode: errcodeValue}
	case "402":
		return loomyWechatPoll{Status: loomyPollExpired, Errcode: errcodeValue}
	}
	return loomyWechatPoll{Status: loomyPollWaiting, Errcode: errcodeValue}
}

// setWechatHeaders 补齐微信侧要求的浏览器头。
func (c *LoomyChannel) setWechatHeaders(req *http.Request, referer string) {
	req.Header.Set("User-Agent", loomyWeChatUA)
	req.Header.Set("Referer", referer)
}

var (
	loomyErrcodeRe = regexp.MustCompile(`wx_errcode\s*=\s*(\d+)`)
	loomyWxCodeRe  = regexp.MustCompile(`wx_code\s*=\s*'([^']*)'`)
)

// extractLoomyWechatUUID 从授权页 HTML 里提取二维码 uuid。
// 先试图片路径形态，再试轮询链接形态；候选值必须通过形状校验。
func extractLoomyWechatUUID(html string) string {
	if html == "" {
		return ""
	}
	if match := loomyUUIDFromImgRe.FindStringSubmatch(html); len(match) > 1 {
		if loomyUUIDRe.MatchString(match[1]) {
			return match[1]
		}
	}
	if match := loomyUUIDFromPollRe.FindStringSubmatch(html); len(match) > 1 {
		if loomyUUIDRe.MatchString(match[1]) {
			return match[1]
		}
	}
	return ""
}

// detectImageMime 按魔数识别二维码图片类型；非图片返回空串。
func detectImageMime(bytes []byte) string {
	if len(bytes) >= 3 && bytes[0] == 0xFF && bytes[1] == 0xD8 && bytes[2] == 0xFF {
		return "image/jpeg"
	}
	if len(bytes) >= 4 && bytes[0] == 0x89 && bytes[1] == 'P' && bytes[2] == 'N' && bytes[3] == 'G' {
		return "image/png"
	}
	if len(bytes) >= 3 && bytes[0] == 'G' && bytes[1] == 'I' && bytes[2] == 'F' {
		return "image/gif"
	}
	return ""
}

// ── 讯飞账号端点（HMAC-SHA1 签名）─────────────────────────────────────────

// loomyAccountBaseInfo 是账号请求体里的 base 字段。
type loomyAccountBaseInfo struct {
	AppID   string `json:"appid"`
	ModelID string `json:"modelid"`
	Version string `json:"version"`
	DevID   string `json:"devid"`
	UA      string `json:"ua"`
	TraceID string `json:"traceid"`
}

// loomyEnvelope 是账号端点的业务信封 {code, desc, message, data}。
type loomyEnvelope struct {
	OK      bool
	Code    string
	Message string
	Data    map[string]any
}

// parseLoomyEnvelope 解析信封。code 为 "000000" 视为成功；否则取 desc（优先）或
// message 作为错误文案。
func parseLoomyEnvelope(payload map[string]any) loomyEnvelope {
	if payload == nil {
		return loomyEnvelope{Code: "", Message: "响应不是 JSON 对象"}
	}
	code := stringField(payload, "code")
	message := firstNonEmpty(stringField(payload, "desc"), stringField(payload, "message"))
	if code != loomyOKCode {
		if message == "" {
			if code == "" {
				message = "业务错误（缺少 code）"
			} else {
				message = "业务错误 " + code
			}
		}
		return loomyEnvelope{OK: false, Code: code, Message: message}
	}
	data, _ := payload["data"].(map[string]any)
	return loomyEnvelope{OK: true, Code: code, Message: message, Data: data}
}

// postAccount 调一个讯飞账号端点：构造请求体、签名、POST、解信封，返回 data。
func (c *LoomyChannel) postAccount(ctx context.Context, path string, param map[string]any) (map[string]any, error) {
	body, err := json.Marshal(map[string]any{
		"base": loomyAccountBaseInfo{
			AppID:   c.appID,
			ModelID: loomyAccountModelID,
			Version: loomyAccountVersion,
			DevID:   loomyAccountDevID,
			UA:      loomyAccountUA,
			TraceID: mustRandomHex(16),
		},
		"param": param,
	})
	if err != nil {
		return nil, fmt.Errorf("讯飞账号请求体构造失败：%w", err)
	}

	headers := c.signHeaders(http.MethodPost, path, string(body))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.accountBase+path, strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("讯飞账号请求构造失败（%s）：%w", path, err)
	}
	for name, value := range headers {
		req.Header.Set(name, value)
	}

	resp, err := c.accountClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("讯飞账号请求失败（%s）：%w", path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("讯飞账号响应读取失败（%s）：%w", path, err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("讯飞账号响应不是 JSON（%s，HTTP %d）", path, resp.StatusCode)
	}
	envelope := parseLoomyEnvelope(parsed)
	if !envelope.OK {
		if envelope.Message != "" {
			return nil, errors.New(envelope.Message)
		}
		return nil, fmt.Errorf("讯飞账号请求失败（%s）", path)
	}
	return envelope.Data, nil
}

// loomySigningString 拼签名原文：9 段以 \n 连接，末两段是签名头与规范化头的占位
// （官方实现固定为空串，照抄）。
func loomySigningString(method, path, body, contentType, date, nonce string) string {
	return strings.Join([]string{
		strings.ToUpper(method),
		buildEscapedPath(path),
		"",
		loomyContentMD5(body),
		contentType,
		date,
		nonce,
		"",
		"",
	}, "\n")
}

// loomySignature 计算 base64(HMAC-SHA1(secret, stringToSign))。
func loomySignature(secret, stringToSign string) string {
	mac := hmac.New(sha1.New, []byte(secret))
	mac.Write([]byte(stringToSign))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// signHeaders 生成账号端点的 HMAC-SHA1 签名头。
//
// stringToSign 由 9 段以 \n 连接：方法、转义路径、转义查询串、Content-MD5(base64)、
// Content-Type、Date(UTC)、Nonce、签名头占位、规范化头占位。签名 =
// base64(HMAC-SHA1(accessKeySecret, stringToSign))。
func (c *LoomyChannel) signHeaders(method, path, body string) map[string]string {
	date := time.Now().UTC().Format(http.TimeFormat)
	nonce := loomyUUID()
	stringToSign := loomySigningString(method, path, body, "application/json", date, nonce)

	headers := map[string]string{
		"Authorization": "account " + c.accessKeyID + ":" + loomySignature(c.accessKeySecret, stringToSign),
		"Date":          date,
		"Nonce":         nonce,
		"Content-Type":  "application/json",
	}
	if md5sum := loomyContentMD5(body); md5sum != "" {
		headers["Content-MD5"] = md5sum
	}
	return headers
}

// loomyContentMD5 返回 body 的 base64(MD5)；空 body 返回空串（此时代码不写 Content-MD5）。
func loomyContentMD5(body string) string {
	if body == "" {
		return ""
	}
	sum := md5.Sum([]byte(body))
	return base64.StdEncoding.EncodeToString(sum[:])
}

// buildEscapedPath 规范化并逐段转义路径：保证前导 /、去掉尾随 /（根路径除外）。
func buildEscapedPath(rawPath string) string {
	clean := rawPath
	if !strings.HasPrefix(clean, "/") {
		clean = "/" + clean
	}
	if len(clean) > 1 && strings.HasSuffix(clean, "/") {
		clean = clean[:len(clean)-1]
	}
	segments := strings.Split(clean, "/")
	for i, segment := range segments {
		if segment != "" {
			segments[i] = loomyEscape(segment)
		}
	}
	return strings.Join(segments, "/")
}

// loomyEscape 等价于 JavaScript 的 encodeURIComponent，并额外把 ! ' ( ) * 转义
// （与官方实现的 escapeRfc3986 一致）。
func loomyEscape(value string) string {
	const hexDigits = "0123456789ABCDEF"
	var out strings.Builder
	for i := 0; i < len(value); i++ {
		ch := value[i]
		if (ch >= 'A' && ch <= 'Z') || (ch >= 'a' && ch <= 'z') ||
			(ch >= '0' && ch <= '9') || ch == '-' || ch == '_' || ch == '.' || ch == '~' {
			out.WriteByte(ch)
			continue
		}
		out.WriteByte('%')
		out.WriteByte(hexDigits[ch>>4])
		out.WriteByte(hexDigits[ch&0x0f])
	}
	return out.String()
}

// loomyUUID 生成一个 v4 UUID（签名用的 Nonce）。随机源失败时退回等长十六进制串。
func loomyUUID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return mustRandomHex(16)
	}
	buf[6] = (buf[6] & 0x0f) | 0x40
	buf[8] = (buf[8] & 0x3f) | 0x80
	return fmt.Sprintf("%s-%s-%s-%s-%s",
		hex.EncodeToString(buf[0:4]),
		hex.EncodeToString(buf[4:6]),
		hex.EncodeToString(buf[6:8]),
		hex.EncodeToString(buf[8:10]),
		hex.EncodeToString(buf[10:16]),
	)
}

// ── 账号级登录流程 ─────────────────────────────────────────────────────────

// loomyWechatAuth 是 /login/thirdAccount/bind/auth 的结果。
type loomyWechatAuth struct {
	Bind     int
	Rcode    string
	Nickname string
	IsNew    bool
}

// loomySession 是账号端点返回的会话。
type loomySession struct {
	Session string
	UserID  string
	Phone   string
}

// bindWechatAccount 用微信 code 换绑定状态：bind==1 表示已绑定过 Loomy 账号，
// 可直接登录；否则需要补绑手机号。
func (c *LoomyChannel) bindWechatAccount(ctx context.Context, code string) (*loomyWechatAuth, error) {
	data, err := c.postAccount(ctx, "/login/thirdAccount/bind/auth", map[string]any{
		"tcode": map[string]any{"code": code},
		"type":  "wx",
	})
	if err != nil {
		return nil, err
	}
	rcode := stringField(data, "rcode")
	if rcode == "" {
		return nil, errors.New("微信授权响应缺少 rcode")
	}
	bind := 0
	if numberField(data, "bind") == 1 {
		bind = 1
	}
	return &loomyWechatAuth{
		Bind:     bind,
		Rcode:    rcode,
		Nickname: stringField(data, "nickname"),
		IsNew:    numberField(data, "isnew") != 0,
	}, nil
}

// bindSendMsg 向待绑定的手机号发短信验证码，返回 msgid。
func (c *LoomyChannel) bindSendMsg(ctx context.Context, rcode, phone string) (string, error) {
	data, err := c.postAccount(ctx, "/login/thirdAccount/bind/sendMsg", map[string]any{
		"rcode":  rcode,
		"phone":  phone,
		"ccode":  "86",
		"expire": loomySmsCodeTTL,
	})
	if err != nil {
		return "", err
	}
	msgid := loomyStringValue(data, "msgid")
	if msgid == "" {
		return "", errors.New("绑定手机号响应缺少 msgid")
	}
	return msgid, nil
}

// bindCheckCode 校验短信验证码，成功即完成绑定并返回会话。
func (c *LoomyChannel) bindCheckCode(ctx context.Context, rcode, mcode, msgid string) (*loomySession, error) {
	data, err := c.postAccount(ctx, "/login/thirdAccount/bind/checkCode", map[string]any{
		"rcode":  rcode,
		"mcode":  mcode,
		"msgid":  msgid,
		"expire": loomySessionTTL,
	})
	if err != nil {
		return nil, err
	}
	session := stringField(data, "session")
	if session == "" {
		return nil, errors.New("绑定登录响应缺少 session")
	}
	userID := stringField(data, "userid")
	if userID == "" {
		return nil, errors.New("绑定登录响应缺少 userid")
	}
	return &loomySession{Session: session, UserID: userID, Phone: stringField(data, "phone")}, nil
}

// bindSkip 跳过手机号绑定直接登录（该微信号此前已绑定过账号）。
func (c *LoomyChannel) bindSkip(ctx context.Context, rcode string) (*loomySession, error) {
	data, err := c.postAccount(ctx, "/login/thirdAccount/bind/skip", map[string]any{
		"rcode":  rcode,
		"expire": loomySessionTTL,
	})
	if err != nil {
		return nil, err
	}
	session := stringField(data, "session")
	if session == "" {
		return nil, errors.New("微信登录响应缺少 session")
	}
	userID := stringField(data, "userid")
	if userID == "" {
		return nil, errors.New("微信登录响应缺少 userid")
	}
	return &loomySession{Session: session, UserID: userID}, nil
}

// ── 凭据与健康探测 ─────────────────────────────────────────────────────────

// loomyProbeToken 是 loomy 凭据里 RefreshToken 的占位值。
//
// loomy 没有续期端点（官方客户端 refreshable 恒为 false），但 Accounts.Refresh 只在
// RefreshToken 非空时才走「真正调用刷新函数」的分支；留空会让 401 后的续期变成
// 「静默返回成功但什么都没做」。这里放一个非空占位值，让刷新函数（= 探测）真正跑起来。
const loomyProbeToken = "probe"

// buildLoomyCredential 把会话装配成落盘凭据。
//
// expires_at 由本地按 14 天推算：服务端响应不带到期时间戳，它只接受登录请求里的
// expire 参数，因此这里必须自己算。
func buildLoomyCredential(result loomyLoginResult, now time.Time) Credential {
	expiresAt := now.Add(time.Duration(loomySessionTTL) * time.Second)
	nickname := result.Nickname
	if nickname == "" {
		nickname = result.Phone
	}
	return Credential{
		AccessToken:  result.Session,
		RefreshToken: loomyProbeToken,
		ExpiresAt:    expiresAt,
		AccountID:    result.UserID,
		Nickname:     nickname,
		Extra: map[string]string{
			"userid": result.UserID,
			"phone":  result.Phone,
		},
	}
}

// refreshCredential 实现 Accounts 的刷新函数：loomy 无续期端点，这里只做一次
// 轻量只读探测，确认凭据仍然有效。
//
// 探测成功时原样返回凭据（令牌不变）；凭据已失效时返回错误，Accounts.Refresh 会据此
// 换下一个账号，全部失效即报告未登录，UI 提示重新扫码。
func (c *LoomyChannel) refreshCredential(ctx context.Context, cred Credential) (Credential, error) {
	if cred.AccessToken == "" {
		return Credential{}, fmt.Errorf("%w: loomy 凭据未配置，请先登录", ErrLoginCancelled)
	}
	if err := c.probeCredential(ctx, cred.AccessToken); err != nil {
		return Credential{}, err
	}
	return cred, nil
}

// probeCredential 用一次最便宜的只读请求验证凭据是否有效。
//
// 选 GET /points/records?pageSize=1 的理由：它不消耗积分、无副作用，且认证语义
// （token 头）与其余业务端点一致。业务码 100002 = 登录已失效。
func (c *LoomyChannel) probeCredential(ctx context.Context, token string) error {
	endpoint := c.apiBase + "/points/records?pageNo=1&pageSize=1&recordType=all"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("loomy 凭据探测请求构造失败: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("token", token)

	resp, err := c.accountClient.Do(req)
	if err != nil {
		return fmt.Errorf("loomy 凭据探测网络失败：%w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("loomy 凭据探测读取失败：%w", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return fmt.Errorf("loomy 凭据探测响应不是 JSON（HTTP %d）", resp.StatusCode)
	}
	envelope := parseLoomyEnvelope(parsed)
	if envelope.OK {
		return nil
	}
	if envelope.Code == loomyAuthErrorCode {
		return fmt.Errorf("%w: loomy 凭据已失效，请重新登录（%s）", ErrLoginCancelled, envelope.Message)
	}
	return fmt.Errorf("loomy 凭据探测失败：%s", envelope.Message)
}

// claimDailyQuota 触发每日额度领取（POST /points/first-login）。best-effort：
// 与官方客户端一致，失败不影响登录结果，由调用方忽略错误。
func (c *LoomyChannel) claimDailyQuota(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	endpoint := c.apiBase + "/points/first-login"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader("{}"))
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("token", token)
	resp, err := c.accountClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	return nil
}

// ── 小工具 ────────────────────────────────────────────────────────────────

// loomyStringValue 读取可能以数字编码的字段并转成字符串（msgid 就可能是数字）。
func loomyStringValue(obj map[string]any, key string) string {
	if obj == nil {
		return ""
	}
	switch value := obj[key].(type) {
	case string:
		return value
	case float64:
		return strconv.FormatInt(int64(value), 10)
	case json.Number:
		return value.String()
	}
	return ""
}

// loomyDigits 只保留数字，用于手机号/验证码输入清洗。
func loomyDigits(value string) string {
	var out strings.Builder
	for _, r := range value {
		if r >= '0' && r <= '9' {
			out.WriteRune(r)
		}
	}
	return out.String()
}

// readLoomyJSONBody 读取本机页面提交的 JSON 体，超过 loomyMaxBodyBytes 报错。
func readLoomyJSONBody(r *http.Request) (map[string]any, error) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, loomyMaxBodyBytes+1))
	if err != nil {
		return nil, errors.New("请求体读取失败")
	}
	if len(raw) > loomyMaxBodyBytes {
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

// writeLoomyJSON 输出本机页面用的 JSON 响应。
func writeLoomyJSON(w http.ResponseWriter, payload map[string]any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(payload)
}

// writeLoomyHTML 输出本机扫码页。
func writeLoomyHTML(w http.ResponseWriter, html string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = io.WriteString(w, html)
}

// loomyQRPageHTML 生成扫码页：内联二维码 + 轮询 + 首次使用时的手机号表单。
func loomyQRPageHTML(qrDataURL string) string {
	page := strings.NewReplacer(
		loomyQRDataURLPlaceholder, qrDataURL,
		loomyPollPathPlaceholder, loomyWeChatPollPath,
		loomyCompletePathPlaceholder, loomyWeChatCompletePath,
	).Replace(loomyQRPageTemplate)
	return page
}

const (
	loomyQRDataURLPlaceholder    = "__LOOMY_QR_DATA_URL__"
	loomyPollPathPlaceholder     = "__LOOMY_POLL_PATH__"
	loomyCompletePathPlaceholder = "__LOOMY_COMPLETE_PATH__"
)

// loomyQRPageTemplate 是扫码页模板（占位符由 loomyQRPageHTML 替换）。
// 轮询节奏、状态文案与官方客户端一致：404「已扫码待确认」必须继续轮询等 405。
const loomyQRPageTemplate = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Loomy 微信登录</title>
<style>
  :root { color-scheme: light dark; }
  body { margin: 0; min-height: 100vh; display: flex; align-items: center; justify-content: center;
         font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", "Microsoft YaHei", sans-serif;
         background: #f5f6f8; color: #1f2329; }
  .card { background: #fff; border-radius: 12px; padding: 28px 32px; box-shadow: 0 4px 24px rgba(0,0,0,.08);
          text-align: center; width: 320px; }
  h1 { font-size: 17px; margin: 0 0 4px; font-weight: 600; }
  .sub { font-size: 13px; color: #8a9099; margin-bottom: 18px; }
  .qr { width: 220px; height: 220px; display: block; margin: 0 auto; border: 1px solid #eceef1; border-radius: 8px; }
  .status { margin-top: 16px; font-size: 13px; color: #4e5969; min-height: 20px; }
  .status[data-tone="ok"] { color: #0f9d58; font-weight: 600; }
  .status[data-tone="err"] { color: #d93026; }
  .form { display: none; margin-top: 18px; text-align: left; }
  .form[data-show="1"] { display: block; }
  .form label { display: block; font-size: 12px; color: #8a9099; margin-bottom: 4px; }
  input { width: 100%; box-sizing: border-box; padding: 8px 10px; font-size: 14px; margin-bottom: 10px;
          border: 1px solid #d9dde3; border-radius: 6px; }
  button { width: 100%; padding: 9px; font-size: 14px; border: 0; border-radius: 6px;
           background: #07c160; color: #fff; cursor: pointer; }
  button:disabled { background: #c9cdd4; cursor: not-allowed; }
  .row { display: flex; gap: 8px; }
  .row input { margin-bottom: 10px; }
  .row button { width: auto; white-space: nowrap; padding: 8px 12px; }
</style>
</head>
<body>
  <div class="card">
    <h1>使用微信扫码登录 Loomy</h1>
    <div class="sub">打开微信扫一扫，扫描下方二维码</div>
    <img class="qr" id="qr" alt="微信登录二维码" src="__LOOMY_QR_DATA_URL__">
    <div class="status" id="status">等待扫码…</div>

    <div class="form" id="phoneForm">
      <label for="phone">首次使用需绑定手机号</label>
      <div class="row">
        <input id="phone" type="tel" inputmode="numeric" maxlength="11" placeholder="手机号">
        <button id="sendBtn" type="button">获取验证码</button>
      </div>
      <input id="code" type="text" inputmode="numeric" maxlength="6" placeholder="短信验证码">
      <button id="verifyBtn" type="button">完成绑定并登录</button>
    </div>
  </div>

<script>
(function () {
  var statusEl = document.getElementById('status');
  var formEl = document.getElementById('phoneForm');
  var phoneEl = document.getElementById('phone');
  var codeEl = document.getElementById('code');
  var sendBtn = document.getElementById('sendBtn');
  var verifyBtn = document.getElementById('verifyBtn');
  var stopped = false;

  function setStatus(text, tone) {
    statusEl.textContent = text;
    if (tone) statusEl.setAttribute('data-tone', tone);
    else statusEl.removeAttribute('data-tone');
  }

  function post(payload) {
    return fetch('__LOOMY_COMPLETE_PATH__', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload),
    }).then(function (r) { return r.json(); });
  }

  function poll() {
    if (stopped) return;
    fetch('__LOOMY_POLL_PATH__')
      .then(function (r) { return r.json(); })
      .then(function (res) {
        if (res.status === 'done') {
          stopped = true;
          setStatus(res.message || '登录成功，可以关闭此窗口了', 'ok');
          formEl.setAttribute('data-show', '0');
          return;
        }
        if (res.status === 'need_phone') {
          stopped = true;
          setStatus('已扫码，请绑定手机号');
          formEl.setAttribute('data-show', '1');
          return;
        }
        if (res.status === 'expired') {
          stopped = true;
          setStatus('二维码已失效，请关闭窗口后重试', 'err');
          return;
        }
        if (res.status === 'cancelled') {
          stopped = true;
          setStatus('你已取消授权，请关闭窗口后重试', 'err');
          return;
        }
        if (res.status === 'error') {
          setStatus(res.message || '微信授权失败', 'err');
          stopped = true;
          return;
        }
        if (res.status === 'scanned') setStatus('已扫码，请在手机上确认');
        else setStatus('等待扫码…');
        setTimeout(poll, 1200);
      })
      .catch(function () {
        setTimeout(poll, 2000);
      });
  }

  sendBtn.addEventListener('click', function () {
    var phone = (phoneEl.value || '').replace(/\D/g, '');
    if (phone.length !== 11) { setStatus('请输入 11 位手机号', 'err'); return; }
    sendBtn.disabled = true;
    setStatus('正在发送验证码…');
    post({ action: 'send_sms', phone: phone })
      .then(function (res) {
        if (res.ok) setStatus('验证码已发送至 ' + phone);
        else setStatus(res.message || '发送失败', 'err');
      })
      .catch(function () { setStatus('发送失败，请重试', 'err'); })
      .finally(function () { sendBtn.disabled = false; });
  });

  verifyBtn.addEventListener('click', function () {
    var phone = (phoneEl.value || '').replace(/\D/g, '');
    var code = (codeEl.value || '').replace(/\D/g, '');
    if (phone.length !== 11) { setStatus('请输入 11 位手机号', 'err'); return; }
    if (code.length === 0) { setStatus('请输入短信验证码', 'err'); return; }
    verifyBtn.disabled = true;
    setStatus('正在完成绑定…');
    post({ action: 'verify_sms', phone: phone, code: code })
      .then(function (res) {
        if (res.ok && res.done) setStatus('登录成功，可以关闭此窗口了', 'ok');
        else setStatus(res.message || '验证码错误，请重试', 'err');
      })
      .catch(function () { setStatus('绑定失败，请重试', 'err'); })
      .finally(function () { verifyBtn.disabled = false; });
  });

  poll();
})();
</script>
</body>
</html>`
