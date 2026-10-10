package channel

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"time"
)

// callbackDefaultTimeout 是回调登录的整体超时兜底：用户可能在登录页停留很久，
// 但不能让一个本地端口被无限占用。
const callbackDefaultTimeout = 10 * time.Minute

// callbackParams 是回调 URL 上的查询参数。
type callbackParams struct {
	Code             string
	State            string
	Error            string
	ErrorDescription string
}

// callbackReply 是业务方回给浏览器的结果。html 非空时按 HTML 渲染，否则用
// status + message 走 text/plain。
type callbackReply struct {
	status  int
	message string
	html    string
}

// callbackRequest 是回调服务器交给业务方的一次授权回调事件。
//
// 业务方**必须**在 reply 上回一个结果（浏览器在等）：回 2xx 表示这次回调已被
// 消费，回 4xx/5xx 表示这次不够（例如换码失败），会话继续等下一次回调，用户
// 刷新页面即可重试。
type callbackRequest struct {
	params callbackParams
	ctx    context.Context
	reply  chan callbackReply
}

// callbackServer 是回调登录共用的本地回环 HTTP 监听器。
//
// lobster 与 gemini 的登录都是「授权码 + 回环回调」而不是设备码，因此不能用
// deviceLogin 的轮询模型；这里起一个监听 127.0.0.1:0 的 HTTP 服务，把浏览器带
// 回来的 ?code=&state= 交给业务方，对上层仍然暴露成 LoginSession（登录地址放
// 在 DeviceCode.VerificationURIComplete 里，Admin API 原样返回 login_url）。
type callbackServer struct {
	path  string
	host  string
	state string
	ln    net.Listener
	srv   *http.Server

	events    chan callbackRequest
	done      chan struct{}
	closeOnce sync.Once
}

// newCallbackServer 起一个回环回调服务器。path 是回调路径（如 /auth/callback）。
func newCallbackServer(path string) (*callbackServer, error) {
	return newCallbackServerOnHost(path, "127.0.0.1")
}

// newCallbackServerOnHost 同上，但可指定回环主机名。Google OAuth 会逐字比对
// redirect_uri，其桌面流程要求主机是 localhost（不是 127.0.0.1），故 gemini 用这个。
func newCallbackServerOnHost(path, host string) (*callbackServer, error) {
	state, err := randomHex(16)
	if err != nil {
		return nil, err
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("监听本地回调端口失败: %w", err)
	}
	s := &callbackServer{
		path:   path,
		host:   host,
		state:  state,
		ln:     ln,
		events: make(chan callbackRequest),
		done:   make(chan struct{}),
	}
	s.srv = &http.Server{Handler: http.HandlerFunc(s.handle)}
	go func() { _ = s.srv.Serve(ln) }()
	return s, nil
}

func (s *callbackServer) port() int { return s.ln.Addr().(*net.TCPAddr).Port }

// stateValue 返回本次登录的防伪 state，业务方需把它拼进登录地址。
func (s *callbackServer) stateValue() string { return s.state }

// callbackURL 是上游登录页需要回调的完整地址（回环，不带 state）。
func (s *callbackServer) callbackURL() string {
	return fmt.Sprintf("http://%s:%d%s", s.host, s.port(), s.path)
}

func (s *callbackServer) handle(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != s.path {
		http.NotFound(w, r)
		return
	}
	q := r.URL.Query()
	params := callbackParams{
		Code:             q.Get("code"),
		State:            q.Get("state"),
		Error:            q.Get("error"),
		ErrorDescription: q.Get("error_description"),
	}
	// state 不匹配基本只有两种成因：伪造回调，或用户开的是上一次登录的旧页面。
	// 两种情况都不该让会话拿到这个 code。
	if params.Error == "" && subtle.ConstantTimeCompare([]byte(params.State), []byte(s.state)) != 1 {
		http.Error(w, "回调 state 校验失败，请回到网关重新发起登录", http.StatusBadRequest)
		return
	}

	req := callbackRequest{params: params, ctx: r.Context(), reply: make(chan callbackReply, 1)}
	select {
	case s.events <- req:
	case <-s.done:
		http.Error(w, "登录会话已结束", http.StatusGone)
		return
	}

	select {
	case reply := <-req.reply:
		if reply.html != "" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(reply.status)
			_, _ = io.WriteString(w, reply.html)
			return
		}
		http.Error(w, reply.message, reply.status)
	case <-s.done:
		// 会话已被收起（超时/取消），给浏览器一个明确的收尾页面。
		http.Error(w, "登录会话已结束", http.StatusGone)
	}
}

// next 等待下一次回调事件。ctx 结束时返回 ctx 的错误。
func (s *callbackServer) next(ctx context.Context) (callbackRequest, error) {
	select {
	case req := <-s.events:
		return req, nil
	case <-ctx.Done():
		return callbackRequest{}, ctx.Err()
	}
}

// close 关闭监听并让在等待的业务方与浏览器都收到收尾信号。可重复调用。
func (s *callbackServer) close() {
	s.closeOnce.Do(func() {
		close(s.done)
		shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = s.srv.Shutdown(shutdownCtx)
	})
}

// randomHex 生成 n 字节随机数的十六进制表示（2n 个字符）。
func randomHex(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("生成随机数失败: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

// mustRandomHex 同 randomHex，仅在错误理论上不可能发生时使用；失败时退化成
// 时间戳派生值，保证登录流程不因随机源故障而整体不可用。
func mustRandomHex(n int) string {
	if value, err := randomHex(n); err == nil {
		return value
	}
	return fmt.Sprintf("%0*x", n*2, time.Now().UnixNano())
}
