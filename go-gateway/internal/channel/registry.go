package channel

import (
	"context"
	"errors"
	"sync"
)

// ErrUnknownChannel 表示请求的渠道未启用或未注册。
var ErrUnknownChannel = errors.New("unknown channel")

// LoginSession 是一次进行中的设备码登录：先拿到展示信息，再等待授权完成。
type LoginSession interface {
	Channel() string
	// DeviceCode 返回展示给用户的信息与验证地址。
	DeviceCode() DeviceCode
	// Wait 阻塞直到用户完成/拒绝/超时，返回新写入账号池的凭据。
	Wait(ctx context.Context) (*Credential, error)
}

// Channel 是一个账号渠道的统一视图：既能为 provider 提供令牌源，也能响应登录管理。
type Channel interface {
	// Name 是渠道标识（与 providers.yaml 的 provider 名一致）。
	Name() string
	// Accounts 返回账号池（同时是 provider 的 AuthSource）。
	Accounts() *Accounts
	// BeginLogin 发起设备码登录，返回可轮询的会话。
	BeginLogin(ctx context.Context) (LoginSession, error)
}

// Registry 汇总已启用的账号渠道，供 main.go 装配 provider、供 Admin API 管理登录。
type Registry struct {
	mu       sync.RWMutex
	channels map[string]Channel
	order    []string
}

// NewRegistry 创建空注册表。
func NewRegistry() *Registry {
	return &Registry{channels: make(map[string]Channel)}
}

// Register 登记一个渠道；同名覆盖但保留顺序。
func (r *Registry) Register(c Channel) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.channels[c.Name()]; !exists {
		r.order = append(r.order, c.Name())
	}
	r.channels[c.Name()] = c
}

// Get 按名字取渠道。
func (r *Registry) Get(name string) (Channel, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.channels[name]
	return c, ok
}

// Names 返回已注册渠道名（登记顺序）。
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]string(nil), r.order...)
}

// Len 返回渠道数量。
func (r *Registry) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.channels)
}
