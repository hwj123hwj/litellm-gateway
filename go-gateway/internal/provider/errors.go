package provider

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

const maxProviderErrorMessageBytes = 4096

// ProviderError preserves the useful part of an upstream HTTP failure so the
// gateway can return the correct status code and make an informed fallback
// decision without parsing human-readable error strings.
type ProviderError struct {
	Provider   string
	StatusCode int
	Message    string
	RequestID  string
	RetryAfter string
	Cause      error
}

func (e *ProviderError) Error() string {
	if e == nil {
		return "provider error"
	}
	prefix := "provider"
	if e.Provider != "" {
		prefix = fmt.Sprintf("provider %s", e.Provider)
	}
	if e.StatusCode > 0 {
		prefix = fmt.Sprintf("%s returned HTTP %d", prefix, e.StatusCode)
	}
	if e.Message != "" {
		return fmt.Sprintf("%s: %s", prefix, e.Message)
	}
	return prefix
}

func (e *ProviderError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// HTTPStatus returns the upstream status when it is a valid HTTP error code.
// Unknown/network failures are represented to clients as 502 by the handler.
func (e *ProviderError) HTTPStatus() int {
	if e == nil || e.StatusCode < http.StatusBadRequest || e.StatusCode > 599 {
		return http.StatusBadGateway
	}
	return e.StatusCode
}

// Retryable reports whether trying another configured provider is reasonable.
// Client/auth/model errors must not be hidden by a fallback provider.
func (e *ProviderError) Retryable() bool {
	if e == nil {
		return false
	}
	switch e.StatusCode {
	case http.StatusRequestTimeout, http.StatusConflict, 425, http.StatusTooManyRequests:
		return true
	default:
		return e.StatusCode >= 500
	}
}

// NewHTTPError converts an upstream HTTP response into a typed error. Only a
// bounded, human-readable message is kept; callers should not log the entire
// response body because it can contain sensitive provider details.
func NewHTTPError(providerName string, resp *http.Response, body []byte) *ProviderError {
	err := &ProviderError{Provider: providerName, Message: extractProviderErrorMessage(body)}
	if resp == nil {
		return err
	}
	err.StatusCode = resp.StatusCode
	err.RequestID = firstHeader(resp.Header, "X-Request-ID", "Request-ID", "X-Amzn-RequestId")
	err.RetryAfter = resp.Header.Get("Retry-After")
	return err
}

// ShouldFallback centralizes fallback policy for both streaming and
// non-streaming handlers. Untyped provider/network errors remain retryable for
// backwards compatibility; typed client errors explicitly opt out.
func ShouldFallback(err error) bool {
	if err == nil {
		return false
	}
	var providerErr *ProviderError
	if errors.As(err, &providerErr) {
		return providerErr.Retryable()
	}
	var unsupported *UnsupportedCapabilityError
	if errors.As(err, &unsupported) {
		return false
	}
	var unknown *UnknownModelError
	if errors.As(err, &unknown) {
		return false
	}
	var unavailable *NoAvailableProvidersError
	if errors.As(err, &unavailable) {
		return false
	}
	return true
}

// UnknownModelError is returned when a client asks for a model that is not
// registered in the router.
type UnknownModelError struct {
	Model string
}

func (e *UnknownModelError) Error() string {
	return fmt.Sprintf("unknown model: %s", e.Model)
}

// NoAvailableProvidersError indicates that a known model has no configured
// provider instance available at runtime.
type NoAvailableProvidersError struct {
	Model      string
	Configured []string
}

func (e *NoAvailableProvidersError) Error() string {
	return fmt.Sprintf("no available providers for model %s (configured: %v)", e.Model, e.Configured)
}

func firstHeader(header http.Header, names ...string) string {
	for _, name := range names {
		if value := header.Get(name); value != "" {
			return value
		}
	}
	return ""
}

func extractProviderErrorMessage(body []byte) string {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return "upstream request failed"
	}

	var envelope struct {
		Error   json.RawMessage `json:"error"`
		Message string          `json:"message"`
	}
	if err := json.Unmarshal(body, &envelope); err == nil {
		if len(envelope.Error) > 0 && string(envelope.Error) != "null" {
			var nested struct {
				Message string `json:"message"`
			}
			if err := json.Unmarshal(envelope.Error, &nested); err == nil && nested.Message != "" {
				return truncateErrorMessage(nested.Message)
			}
			var nestedText string
			if err := json.Unmarshal(envelope.Error, &nestedText); err == nil && nestedText != "" {
				return truncateErrorMessage(nestedText)
			}
		}
		if envelope.Message != "" {
			return truncateErrorMessage(envelope.Message)
		}
	}
	return truncateErrorMessage(trimmed)
}

func truncateErrorMessage(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= maxProviderErrorMessageBytes {
		return value
	}
	return value[:maxProviderErrorMessageBytes] + "..."
}
