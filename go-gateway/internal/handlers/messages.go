package handlers

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/weijian/go-llm-gateway/internal/provider"
)

// MessageHandler 处理 /v1/messages 端点
type MessageHandler struct {
	router *provider.Router
	logger *log.Logger
}

func NewMessageHandler(router *provider.Router, logger *log.Logger) *MessageHandler {
	return &MessageHandler{router: router, logger: logger}
}

// Handle 处理消息请求，支持流式和非流式
func (h *MessageHandler) Handle(c *gin.Context) {
	// Read once so BindJSON can validate the same payload without retaining or
	// logging the full request body (which may contain credentials or Base64).
	rawBody, _ := io.ReadAll(c.Request.Body)
	logRequestSummary(h.logger, c, "messages", len(rawBody))
	// 把 body 放回去供 BindJSON 使用
	c.Request.Body = io.NopCloser(bytes.NewReader(rawBody))

	var req provider.Request
	if err := c.BindJSON(&req); err != nil {
		h.logger.Printf("BindJSON error: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}
	if req.Model == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Model is required"})
		return
	}
	if len(req.Messages) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Messages are required"})
		return
	}
	setRequestMetadata(c, req.Model, req.Stream)

	if req.Stream {
		h.handleStream(c, &req)
	} else {
		h.handleNonStream(c, &req)
	}
}

func (h *MessageHandler) handleNonStream(c *gin.Context, req *provider.Request) {
	resp, finalProvider, attempts, err := h.router.ForwardWithDetails(c.Request.Context(), req.Model, req)
	setForwardMetadata(c, finalProvider, attempts, err)
	if err != nil {
		h.logger.Printf("Forward failed: %v", err)
		setProviderErrorHeaders(c, err)
		c.JSON(routingErrorStatus(err), gin.H{"error": err.Error()})
		return
	}
	setUsageMetadata(c, resp.Usage.InputTokens, resp.Usage.OutputTokens)
	c.JSON(http.StatusOK, resp)
}

func (h *MessageHandler) handleStream(c *gin.Context, req *provider.Request) {
	providerChain, err := h.router.RouteForStreamRequest(req.Model, req)
	if err != nil {
		setProviderErrorHeaders(c, err)
		c.JSON(routingErrorStatus(err), gin.H{"error": err.Error()})
		return
	}

	originalModel := req.Model
	var lastErr error
	for i, p := range providerChain {
		if !h.router.AllowProviderRequestFor(originalModel, p) {
			continue
		}
		started := time.Now()
		h.logger.Printf("Stream: trying provider %d/%d: %s", i+1, len(providerChain), p.Name())
		if bmp, ok := p.(provider.BoundModelProvider); ok {
			req.Model = bmp.BoundModel()
		} else {
			req.Model = h.router.MapModel(originalModel, p.Name())
		}
		if err := h.streamFromProvider(c, req, p); err == nil {
			recordProviderAttempt(c, p.Name(), started, nil)
			h.router.RecordProviderSuccessFor(originalModel, p)
			return
		} else {
			recordProviderAttempt(c, p.Name(), started, err)
			h.router.RecordProviderFailureFor(originalModel, p, err)
			h.logger.Printf("Stream provider %s failed: %v", p.Name(), err)
			if c.Writer.Written() {
				_ = writeAnthropicStreamError(c.Writer, err)
				return
			}
			lastErr = err
			if !provider.ShouldFallback(err) {
				setProviderErrorHeaders(c, err)
				c.JSON(routingErrorStatus(err), gin.H{"error": err.Error()})
				return
			}
		}
	}
	if lastErr == nil {
		lastErr = &provider.NoAvailableProvidersError{Model: req.Model, Reason: "disabled, unavailable, or circuit open"}
	}

	h.logger.Printf("All stream providers failed: %v", lastErr)
	if !c.Writer.Written() {
		setProviderErrorHeaders(c, lastErr)
		c.JSON(routingErrorStatus(lastErr), gin.H{"error": lastErr.Error()})
	}
}

// streamFromProvider 向指定提供商发流式请求并透传 SSE 到客户端。
// 若提供商实现了 StreamProvider 接口（如 OpenAI 格式），走其自有流式逻辑（含格式转换）；
// 否则直接透传上游 SSE 流（适用于 Anthropic 兼容接口）。
func (h *MessageHandler) streamFromProvider(c *gin.Context, req *provider.Request, p provider.Provider) error {
	streamWriter := newDeferredStreamWriter(c)

	// 若提供商实现了 StreamProvider（如 OpenAI 格式），使用其内置流式逻辑
	if sp, ok := p.(provider.StreamProvider); ok {
		err := sp.ForwardStream(c.Request.Context(), req, streamWriter)
		if err == nil {
			streamWriter.Commit()
		}
		return err
	}

	// 默认：直接透传 Anthropic 兼容的 SSE 流
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, p.URL(), bytes.NewReader(body))
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

	client := &http.Client{} // 流式不设 Timeout，靠 context cancel 控制
	resp, err := client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("send: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return provider.NewHTTPError(p.Name(), resp, b)
	}

	// 逐行透传 SSE 流
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		if _, err := fmt.Fprintf(streamWriter, "%s\n", scanner.Bytes()); err != nil {
			return nil // 客户端断开，正常退出
		}
		streamWriter.Flush()
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	streamWriter.Commit()
	return nil
}
