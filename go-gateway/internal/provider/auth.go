package provider

import (
	"context"
	"net/http"
	"sync"
)

// AuthSource 为 provider 提供动态访问令牌，取代静态 API Key。
//
// 账号渠道（internal/channel）用它把「交互式登录 + 自动续期」得到的 OAuth token
// 注入每次上游请求。provider 仍走既有的协议转换，不感知渠道细节——渠道只需在
// 构造 Config 时塞一个 AuthSource 进来。
//
// 约定：实现必须并发安全，且 BearerToken 要便宜（允许每次请求调用）。
type AuthSource interface {
	// BearerToken 返回当前可用的访问令牌。空串表示尚未登录：provider 会据此
	// 判定自己不可用（见 authAvailable），路由层直接跳过，不必先撞一次 401。
	BearerToken() string
	// Refresh 在收到 401/403 后调用。返回 true 表示令牌已更新，调用方应重取
	// BearerToken 并重试一次；返回 false 表示续期失败（无 refresh_token、或被
	// 上游拒绝），错误按原样返回给下游走降级。
	Refresh(ctx context.Context) bool
}

// staticAuth 用固定令牌实现 AuthSource，供测试与简单场景使用。
type staticAuth struct {
	mu    sync.RWMutex
	token string
}

// NewStaticAuth 用固定令牌构造 AuthSource（主要供测试）。
func NewStaticAuth(token string) *staticAuth { return &staticAuth{token: token} }

func (a *staticAuth) BearerToken() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.token
}

// Refresh 对静态令牌无意义：没有续期来源，报告失败让上游错误原样返回。
func (a *staticAuth) Refresh(context.Context) bool { return false }

// RequestTransform 在请求体序列化后、发送前做最后一次改写。账号渠道用它复刻
// 上游客户端特有的字段形态（例如给强制思考的模型补 thinking），不改变 provider
// 的协议职责。
type RequestTransform func(body []byte) ([]byte, error)

// applyAuth 把鉴权与渠道附加头写入请求。有 AuthSource 时以它为准；未登录
// （空令牌）时不写 Authorization，并返回 false 供调用方提前短路。
//
// staticKey（providers.yaml 里的 api_key）仅在没有 AuthSource 时生效。
func (c *Config) applyAuth(req *http.Request, bearer bool) bool {
	if c.Auth != nil {
		token := c.Auth.BearerToken()
		if token == "" {
			return false
		}
		if bearer {
			req.Header.Set("Authorization", "Bearer "+token)
		} else {
			req.Header.Set("x-api-key", token)
		}
	} else if c.APIKey != "" {
		if bearer {
			req.Header.Set("Authorization", "Bearer "+c.APIKey)
		} else {
			req.Header.Set("x-api-key", c.APIKey)
		}
	}
	for name, value := range c.ExtraHeaders {
		if value != "" {
			req.Header.Set(name, value)
		}
	}
	return true
}

// refreshAuth 在 AuthSource 存在且上游返回鉴权失败时尝试续期一次。
// 返回 true 表示令牌已更新，调用方应重建请求重试。
func (c *Config) refreshAuth(ctx context.Context, status int) bool {
	if c.Auth == nil {
		return false
	}
	if status != http.StatusUnauthorized && status != http.StatusForbidden {
		return false
	}
	return c.Auth.Refresh(ctx)
}

// authAvailable 报告 provider 当前是否有可用凭据。无 AuthSource 时恒为 true
// （静态 api_key 的可用性不由这里判断）。
func (c *Config) authAvailable() bool {
	if c.Auth == nil {
		return true
	}
	return c.Auth.BearerToken() != ""
}

// availabilityProvider 是可选接口：provider 报告自己此刻能否发起请求。
// 账号渠道用它把「未登录」告诉路由层，让请求直接走链上下一个 provider，
// 而不是先发一次必然失败的请求。未实现该接口的 provider 视为始终可用。
type availabilityProvider interface {
	Available() bool
}

// providerAvailable 是路由层调用的判定入口，兼容三层包装。
func providerAvailable(p Provider) bool {
	if av, ok := p.(availabilityProvider); ok {
		return av.Available()
	}
	return true
}
