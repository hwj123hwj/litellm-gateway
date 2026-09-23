package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// AnthropicProvider 实现 Anthropic API 兼容的提供商
type AnthropicProvider struct {
	config *Config
	client *http.Client
}

// NewAnthropicProvider 创建新的提供商实例
func NewAnthropicProvider(config *Config) *AnthropicProvider {
	// 不用 http.Client.Timeout：它会把整个流式 body 透传计入总时长，长生成会被
	// 砍断。改为 ResponseHeaderTimeout 限制首字节，流式停滞由看门狗中止
	requestTimeout := config.RequestTimeout
	if requestTimeout <= 0 {
		requestTimeout = defaultRequestTimeout
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = requestTimeout
	return &AnthropicProvider{
		config: config,
		client: &http.Client{Transport: transport},
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

	// 派生可取消 context：空闲看门狗超时触发 cancel，中止停滞的上游流
	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(streamCtx, http.MethodPost, p.config.URL, bytes.NewReader(reqBody))
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

	idleTimeout := p.config.RequestTimeout
	if idleTimeout <= 0 {
		idleTimeout = defaultRequestTimeout
	}
	kick, idleTripped := startStreamIdleWatchdog(streamCtx, idleTimeout, cancel)

	// 透传没有行循环，包装 reader 在每次 Read 时喂看门狗
	_, err = io.Copy(w, kickingReader{R: resp.Body, Kick: kick})
	if err != nil && idleTripped() {
		return fmt.Errorf("upstream stream idle over %v: no data received", idleTimeout)
	}
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

// IsHealthy 只反映本地配置，不代表上游可用；上游可用性请用 Probe。
func (p *AnthropicProvider) IsHealthy(ctx context.Context) bool {
	return true
}

// Probe 发一个最小请求验证鉴权与模型可用性。
func (p *AnthropicProvider) Probe(ctx context.Context) ProbeResult {
	return p.ProbeModel(ctx, p.config.Name)
}

// ProbeModel 用指定模型探测。Anthropic 协议要求 max_tokens 必填，给一个最小值。
func (p *AnthropicProvider) ProbeModel(ctx context.Context, model string) ProbeResult {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	return probeVia(ctx, model, 16, p.ForwardRequest)
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
