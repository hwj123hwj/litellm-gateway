package provider

import (
	"context"
	"fmt"
	"log"
	"sync"
)

// Router 管理提供商路由和 fallback
type Router struct {
	providers map[string]Provider
	chains    map[string][]string // 模型名 -> 提供商名列表
	mu        sync.RWMutex
	logger    *log.Logger
}

// NewRouter 创建新的路由器
func NewRouter(logger *log.Logger) *Router {
	return &Router{
		providers: make(map[string]Provider),
		chains:    make(map[string][]string),
		logger:    logger,
	}
}

// RegisterProvider 注册提供商
func (r *Router) RegisterProvider(name string, p Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers[name] = p
	r.logger.Printf("Registered provider: %s", name)
}

// RegisterChain 注册 fallback 链
func (r *Router) RegisterChain(modelName string, providers []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.chains[modelName] = providers
	r.logger.Printf("Registered chain: %s -> %v", modelName, providers)
}

// Route 返回模型对应的提供商链
func (r *Router) Route(modelName string) ([]Provider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	providerNames, ok := r.chains[modelName]
	if !ok {
		return nil, fmt.Errorf("unknown model: %s", modelName)
	}

	var result []Provider
	for _, name := range providerNames {
		p, ok := r.providers[name]
		if !ok {
			return nil, fmt.Errorf("provider not found: %s", name)
		}
		result = append(result, p)
	}
	return result, nil
}

// RouteForStream 返回流式请求的提供商链（功能同 Route，供 handler 使用）
func (r *Router) RouteForStream(modelName string) ([]Provider, error) {
	return r.Route(modelName)
}

// Forward 转发请求，带 fallback 逻辑（非流式）
func (r *Router) Forward(ctx context.Context, modelName string, req *Request) (*Response, error) {
	providers, err := r.Route(modelName)
	if err != nil {
		return nil, err
	}

	originalModel := req.Model
	var lastErr error
	for i, p := range providers {
		r.logger.Printf("Attempting provider %d/%d: %s", i+1, len(providers), p.Name())
		if bmp, ok := p.(BoundModelProvider); ok {
			req.Model = bmp.BoundModel()
		} else {
			req.Model = r.mapModelName(modelName, p.Name())
		}

		resp, err := p.ForwardRequest(ctx, req)
		req.Model = originalModel

		if err != nil {
			r.logger.Printf("Provider %s failed: %v", p.Name(), err)
			lastErr = err
			continue
		}
		r.logger.Printf("Provider %s succeeded", p.Name())
		return resp, nil
	}
	return nil, fmt.Errorf("all providers failed, last error: %w", lastErr)
}

	// mapModelName 将通用模型名映射到具体提供商的实际模型名（对齐 config.yaml）
	func (r *Router) mapModelName(modelName, providerName string) string {
		mappings := map[string]map[string]string{
			"coding": {
				"glm":      "glm-5-turbo",
				"mimo":     "mimo-v2.5",
				"longcat":  "LongCat-Flash-Chat",
				"easyclaw": "claude-sonnet-4-6",
			},
			"glm-haiku":  {"glm": "glm-4.7"},
			"glm-sonnet": {"glm": "glm-5-turbo"},
			"glm-opus":   {"glm": "glm-5.1"},
			"mimo-haiku":  {"mimo": "mimo-v2.5"},
			"mimo-sonnet": {"mimo": "mimo-v2.5"},
			"mimo-opus":   {"mimo": "mimo-v2.5-pro"},
			"longcat-sonnet": {"longcat": "LongCat-Flash-Chat"},
			"longcat-opus":   {"longcat": "LongCat-2.0-Preview"},
			"easyclaw-sonnet": {"easyclaw": "claude-sonnet-4-6"},
			"easyclaw-opus":   {"easyclaw": "claude-opus-4-6"},
			// DeepV Server 模型
			"deepseek-flash": {"deepv-deepseek": "deepseek-v4-flash"},
			"glm-5":          {"deepv-glm5": "glm-5"},
			"claude-sonnet-4-6": {"deepv-claude": "claude-sonnet-4-6"},
			// 免费/极低成本模型
			"free":      {"glm-free": "glm-4.7-flash"},
			"glm-flash": {"glm-free": "glm-4.7-flash"},
		}

	if mapping, ok := mappings[modelName]; ok {
		if actualModel, ok := mapping[providerName]; ok {
			return actualModel
		}
	}
	return modelName
}

// MapModel 是 mapModelName 的公开版本，供 handler 使用
func (r *Router) MapModel(modelName, providerName string) string {
	return r.mapModelName(modelName, providerName)
}

// ListProviders 列出所有提供商名称
func (r *Router) ListProviders() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var names []string
	for name := range r.providers {
		names = append(names, name)
	}
	return names
}

// ListChains 列出所有已注册的 chain 名称（即可用模型名）
func (r *Router) ListChains() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.chains))
	for name := range r.chains {
		names = append(names, name)
	}
	return names
}
