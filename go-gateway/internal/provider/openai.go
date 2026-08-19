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
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

type openAIToolCall struct {
	ID       string             `json:"id"`
	Type     string             `json:"type"`
	Function openAIToolFunction `json:"function"`
}

type openAIMessage struct {
	Role       string           `json:"role"`
	Content    any              `json:"content,omitempty"`
	ToolCalls  []openAIToolCall `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"` // for tool result messages
}

// chatTemplateKwargs 用于 skyclaw 等模型的 chat_template_kwargs 字段
type chatTemplateKwargs struct {
	EnableThinking *bool `json:"enable_thinking,omitempty"`
}

type openAIRequest struct {
	Model               string                     `json:"model"`
	Messages            []openAIMessage            `json:"messages"`
	MaxTokens           int                        `json:"max_tokens,omitempty"`
	MaxCompletionTokens int                        `json:"max_completion_tokens,omitempty"`
	Temperature         float64                    `json:"temperature,omitempty"`
	TopP                float64                    `json:"top_p,omitempty"`
	TopK                int                        `json:"top_k,omitempty"`
	Stream              bool                       `json:"stream,omitempty"`
	Tools               []openAITool               `json:"tools,omitempty"`
	ToolChoice          json.RawMessage            `json:"tool_choice,omitempty"`
	Thinking            json.RawMessage            `json:"thinking,omitempty"`
	ReasoningEffort     string                     `json:"reasoning_effort,omitempty"`
	ExtraBody           json.RawMessage            `json:"extra_body,omitempty"`
	ResponseFormat      json.RawMessage            `json:"response_format,omitempty"`
	StreamOptions       json.RawMessage            `json:"stream_options,omitempty"`
	ChatTemplateKwargs  *chatTemplateKwargs        `json:"chat_template_kwargs,omitempty"`
	ExtraFields         map[string]json.RawMessage `json:"-"`
}

func (r openAIRequest) MarshalJSON() ([]byte, error) {
	type alias openAIRequest
	knownJSON, err := json.Marshal(alias(r))
	if err != nil {
		return nil, err
	}
	var known map[string]json.RawMessage
	if err := json.Unmarshal(knownJSON, &known); err != nil {
		return nil, err
	}
	out := make(map[string]json.RawMessage, len(r.ExtraFields)+len(known))
	for key, value := range r.ExtraFields {
		out[key] = append(json.RawMessage(nil), value...)
	}
	for key, value := range known {
		out[key] = value
	}
	return json.Marshal(out)
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
	Usage   *openAIStreamUsage   `json:"usage,omitempty"`
}

// openAIStreamUsage 流式 chunk 中的 usage（最后一条 chunk 携带）
type openAIStreamUsage struct {
	PromptTokens        int                  `json:"prompt_tokens"`
	CompletionTokens    int                  `json:"completion_tokens"`
	TotalTokens         int                  `json:"total_tokens"`
	PromptTokensDetails *promptTokensDetails `json:"prompt_tokens_details,omitempty"`
}

type promptTokensDetails struct {
	CachedTokens int `json:"cached_tokens"`
}

// ─── OpenAIProvider ──────────────────────────────────────────────────────────

type OpenAIProvider struct {
	config *Config
	client *http.Client
}

const defaultOpenAIRequestTimeout = 120 * time.Second

func NewOpenAIProvider(config *Config) *OpenAIProvider {
	requestTimeout := config.RequestTimeout
	if requestTimeout <= 0 {
		requestTimeout = defaultOpenAIRequestTimeout
	}
	return &OpenAIProvider{
		config: config,
		client: &http.Client{Timeout: requestTimeout},
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
		return nil, NewHTTPError(p.Name(), resp, respBody)
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
		return NewHTTPError(p.Name(), resp, b)
	}

	msgID := "msg_oai_" + time.Now().Format("20060102150405")

	// 跟踪工具调用的 Anthropic block index 及发送状态
	type toolCallState struct {
		blockIdx int
		started  bool
	}
	toolStates := map[int]*toolCallState{}
	nextToolBlockIdx := 0
	textBlockStarted := false

	writeSSE(w, "message_start", fmt.Sprintf(
		`{"type":"message_start","message":{"id":%q,"type":"message","role":"assistant","model":%q,"content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":0,"output_tokens":0}}}`,
		msgID, req.Model,
	))
	writeSSE(w, "ping", `{"type":"ping"}`)

	scanner := bufio.NewScanner(resp.Body)
	// 增大 scanner buffer，防止超长行截断
	scanner.Buffer(make([]byte, 256*1024), 256*1024)

	finishReason := ""
	var streamUsage *openAIStreamUsage

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
		// --- usage（最后一条 chunk 携带，可能在 choices 为空的独立 chunk 中） ---
		if chunk.Usage != nil {
			streamUsage = chunk.Usage
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
				if nextToolBlockIdx == 0 {
					nextToolBlockIdx = 1
				}
				writeSSE(w, "content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`)
			}
			writeSSE(w, "content_block_delta", fmt.Sprintf(
				`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":%s}}`,
				jsonString(delta.Content),
			))
		}

		// --- 工具调用（实时流式转发）---
		for _, tc := range delta.ToolCalls {
			idx := tc.Index
			state, ok := toolStates[idx]
			if !ok {
				bIdx := nextToolBlockIdx
				nextToolBlockIdx++
				state = &toolCallState{blockIdx: bIdx, started: false}
				toolStates[idx] = state
			}

			if !state.started {
				state.started = true
				writeSSE(w, "content_block_start", fmt.Sprintf(
					`{"type":"content_block_start","index":%d,"content_block":{"type":"tool_use","id":%s,"name":%s,"input":{}}}`,
					state.blockIdx, jsonString(tc.ID), jsonString(tc.Function.Name),
				))
			}

			if tc.Function.Arguments != "" {
				writeSSE(w, "content_block_delta", fmt.Sprintf(
					`{"type":"content_block_delta","index":%d,"delta":{"type":"input_json_delta","partial_json":%s}}`,
					state.blockIdx, jsonString(tc.Function.Arguments),
				))
			}
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

	// 关闭工具调用 blocks
	for _, state := range toolStates {
		if state.started {
			writeSSE(w, "content_block_stop", fmt.Sprintf(`{"type":"content_block_stop","index":%d}`, state.blockIdx))
		}
	}

	stopReason := mapFinishReason(finishReason)

	// 用上游真实 usage 构造 message_delta（Anthropic SSE 格式）
	inputTokens := 0
	outputTokens := 0
	cacheReadTokens := 0
	if streamUsage != nil {
		inputTokens = streamUsage.PromptTokens
		outputTokens = streamUsage.CompletionTokens
		if streamUsage.PromptTokensDetails != nil {
			cacheReadTokens = streamUsage.PromptTokensDetails.CachedTokens
		}
	}
	writeSSE(w, "message_delta", fmt.Sprintf(
		`{"type":"message_delta","delta":{"stop_reason":%q,"stop_sequence":null},"usage":{"input_tokens":%d,"output_tokens":%d,"cache_read_input_tokens":%d}}`,
		stopReason, inputTokens, outputTokens, cacheReadTokens,
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

// ─── 始终思考模型处理 ──────────────────────────────────────────────────────────

// alwaysThinkingModels 列出上游强制思考、不接受 thinking:disabled 的模型。
// 请求带 disabled 转发到这些模型会被上游直接拒绝。
var alwaysThinkingModels = map[string]bool{
	"glm-5.3": true,
}

// thinkingIsDisabled 判断 thinking 字段是否为 {"type":"disabled"}。
func thinkingIsDisabled(raw json.RawMessage) bool {
	var t struct {
		Type string `json:"type"`
	}
	return json.Unmarshal(raw, &t) == nil && t.Type == "disabled"
}

// ─── 格式转换辅助函数 ──────────────────────────────────────────────────────────

func toOpenAIRequest(req *Request) *openAIRequest {
	var msgs []openAIMessage

	// 重建 system 消息（toProviderRequest 提取到了 raw["system"]，不在 Messages 里）
	if sysRaw, ok := req.raw["system"]; ok {
		var sysContent any
		if err := json.Unmarshal(sysRaw, &sysContent); err == nil && sysContent != nil {
			msgs = append(msgs, openAIMessage{
				Role:    "system",
				Content: sysContent,
			})
		}
	}

	for _, m := range req.Messages {
		switch m.Role {
		case "user":
			blocks := m.Content.Blocks()
			if len(blocks) == 0 {
				msgs = append(msgs, openAIMessage{Role: "user", Content: m.Content.String()})
				continue
			}

			var contentParts []json.RawMessage
			flushUserContent := func() {
				if len(contentParts) == 0 {
					return
				}
				content := any(nil)
				allText := true
				var textParts []string
				for _, part := range contentParts {
					var textBlock struct {
						Type string `json:"type"`
						Text string `json:"text"`
					}
					if err := json.Unmarshal(part, &textBlock); err != nil || textBlock.Type != "text" {
						allText = false
						break
					}
					textParts = append(textParts, textBlock.Text)
				}
				if allText {
					content = strings.Join(textParts, "")
				} else {
					content = contentParts
				}
				msgs = append(msgs, openAIMessage{
					Role:    "user",
					Content: content,
				})
				contentParts = nil
			}

			for _, b := range blocks {
				switch b.Type {
				case "text":
					if part := openAIContentPart(b); len(part) > 0 {
						contentParts = append(contentParts, part)
					}
				case "tool_result":
					flushUserContent()

					var resultParts []string
					for _, inner := range b.ContentBlocks {
						if inner.Type == "text" {
							resultParts = append(resultParts, inner.Text)
						}
					}
					if b.ContentStr != "" {
						resultParts = []string{b.ContentStr}
					}
					resultContent := strings.Join(resultParts, "")
					if resultContent == "" {
						resultContent = "(tool result)"
					}

					msgs = append(msgs, openAIMessage{
						Role:       "tool",
						Content:    resultContent,
						ToolCallID: b.ToolUseID,
					})
				default:
					if part := openAIContentPart(b); len(part) > 0 {
						contentParts = append(contentParts, part)
					}
				}
			}

			flushUserContent()
		case "assistant":
			// assistant 消息：可能含 text 和 tool_use block
			// OpenAI 格式：tool_use 写进 ToolCalls，text 写进 Content
			var content string
			var toolCalls []openAIToolCall
			for _, b := range m.Content.Blocks() {
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
			if len(m.Content.Blocks()) == 0 {
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
		Model:       req.Model,
		Messages:    msgs,
		MaxTokens:   req.MaxTokens,
		ExtraFields: make(map[string]json.RawMessage),
	}
	for key, value := range req.raw {
		switch key {
		case "messages", "system", "tools":
			continue
		}
		oaiReq.ExtraFields[key] = append(json.RawMessage(nil), value...)
	}

	// 透传额外字段（例如 skyclaw 的 top_k、chat_template_kwargs 等）
	if v, ok := req.raw["top_k"]; ok {
		_ = json.Unmarshal(v, &oaiReq.TopK)
	}
	if v, ok := req.raw["chat_template_kwargs"]; ok {
		_ = json.Unmarshal(v, &oaiReq.ChatTemplateKwargs)
	}
	if v, ok := req.raw["temperature"]; ok {
		_ = json.Unmarshal(v, &oaiReq.Temperature)
	}
	if v, ok := req.raw["top_p"]; ok {
		_ = json.Unmarshal(v, &oaiReq.TopP)
	}
	if v, ok := req.raw["tool_choice"]; ok {
		oaiReq.ToolChoice = v
	}
	if v, ok := req.raw["thinking"]; ok {
		// 始终思考模型（如 glm-5.3）不接受 thinking:disabled——上游报错
		// 「该模型始终思考，不支持关闭思考」。丢弃 disabled 让其走默认思考档。
		if !alwaysThinkingModels[req.Model] || !thinkingIsDisabled(v) {
			oaiReq.Thinking = append(json.RawMessage(nil), v...)
		}
	}
	if v, ok := req.raw["reasoning_effort"]; ok {
		_ = json.Unmarshal(v, &oaiReq.ReasoningEffort)
	}
	if v, ok := req.raw["extra_body"]; ok {
		oaiReq.ExtraBody = append(json.RawMessage(nil), v...)
	}
	if v, ok := req.raw["response_format"]; ok {
		oaiReq.ResponseFormat = append(json.RawMessage(nil), v...)
	}
	if v, ok := req.raw["stream_options"]; ok {
		oaiReq.StreamOptions = append(json.RawMessage(nil), v...)
	}
	if v, ok := req.raw["max_completion_tokens"]; ok {
		_ = json.Unmarshal(v, &oaiReq.MaxCompletionTokens)
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

// openAIContentPart converts one internal non-text block without reducing it
// to text. Raw blocks from an OpenAI request are emitted unchanged so fields
// added by a compatible provider are not lost in the round trip.
func openAIContentPart(block ContentBlock) json.RawMessage {
	if block.Type == "image" && len(block.Source) > 0 {
		var source struct {
			Type      string `json:"type"`
			MediaType string `json:"media_type"`
			Data      string `json:"data"`
			URL       string `json:"url"`
		}
		if err := json.Unmarshal(block.Source, &source); err == nil {
			url := source.URL
			if url == "" && source.Data != "" {
				mediaType := source.MediaType
				if mediaType == "" {
					mediaType = "image/*"
				}
				url = "data:" + mediaType + ";base64," + source.Data
			}
			if url != "" {
				data, _ := json.Marshal(map[string]any{
					"type":      "image_url",
					"image_url": map[string]string{"url": url},
				})
				return data
			}
		}
	}
	if len(block.Raw) > 0 {
		var responseBlock struct {
			Type     string          `json:"type"`
			Text     string          `json:"text"`
			ImageURL json.RawMessage `json:"image_url"`
			Detail   string          `json:"detail"`
		}
		if json.Unmarshal(block.Raw, &responseBlock) == nil {
			switch responseBlock.Type {
			case "input_text", "output_text":
				data, _ := json.Marshal(map[string]any{
					"type": "text",
					"text": responseBlock.Text,
				})
				return data
			case "input_image":
				var imageURL any
				var url string
				if json.Unmarshal(responseBlock.ImageURL, &url) == nil {
					image := map[string]any{"url": url}
					if responseBlock.Detail != "" {
						image["detail"] = responseBlock.Detail
					}
					imageURL = image
				} else {
					var image map[string]any
					if json.Unmarshal(responseBlock.ImageURL, &image) == nil {
						if responseBlock.Detail != "" && image["detail"] == nil {
							image["detail"] = responseBlock.Detail
						}
						imageURL = image
					}
				}
				if imageURL != nil {
					data, _ := json.Marshal(map[string]any{
						"type":      "image_url",
						"image_url": imageURL,
					})
					return data
				}
			}
		}
		return append(json.RawMessage(nil), block.Raw...)
	}
	var value any
	switch block.Type {
	case "text":
		value = map[string]any{"type": block.Type, "text": block.Text}
	case "image_url":
		value = map[string]any{"type": block.Type, "image_url": json.RawMessage(block.ImageURL)}
	case "video_url":
		value = map[string]any{"type": block.Type, "video_url": json.RawMessage(block.VideoURL)}
	case "file", "file_url":
		value = map[string]any{"type": block.Type, "file": json.RawMessage(block.File)}
	case "input_audio", "audio":
		value = map[string]any{"type": block.Type, "input_audio": json.RawMessage(block.InputAudio)}
	default:
		return nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	return data
}

func fromOpenAIResponse(oai *openAIResponse) *Response {
	var blocks []ContentBlock
	var stopReason string

	if len(oai.Choices) > 0 {
		choice := oai.Choices[0]
		stopReason = mapFinishReason(choice.FinishReason)

		// 文本内容
		if content := openAIResponseContentText(choice.Message.Content); content != "" {
			blocks = append(blocks, ContentBlock{Type: "text", Text: content})
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

func openAIResponseContentText(content any) string {
	switch value := content.(type) {
	case string:
		return value
	case []any:
		var text strings.Builder
		for _, item := range value {
			if block, ok := item.(map[string]any); ok {
				if value, ok := block["text"].(string); ok {
					text.WriteString(value)
				}
			}
		}
		return text.String()
	default:
		return ""
	}
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
