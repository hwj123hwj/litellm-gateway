package handlers

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"

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
	// 先读原始 body，用于调试
	rawBody, _ := io.ReadAll(c.Request.Body)
	h.logger.Printf("Raw request body: %s", string(rawBody))
	// 把 body 放回去供 BindJSON 使用
	c.Request.Body = io.NopCloser(bytes.NewReader(rawBody))

	var req provider.Request
	if err := c.BindJSON(&req); err != nil {
		h.logger.Printf("BindJSON error: %v, body was: %s", err, string(rawBody))
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

	if req.Stream {
		h.handleStream(c, &req)
	} else {
		h.handleNonStream(c, &req)
	}
}

func (h *MessageHandler) handleNonStream(c *gin.Context, req *provider.Request) {
	resp, err := h.router.Forward(c.Request.Context(), req.Model, req)
	if err != nil {
		h.logger.Printf("Forward failed: %v", err)
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, resp)
}

func (h *MessageHandler) handleStream(c *gin.Context, req *provider.Request) {
	providerChain, err := h.router.RouteForStream(req.Model)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	originalModel := req.Model
	var lastErr error
	for i, p := range providerChain {
		h.logger.Printf("Stream: trying provider %d/%d: %s", i+1, len(providerChain), p.Name())
		req.Model = h.router.MapModel(originalModel, p.Name())
		if err := h.streamFromProvider(c, req, p); err == nil {
			return
		} else {
			h.logger.Printf("Stream provider %s failed: %v", p.Name(), err)
			lastErr = err
		}
	}

	h.logger.Printf("All stream providers failed: %v", lastErr)
	if !c.Writer.Written() {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "all providers failed"})
	}
}

// streamFromProvider 向指定提供商发流式请求并透传 SSE 到客户端。
// 若提供商实现了 StreamProvider 接口（如 OpenAI 格式），走其自有流式逻辑（含格式转换）；
// 否则直接透传上游 SSE 流（适用于 Anthropic 兼容接口）。
func (h *MessageHandler) streamFromProvider(c *gin.Context, req *provider.Request, p provider.Provider) error {
	// 设置 SSE 响应头（只写一次）
	if !c.Writer.Written() {
		c.Header("Content-Type", "text/event-stream")
		c.Header("Cache-Control", "no-cache")
		c.Header("Connection", "keep-alive")
		c.Header("X-Accel-Buffering", "no")
		c.Writer.WriteHeader(http.StatusOK)
	}

	// 若提供商实现了 StreamProvider（如 OpenAI 格式），使用其内置流式逻辑
	if sp, ok := p.(provider.StreamProvider); ok {
		return sp.ForwardStream(c.Request.Context(), req, c.Writer)
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
		return fmt.Errorf("provider %d: %s", resp.StatusCode, string(b))
	}

	// 逐行透传 SSE 流
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		if _, err := fmt.Fprintf(c.Writer, "%s\n", scanner.Bytes()); err != nil {
			return nil // 客户端断开，正常退出
		}
		c.Writer.Flush()
	}
	return scanner.Err()
}
