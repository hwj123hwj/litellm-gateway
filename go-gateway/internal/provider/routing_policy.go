package provider

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"
)

// RoutingPolicy bounds waiting, not the duration of a progressing stream.
type RoutingPolicy struct {
	FirstOutputTimeout          time.Duration
	ReasoningFirstOutputTimeout time.Duration
	StreamIdleTimeout           time.Duration
	AttemptTimeout              time.Duration
	FallbackWaitBudget          time.Duration
}

func DefaultRoutingPolicy() RoutingPolicy {
	return RoutingPolicy{15 * time.Second, 45 * time.Second, 30 * time.Second, 30 * time.Second, 60 * time.Second}
}

func (r *Router) SetRoutingPolicy(p RoutingPolicy) {
	d := DefaultRoutingPolicy()
	if p.FirstOutputTimeout <= 0 {
		p.FirstOutputTimeout = d.FirstOutputTimeout
	}
	if p.ReasoningFirstOutputTimeout <= 0 {
		p.ReasoningFirstOutputTimeout = d.ReasoningFirstOutputTimeout
	}
	if p.StreamIdleTimeout <= 0 {
		p.StreamIdleTimeout = d.StreamIdleTimeout
	}
	if p.AttemptTimeout <= 0 {
		p.AttemptTimeout = d.AttemptTimeout
	}
	if p.FallbackWaitBudget <= 0 {
		p.FallbackWaitBudget = d.FallbackWaitBudget
	}
	r.mu.Lock()
	r.policy = p
	r.mu.Unlock()
}

var ErrRoutingBudgetExceeded = errors.New("fallback wait budget exhausted")

type routingDeadlineKey struct{}

type routingState struct {
	deadline  time.Time
	mu        sync.Mutex
	attempted map[string]bool
}

// ProviderAttempted prevents retrying a failed native Responses provider through a second protocol path.
func (r *Router) ProviderAttempted(ctx context.Context, model string, p Provider) bool {
	state, _ := ctx.Value(routingDeadlineKey{}).(*routingState)
	if state == nil {
		return false
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	return state.attempted[r.providerRuntimeName(model, p)]
}
func (r *Router) MarkProviderAttempted(ctx context.Context, model string, p Provider) {
	state, _ := ctx.Value(routingDeadlineKey{}).(*routingState)
	if state == nil {
		return
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	state.attempted[r.providerRuntimeName(model, p)] = true
}

// BeginRouting shares one waiting budget across all attempts of a request.
func (r *Router) BeginRouting(ctx context.Context) context.Context {
	if _, ok := ctx.Value(routingDeadlineKey{}).(*routingState); ok {
		return ctx
	}
	r.mu.RLock()
	budget := r.policy.FallbackWaitBudget
	r.mu.RUnlock()
	return context.WithValue(ctx, routingDeadlineKey{}, &routingState{deadline: time.Now().Add(budget), attempted: make(map[string]bool)})
}

func (r *Router) waitingTimeout(ctx context.Context, limit time.Duration) time.Duration {
	if deadline, ok := ctx.Value(routingDeadlineKey{}).(*routingState); ok {
		if remaining := time.Until(deadline.deadline); remaining < limit {
			return remaining
		}
	}
	return limit
}

func routingTimeout(name, phase string) *ProviderError {
	cause := error(context.DeadlineExceeded)
	if phase == "fallback wait budget" {
		cause = ErrRoutingBudgetExceeded
	}
	return &ProviderError{Provider: name, StatusCode: http.StatusGatewayTimeout, Message: "upstream " + phase + " timeout", Cause: cause}
}

// RunNonStreamAttempt applies both a per-attempt timeout and the shared budget.
func (r *Router) RunNonStreamAttempt(ctx context.Context, name string, forward func(context.Context) error) error {
	ctx = r.BeginRouting(ctx)
	r.mu.RLock()
	limit := r.policy.AttemptTimeout
	r.mu.RUnlock()
	phase := "response"
	bounded := r.waitingTimeout(ctx, limit)
	if bounded < limit {
		phase = "fallback wait budget"
	}
	limit = bounded
	if limit <= 0 {
		return routingTimeout(name, "fallback wait budget")
	}
	child, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	err := forward(child)
	if ctx.Err() != nil {
		return &RequestCanceledError{Cause: ctx.Err()}
	}
	if child.Err() != nil {
		return routingTimeout(name, phase)
	}
	if err == context.DeadlineExceeded {
		return routingTimeout(name, "response")
	}
	return err
}

func (r *Router) firstOutputTimeout(req *Request, p Provider) time.Duration {
	r.mu.RLock()
	policy := r.policy
	r.mu.RUnlock()
	if req != nil {
		for _, field := range []string{"thinking", "reasoning", "reasoning_effort"} {
			if value, ok := req.RawField(field); ok && string(value) != "null" && string(value) != `"none"` && string(value) != `"off"` {
				return policy.ReasoningFirstOutputTimeout
			}
		}
	}
	if cp, ok := p.(CapabilityProvider); ok {
		for _, c := range cp.Capabilities() {
			if c == CapabilityReasoning {
				return policy.ReasoningFirstOutputTimeout
			}
		}
	}
	return policy.FirstOutputTimeout
}

// Client cancellation must not poison a healthy provider's circuit.
func isClientCancellation(err error) bool {
	var canceled *RequestCanceledError
	return errors.Is(err, context.Canceled) || errors.As(err, &canceled)
}

// RequestCanceledError separates a client's deadline from an upstream timeout.
type RequestCanceledError struct{ Cause error }

func (e *RequestCanceledError) Error() string { return "client request ended: " + e.Cause.Error() }
func (e *RequestCanceledError) Unwrap() error { return e.Cause }
