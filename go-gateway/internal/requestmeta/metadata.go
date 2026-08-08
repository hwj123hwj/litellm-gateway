// Package requestmeta contains the small, protocol-neutral metadata contract
// shared by handlers, middleware, routing, and metrics persistence.
package requestmeta

const (
	RequestIDKey        = "request_id"
	ModelKey            = "request_model"
	ProviderKey         = "request_provider"
	ProviderAttemptsKey = "request_provider_attempts"
	RequestErrorKey     = "request_error"
	RequestIsStreamKey  = "request_is_stream"
	InputTokensKey      = "request_input_tokens"
	OutputTokensKey     = "request_output_tokens"
)

// ProviderAttempt describes one provider selected by the router for a
// logical request. It intentionally contains only bounded metadata; request
// and response bodies never belong in metrics.
type ProviderAttempt struct {
	Provider   string  `json:"provider"`
	Status     string  `json:"status"` // success, error, or skipped
	StatusCode int     `json:"status_code,omitempty"`
	LatencyMs  float64 `json:"latency_ms"`
	Error      string  `json:"error,omitempty"`
}
