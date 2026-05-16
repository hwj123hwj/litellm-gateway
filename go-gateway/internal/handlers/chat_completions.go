package handlers

import (
	"bufio"
	"bytes"
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

type openAIChatCompletionsHandler struct {
	router *provider.Router
	logger *log.Logger
}

type openAIChatCompletionsRequest struct {
	Model     string                `json:"model"`
	Messages  []openAIChatMessage   `json:"messages"`
	MaxTokens int                   `json:"max_tokens,omitempty"`
	Stream    bool                  `json:"stream,omitempty"`
	Tools     []openAIChatTool      `json:"tools,omitempty"`
	raw       map[string]json.RawMessage
}

type openAIChatMessage struct {
	Role       string               `json:"role"`
	Content    string               `json:"content"`
	ToolCalls  []openAIChatToolCall `json:"tool_calls,omitempty"`
	ToolCallID string               `json:"tool_call_id,omitempty"`
}

type openAIChatTool struct {
	Type     string                 `json:"type"`
	Function openAIChatToolFunction `json:"function"`
}

type openAIChatToolFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
	Arguments   string          `json:"arguments,omitempty"`
}

type openAIChatToolCall struct {
	ID       string                 `json:"id"`
	Type     string                 `json:"type"`
	Function openAIChatToolFunction `json:"function"`
}

type openAIChatCompletionResponse struct {
	ID      string                    `json:"id"`
	Object  string                    `json:"object"`
	Created int64                     `json:"created"`
	Model   string                    `json:"model"`
	Choices []openAIChatChoice        `json:"choices"`
	Usage   openAIChatCompletionUsage `json:"usage"`
}

type openAIChatChoice struct {
	Index        int               `json:"index"`
	Message      openAIChatMessage `json:"message"`
	FinishReason string            `json:"finish_reason"`
}

type openAIChatCompletionUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type openAIStreamChunkResponse struct {
	ID      string                    `json:"id,omitempty"`
	Object  string                    `json:"object"`
	Created int64                     `json:"created"`
	Model   string                    `json:"model,omitempty"`
	Choices []openAIStreamChunkChoice `json:"choices"`
}

type openAIStreamChunkChoice struct {
	Index        int               `json:"index"`
	Delta        openAIStreamDelta `json:"delta"`
	FinishReason *string           `json:"finish_reason"`
}

type openAIStreamDelta struct {
	Role      string               `json:"role,omitempty"`
	Content   string               `json:"content,omitempty"`
	ToolCalls []openAIChatToolCall `json:"tool_calls,omitempty"`
}

func NewChatCompletionsHandler(router *provider.Router, logger *log.Logger) *openAIChatCompletionsHandler {
	return &openAIChatCompletionsHandler{router: router, logger: logger}
}

func (h *openAIChatCompletionsHandler) Handle(c *gin.Context) {
	rawBody, _ := io.ReadAll(c.Request.Body)
	h.logger.Printf("Raw chat.completions request body: %s", string(rawBody))
	c.Request.Body = io.NopCloser(bytes.NewReader(rawBody))

	var req openAIChatCompletionsRequest
	if err := json.Unmarshal(rawBody, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}
	if err := json.Unmarshal(rawBody, &req.raw); err != nil {
		req.raw = map[string]json.RawMessage{}
	}
	if req.Model == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Model is required"})
		return
	}
	if len(req.Messages) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Messages are required"})
		return
	}

	providerReq, err := toProviderRequest(&req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.Stream {
		h.handleStream(c, providerReq)
		return
	}

	resp, err := h.router.Forward(c.Request.Context(), providerReq.Model, providerReq)
	if err != nil {
		h.logger.Printf("Forward failed: %v", err)
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, toOpenAIChatCompletionResponse(resp))
}

func (h *openAIChatCompletionsHandler) handleStream(c *gin.Context, req *provider.Request) {
	providerChain, err := h.router.RouteForStream(req.Model)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	originalModel := req.Model
	var lastErr error
	for _, p := range providerChain {
		if bmp, ok := p.(provider.BoundModelProvider); ok {
			req.Model = bmp.BoundModel()
		} else {
			req.Model = h.router.MapModel(originalModel, p.Name())
		}
		if err := h.streamFromProvider(c, req, p); err == nil {
			return
		} else {
			lastErr = err
			h.logger.Printf("chat.completions stream provider %s failed: %v", p.Name(), err)
		}
	}
	if !c.Writer.Written() {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": fmt.Sprintf("all providers failed: %v", lastErr)})
	}
}

func (h *openAIChatCompletionsHandler) streamFromProvider(c *gin.Context, req *provider.Request, p provider.Provider) error {
	pr, pw := io.Pipe()
	errCh := make(chan error, 1)
	go func() {
		if sp, ok := p.(provider.StreamProvider); ok {
			errCh <- sp.ForwardStream(c.Request.Context(), req, pw)
		} else {
			errCh <- fmt.Errorf("provider %s does not support streaming", p.Name())
		}
		_ = pw.Close()
	}()

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	c.Writer.WriteHeader(http.StatusOK)

	if err := anthropicSSEToOpenAISSE(pr, c.Writer); err != nil {
		_ = pr.Close()
		return err
	}
	if err := <-errCh; err != nil {
		return err
	}
	return nil
}

func toProviderRequest(req *openAIChatCompletionsRequest) (*provider.Request, error) {
	out := &provider.Request{
		Model:     req.Model,
		MaxTokens: req.MaxTokens,
		Stream:    req.Stream,
	}
	for key, raw := range req.raw {
		if key == "messages" || key == "tools" || key == "model" || key == "max_tokens" || key == "stream" {
			continue
		}
		var decoded any
		if err := json.Unmarshal(raw, &decoded); err == nil {
			_ = out.SetRawField(key, decoded)
		}
	}

	var systemTexts []string
	for _, msg := range req.Messages {
		switch msg.Role {
		case "system":
			if msg.Content != "" {
				systemTexts = append(systemTexts, msg.Content)
			}
		case "tool":
			out.Messages = append(out.Messages, provider.Message{
				Role: "user",
				Content: provider.NewBlocksContent([]provider.ContentBlock{{
					Type:       "tool_result",
					ToolUseID:  msg.ToolCallID,
					ContentStr: msg.Content,
				}}),
			})
		default:
			blocks := make([]provider.ContentBlock, 0, 1+len(msg.ToolCalls))
			if msg.Content != "" {
				blocks = append(blocks, provider.ContentBlock{Type: "text", Text: msg.Content})
			}
			for _, tc := range msg.ToolCalls {
				input := json.RawMessage(`{}`)
				if tc.Function.Arguments != "" {
					if json.Valid([]byte(tc.Function.Arguments)) {
						input = json.RawMessage(tc.Function.Arguments)
					} else {
						return nil, fmt.Errorf("invalid tool arguments for %s", tc.Function.Name)
					}
				}
				blocks = append(blocks, provider.ContentBlock{
					Type:  "tool_use",
					ID:    tc.ID,
					Name:  tc.Function.Name,
					Input: input,
				})
			}
			content := provider.NewStringContent(msg.Content)
			if len(blocks) > 0 && (len(blocks) > 1 || len(msg.ToolCalls) > 0 || msg.Role == "assistant") {
				content = provider.NewBlocksContent(blocks)
			}
			out.Messages = append(out.Messages, provider.Message{Role: msg.Role, Content: content})
		}
	}

	if len(systemTexts) > 0 {
		_ = out.SetRawField("system", joinStrings(systemTexts, "\n\n"))
	}
	if len(out.Messages) > 0 {
		_ = out.SetRawField("messages", out.Messages)
	}
	if req.MaxTokens > 0 {
		_ = out.SetRawField("max_tokens", req.MaxTokens)
	}
	if req.Stream {
		_ = out.SetRawField("stream", req.Stream)
	}
	if len(req.Tools) > 0 {
		anthropicTools := make([]map[string]any, 0, len(req.Tools))
		for _, tool := range req.Tools {
			anthropicTools = append(anthropicTools, map[string]any{
				"name":         tool.Function.Name,
				"description":  tool.Function.Description,
				"input_schema": json.RawMessage(tool.Function.Parameters),
			})
		}
		_ = out.SetRawField("tools", anthropicTools)
	}
	return out, nil
}

func toOpenAIChatCompletionResponse(resp *provider.Response) *openAIChatCompletionResponse {
	message := openAIChatMessage{Role: "assistant"}
	var content string
	for _, block := range resp.Content {
		switch block.Type {
		case "text":
			content += block.Text
		case "tool_use":
			arguments := "{}"
			if len(block.Input) > 0 {
				arguments = string(block.Input)
			}
			message.ToolCalls = append(message.ToolCalls, openAIChatToolCall{
				ID:   block.ID,
				Type: "function",
				Function: openAIChatToolFunction{
					Name:      block.Name,
					Arguments: arguments,
				},
			})
		}
	}
	message.Content = content
	usage := openAIChatCompletionUsage{
		PromptTokens:     resp.Usage.InputTokens,
		CompletionTokens: resp.Usage.OutputTokens,
		TotalTokens:      resp.Usage.InputTokens + resp.Usage.OutputTokens,
	}
	return &openAIChatCompletionResponse{
		ID:      resp.ID,
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   resp.Model,
		Choices: []openAIChatChoice{{
			Index:        0,
			Message:      message,
			FinishReason: mapStopReasonToOpenAI(resp.StopReason),
		}},
		Usage: usage,
	}
}

func anthropicSSEToOpenAISSE(r io.Reader, w io.Writer) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 256*1024), 1024*1024)

	var eventType string
	var dataLines []string
	created := time.Now().Unix()
	messageID := ""
	model := ""
	var pendingToolCalls map[int]*openAIChatToolCall

	flushEvent := func() error {
		if eventType == "" || len(dataLines) == 0 {
			return nil
		}
		payload := strings.Join(dataLines, "\n")
		chunk, done, err := anthropicEventToOpenAIChunk(eventType, payload, created, messageID, model, pendingToolCalls)
		if err != nil {
			return err
		}
		if chunk != nil {
			if chunk.ID != "" {
				messageID = chunk.ID
			}
			if chunk.Model != "" {
				model = chunk.Model
			}
			if err := writeOpenAIChunk(w, chunk); err != nil {
				return err
			}
		}
		if done {
			_, err := fmt.Fprint(w, "data: [DONE]\n\n")
			return err
		}
		eventType = ""
		dataLines = nil
		return nil
	}

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if err := flushEvent(); err != nil {
				return err
			}
			continue
		}
		if strings.HasPrefix(line, "event: ") {
			eventType = strings.TrimPrefix(line, "event: ")
			if eventType == "message_start" {
				pendingToolCalls = make(map[int]*openAIChatToolCall)
			}
			continue
		}
		if strings.HasPrefix(line, "data: ") {
			dataLines = append(dataLines, strings.TrimPrefix(line, "data: "))
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return flushEvent()
}

func anthropicEventToOpenAIChunk(eventType, payload string, created int64, currentID, currentModel string, pendingToolCalls map[int]*openAIChatToolCall) (*openAIStreamChunkResponse, bool, error) {
	switch eventType {
	case "ping", "content_block_stop":
		return nil, false, nil
	case "message_start":
		var evt struct {
			Message struct {
				ID    string `json:"id"`
				Model string `json:"model"`
			} `json:"message"`
		}
		if err := json.Unmarshal([]byte(payload), &evt); err != nil {
			return nil, false, err
		}
		return &openAIStreamChunkResponse{
			ID:      evt.Message.ID,
			Object:  "chat.completion.chunk",
			Created: created,
			Model:   evt.Message.Model,
			Choices: []openAIStreamChunkChoice{{
				Index: 0,
				Delta: openAIStreamDelta{Role: "assistant"},
			}},
		}, false, nil
	case "content_block_start":
		var evt struct {
			Index int `json:"index"`
			Block struct {
				Type  string `json:"type"`
				ID    string `json:"id"`
				Name  string `json:"name"`
				Input any    `json:"input"`
			} `json:"content_block"`
		}
		if err := json.Unmarshal([]byte(payload), &evt); err != nil {
			return nil, false, err
		}
		if evt.Block.Type != "tool_use" {
			return nil, false, nil
		}
		if pendingToolCalls != nil {
			pendingToolCalls[evt.Index] = &openAIChatToolCall{
				ID:   evt.Block.ID,
				Type: "function",
				Function: openAIChatToolFunction{
					Name: evt.Block.Name,
				},
			}
		}
		return nil, false, nil
	case "content_block_delta":
		var meta struct {
			Delta struct {
				Type string `json:"type"`
			} `json:"delta"`
		}
		if err := json.Unmarshal([]byte(payload), &meta); err != nil {
			return nil, false, err
		}
		switch meta.Delta.Type {
		case "text_delta":
			var evt struct {
				Delta struct {
					Text string `json:"text"`
				} `json:"delta"`
			}
			if err := json.Unmarshal([]byte(payload), &evt); err != nil {
				return nil, false, err
			}
			return &openAIStreamChunkResponse{
				ID:      currentID,
				Object:  "chat.completion.chunk",
				Created: created,
				Model:   currentModel,
				Choices: []openAIStreamChunkChoice{{
					Index: 0,
					Delta: openAIStreamDelta{Content: evt.Delta.Text},
				}},
			}, false, nil
		case "input_json_delta":
			var evt struct {
				Index int `json:"index"`
				Delta struct {
					PartialJSON string `json:"partial_json"`
				} `json:"delta"`
			}
			if err := json.Unmarshal([]byte(payload), &evt); err != nil {
				return nil, false, err
			}
			call, ok := pendingToolCalls[evt.Index]
			if !ok {
				return nil, false, nil
			}
			call.Function.Arguments += evt.Delta.PartialJSON
			return &openAIStreamChunkResponse{
				ID:      currentID,
				Object:  "chat.completion.chunk",
				Created: created,
				Model:   currentModel,
				Choices: []openAIStreamChunkChoice{{
					Index: 0,
					Delta: openAIStreamDelta{ToolCalls: []openAIChatToolCall{*call}},
				}},
			}, false, nil
		default:
			return nil, false, nil
		}
	case "message_delta":
		var evt struct {
			Delta struct {
				StopReason string `json:"stop_reason"`
			} `json:"delta"`
		}
		if err := json.Unmarshal([]byte(payload), &evt); err != nil {
			return nil, false, err
		}
		finishReason := mapStopReasonToOpenAI(evt.Delta.StopReason)
		return &openAIStreamChunkResponse{
			ID:      currentID,
			Object:  "chat.completion.chunk",
			Created: created,
			Model:   currentModel,
			Choices: []openAIStreamChunkChoice{{
				Index:        0,
				Delta:        openAIStreamDelta{},
				FinishReason: &finishReason,
			}},
		}, false, nil
	case "message_stop":
		return nil, true, nil
	default:
		return nil, false, nil
	}
}

func writeOpenAIChunk(w io.Writer, chunk *openAIStreamChunkResponse) error {
	b, err := json.Marshal(chunk)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", b); err != nil {
		return err
	}
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	return nil
}

func mapStopReasonToOpenAI(reason string) string {
	switch reason {
	case "max_tokens":
		return "length"
	case "tool_use":
		return "tool_calls"
	default:
		return "stop"
	}
}

func joinStrings(items []string, sep string) string {
	if len(items) == 0 {
		return ""
	}
	result := items[0]
	for i := 1; i < len(items); i++ {
		result += sep + items[i]
	}
	return result
}
