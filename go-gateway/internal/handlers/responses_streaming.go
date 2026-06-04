package handlers

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/weijian/go-llm-gateway/internal/provider"
)

// handleStream 处理流式 Responses API 请求
func (h *responsesHandler) handleStream(c *gin.Context, req *responsesRequest) {
	providerReq, err := responsesToProviderRequest(req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{
			"message": err.Error(),
			"type":    "invalid_request_error",
		}})
		return
	}

	providerChain, err := h.router.RouteForStream(providerReq.Model)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{
			"message": err.Error(),
			"type":    "invalid_request_error",
		}})
		return
	}

	originalModel := req.Model
	var lastErr error
	for i, p := range providerChain {
		h.logger.Printf("Responses stream: trying provider %d/%d: %s", i+1, len(providerChain), p.Name())

		if bmp, ok := p.(provider.BoundModelProvider); ok {
			providerReq.Model = bmp.BoundModel()
		} else {
			providerReq.Model = h.router.MapModel(providerReq.Model, p.Name())
		}

		if err := h.streamFromProvider(c, providerReq, p, originalModel); err == nil {
			return
		} else {
			h.logger.Printf("Responses stream provider %s failed: %v", p.Name(), err)
			lastErr = err
		}
	}

	if !c.Writer.Written() {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{
			"message": fmt.Sprintf("all providers failed: %v", lastErr),
			"type":    "server_error",
		}})
	}
}

// streamFromProvider 从指定 provider 获取流式响应并转换为 Responses SSE
func (h *responsesHandler) streamFromProvider(c *gin.Context, req *provider.Request, p provider.Provider, model string) error {
	if !c.Writer.Written() {
		c.Header("Content-Type", "text/event-stream")
		c.Header("Cache-Control", "no-cache")
		c.Header("Connection", "keep-alive")
		c.Header("X-Accel-Buffering", "no")
		c.Writer.WriteHeader(http.StatusOK)
	}

	pr, pw := io.Pipe()
	errCh := make(chan error, 1)

	go func() {
		defer pw.Close()
		if sp, ok := p.(provider.StreamProvider); ok {
			errCh <- sp.ForwardStream(c.Request.Context(), req, pw)
		} else {
			errCh <- h.forwardRawStream(c, req, p, pw)
		}
	}()

	err := anthropicSSEToResponsesSSE(pr, c.Writer, model)
	_ = pr.Close()

	if streamErr := <-errCh; streamErr != nil && err == nil {
		return streamErr
	}
	return err
}

// forwardRawStream 对非 StreamProvider 做原始 Anthropic SSE 转发
func (h *responsesHandler) forwardRawStream(c *gin.Context, req *provider.Request, p provider.Provider, w io.Writer) error {
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, p.URL(), strings.NewReader(string(body)))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("User-Agent", "go-llm-gateway/1.0")
	if p.UseBearer() {
		httpReq.Header.Set("Authorization", "Bearer "+p.APIKey())
	} else {
		httpReq.Header.Set("x-api-key", p.APIKey())
	}

	client := &http.Client{}
	resp, err := client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("send: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("provider %d: %s", resp.StatusCode, string(b))
	}

	_, err = io.Copy(w, resp.Body)
	return err
}

// ─── Anthropic SSE → Responses SSE 转换 ────────────────────────────────────
// 严格对齐 Codex CLI 的解析要求：
//   - SSE 格式：event: <type>\ndata: <json>\n\n
//   - 每个 data JSON 必须有 "type" 字段
//   - response.completed 只需 response.id（其他字段可选）
//   - response.output_item.done 的 item 必须有完整结构
//   - response.content_part.added 被 Codex 忽略，不需要发

func anthropicSSEToResponsesSSE(r io.Reader, w io.Writer, model string) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 256*1024), 1024*1024)

	var eventType string
	var dataLines []string

	respID := fmt.Sprintf("resp_%d", time.Now().UnixNano()/1000000)
	created := time.Now().Unix()
	outputIndex := 0
	textStarted := false
	toolStarted := false
	toolCallID := ""
	toolCallName := ""
	inputTokens := 0
	outputTokens := 0

	// 收集完整文本内容（用于 output_item.done）
	var fullText strings.Builder
	// 收集完整 tool arguments
	var fullToolArgs strings.Builder

	// 发送 response.created
	writeResponsesSSE(w, "response.created", map[string]any{
		"type":     "response.created",
		"response": map[string]any{"id": respID, "object": "response", "created_at": created, "status": "in_progress", "model": model},
	})

	flushEvent := func() error {
		if eventType == "" || len(dataLines) == 0 {
			return nil
		}
		payload := strings.Join(dataLines, "\n")

		switch eventType {
		case "message_start":
			// 提取 input_tokens
			var evt struct {
				Message struct {
					Usage struct {
						InputTokens int `json:"input_tokens"`
					} `json:"usage"`
				} `json:"message"`
			}
			if err := json.Unmarshal([]byte(payload), &evt); err == nil {
				inputTokens = evt.Message.Usage.InputTokens
			}

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
				return nil
			}

			switch evt.Block.Type {
			case "text":
				textStarted = true
				fullText.Reset()
				// output_item.added (message type)
				writeResponsesSSE(w, "response.output_item.added", map[string]any{
					"type":         "response.output_item.added",
					"output_index": outputIndex,
					"item": map[string]any{
						"type":   "message",
						"id":     fmt.Sprintf("msg_%d", outputIndex),
						"status": "in_progress",
						"role":   "assistant",
						"content": []any{},
					},
				})
			case "tool_use":
				toolStarted = true
				toolCallID = evt.Block.ID
				toolCallName = evt.Block.Name
				fullToolArgs.Reset()
				// output_item.added (function_call type)
				writeResponsesSSE(w, "response.output_item.added", map[string]any{
					"type":         "response.output_item.added",
					"output_index": outputIndex,
					"item": map[string]any{
						"type":   "function_call",
						"id":     fmt.Sprintf("fc_%s", evt.Block.ID),
						"call_id": evt.Block.ID,
						"name":   evt.Block.Name,
						"status": "in_progress",
					},
				})
			}

		case "content_block_delta":
			var meta struct {
				Index int `json:"index"`
				Delta struct {
					Type string `json:"type"`
				} `json:"delta"`
			}
			if err := json.Unmarshal([]byte(payload), &meta); err != nil {
				return nil
			}

			switch meta.Delta.Type {
			case "text_delta":
				var evt struct {
					Delta struct {
						Text string `json:"text"`
					} `json:"delta"`
				}
				if err := json.Unmarshal([]byte(payload), &evt); err != nil {
					return nil
				}
				fullText.WriteString(evt.Delta.Text)
				writeResponsesSSE(w, "response.output_text.delta", map[string]any{
					"type":         "response.output_text.delta",
					"output_index": outputIndex,
					"content_index": 0,
					"delta":        evt.Delta.Text,
				})

			case "input_json_delta":
				var evt struct {
					Index int `json:"index"`
					Delta struct {
						PartialJSON string `json:"partial_json"`
					} `json:"delta"`
				}
				if err := json.Unmarshal([]byte(payload), &evt); err != nil {
					return nil
				}
				fullToolArgs.WriteString(evt.Delta.PartialJSON)
				writeResponsesSSE(w, "response.function_call_arguments.delta", map[string]any{
					"type":         "response.function_call_arguments.delta",
					"output_index": outputIndex,
					"delta":        evt.Delta.PartialJSON,
				})
			}

		case "content_block_stop":
			if textStarted {
				finalText := fullText.String()
				// output_item.done — 带完整内容
				writeResponsesSSE(w, "response.output_item.done", map[string]any{
					"type":         "response.output_item.done",
					"output_index": outputIndex,
					"item": map[string]any{
						"type":   "message",
						"id":     fmt.Sprintf("msg_%d", outputIndex),
						"status": "completed",
						"role":   "assistant",
						"content": []any{
							map[string]any{
								"type": "output_text",
								"text": finalText,
							},
						},
					},
				})
				textStarted = false
				outputIndex++
			} else if toolStarted {
				finalArgs := fullToolArgs.String()
				writeResponsesSSE(w, "response.output_item.done", map[string]any{
					"type":         "response.output_item.done",
					"output_index": outputIndex,
					"item": map[string]any{
						"type":   "function_call",
						"id":     fmt.Sprintf("fc_%s", toolCallID),
						"call_id": toolCallID,
						"name":   toolCallName,
						"arguments": finalArgs,
						"status": "completed",
					},
				})
				toolStarted = false
				outputIndex++
			}

		case "message_delta":
			var evt struct {
				Delta struct {
					StopReason string `json:"stop_reason"`
				} `json:"delta"`
				Usage struct {
					OutputTokens int `json:"output_tokens"`
				} `json:"usage"`
			}
			if err := json.Unmarshal([]byte(payload), &evt); err != nil {
				return nil
			}
			outputTokens = evt.Usage.OutputTokens

			status := "completed"
			if evt.Delta.StopReason == "max_tokens" {
				status = "incomplete"
			}

			// response.completed — 只需 id，其他可选
			writeResponsesSSE(w, "response.completed", map[string]any{
				"type": "response.completed",
				"response": map[string]any{
					"id":         respID,
					"object":     "response",
					"created_at": created,
					"status":     status,
					"model":      model,
					"output":     []any{},
					"usage": map[string]any{
						"input_tokens":  inputTokens,
						"output_tokens": outputTokens,
						"total_tokens":  inputTokens + outputTokens,
					},
				},
			})

		case "message_stop":
			// 已通过 response.completed 处理

		case "ping":
			// 跳过
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

// writeResponsesSSE 写一条 Responses API 格式的 SSE 事件
func writeResponsesSSE(w io.Writer, event string, data map[string]any) {
	dataJSON, err := json.Marshal(data)
	if err != nil {
		return
	}
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, string(dataJSON))
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}
