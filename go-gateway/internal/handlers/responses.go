package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/weijian/go-llm-gateway/internal/provider"
)

// ─── Responses API 数据结构 ──────────────────────────────────────────────────

// responsesRequest 是 OpenAI Responses API 请求格式
type responsesRequest struct {
	Model           string          `json:"model"`
	Instructions    string          `json:"instructions,omitempty"`
	Input           json.RawMessage `json:"input"`
	Tools           json.RawMessage `json:"tools,omitempty"`
	ToolChoice      json.RawMessage `json:"tool_choice,omitempty"`
	MaxOutputTokens int             `json:"max_output_tokens,omitempty"`
	Temperature     *float64        `json:"temperature,omitempty"`
	Stream          bool            `json:"stream,omitempty"`
	Reasoning       json.RawMessage `json:"reasoning,omitempty"`
	// 透传其他字段
	raw map[string]json.RawMessage
}

func (r *responsesRequest) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	r.raw = raw
	if v, ok := raw["model"]; ok {
		_ = json.Unmarshal(v, &r.Model)
	}
	if v, ok := raw["instructions"]; ok {
		_ = json.Unmarshal(v, &r.Instructions)
	}
	if v, ok := raw["input"]; ok {
		r.Input = v
	}
	if v, ok := raw["tools"]; ok {
		r.Tools = v
	}
	if v, ok := raw["tool_choice"]; ok {
		r.ToolChoice = v
	}
	if v, ok := raw["max_output_tokens"]; ok {
		_ = json.Unmarshal(v, &r.MaxOutputTokens)
	}
	if v, ok := raw["temperature"]; ok {
		var temp float64
		if err := json.Unmarshal(v, &temp); err == nil {
			r.Temperature = &temp
		}
	}
	if v, ok := raw["stream"]; ok {
		_ = json.Unmarshal(v, &r.Stream)
	}
	if v, ok := raw["reasoning"]; ok {
		r.Reasoning = v
	}
	return nil
}

// inputItem 表示 input 数组中的一个元素
type inputItem struct {
	Type    string          `json:"type,omitempty"`
	Role    string          `json:"role,omitempty"`
	Content json.RawMessage `json:"content,omitempty"`
	// function_call 字段
	CallID    string `json:"call_id,omitempty"`
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
	// function_call_output 字段
	Output string `json:"output,omitempty"`
	// custom_tool_call 字段
	ID    string `json:"id,omitempty"`
	Input string `json:"input,omitempty"` // custom tool 的 input 是字符串
}

// responsesTool 表示 Responses API 中的工具定义
type responsesTool struct {
	Type        string          `json:"type"`
	Name        string          `json:"name,omitempty"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
	Strict      bool            `json:"strict,omitempty"`
}

// responsesResponse 是 OpenAI Responses API 响应格式
type responsesResponse struct {
	ID        string         `json:"id"`
	Object    string         `json:"object"` // "response"
	CreatedAt int64          `json:"created_at"`
	Status    string         `json:"status"` // "completed", "incomplete", "failed"
	Model     string         `json:"model"`
	Output    []outputItem   `json:"output"`
	Usage     responsesUsage `json:"usage"`
}

type responsesUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

// outputItem 是 Responses API 响应中的输出项
type outputItem struct {
	ID      string          `json:"id,omitempty"`
	Type    string          `json:"type"` // "message", "function_call", "reasoning"
	Status  string          `json:"status,omitempty"`
	Role    string          `json:"role,omitempty"`
	Content []outputContent `json:"content,omitempty"`
	// function_call 字段
	CallID    string `json:"call_id,omitempty"`
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

type outputContent struct {
	Type string `json:"type"` // "output_text"
	Text string `json:"text,omitempty"`
}

// ─── ResponsesHandler ──────────────────────────────────────────────────────

type responsesHandler struct {
	router *provider.Router
	logger *log.Logger
}

func NewResponsesHandler(router *provider.Router, logger *log.Logger) *responsesHandler {
	return &responsesHandler{router: router, logger: logger}
}

// Handle 处理 /v1/responses 端点
func (h *responsesHandler) Handle(c *gin.Context) {
	rawBody, _ := io.ReadAll(c.Request.Body)
	// 打印请求体的前 500 字符，方便调试
	preview := string(rawBody)
	if len(preview) > 500 {
		preview = preview[:500] + "..."
	}
	h.logger.Printf("[RESPONSES] Request body preview: %s", preview)
	c.Request.Body = io.NopCloser(strings.NewReader(string(rawBody)))

	var req responsesRequest
	if err := json.Unmarshal(rawBody, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{
			"message": fmt.Sprintf("Invalid request body: %v", err),
			"type":    "invalid_request_error",
		}})
		return
	}
	if req.Model == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{
			"message": "Model is required",
			"type":    "invalid_request_error",
		}})
		return
	}

	// 检查是否走 ChatGPT 透传路径（Responses API 原生格式，不需要 Anthropic 转换）
	if h.tryChatGPTPassthrough(c, &req, rawBody) {
		return
	}

	if req.Stream {
		h.handleStream(c, &req)
	} else {
		h.handleNonStream(c, &req)
	}
}

// handleNonStream 处理非流式请求
func (h *responsesHandler) handleNonStream(c *gin.Context, req *responsesRequest) {
	providerReq, err := responsesToProviderRequest(req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{
			"message": err.Error(),
			"type":    "invalid_request_error",
		}})
		return
	}

	resp, err := h.router.Forward(c.Request.Context(), providerReq.Model, providerReq)
	if err != nil {
		h.logger.Printf("Responses forward failed: %v", err)
		setProviderErrorHeaders(c, err)
		c.JSON(routingErrorStatus(err), gin.H{"error": gin.H{
			"message": err.Error(),
			"type":    "server_error",
		}})
		return
	}

	c.JSON(http.StatusOK, providerToResponsesResponse(resp, req.Model))
}

// ─── Responses → Anthropic 格式转换 ──────────────────────────────────────────

func responsesToProviderRequest(req *responsesRequest) (*provider.Request, error) {
	out := &provider.Request{
		Model: req.Model,
	}

	out.Stream = req.Stream

	// 解析 input（可能是数组或字符串）
	var messages []provider.Message

	// 先尝试作为字符串
	var inputStr string
	if err := json.Unmarshal(req.Input, &inputStr); err == nil {
		messages = append(messages, provider.Message{
			Role:    "user",
			Content: provider.NewStringContent(inputStr),
		})
	} else {
		// 作为数组解析
		var items []inputItem
		if err := json.Unmarshal(req.Input, &items); err != nil {
			return nil, fmt.Errorf("invalid input format: expected string or array")
		}

		for _, item := range items {
			switch {
			case item.Type == "":
				// 可能是简单 message（有 role 无 type）
				if item.Role != "" {
					content := parseContentField(item.Content)
					messages = append(messages, provider.Message{
						Role:    normalizeRole(item.Role),
						Content: content,
					})
				}
			case item.Type == "message":
				content := parseContentField(item.Content)
				messages = append(messages, provider.Message{
					Role:    normalizeRole(item.Role),
					Content: content,
				})
			case item.Type == "function_call":
				// function_call → assistant message with tool_use
				var inputJSON json.RawMessage
				if item.Arguments != "" {
					inputJSON = json.RawMessage(item.Arguments)
				} else {
					inputJSON = json.RawMessage(`{}`)
				}
				messages = append(messages, provider.Message{
					Role: "assistant",
					Content: provider.NewBlocksContent([]provider.ContentBlock{
						{
							Type:  "tool_use",
							ID:    item.CallID,
							Name:  item.Name,
							Input: inputJSON,
						},
					}),
				})
			case item.Type == "function_call_output":
				// function_call_output → user message with tool_result
				messages = append(messages, provider.Message{
					Role: "user",
					Content: provider.NewBlocksContent([]provider.ContentBlock{
						{
							Type:       "tool_result",
							ToolUseID:  item.CallID,
							ContentStr: item.Output,
						},
					}),
				})
			default:
				// 其他类型（reasoning 等）暂跳过
			}
		}
	}

	// 合并连续的 assistant 消息（多个 function_call 可能产生连续 assistant 消息）
	messages = mergeAssistantMessages(messages)

	out.Messages = messages

	// 构建 raw 字段
	out.SetRawField("messages", out.Messages)
	if req.Instructions != "" {
		out.SetRawField("system", req.Instructions)
	}
	// Responses API 的 max_output_tokens 对应内部/Anthropic 兼容请求的
	// max_tokens。之前为了规避部分 GLM 在极小预算下的空响应而直接丢弃，
	// 导致调用方的输出上限失效；兼容性应由具体 provider 处理，而不是在
	// 协议转换层静默删除。
	if req.MaxOutputTokens > 0 {
		out.MaxTokens = req.MaxOutputTokens
		out.SetRawField("max_tokens", req.MaxOutputTokens)
	}
	if out.Stream {
		out.SetRawField("stream", true)
	}
	if req.Temperature != nil {
		out.SetRawField("temperature", *req.Temperature)
	}

	// 转换 tools
	if req.Tools != nil {
		var respTools []responsesTool
		if err := json.Unmarshal(req.Tools, &respTools); err == nil {
			anthropicTools := make([]map[string]any, 0, len(respTools))
			for _, t := range respTools {
				if t.Type == "function" {
					anthropicTools = append(anthropicTools, map[string]any{
						"name":         t.Name,
						"description":  t.Description,
						"input_schema": t.Parameters,
					})
				}
			}
			if len(anthropicTools) > 0 {
				out.SetRawField("tools", anthropicTools)
			}
		}
	}

	return out, nil
}

// parseContentField 解析 Responses API 的 content 字段。
// 文本块和多模态块都保留到内部结构，避免 Responses → Chat/Anthropic
// 转换时把 input_image、input_file 等内容压成空文本。
func parseContentField(content json.RawMessage) provider.MessageContent {
	if content == nil {
		return provider.NewStringContent("")
	}

	// 尝试字符串
	var s string
	if err := json.Unmarshal(content, &s); err == nil {
		return provider.NewStringContent(s)
	}

	// 尝试数组（input_text / input_image / input_file 等格式）。
	var parts []json.RawMessage
	if err := json.Unmarshal(content, &parts); err == nil {
		blocks := make([]provider.ContentBlock, 0, len(parts))
		for _, part := range parts {
			var meta struct {
				Type string `json:"type"`
				Text string `json:"text"`
			}
			if err := json.Unmarshal(part, &meta); err != nil {
				continue
			}
			switch meta.Type {
			case "input_text", "output_text", "text":
				blocks = append(blocks, provider.ContentBlock{Type: "text", Text: meta.Text})
			case "input_image", "image_url":
				blocks = append(blocks, provider.ContentBlock{Type: "image_url", Raw: append([]byte(nil), part...)})
			case "input_file", "file", "file_url":
				blocks = append(blocks, provider.ContentBlock{Type: "file", Raw: append([]byte(nil), part...)})
			case "input_audio", "audio":
				blocks = append(blocks, provider.ContentBlock{Type: "input_audio", Raw: append([]byte(nil), part...)})
			default:
				// Unknown blocks remain visible to capability validation and can be
				// handled by a provider that understands the original shape.
				blocks = append(blocks, provider.ContentBlock{Type: meta.Type, Raw: append([]byte(nil), part...)})
			}
		}
		if len(blocks) > 0 {
			return provider.NewBlocksContent(blocks)
		}
	}

	return provider.NewStringContent(string(content))
}

// normalizeRole 将 Responses API 的 role 映射到 Anthropic 格式
func normalizeRole(role string) string {
	switch role {
	case "developer", "system":
		return "user" // Anthropic 没有 system role，用 system 字段代替
	case "assistant":
		return "assistant"
	case "user", "tool":
		return "user"
	default:
		return role
	}
}

// mergeAssistantMessages 合并连续的 assistant 消息（多个 function_call 场景）
func mergeAssistantMessages(msgs []provider.Message) []provider.Message {
	if len(msgs) <= 1 {
		return msgs
	}
	var result []provider.Message
	i := 0
	for i < len(msgs) {
		if msgs[i].Role == "assistant" && i+1 < len(msgs) && msgs[i+1].Role == "assistant" {
			// 合并：收集连续的 assistant 消息的所有 blocks
			var allBlocks []provider.ContentBlock
			j := i
			for j < len(msgs) && msgs[j].Role == "assistant" {
				allBlocks = append(allBlocks, msgs[j].Content.Blocks()...)
				j++
			}
			result = append(result, provider.Message{
				Role:    "assistant",
				Content: provider.NewBlocksContent(allBlocks),
			})
			i = j
		} else {
			result = append(result, msgs[i])
			i++
		}
	}
	return result
}

// ─── Anthropic → Responses 格式转换 ──────────────────────────────────────────

func providerToResponsesResponse(resp *provider.Response, model string) *responsesResponse {
	now := time.Now().Unix()
	output := make([]outputItem, 0)
	outputIndex := 0

	// 收集文本内容块
	var textBlocks []provider.ContentBlock
	var toolBlocks []provider.ContentBlock

	for _, block := range resp.Content {
		if block.Type == "text" {
			textBlocks = append(textBlocks, block)
		} else if block.Type == "tool_use" {
			toolBlocks = append(toolBlocks, block)
		}
	}

	// 如果有文本内容，生成一个 message output item
	if len(textBlocks) > 0 {
		var text string
		for _, b := range textBlocks {
			text += b.Text
		}
		output = append(output, outputItem{
			ID:     fmt.Sprintf("resp_msg_%d", outputIndex),
			Type:   "message",
			Status: "completed",
			Role:   "assistant",
			Content: []outputContent{
				{
					Type: "output_text",
					Text: text,
				},
			},
		})
		outputIndex++
	}

	// 每个 tool_use 生成一个 function_call output item
	for _, block := range toolBlocks {
		arguments := "{}"
		if len(block.Input) > 0 {
			arguments = string(block.Input)
		}
		output = append(output, outputItem{
			ID:        fmt.Sprintf("fc_%s", block.ID),
			Type:      "function_call",
			Status:    "completed",
			CallID:    block.ID,
			Name:      block.Name,
			Arguments: arguments,
		})
		outputIndex++
	}

	// 如果没有任何输出，返回空 message
	if len(output) == 0 {
		output = append(output, outputItem{
			ID:     "resp_msg_0",
			Type:   "message",
			Status: "completed",
			Role:   "assistant",
			Content: []outputContent{
				{Type: "output_text", Text: ""},
			},
		})
	}

	status := "completed"
	if resp.StopReason == "max_tokens" {
		status = "incomplete"
	}

	return &responsesResponse{
		ID:        resp.ID,
		Object:    "response",
		CreatedAt: now,
		Status:    status,
		Model:     model,
		Output:    output,
		Usage: responsesUsage{
			InputTokens:  resp.Usage.InputTokens,
			OutputTokens: resp.Usage.OutputTokens,
			TotalTokens:  resp.Usage.InputTokens + resp.Usage.OutputTokens,
		},
	}
}

// ─── ChatGPT 透传路径 ──────────────────────────────────────────────────────

// tryChatGPTPassthrough 检查请求是否应该走 ChatGPT 透传路径
// ChatGPT 的 Codex API 直接返回 Responses API SSE 格式，不需要 Anthropic 转换
func (h *responsesHandler) tryChatGPTPassthrough(c *gin.Context, req *responsesRequest, rawBody []byte) bool {
	if !req.Stream {
		return false
	}
	providerChain, err := h.router.RouteForStream(req.Model)
	if err != nil {
		return false
	}

	for _, p := range providerChain {
		if chatgptPassthrough, ok := p.(provider.ChatGPTPassthroughMarker); ok {
			if !h.router.AllowProviderRequestFor(req.Model, p) {
				continue
			}
			h.logger.Printf("[RESPONSES] Using ChatGPT passthrough for model %s", req.Model)
			streamWriter := newDeferredStreamWriter(c)

			// 直接透传原始请求体到 ChatGPT
			if err := chatgptPassthrough.ForwardRawResponsesStream(c.Request.Context(), json.RawMessage(rawBody), streamWriter); err != nil {
				h.router.RecordProviderFailureFor(req.Model, p, err)
				h.logger.Printf("[RESPONSES] ChatGPT passthrough failed: %v", err)
				if c.Writer.Written() {
					_ = writeResponsesStreamError(c.Writer, err)
				} else {
					setProviderErrorHeaders(c, err)
					c.JSON(routingErrorStatus(err), gin.H{"error": gin.H{
						"message": err.Error(),
						"type":    "server_error",
					}})
				}
				return true
			}
			h.router.RecordProviderSuccessFor(req.Model, p)
			streamWriter.Commit()
			return true
		}
	}

	return false
}
