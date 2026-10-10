package provider

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
)

// postWithAuth 发送一次 POST 并注入鉴权头；上游返回 401/403 且配置了 AuthSource
// 时，先用它续期一次再重试，让 OAuth token 过期不影响本次请求。
//
// 返回值语义：成功或非鉴权失败时返回响应，body 未读取，由调用方 Close；
// 未登录或续期失败时返回 *ProviderError，此时上游 body 已被读掉并关闭，
// 不会泄漏连接。
func postWithAuth(ctx context.Context, client *http.Client, name, url string, cfg *Config, setHeaders func(*http.Request), body []byte) (*http.Response, error) {
	if cfg.Auth != nil && !cfg.authAvailable() {
		return nil, &ProviderError{
			Provider:   name,
			StatusCode: http.StatusUnauthorized,
			Message:    "channel has no credential; login required",
		}
	}
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("create request: %w", err)
		}
		setHeaders(req)
		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("send request: %w", err)
		}
		if attempt == 0 && cfg.refreshAuth(ctx, resp.StatusCode) {
			drainAndClose(resp.Body)
			continue
		}
		return resp, nil
	}
}

// applyTransform 对请求体做渠道自定义改写。未配置 Transform 时原样返回。
func (c *Config) applyTransform(body []byte) ([]byte, error) {
	if c.Transform == nil {
		return body, nil
	}
	return c.Transform(body)
}

// Available 报告 provider 当前是否具备发起请求的条件。路由层借此跳过未登录的
// 渠道 provider，避免每次请求都先撞一次「凭据缺失」再降级。
func (p *AnthropicProvider) Available() bool { return p.config.authAvailable() }

// Available 见 AnthropicProvider.Available。
func (p *OpenAIProvider) Available() bool { return p.config.authAvailable() }

func drainAndClose(body io.ReadCloser) {
	if body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(body, 1<<16))
	_ = body.Close()
}
