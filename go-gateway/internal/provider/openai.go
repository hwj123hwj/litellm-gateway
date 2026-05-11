package provider

// OpenAI provider：把 Anthropic 格式请求转成 OpenAI 格式发出，再把响应转回 Anthropic 格式。
// 用于对接 EasyClaw 等只支持 /v1/chat/completions 的中转服务。

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ─── OpenAI 请求/响应结构 ──────────────────────────────────────────────────────

type openAIToolFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type openAIToolCall struct {
	ID       string             `json:"id"`
	Type     string             `json:"type"`
	Function openAIToolFunction `json:"function"`
}

type openAIMessage struct {
	Role      string           `json:"role"`
	Content   string           `json:"content"`
	ToolCalls []openAIToolCall `json:"tool_calls,omitempty"`
}

type openAIRequest struct {
	Model       string           `json:"model"`
	Messages    []openAIMessage  `json:"messages"`
	MaxTokens   int              `json:"max_tokens,omitempty"`
	Temperature float64          `json:"temperature,omitempty"`
	TopP        float64          `json:"top_p,omitempty"`
	Stream      bool             `json:"stream,omitempty"`
	Tools       []openAITool     `json:"tools,omitempty"`
}

// openAITool 是 OpenAI 格式的工具定义
type openAITool struct {
	Type     string           `json:"type"` // "function"
	Function openAIToolSchema `json:"function"`
}

type openAIToolSchema struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

type openAIChoice struct {
	Index        int           `json:"index"`
	Message      openAIMessage `json:"message"`
	FinishReason string        `json:"finish_reason"`
}

type openAIUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type openAIResponse struct {
	ID      string         `json:"id"`
	Object  string         `json:"object"`
	Model   string         `json:"model"`
	Choices []openAIChoice `json:"choices"`
	Usage   openAIUsage    `json:"usage"`
}

// ─── OpenAI 流式 SSE 结构 ──────────────────────────────────────────────────────

type openAIStreamDelta struct {
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`
}

type openAIStreamChoice struct {
	Index        int               `json:"index"`
	Delta        openAIStreamDelta `json:"delta"`
	FinishReason *string           `json:"finish_reason"`
}

type openAIStreamChunk struct {
	ID      string               `json:"id"`
	Object  string               `json:"object"`
	Model   string               `json:"model"`
	Choices []openAIStreamChoice `json:"choices"`
}

// ─── OpenAIProvider ──────────────────────────────────────────────────────────

type OpenAIProvider struct {
	config *Config
	client *http.Client
}

func NewOpenAIProvider(config *Config) *OpenAIProvider {
	return &OpenAIProvider{
		config: config,
		client: &http.Client{Timeout: 120 * time.Second},
	}
}

func (p *OpenAIProvider) Name() string    { return p.config.Name }
func (p *OpenAIProvider) URL() string     { return p.config.URL }
func (p *OpenAIProvider) APIKey() string  { return p.config.APIKey }
func (p *OpenAIProvider) UseBearer() bool { return true }

// ForwardRequest 将 Anthropic 格式请求转为 OpenAI 格式，发出后将响应转回 Anthropic 格式
func (p *OpenAIProvider) ForwardRequest(ctx context.Context, req *Request) (*Response, error) {
	oaiReq := toOpenAIRequest(req)

	body, err := json.Marshal(oaiReq)
	if err != nil {
		return nil, fmt.Errorf("marshal openai request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.config.URL, bytes.NewReader(body))
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
		return nil, fmt.Errorf("openai provider error %d: %s", resp.StatusCode, string(respBody))
	}

	var oaiResp openAIResponse
	if err := json.Unmarshal(respBody, &oaiResp); err != nil {
		return nil, fmt.Errorf("parse openai response: %w", err)
	}

	return fromOpenAIResponse(&oaiResp), nil
}

// ForwardStream 将 Anthropic 格式请求转为 OpenAI 流式请求，把 OpenAI SSE 转为 Anthropic SSE 写入 w
func (p *OpenAIProvider) ForwardStream(ctx context.Context, req *Request, w io.Writer) error {
	streamReq := toOpenAIRequest(req)
	streamReq.Stream = true

	body, err := json.Marshal(streamReq)
	if err != nil {
		return fmt.Errorf("marshal openai stream request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.config.URL, bytes.NewReader(body))
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
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("openai stream error %d: %s", resp.StatusCode, string(b))
	}

	// 发送 Anthropic 流式开场事件
	msgID := "msg_oai_" + time.Now().Format("20060102150405")
	writeSSE(w, "message_start", fmt.Sprintf(
		`{"type":"message_start","message":{"id":%q,"type":"message","role":"assistant","model":%q,"content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":0,"output_tokens":0}}}`,
		msgID, req.Model,
	))
	writeSSE(w, "ping", `{"type":"ping"}`)
	writeSSE(w, "content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`)

	// 逐行读取 OpenAI SSE，转换成 Anthropic SSE
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")
		if payload == "[DONE]" {
			break
		}

		var chunk openAIStreamChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			continue
		}
		if len(chunk.Choices) == 0 {
			continue
		}

		delta := chunk.Choices[0].Delta
		if delta.Content != "" {
			deltaJSON := fmt.Sprintf(
				`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":%s}}`,
				jsonString(delta.Content),
			)
			writeSSE(w, "content_block_delta", deltaJSON)
		}

		// 收到 finish_reason 表示结束
		if chunk.Choices[0].FinishReason != nil {
			stopReason := mapFinishReason(*chunk.Choices[0].FinishReason)
			writeSSE(w, "content_block_stop", `{"type":"content_block_stop","index":0}`)
			writeSSE(w, "message_delta", fmt.Sprintf(
				`{"type":"message_delta","delta":{"stop_reason":%q,"stop_sequence":null},"usage":{"output_tokens":0}}`,
				stopReason,
			))
			writeSSE(w, "message_stop", `{"type":"message_stop"}`)
		}
	}

	return scanner.Err()
}

func (p *OpenAIProvider) IsHealthy(ctx context.Context) bool {
	return true
}

func (p *OpenAIProvider) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.config.APIKey)
	req.Header.Set("User-Agent", "go-llm-gateway/1.0")
}

// ─── 格式转换辅助函数 ──────────────────────────────────────────────────────────

func toOpenAIRequest(req *Request) *openAIRequest {
	msgs := make([]openAIMessage, len(req.Messages))
	for i, m := range req.Messages {
		msgs[i] = openAIMessage{Role: m.Role, Content: m.Content.String()}
	}

	oaiReq := &openAIRequest{
		Model:     req.Model,
		Messages:  msgs,
		MaxTokens: req.MaxTokens,
	}

	// 转换 tools：Anthropic input_schema → OpenAI parameters
	if toolsRaw, ok := req.raw["tools"]; ok {
		var anthropicTools []struct {
			Name        string          `json:"name"`
			Description string          `json:"description"`
			InputSchema json.RawMessage `json:"input_schema"`
		}
		if err := json.Unmarshal(toolsRaw, &anthropicTools); err == nil {
			for _, t := range anthropicTools {
				oaiReq.Tools = append(oaiReq.Tools, openAITool{
					Type: "function",
					Function: openAIToolSchema{
						Name:        t.Name,
						Description: t.Description,
						Parameters:  t.InputSchema,
					},
				})
			}
		}
	}

	return oaiReq
}

func fromOpenAIResponse(oai *openAIResponse) *Response {
	var blocks []ContentBlock
	var stopReason string

	if len(oai.Choices) > 0 {
		choice := oai.Choices[0]
		stopReason = mapFinishReason(choice.FinishReason)

		// 文本内容
		if choice.Message.Content != "" {
			blocks = append(blocks, ContentBlock{Type: "text", Text: choice.Message.Content})
		}

		// 工具调用：OpenAI tool_calls → Anthropic tool_use
		for _, tc := range choice.Message.ToolCalls {
			var input json.RawMessage
			if err := json.Unmarshal([]byte(tc.Function.Arguments), &input); err != nil {
				// arguments 不是合法 JSON，包成字符串
				input, _ = json.Marshal(tc.Function.Arguments)
			}
			blocks = append(blocks, ContentBlock{
				Type:  "tool_use",
				ID:    tc.ID,
				Name:  tc.Function.Name,
				Input: input,
			})
		}
	}

	if len(blocks) == 0 {
		blocks = []ContentBlock{{Type: "text", Text: ""}}
	}

	resp := &Response{
		ID:         oai.ID,
		Type:       "message",
		Role:       "assistant",
		Model:      oai.Model,
		StopReason: stopReason,
		Content:    blocks,
	}
	resp.Usage.InputTokens = oai.Usage.PromptTokens
	resp.Usage.OutputTokens = oai.Usage.CompletionTokens
	return resp
}

// mapFinishReason 把 OpenAI finish_reason 转成 Anthropic stop_reason
func mapFinishReason(reason string) string {
	switch reason {
	case "stop":
		return "end_turn"
	case "length":
		return "max_tokens"
	case "tool_calls":
		return "tool_use"
	default:
		return "end_turn"
	}
}

// writeSSE 写一条 SSE 事件
func writeSSE(w io.Writer, event, data string) {
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

// jsonString 把字符串转为 JSON 字符串字面量（含引号和转义）
func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
