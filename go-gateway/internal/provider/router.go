package provider

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"reflect"
	"sort"
	"sync"
	"time"

	"github.com/weijian/go-llm-gateway/internal/requestmeta"
)

// Router 管理提供商路由和 fallback
type Router struct {
	providers      map[string]Provider
	chains         map[string][]string // 模型名 -> 提供商名列表
	models         map[string]ModelInfo
	runtime        map[string]*providerRuntime
	modelOverrides map[string]modelCapabilityOverride
	circuitConfig  CircuitBreakerConfig
	mu             sync.RWMutex
	logger         *log.Logger
}

type providerRuntime struct {
	enabled bool
	breaker *CircuitBreaker
}

type modelCapabilityOverride struct {
	capabilities    []string
	inputModalities []string
}

// RuntimeProviderStatus is the redacted provider state returned by the admin
// API. It never includes URL, API key, or any other credential material.
type RuntimeProviderStatus struct {
	Name                string
	Enabled             bool
	State               CircuitState
	Status              string
	ConsecutiveFailures int
	TotalFailures       int
	TotalSuccesses      int
	HasResult           bool
	LastFailureAt       time.Time
	LastSuccessAt       time.Time
	OpenedAt            time.Time
	NextRetryAt         time.Time
}

type RouteProviderStatus struct {
	Name    string       `json:"name"`
	Enabled bool         `json:"enabled"`
	State   CircuitState `json:"state"`
	Status  string       `json:"status"`
}

type RouteStatus struct {
	Model     string                `json:"model"`
	Providers []RouteProviderStatus `json:"providers"`
}

var (
	ErrUnknownProvider = errors.New("unknown provider")
	ErrUnknownChain    = errors.New("unknown model route")
)

// NewRouter 创建新的路由器
func NewRouter(logger *log.Logger) *Router {
	return NewRouterWithCircuitConfig(logger, defaultCircuitBreakerConfig)
}

// NewRouterWithCircuitConfig creates a router with explicit circuit settings.
// The default constructor remains compatible with existing embedders.
func NewRouterWithCircuitConfig(logger *log.Logger, circuitConfig CircuitBreakerConfig) *Router {
	if logger == nil {
		logger = log.New(io.Discard, "", 0)
	}
	return &Router{
		providers:      make(map[string]Provider),
		chains:         make(map[string][]string),
		models:         make(map[string]ModelInfo),
		runtime:        make(map[string]*providerRuntime),
		modelOverrides: make(map[string]modelCapabilityOverride),
		circuitConfig:  circuitConfig,
		logger:         logger,
	}
}

// RegisterProvider 注册提供商
func (r *Router) RegisterProvider(name string, p Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers[name] = p
	if existing, ok := r.runtime[name]; ok {
		// Preserve an administrator's enabled/disabled choice across a provider
		// refresh while replacing the circuit for the new client instance.
		existing.breaker = NewCircuitBreaker(r.circuitConfig)
	} else {
		r.runtime[name] = &providerRuntime{
			enabled: true,
			breaker: NewCircuitBreaker(r.circuitConfig),
		}
	}
	r.logger.Printf("Registered provider: %s", name)
}

// RegisterChain 注册 fallback 链
func (r *Router) RegisterChain(modelName string, providers []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.chains[modelName] = append([]string(nil), providers...)
	if _, exists := r.models[modelName]; !exists {
		r.models[modelName] = r.inferChainModelInfoLocked(modelName, providers)
	}
	r.logger.Printf("Registered chain: %s -> %v", modelName, providers)
}

func (r *Router) inferChainModelInfoLocked(modelName string, providerNames []string) ModelInfo {
	capabilities := make([]string, 0)
	seen := make(map[string]bool)
	for _, providerName := range providerNames {
		configured, exists := r.providers[providerName]
		if !exists {
			continue
		}
		declared := []string{CapabilityText}
		if capable, ok := configured.(CapabilityProvider); ok {
			declared = capable.Capabilities()
		}
		for _, capability := range declared {
			if capability == "" || seen[capability] {
				continue
			}
			seen[capability] = true
			capabilities = append(capabilities, capability)
		}
	}
	if len(capabilities) == 0 {
		capabilities = []string{CapabilityText}
	}
	return ModelInfo{
		ID:              modelName,
		Provider:        "router",
		Capabilities:    capabilities,
		InputModalities: modelInputModalities(capabilities, nil),
	}
}

// RegisterModel records public metadata for a model or alias. It is separate
// from RegisterChain so routing remains compatible with manually registered
// providers while /v1/models can expose capabilities and limits.
func (r *Router) RegisterModel(info ModelInfo) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if info.ID == "" {
		return
	}
	if len(info.Capabilities) == 0 {
		info.Capabilities = []string{CapabilityText}
	}
	info.Capabilities = append([]string(nil), info.Capabilities...)
	info.InputModalities = append([]string(nil), info.InputModalities...)
	r.models[info.ID] = info
}

// Route 返回模型对应的提供商链
func (r *Router) Route(modelName string) ([]Provider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	providerNames, ok := r.chains[modelName]
	if !ok {
		return nil, &UnknownModelError{Model: modelName}
	}

	var result []Provider
	var unavailable []string
	for _, name := range providerNames {
		p, ok := r.providers[name]
		if !ok {
			unavailable = append(unavailable, name)
			continue
		}
		if runtime := r.runtime[name]; runtime != nil {
			if !runtime.enabled || !runtime.breaker.CanAttempt() {
				unavailable = append(unavailable, name)
				continue
			}
		}
		result = append(result, p)
	}
	if len(result) == 0 {
		return nil, &NoAvailableProvidersError{Model: modelName, Configured: append([]string(nil), providerNames...), Reason: "disabled, unavailable, or circuit open"}
	}
	if len(unavailable) > 0 {
		r.logger.Printf("Skipping unavailable providers for model %s: %v", modelName, unavailable)
	}
	return result, nil
}

// RouteForRequest applies capability filtering before a request reaches a
// provider. If no configured provider supports the request, returning a client
// error is safer than dropping an image and retrying a text-only provider.
func (r *Router) RouteForRequest(modelName string, req *Request) ([]Provider, error) {
	providers, err := r.Route(modelName)
	if err != nil {
		return nil, err
	}
	if req == nil {
		return providers, nil
	}
	required := req.RequiredCapabilities()
	r.mu.RLock()
	override, hasOverride := r.modelOverrides[modelName]
	r.mu.RUnlock()
	if hasOverride && !capabilitiesSupportRequired(override.capabilities, required) {
		return nil, &UnsupportedCapabilityError{Model: modelName, Required: required}
	}
	filtered := make([]Provider, 0, len(providers))
	for _, p := range providers {
		if supportsCapabilities(p, required) {
			filtered = append(filtered, p)
			continue
		}
		r.logger.Printf("Skipping provider %s for model %s: unsupported capabilities %v", p.Name(), modelName, required)
	}
	if len(filtered) == 0 {
		return nil, &UnsupportedCapabilityError{Model: modelName, Required: required}
	}
	return filtered, nil
}

func capabilitiesSupportRequired(declared, required []string) bool {
	set := make(map[string]bool, len(declared))
	for _, capability := range declared {
		set[capability] = true
	}
	for _, capability := range required {
		if !set[capability] {
			return false
		}
	}
	return true
}

// RouteForStream 返回流式请求的提供商链（功能同 Route，供 handler 使用）
func (r *Router) RouteForStream(modelName string) ([]Provider, error) {
	return r.Route(modelName)
}

// RouteForStreamRequest is the capability-aware variant used by streaming
// handlers. RouteForStream is retained for callers that only know the model.
func (r *Router) RouteForStreamRequest(modelName string, req *Request) ([]Provider, error) {
	return r.RouteForRequest(modelName, req)
}

// Forward 转发请求，带 fallback 逻辑（非流式）
func (r *Router) Forward(ctx context.Context, modelName string, req *Request) (*Response, error) {
	resp, _, _, err := r.ForwardWithDetails(ctx, modelName, req)
	return resp, err
}

// ForwardWithDetails forwards a non-streaming request and returns the final
// provider together with one bounded result for every provider attempt. The
// existing Forward method remains as a compatibility wrapper for embedders.
func (r *Router) ForwardWithDetails(ctx context.Context, modelName string, req *Request) (*Response, string, []requestmeta.ProviderAttempt, error) {
	providers, err := r.RouteForRequest(modelName, req)
	if err != nil {
		return nil, "", nil, err
	}

	originalModel := req.Model
	var lastErr error
	var finalProvider string
	attempts := make([]requestmeta.ProviderAttempt, 0, len(providers))
	attempted := false
	for i, p := range providers {
		if !r.AllowProviderRequestFor(modelName, p) {
			continue
		}
		attempted = true
		started := time.Now()
		r.logger.Printf("Attempting provider %d/%d: %s", i+1, len(providers), p.Name())
		if bmp, ok := p.(BoundModelProvider); ok {
			req.Model = bmp.BoundModel()
		} else {
			req.Model = r.mapModelName(modelName, p.Name())
		}

		resp, err := p.ForwardRequest(ctx, req)
		req.Model = originalModel

		if err != nil {
			r.RecordProviderFailureFor(modelName, p, err)
			r.logger.Printf("Provider %s failed: %v", p.Name(), err)
			lastErr = err
			attempt := requestmeta.ProviderAttempt{
				Provider:  p.Name(),
				Status:    "error",
				LatencyMs: float64(time.Since(started).Microseconds()) / 1000,
				Error:     boundedErrorSummary(err),
			}
			var providerErr *ProviderError
			if errors.As(err, &providerErr) {
				attempt.StatusCode = providerErr.StatusCode
			}
			attempts = append(attempts, attempt)
			finalProvider = p.Name()
			if !ShouldFallback(err) {
				return nil, finalProvider, attempts, err
			}
			continue
		}
		r.RecordProviderSuccessFor(modelName, p)
		r.logger.Printf("Provider %s succeeded", p.Name())
		attempts = append(attempts, requestmeta.ProviderAttempt{
			Provider:  p.Name(),
			Status:    "success",
			LatencyMs: float64(time.Since(started).Microseconds()) / 1000,
		})
		return resp, p.Name(), attempts, nil
	}
	if !attempted && lastErr == nil {
		return nil, finalProvider, attempts, &NoAvailableProvidersError{Model: modelName, Configured: r.chainProviders(modelName), Reason: "disabled, unavailable, or circuit open"}
	}
	return nil, finalProvider, attempts, fmt.Errorf("all providers failed, last error: %w", lastErr)
}

func boundedErrorSummary(err error) string {
	if err == nil {
		return ""
	}
	const maxBytes = 512
	value := err.Error()
	if len(value) > maxBytes {
		return value[:maxBytes] + "..."
	}
	return value
}

// mapModelName 将通用模型名映射到具体提供商的实际模型名（对齐 providers.yaml）
// 仅在默认 setup 路径（无 providers.yaml）下使用
func (r *Router) mapModelName(modelName, providerName string) string {
	mappings := map[string]map[string]string{
		// GLM 核心：保留 haiku/sonnet/opus 别名
		"glm-opus":   {"glm": "glm-5.2"},
		"glm-sonnet": {"glm": "glm-5-turbo"},
		"glm-haiku":  {"glm": "glm-4.7"},
		// GitHub Copilot（免费教育套餐，仅支持 GPT 系列模型）
		"copilot-opus":   {"copilot": "gpt-4.1"},
		"copilot-sonnet": {"copilot": "gpt-4o-2024-11-20"},
		"copilot-haiku":  {"copilot": "gpt-4o-mini"},
	}

	if mapping, ok := mappings[modelName]; ok {
		if actualModel, ok := mapping[providerName]; ok {
			return actualModel
		}
	}
	return modelName
}

// MapModel 是 mapModelName 的公开版本，供 handler 使用
func (r *Router) MapModel(modelName, providerName string) string {
	return r.mapModelName(modelName, providerName)
}

// ListProviders 列出所有提供商名称
func (r *Router) ListProviders() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var names []string
	for name := range r.providers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ListChains 列出所有已注册的 chain 名称（即可用模型名）
func (r *Router) ListChains() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.chains))
	for name := range r.chains {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ListModelInfos returns a stable snapshot for /v1/models and admin tooling.
func (r *Router) ListModelInfos() []ModelInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]ModelInfo, 0, len(r.models))
	for _, info := range r.models {
		info.Capabilities = append([]string(nil), info.Capabilities...)
		info.InputModalities = append([]string(nil), info.InputModalities...)
		result = append(result, info)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}

// AllowProviderRequest reserves permission for one actual provider attempt.
// Route snapshots are intentionally non-reserving so failed stream attempts
// can safely fall back without leaking a half-open probe.
func (r *Router) AllowProviderRequest(name string) bool {
	r.mu.RLock()
	runtime, ok := r.runtime[name]
	if !ok || !runtime.enabled {
		r.mu.RUnlock()
		return false
	}
	allowed := runtime.breaker.Allow()
	r.mu.RUnlock()
	return allowed
}

// AllowProviderRequestFor resolves the configured chain key instead of using
// Provider.Name(). A model-bound wrapper may intentionally expose the base
// provider name, while its runtime circuit belongs to the chain key.
func (r *Router) AllowProviderRequestFor(modelName string, p Provider) bool {
	name := r.providerRuntimeName(modelName, p)
	if name == "" {
		return false
	}
	return r.AllowProviderRequest(name)
}

// RecordProviderSuccess records a completed request or stream.
func (r *Router) RecordProviderSuccess(name string) {
	r.mu.RLock()
	runtime := r.runtime[name]
	r.mu.RUnlock()
	if runtime != nil {
		runtime.breaker.RecordSuccess()
	}
}

func (r *Router) RecordProviderSuccessFor(modelName string, p Provider) {
	name := r.providerRuntimeName(modelName, p)
	if name != "" {
		r.RecordProviderSuccess(name)
	}
}

// RecordProviderFailure updates the circuit only for provider-level failures.
// Client validation/auth errors must not take a healthy provider offline.
func (r *Router) RecordProviderFailure(name string, err error) {
	if err == nil || !ShouldFallback(err) {
		return
	}
	r.mu.RLock()
	runtime := r.runtime[name]
	r.mu.RUnlock()
	if runtime != nil {
		runtime.breaker.RecordFailure()
	}
}

func (r *Router) RecordProviderFailureFor(modelName string, p Provider, err error) {
	name := r.providerRuntimeName(modelName, p)
	if name != "" {
		r.RecordProviderFailure(name, err)
	}
}

func (r *Router) providerRuntimeName(modelName string, p Provider) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, name := range r.chains[modelName] {
		candidate := r.providers[name]
		if providersEqual(candidate, p) {
			return name
		}
	}
	if _, ok := r.runtime[p.Name()]; ok {
		return p.Name()
	}
	return ""
}

func providersEqual(left, right Provider) bool {
	if left == nil || right == nil {
		return false
	}
	leftValue, rightValue := reflect.ValueOf(left), reflect.ValueOf(right)
	if leftValue.Type() != rightValue.Type() || !leftValue.Type().Comparable() {
		return false
	}
	return leftValue.Interface() == rightValue.Interface()
}

// SetProviderEnabled enables or disables a provider for all chains.
func (r *Router) SetProviderEnabled(name string, enabled bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	runtime, ok := r.runtime[name]
	if !ok {
		return fmt.Errorf("%w: %s", ErrUnknownProvider, name)
	}
	runtime.enabled = enabled
	return nil
}

// ResetProvider closes a provider circuit without changing its enabled state.
func (r *Router) ResetProvider(name string) error {
	r.mu.RLock()
	runtime, ok := r.runtime[name]
	r.mu.RUnlock()
	if !ok {
		return fmt.Errorf("%w: %s", ErrUnknownProvider, name)
	}
	runtime.breaker.Reset()
	return nil
}

// CheckProviderHealth runs the provider's explicit health probe and records
// the result in the same circuit used by normal requests.
func (r *Router) CheckProviderHealth(ctx context.Context, name string) (bool, error) {
	r.mu.RLock()
	p, ok := r.providers[name]
	runtime := r.runtime[name]
	r.mu.RUnlock()
	if !ok || runtime == nil {
		return false, fmt.Errorf("%w: %s", ErrUnknownProvider, name)
	}
	if !runtime.enabled {
		return false, fmt.Errorf("provider %s is disabled", name)
	}
	healthy := p.IsHealthy(ctx)
	if healthy {
		runtime.breaker.RecordSuccess()
	} else {
		runtime.breaker.RecordFailure()
	}
	return healthy, nil
}

func (r *Router) ProviderStatus(name string) (RuntimeProviderStatus, bool) {
	r.mu.RLock()
	runtime, ok := r.runtime[name]
	r.mu.RUnlock()
	if !ok || runtime == nil {
		return RuntimeProviderStatus{}, false
	}
	return runtimeStatus(name, runtime), true
}

func (r *Router) ListProviderStatuses() []RuntimeProviderStatus {
	names := r.ListProviders()
	result := make([]RuntimeProviderStatus, 0, len(names))
	for _, name := range names {
		if status, ok := r.ProviderStatus(name); ok {
			result = append(result, status)
		}
	}
	return result
}

func runtimeStatus(name string, runtime *providerRuntime) RuntimeProviderStatus {
	snapshot := runtime.breaker.Snapshot()
	status := "unknown"
	if !runtime.enabled || snapshot.State == CircuitOpen {
		status = "offline"
	} else if snapshot.State == CircuitHalfOpen || (snapshot.HasResult && snapshot.ConsecutiveFailures > 0) {
		status = "degraded"
	} else if snapshot.State == CircuitClosed && snapshot.HasResult {
		status = "online"
	}
	return RuntimeProviderStatus{
		Name:                name,
		Enabled:             runtime.enabled,
		State:               snapshot.State,
		Status:              status,
		ConsecutiveFailures: snapshot.ConsecutiveFailures,
		TotalFailures:       snapshot.TotalFailures,
		TotalSuccesses:      snapshot.TotalSuccesses,
		HasResult:           snapshot.HasResult,
		LastFailureAt:       snapshot.LastFailureAt,
		LastSuccessAt:       snapshot.LastSuccessAt,
		OpenedAt:            snapshot.OpenedAt,
		NextRetryAt:         snapshot.NextRetryAt,
	}
}

// SetChainOrder changes only the order of providers already present in a
// route. This prevents an admin typo from injecting an unregistered provider.
func (r *Router) SetChainOrder(modelName string, providers []string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	current, ok := r.chains[modelName]
	if !ok {
		return fmt.Errorf("%w: %s", ErrUnknownChain, modelName)
	}
	if len(providers) != len(current) {
		return fmt.Errorf("route %s must contain exactly %d providers", modelName, len(current))
	}
	wanted := make(map[string]bool, len(current))
	for _, name := range current {
		wanted[name] = true
	}
	seen := make(map[string]bool, len(providers))
	for _, name := range providers {
		if !wanted[name] {
			return fmt.Errorf("provider %s is not configured for route %s", name, modelName)
		}
		if seen[name] {
			return fmt.Errorf("provider %s is duplicated in route %s", name, modelName)
		}
		seen[name] = true
	}
	r.chains[modelName] = append([]string(nil), providers...)
	return nil
}

func (r *Router) chainProviders(modelName string) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]string(nil), r.chains[modelName]...)
}

// ListRouteStatuses returns ordered chains with redacted provider runtime
// state. The order is the actual failover priority used by the router.
func (r *Router) ListRouteStatuses() []RouteStatus {
	r.mu.RLock()
	models := make([]string, 0, len(r.chains))
	for model := range r.chains {
		models = append(models, model)
	}
	sort.Strings(models)
	result := make([]RouteStatus, 0, len(models))
	for _, model := range models {
		status := RouteStatus{Model: model}
		for _, name := range r.chains[model] {
			runtime := r.runtime[name]
			if runtime == nil {
				status.Providers = append(status.Providers, RouteProviderStatus{Name: name, State: CircuitOpen, Status: "offline"})
				continue
			}
			providerStatus := runtimeStatus(name, runtime)
			status.Providers = append(status.Providers, RouteProviderStatus{
				Name: providerStatus.Name, Enabled: providerStatus.Enabled, State: providerStatus.State, Status: providerStatus.Status,
			})
		}
		result = append(result, status)
	}
	r.mu.RUnlock()
	return result
}

// SetModelCapabilities updates the public model registry and applies an
// override used by capability-aware routing for subsequent requests.
func (r *Router) SetModelCapabilities(modelName string, capabilities, modalities []string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	info, ok := r.models[modelName]
	if !ok {
		return fmt.Errorf("%w: %s", ErrUnknownChain, modelName)
	}
	capabilities = normalizeModelCapabilities(capabilities)
	if len(modalities) == 0 {
		modalities = modelInputModalities(capabilities, nil)
	}
	info.Capabilities = append([]string(nil), capabilities...)
	info.InputModalities = append([]string(nil), modalities...)
	r.models[modelName] = info
	r.modelOverrides[modelName] = modelCapabilityOverride{
		capabilities: append([]string(nil), capabilities...), inputModalities: append([]string(nil), modalities...),
	}
	return nil
}
