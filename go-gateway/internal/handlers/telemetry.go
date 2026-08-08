package handlers

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/weijian/go-llm-gateway/internal/provider"
	"github.com/weijian/go-llm-gateway/internal/requestmeta"
)

const maxRequestErrorBytes = 512

func setRequestMetadata(c *gin.Context, model string, stream bool) {
	c.Set(requestmeta.ModelKey, model)
	c.Set(requestmeta.RequestIsStreamKey, stream)
}

func setUsageMetadata(c *gin.Context, inputTokens, outputTokens int) {
	c.Set(requestmeta.InputTokensKey, inputTokens)
	c.Set(requestmeta.OutputTokensKey, outputTokens)
}

func setForwardMetadata(c *gin.Context, finalProvider string, attempts []requestmeta.ProviderAttempt, err error) {
	if finalProvider != "" {
		c.Set(requestmeta.ProviderKey, finalProvider)
	}
	if len(attempts) > 0 {
		c.Set(requestmeta.ProviderAttemptsKey, attempts)
	}
	if err != nil {
		c.Set(requestmeta.RequestErrorKey, summarizeRequestError(err))
	} else {
		c.Set(requestmeta.RequestErrorKey, "")
	}
}

func recordProviderAttempt(c *gin.Context, providerName string, started time.Time, err error) {
	attempt := requestmeta.ProviderAttempt{
		Provider:  providerName,
		Status:    "success",
		LatencyMs: float64(time.Since(started).Microseconds()) / 1000,
	}
	if err != nil {
		attempt.Status = "error"
		attempt.Error = summarizeRequestError(err)
		var providerErr *provider.ProviderError
		if errors.As(err, &providerErr) {
			attempt.StatusCode = providerErr.StatusCode
		}
		c.Set(requestmeta.RequestErrorKey, attempt.Error)
	} else {
		c.Set(requestmeta.RequestErrorKey, "")
	}

	attempts := providerAttempts(c)
	attempts = append(attempts, attempt)
	c.Set(requestmeta.ProviderAttemptsKey, attempts)
	c.Set(requestmeta.ProviderKey, providerName)
}

func providerAttempts(c *gin.Context) []requestmeta.ProviderAttempt {
	value, ok := c.Get(requestmeta.ProviderAttemptsKey)
	if !ok {
		return nil
	}
	attempts, _ := value.([]requestmeta.ProviderAttempt)
	return append([]requestmeta.ProviderAttempt(nil), attempts...)
}

func summarizeRequestError(err error) string {
	if err == nil {
		return ""
	}
	var providerErr *provider.ProviderError
	if errors.As(err, &providerErr) {
		providerName := providerErr.Provider
		if providerName == "" {
			providerName = "upstream"
		}
		message := providerErr.Message
		if message == "" {
			message = "request failed"
		}
		if providerErr.StatusCode > 0 {
			return truncateRequestError(fmt.Sprintf("%s HTTP %d: %s", providerName, providerErr.StatusCode, message))
		}
		return truncateRequestError(fmt.Sprintf("%s: %s", providerName, message))
	}
	return truncateRequestError(err.Error())
}

func truncateRequestError(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= maxRequestErrorBytes {
		return value
	}
	return value[:maxRequestErrorBytes] + "..."
}

func logRequestSummary(logger interface{ Printf(string, ...interface{}) }, c *gin.Context, endpoint string, size int) {
	logger.Printf("[%s] request_id=%s body_bytes=%d", endpoint, c.GetString(requestmeta.RequestIDKey), size)
}
