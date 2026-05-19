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
	Role       string           `json:"role"`
	Content    string           `json:"content"`
	ToolCalls  []openAIToolCall `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"` // for tool result messages
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

// openAIStreamToolCall 是流式 tool_calls 中的单个增量片段
type openAIStreamToolCall struct {
	Index    int                `json:"index"`
	ID       string             `json:"id"`
	Type     string             `json:"type"`
	Function openAIToolFunction `json:"function"`
}

type openAIStreamDelta struct {
	Role      string                 `json:"role,omitempty"`
	Content   string                 `json:"content,omitempty"`
	ToolCalls []openAIStreamToolCall `json:"tool_calls,omitempty"`
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

	msgID := "msg_oai_" + time.Now().Format("20060102150405")

	// 用于拼装流式 tool_calls（key = index）
	type toolCallAccum struct {
		id        string
		name      string
		arguments strings.Builder
	}
	toolCalls := map[int]*toolCallAccum{}

	// 文本内容 block 是否已开始
	textBlockStarted := false
	// tool_use block 的当前 Anthropic index（文本占 0，每个工具往后排）
	toolBlockIndex := 1

	writeSSE(w, "message_start", fmt.Sprintf(
		`{"type":"message_start","message":{"id":%q,"type":"message","role":"assistant","model":%q,"content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":0,"output_tokens":0}}}`,
		msgID, req.Model,
	))
	writeSSE(w, "ping", `{"type":"ping"}`)

	scanner := bufio.NewScanner(resp.Body)
	// 增大 scanner buffer，防止超长行截断
	scanner.Buffer(make([]byte, 256*1024), 256*1024)

	finishReason := ""

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

		choice := chunk.Choices[0]
		delta := choice.Delta

		// --- 文本内容 ---
		if delta.Content != "" {
			if !textBlockStarted {
				textBlockStarted = true
				writeSSE(w, "content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`)
			}
			writeSSE(w, "content_block_delta", fmt.Sprintf(
				`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":%s}}`,
				jsonString(delta.Content),
			))
		}

		// --- 工具调用（增量拼装）---
		for _, tc := range delta.ToolCalls {
			idx := tc.Index
			if _, ok := toolCalls[idx]; !ok {
				toolCalls[idx] = &toolCallAccum{}
			}
			acc := toolCalls[idx]
			if tc.ID != "" {
				acc.id = tc.ID
			}
			if tc.Function.Name != "" {
				acc.name = tc.Function.Name
			}
			acc.arguments.WriteString(tc.Function.Arguments)
		}

		// --- finish_reason ---
		if choice.FinishReason != nil && *choice.FinishReason != "" {
			finishReason = *choice.FinishReason
		}
	}

	if err := scanner.Err(); err != nil {
		return err
	}

	// 关闭文本 block
	if textBlockStarted {
		writeSSE(w, "content_block_stop", `{"type":"content_block_stop","index":0}`)
	}

	// 输出拼装完整的 tool_use blocks（按 index 顺序）
	for i := 0; i < len(toolCalls); i++ {
		acc, ok := toolCalls[i]
		if !ok {
			continue
		}
		blockIdx := toolBlockIndex
		if !textBlockStarted {
			// 没有文本块时，tool_use 从 index 0 开始
			blockIdx = i
		} else {
			blockIdx = 1 + i
		}

		// 把 arguments 字符串解析为 JSON object（确保合法）
		var inputRaw json.RawMessage
		if err := json.Unmarshal([]byte(acc.arguments.String()), &inputRaw); err != nil {
			inputRaw = json.RawMessage(`{}`)
		}
		inputJSON, _ := json.Marshal(inputRaw)

		writeSSE(w, "content_block_start", fmt.Sprintf(
			`{"type":"content_block_start","index":%d,"content_block":{"type":"tool_use","id":%s,"name":%s,"input":{}}}`,
			blockIdx, jsonString(acc.id), jsonString(acc.name),
		))
		writeSSE(w, "content_block_delta", fmt.Sprintf(
			`{"type":"content_block_delta","index":%d,"delta":{"type":"input_json_delta","partial_json":%s}}`,
			blockIdx, jsonString(string(inputJSON)),
		))
		writeSSE(w, "content_block_stop", fmt.Sprintf(
			`{"type":"content_block_stop","index":%d}`, blockIdx,
		))
		_ = toolBlockIndex
	}

	stopReason := mapFinishReason(finishReason)
	writeSSE(w, "message_delta", fmt.Sprintf(
		`{"type":"message_delta","delta":{"stop_reason":%q,"stop_sequence":null},"usage":{"output_tokens":0}}`,
		stopReason,
	))
	writeSSE(w, "message_stop", `{"type":"message_stop"}`)

	return nil
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
	var msgs []openAIMessage
	for _, m := range req.Messages {
		switch m.Role {
		case "user":
			// user 消息：可能是普通文本，也可能包含 tool_result block
			// 先检查是否有 tool_result
			var toolResults []ContentBlock
			var textContent string

			for _, b := range m.Content.blocks {
				if b.Type == "tool_result" {
					toolResults = append(toolResults, b)
				} else if b.Type == "text" {
					textContent += b.Text
				}
			}

			// 如果有 tool_result，需要生成 role: "tool" 的消息
			if len(toolResults) > 0 {
				for _, tr := range toolResults {
					// tool_result 的内容
					var resultContent string
					for _, inner := range tr.ContentBlocks {
						if inner.Type == "text" {
							resultContent += inner.Text
						}
					}
					if tr.ContentStr != "" {
						resultContent = tr.ContentStr
					}
					if resultContent == "" {
						resultContent = "(tool result)"
					}

					msgs = append(msgs, openAIMessage{
						Role:       "tool",
						Content:    resultContent,
						ToolCallID: tr.ToolUseID, // 关联到 tool_use 的 ID
					})
				}
			} else if textContent != "" {
				// 普通文本消息
				msgs = append(msgs, openAIMessage{Role: "user", Content: textContent})
			} else {
				// 空内容
				msgs = append(msgs, openAIMessage{Role: "user", Content: m.Content.String()})
			}
		case "assistant":
			// assistant 消息：可能含 text 和 tool_use block
			// OpenAI 格式：tool_use 写进 ToolCalls，text 写进 Content
			var content string
			var toolCalls []openAIToolCall
			for _, b := range m.Content.blocks {
				switch b.Type {
				case "text":
					content += b.Text
				case "tool_use":
					args := "{}"
					if b.Input != nil {
						args = string(b.Input)
					}
					toolCalls = append(toolCalls, openAIToolCall{
						ID:   b.ID,
						Type: "function",
						Function: openAIToolFunction{
							Name:      b.Name,
							Arguments: args,
						},
					})
				}
			}
			if len(m.Content.blocks) == 0 {
				content = m.Content.String()
			}
			msgs = append(msgs, openAIMessage{
				Role:      "assistant",
				Content:   content,
				ToolCalls: toolCalls,
			})
		default:
			msgs = append(msgs, openAIMessage{Role: m.Role, Content: m.Content.String()})
		}
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
