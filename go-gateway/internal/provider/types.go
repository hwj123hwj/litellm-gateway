package provider

import (
	"context"
	"io"
)

// Message 表示一条消息
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Request 是 Anthropic API 请求体
type Request struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	MaxTokens   int       `json:"max_tokens"`
	Temperature float64   `json:"temperature,omitempty"`
	TopP        float64   `json:"top_p,omitempty"`
	Stream      bool      `json:"stream,omitempty"`
}

// ContentBlock 是 Anthropic 响应中的内容块（数组元素）
type ContentBlock struct {
	Type string `json:"type"` // "text"
	Text string `json:"text"`
}

// Response 是 Anthropic API 响应体（对齐真实 Anthropic 格式）
type Response struct {
	ID           string         `json:"id"`
	Type         string         `json:"type"`     // "message"
	Role         string         `json:"role"`     // "assistant"
	Model        string         `json:"model"`
	Content      []ContentBlock `json:"content"` // 数组，非对象
	StopReason   string         `json:"stop_reason"`
	StopSequence *string        `json:"stop_sequence"`
	Usage        struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

// ErrorResponse 错误响应结构
type ErrorResponse struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    string `json:"code,omitempty"`
	} `json:"error"`
}

// Provider 定义提供商的接口
type Provider interface {
	Name() string
	URL() string
	APIKey() string
	UseBearer() bool
	ForwardRequest(ctx context.Context, req *Request) (*Response, error)
	IsHealthy(ctx context.Context) bool
}

// StreamProvider 是可选接口：提供商自行实现流式逻辑（例如需要格式转换的 OpenAI 兼容接口）
// 若提供商未实现此接口，handler 将直接透传上游 SSE 流。
type StreamProvider interface {
	Provider
	ForwardStream(ctx context.Context, req *Request, w io.Writer) error
}

// Config 提供商配置
type Config struct {
	Name      string
	URL       string
	APIKey    string
	UseBearer bool
}
