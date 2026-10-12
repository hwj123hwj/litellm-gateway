package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"strings"
	"testing"
	"time"
)

type overridePolicyProvider struct {
	errorPolicyProvider
	plain, reasoning time.Duration
}

func (p *overridePolicyProvider) FirstOutputTimeoutOverride() (time.Duration, time.Duration) {
	return p.plain, p.reasoning
}

func TestFirstOutputTimeoutOverridePerPhase(t *testing.T) {
	r := NewRouter(log.New(io.Discard, "", 0))
	p := &overridePolicyProvider{
		errorPolicyProvider: errorPolicyProvider{name: "glm"},
		plain:               10 * time.Second,
		reasoning:           180 * time.Second,
	}

	reasoningReq := &Request{Model: "m"}
	reasoningReq.raw = map[string]json.RawMessage{"thinking": json.RawMessage(`{"type":"enabled"}`)}
	limit, overridden := r.firstOutputTimeout(reasoningReq, p)
	if !overridden || limit != 180*time.Second {
		t.Fatalf("reasoning override = %s (overridden=%v), want 180s", limit, overridden)
	}

	plainReq := &Request{Model: "m"}
	limit, overridden = r.firstOutputTimeout(plainReq, p)
	if !overridden || limit != 10*time.Second {
		t.Fatalf("plain override = %s (overridden=%v), want 10s", limit, overridden)
	}

	// 未配置覆盖的 provider 维持全局策略，且不触发预算抬升。
	limit, overridden = r.firstOutputTimeout(reasoningReq, &errorPolicyProvider{name: "plain"})
	if overridden || limit != DefaultRoutingPolicy().ReasoningFirstOutputTimeout {
		t.Fatalf("no-override reasoning = %s (overridden=%v), want global default", limit, overridden)
	}
}

func TestStreamAttemptOverrideLiftsWaitingBudget(t *testing.T) {
	// 预算 50ms 远小于思考档覆盖 250ms：覆盖生效时请求必须活到覆盖期限，
	// 输出在 100ms 到达，不被默认预算在 50ms 钳死。
	r := NewRouter(log.New(io.Discard, "", 0))
	r.SetRoutingPolicy(RoutingPolicy{FirstOutputTimeout: 80 * time.Millisecond, ReasoningFirstOutputTimeout: 80 * time.Millisecond, StreamIdleTimeout: time.Second, AttemptTimeout: time.Second, FallbackWaitBudget: 50 * time.Millisecond})

	req := &Request{Model: "m"}
	req.raw = map[string]json.RawMessage{"thinking": json.RawMessage(`{"type":"enabled"}`)}
	p := &overridePolicyProvider{
		errorPolicyProvider: errorPolicyProvider{name: "glm"},
		reasoning:           250 * time.Millisecond,
	}
	var dst bytes.Buffer
	_, err := r.RunStreamAttempt(context.Background(), req, p, &dst, func(ctx context.Context, w io.Writer) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
		_, err := io.WriteString(w, progressFrame+terminalFrame)
		return err
	})
	if err != nil || !strings.Contains(dst.String(), "message_stop") {
		t.Fatalf("override should keep the attempt alive past the default budget: err=%v output=%s", err, dst.String())
	}

	// 未配置覆盖的同样场景仍被预算钳回，保持原语义。
	var dst2 bytes.Buffer
	_, err = r.RunStreamAttempt(context.Background(), req, &errorPolicyProvider{name: "slow"}, &dst2, func(ctx context.Context, w io.Writer) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
		_, werr := io.WriteString(w, progressFrame+terminalFrame)
		return werr
	})
	var pe *ProviderError
	if !errors.As(err, &pe) || !strings.Contains(pe.Message, "fallback wait budget") {
		t.Fatalf("without override the budget must still cap the attempt: err=%v", err)
	}
}

func TestProviderConfigWiresFirstOutputOverride(t *testing.T) {
	t.Setenv("GLM_API_KEY", "glm-test-key")

	configured, err := NewOpenAIProviderFromConfig(&ProviderConfig{
		Name:                               "glm",
		Type:                               "openai",
		URL:                                "https://example.invalid/v1/chat/completions",
		APIKeyEnv:                          "GLM_API_KEY",
		ReasoningFirstOutputTimeoutSeconds: 180,
	})
	if err != nil {
		t.Fatalf("expected provider to configure: %v", err)
	}
	openAI, ok := configured.(*OpenAIProvider)
	if !ok {
		t.Fatalf("expected *OpenAIProvider, got %T", configured)
	}
	plain, reasoning := openAI.FirstOutputTimeoutOverride()
	if plain != 0 || reasoning != 180*time.Second {
		t.Fatalf("override = (%s, %s), want (0, 180s)", plain, reasoning)
	}

	// 路由层实际看到的是绑定模型的 wrapper：覆盖必须经接口提升透出。
	wrapped := NewBoundModelProviderWrapper(openAI, "glm-5.3")
	o, ok := Provider(wrapped).(FirstOutputOverrideProvider)
	if !ok {
		t.Fatal("BoundModelProviderWrapper should promote FirstOutputOverrideProvider")
	}
	if _, reasoning := o.FirstOutputTimeoutOverride(); reasoning != 180*time.Second {
		t.Fatalf("wrapped override = %s, want 180s", reasoning)
	}
}
