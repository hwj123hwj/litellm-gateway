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

// DefaultRoutingPolicy 的思考档首字与等待预算是一对：预算必须容得下思考档
// 首字期限（glm 等始终思考模型带大上下文时，首 token 前的思考可能远超 45s），
// 否则更长的首字期限会被预算悄悄钳回，等于没放宽。
func DefaultRoutingPolicy() RoutingPolicy {
	return RoutingPolicy{15 * time.Second, 120 * time.Second, 30 * time.Second, 30 * time.Second, 180 * time.Second}
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

// extendWaitingBudget 把本请求的共享等待预算抬升到至少 limit。per-provider
// 首字覆盖表达的是运维对该档的显式等待取舍，被默认 fallback 预算钳回会让
// 覆盖不生效；只做抬升，未配置覆盖的 provider 预算语义不变。
func (r *Router) extendWaitingBudget(ctx context.Context, limit time.Duration) {
	state, _ := ctx.Value(routingDeadlineKey{}).(*routingState)
	if state == nil {
		return
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if needed := time.Now().Add(limit); needed.After(state.deadline) {
		state.deadline = needed
	}
}

// firstOutputTimeout 返回本次尝试的首字期限，以及它是否来自 per-provider
// 显式覆盖（调用方需要据此抬升共享等待预算）。
func (r *Router) firstOutputTimeout(req *Request, p Provider) (time.Duration, bool) {
	r.mu.RLock()
	policy := r.policy
	r.mu.RUnlock()
	reasoning := false
	if req != nil {
		for _, field := range []string{"thinking", "reasoning", "reasoning_effort"} {
			if value, ok := req.RawField(field); ok && string(value) != "null" && string(value) != `"none"` && string(value) != `"off"` {
				reasoning = true
				break
			}
		}
	}
	if !reasoning {
		if cp, ok := p.(CapabilityProvider); ok {
			for _, c := range cp.Capabilities() {
				if c == CapabilityReasoning {
					reasoning = true
					break
				}
			}
		}
	}
	if o, ok := p.(FirstOutputOverrideProvider); ok {
		plain, reasoningOverride := o.FirstOutputTimeoutOverride()
		if reasoning && reasoningOverride > 0 {
			return reasoningOverride, true
		}
		if !reasoning && plain > 0 {
			return plain, true
		}
	}
	if reasoning {
		return policy.ReasoningFirstOutputTimeout, false
	}
	return policy.FirstOutputTimeout, false
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
