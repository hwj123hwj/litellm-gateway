package provider

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"testing"
	"time"
)

const progressFrame = "data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"OK\"}}\n\n"
const terminalFrame = "data: {\"type\":\"message_stop\"}\n\n"

func testPolicyRouter() *Router {
	r := NewRouter(log.New(io.Discard, "", 0))
	r.SetRoutingPolicy(RoutingPolicy{FirstOutputTimeout: 30 * time.Millisecond, ReasoningFirstOutputTimeout: 150 * time.Millisecond, StreamIdleTimeout: 70 * time.Millisecond, AttemptTimeout: 40 * time.Millisecond, FallbackWaitBudget: 150 * time.Millisecond})
	return r
}
func TestStreamGuardCancelsPreludeWithoutCommitting(t *testing.T) {
	r := testPolicyRouter()
	p := &errorPolicyProvider{name: "slow"}
	var dst bytes.Buffer
	timing, err := r.RunStreamAttempt(context.Background(), &Request{}, p, &dst, func(ctx context.Context, w io.Writer) error {
		if _, err := io.WriteString(w, "data: {\"type\":\"message_start\"}\n\n: ping\n\n"); err != nil {
			return err
		}
		<-ctx.Done()
		return ctx.Err()
	})
	var pe *ProviderError
	if !errors.As(err, &pe) || pe.StatusCode != 504 || dst.Len() != 0 || timing.FirstOutputMs != nil {
		t.Fatalf("err=%v body=%s timing=%v", err, dst.String(), timing)
	}
	if !ShouldFallback(err) {
		t.Fatal("first output timeout must allow fallback")
	}
}
func TestStreamGuardProgressOutlivesWaitingBudget(t *testing.T) {
	r := testPolicyRouter()
	r.SetRoutingPolicy(RoutingPolicy{FirstOutputTimeout: 100 * time.Millisecond, StreamIdleTimeout: 200 * time.Millisecond, FallbackWaitBudget: 40 * time.Millisecond})
	var dst bytes.Buffer
	timing, err := r.RunStreamAttempt(context.Background(), &Request{}, &errorPolicyProvider{name: "long"}, &dst, func(ctx context.Context, w io.Writer) error {
		for i := 0; i < 5; i++ {
			if _, err := io.WriteString(w, progressFrame); err != nil {
				return err
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(20 * time.Millisecond):
			}
		}
		_, err := io.WriteString(w, terminalFrame)
		return err
	})
	if err != nil || timing.FirstOutputMs == nil || !bytes.Contains(dst.Bytes(), []byte("message_stop")) {
		t.Fatalf("err=%v output=%s timing=%v", err, dst.String(), timing)
	}
}
func TestStreamGuardKeepaliveDoesNotResetIdle(t *testing.T) {
	r := testPolicyRouter()
	var dst bytes.Buffer
	_, err := r.RunStreamAttempt(context.Background(), &Request{}, &errorPolicyProvider{name: "stuck"}, &dst, func(ctx context.Context, w io.Writer) error {
		io.WriteString(w, progressFrame)
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(10 * time.Millisecond):
				if _, err := io.WriteString(w, ": ping\n\n"); err != nil {
					return err
				}
			}
		}
	})
	var pe *ProviderError
	if !errors.As(err, &pe) || pe.Message != "upstream stream idle timeout" || dst.Len() == 0 {
		t.Fatalf("%v output=%s", err, dst.String())
	}
}
func TestReasoningGetsSeparateFirstOutputLimit(t *testing.T) {
	r := testPolicyRouter()
	req := newProbeRequest("reason", "hi")
	req.raw["reasoning_effort"] = []byte(`"high"`)
	var dst bytes.Buffer
	timing, err := r.RunStreamAttempt(context.Background(), req, &errorPolicyProvider{name: "reason"}, &dst, func(ctx context.Context, w io.Writer) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(55 * time.Millisecond):
		}
		_, err := io.WriteString(w, "data: {\"type\":\"content_block_delta\",\"delta\":{\"thinking\":\"hmm\"}}\n\n"+terminalFrame)
		return err
	})
	if err != nil || timing.FirstOutputMs == nil {
		t.Fatalf("reasoning failed: %v", err)
	}
}
func TestSharedBudgetAndClientCancellationDoNotPoisonCircuit(t *testing.T) {
	r := testPolicyRouter()
	p := &errorPolicyProvider{name: "p"}
	r.RegisterProvider("p", p)
	ctx := r.BeginRouting(context.Background())
	err := r.RunNonStreamAttempt(ctx, "p", func(c context.Context) error { <-c.Done(); return c.Err() })
	if !ShouldFallback(err) {
		t.Fatalf("attempt timeout should fall back: %v", err)
	}
	r.SetRoutingPolicy(RoutingPolicy{FallbackWaitBudget: time.Millisecond})
	expired := r.BeginRouting(context.Background())
	time.Sleep(3 * time.Millisecond)
	called := false
	err = r.RunNonStreamAttempt(expired, "p", func(context.Context) error { called = true; return nil })
	if called || ShouldFallback(err) || !errors.Is(err, ErrRoutingBudgetExceeded) {
		t.Fatalf("budget err=%v called=%v", err, called)
	}
	r.RecordProviderFailure("p", err)
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	err = r.RunNonStreamAttempt(canceled, "p", func(c context.Context) error { return c.Err() })
	r.RecordProviderFailure("p", err)
	if ShouldFallback(err) || r.runtime["p"].breaker.Snapshot().TotalFailures != 0 {
		t.Fatalf("cancellation affected circuit: %v", err)
	}
}
func TestRateLimitImmediatelyCoolsAndReservesSingleRecovery(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	b := NewCircuitBreaker(CircuitBreakerConfig{FailureThreshold: 3, RateLimitInitial: 30 * time.Second, RateLimitMax: 120 * time.Second})
	b.RecordRateLimitAt(now, "10")
	b.RecordSuccessAt(now.Add(time.Second))
	if b.AllowAt(now.Add(9 * time.Second)) {
		t.Fatal("429 or a late success did not preserve cooldown")
	}
	if !b.AllowAt(now.Add(10*time.Second)) || b.AllowAt(now.Add(10*time.Second)) {
		t.Fatal("expected exactly one half-open probe")
	}
	b.RecordRateLimitAt(now.Add(10*time.Second), "invalid")
	if want := now.Add(70 * time.Second); !b.SnapshotAt(now.Add(10 * time.Second)).NextRetryAt.Equal(want) {
		t.Fatalf("expected exponential retry %s got %v", want, b.Snapshot())
	}
	b.RecordRateLimitAt(now.Add(11*time.Second), "1")
	if b.Snapshot().NextRetryAt.Before(now.Add(70 * time.Second)) {
		t.Fatal("parallel short 429 shortened cooldown")
	}
	b.Reset()
	date := now.Add(45 * time.Second)
	b.RecordRateLimitAt(now, date.Format(http.TimeFormat))
	if !b.SnapshotAt(now).NextRetryAt.Equal(date) {
		t.Fatal("HTTP-date Retry-After ignored")
	}
}
func TestOptionalChannelCooldownAndNativeSkip(t *testing.T) {
	r := testPolicyRouter()
	for _, name := range []string{"a", "b", "c"} {
		r.RegisterProvider(name, &errorPolicyProvider{name: name})
	}
	r.RegisterChain("coding", []string{"a", "b", "c"})
	r.SetProviderRateLimitGroup("a", "shared")
	r.SetProviderRateLimitGroup("b", "shared")
	r.RecordProviderFailure("a", &ProviderError{StatusCode: 429, RetryAfter: "60"})
	chain, err := r.Route("coding")
	if err != nil || len(chain) != 1 || chain[0].Name() != "c" {
		t.Fatalf("chain=%v err=%v", chain, err)
	}
	ctx := r.BeginRouting(context.Background())
	r.MarkProviderAttempted(ctx, "coding", chain[0])
	if !r.ProviderAttempted(ctx, "coding", chain[0]) || r.ProviderAttempted(context.Background(), "coding", chain[0]) {
		t.Fatal("request-local attempt tracking failed")
	}
}
func TestProgressClassification(t *testing.T) {
	for _, tt := range []struct {
		frame              string
		progress, terminal bool
	}{
		{": ping", false, false}, {`data: {"type":"message_start"}`, false, false},
		{`data: {"type":"content_block_start","content_block":{"type":"tool_use","name":"read"}}`, true, false},
		{`data: {"type":"response.function_call_arguments.delta","delta":"{}"}`, true, false},
		{`data: {"type":"response.reasoning_summary_text.delta","delta":"thinking"}`, true, false},
		{`data: {"type":"response.completed"}`, false, true},
	} {
		p, end := sseProgress([]byte(tt.frame))
		if p != tt.progress || end != tt.terminal {
			t.Errorf("%s => %v %v", tt.frame, p, end)
		}
	}
}

type waitingPolicyProvider struct{ errorPolicyProvider }

func (p *waitingPolicyProvider) ForwardRequest(ctx context.Context, _ *Request) (*Response, error) {
	p.calls++
	<-ctx.Done()
	return nil, ctx.Err()
}
func TestFallbackChainSharesBudgetAndDoesNotStartThirdCall(t *testing.T) {
	r := testPolicyRouter()
	r.SetRoutingPolicy(RoutingPolicy{AttemptTimeout: 40 * time.Millisecond, FallbackWaitBudget: 65 * time.Millisecond})
	a := &waitingPolicyProvider{errorPolicyProvider: errorPolicyProvider{name: "a"}}
	b := &waitingPolicyProvider{errorPolicyProvider: errorPolicyProvider{name: "b"}}
	c := &errorPolicyProvider{name: "c", resp: &Response{}}
	r.RegisterProvider("a", a)
	r.RegisterProvider("b", b)
	r.RegisterProvider("c", c)
	r.RegisterChain("coding", []string{"a", "b", "c"})
	started := time.Now()
	_, _, attempts, err := r.ForwardWithDetails(context.Background(), "coding", newProbeRequest("coding", "hi"))
	if !errors.Is(err, ErrRoutingBudgetExceeded) || a.calls != 1 || b.calls != 1 || c.calls != 0 || len(attempts) != 2 {
		t.Fatalf("err=%v calls=%d/%d/%d attempts=%+v", err, a.calls, b.calls, c.calls, attempts)
	}
	if time.Since(started) > 500*time.Millisecond {
		t.Fatal("chain exceeded shared budget")
	}
	if r.runtime["a"].breaker.Snapshot().TotalFailures != 1 || r.runtime["b"].breaker.Snapshot().TotalFailures != 0 {
		t.Fatal("budget exhaustion blamed last provider")
	}
}
func TestStreamFailureEventDoesNotCountAsSuccess(t *testing.T) {
	r := testPolicyRouter()
	var dst bytes.Buffer
	_, err := r.RunStreamAttempt(context.Background(), &Request{}, &errorPolicyProvider{name: "failed"}, &dst, func(_ context.Context, w io.Writer) error {
		_, err := io.WriteString(w, "data: {\"type\":\"response.failed\"}\n\n")
		return err
	})
	if err == nil || dst.Len() != 0 {
		t.Fatalf("failed response err=%v body=%s", err, dst.String())
	}
}
