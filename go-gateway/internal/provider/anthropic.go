package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// AnthropicProvider 实现 Anthropic API 兼容的提供商
type AnthropicProvider struct {
	config *Config
	client *http.Client
}

// NewAnthropicProvider 创建新的提供商实例
func NewAnthropicProvider(config *Config) *AnthropicProvider {
	return &AnthropicProvider{
		config: config,
		client: &http.Client{Timeout: 120 * time.Second},
	}
}

func (p *AnthropicProvider) Name() string    { return p.config.Name }
func (p *AnthropicProvider) URL() string     { return p.config.URL }
func (p *AnthropicProvider) APIKey() string  { return p.config.APIKey }
func (p *AnthropicProvider) UseBearer() bool { return p.config.UseBearer }

// ForwardStream 转发流式请求到提供商（Anthropic SSE 直接透传）
func (p *AnthropicProvider) ForwardStream(ctx context.Context, req *Request, w io.Writer) error {
	req.Stream = true
	_ = req.SetRawField("stream", true)

	reqBody, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal stream request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.config.URL, bytes.NewReader(reqBody))
	if err != nil {
		return fmt.Errorf("create stream request: %w", err)
	}
	p.setHeaders(httpReq)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("send stream request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return NewHTTPError(p.Name(), resp, respBody)
	}

	_, err = io.Copy(w, resp.Body)
	return err
}

// ForwardRequest 转发请求到提供商（非流式）
func (p *AnthropicProvider) ForwardRequest(ctx context.Context, req *Request) (*Response, error) {
	reqBody, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.config.URL, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	p.setHeaders(httpReq)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, NewHTTPError(p.Name(), resp, respBody)
	}

	var response Response
	if err := json.Unmarshal(respBody, &response); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	return &response, nil
}

// IsHealthy 检查提供商是否健康
func (p *AnthropicProvider) IsHealthy(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodHead, p.config.URL, nil)
	if err != nil {
		return false
	}
	p.setHeaders(req)

	resp, err := p.client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}

// setHeaders 设置公共请求头
func (p *AnthropicProvider) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "go-llm-gateway/1.0")
	req.Header.Set("anthropic-version", "2023-06-01")
	if p.config.UseBearer {
		req.Header.Set("Authorization", "Bearer "+p.config.APIKey)
	} else {
		req.Header.Set("x-api-key", p.config.APIKey)
	}
}
