package provider

import (
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// CircuitState describes the lifecycle of a provider circuit.
type CircuitState string

const (
	CircuitClosed   CircuitState = "closed"
	CircuitOpen     CircuitState = "open"
	CircuitHalfOpen CircuitState = "half_open"
)

// CircuitBreakerConfig controls when a provider is removed from the routing
// chain and how long it must recover before receiving a probe request.
type CircuitBreakerConfig struct {
	FailureThreshold int
	RecoveryTimeout  time.Duration
	SuccessThreshold int
	RateLimitInitial time.Duration
	RateLimitMax     time.Duration
}

var defaultCircuitBreakerConfig = CircuitBreakerConfig{
	FailureThreshold: 3,
	RecoveryTimeout:  30 * time.Second,
	SuccessThreshold: 1,
}

// CircuitSnapshot is safe to expose through the admin API. It deliberately
// contains counters and timestamps only; provider credentials never enter
// the runtime status object.
type CircuitSnapshot struct {
	State                CircuitState
	ConsecutiveFailures  int
	ConsecutiveSuccesses int
	TotalFailures        int
	TotalSuccesses       int
	HasResult            bool
	LastFailureAt        time.Time
	LastSuccessAt        time.Time
	OpenedAt             time.Time
	NextRetryAt          time.Time
}

type CircuitBreaker struct {
	mu            sync.Mutex
	config        CircuitBreakerConfig
	state         CircuitState
	failures      int
	successes     int
	totalFailure  int
	totalSuccess  int
	hasResult     bool
	halfOpenProbe bool
	lastFailure   time.Time
	lastSuccess   time.Time
	openedAt      time.Time
	rateUntil     time.Time
	rateStreak    int
}

// NewCircuitBreaker creates an independent circuit for one provider instance.
func NewCircuitBreaker(config CircuitBreakerConfig) *CircuitBreaker {
	if config.FailureThreshold <= 0 {
		config.FailureThreshold = defaultCircuitBreakerConfig.FailureThreshold
	}
	if config.RecoveryTimeout <= 0 {
		config.RecoveryTimeout = defaultCircuitBreakerConfig.RecoveryTimeout
	}
	if config.SuccessThreshold <= 0 {
		config.SuccessThreshold = defaultCircuitBreakerConfig.SuccessThreshold
	}
	if config.RateLimitInitial <= 0 {
		config.RateLimitInitial = 30 * time.Second
	}
	if config.RateLimitMax < config.RateLimitInitial {
		config.RateLimitMax = 5 * time.Minute
		if config.RateLimitMax < config.RateLimitInitial {
			config.RateLimitMax = config.RateLimitInitial
		}
	}
	return &CircuitBreaker{config: config, state: CircuitClosed}
}

// Allow reports whether a request may start and reserves a half-open probe
// when the recovery timeout has elapsed.
func (b *CircuitBreaker) Allow() bool {
	return b.AllowAt(time.Now())
}

// AllowAt is deterministic for tests and clock-sensitive callers.
func (b *CircuitBreaker) AllowAt(now time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.allowLocked(now)
}

func (b *CircuitBreaker) allowLocked(now time.Time) bool {
	switch b.state {
	case CircuitClosed:
		return true
	case CircuitOpen:
		if now.Before(b.retryAtLocked()) {
			return false
		}
		b.state = CircuitHalfOpen
		b.halfOpenProbe = true
		b.successes = 0
		return true
	case CircuitHalfOpen:
		if b.halfOpenProbe {
			return false
		}
		b.halfOpenProbe = true
		return true
	default:
		b.state = CircuitClosed
		return true
	}
}

// CanAttempt is a non-reserving check used while building a route snapshot.
// The actual request must still call Allow to avoid a half-open race.
func (b *CircuitBreaker) CanAttempt() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.state == CircuitOpen && !time.Now().Before(b.retryAtLocked()) {
		b.state = CircuitHalfOpen
		b.halfOpenProbe = false
		b.successes = 0
	}
	return b.state == CircuitClosed || (b.state == CircuitHalfOpen && !b.halfOpenProbe)
}

func (b *CircuitBreaker) RecordSuccess() {
	b.RecordSuccessAt(time.Now())
}

func (b *CircuitBreaker) RecordSuccessAt(now time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.hasResult = true
	b.totalSuccess++
	b.lastSuccess = now
	switch b.state {
	case CircuitHalfOpen:
		b.successes++
		b.halfOpenProbe = false
		if b.successes >= b.config.SuccessThreshold {
			b.state = CircuitClosed
			b.rateStreak = 0
			b.rateUntil = time.Time{}
			b.failures = 0
			b.successes = 0
		}
	case CircuitClosed:
		b.failures = 0
		b.rateStreak = 0
	}
}

func (b *CircuitBreaker) RecordFailure() {
	b.RecordFailureAt(time.Now())
}

func (b *CircuitBreaker) RecordFailureAt(now time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.hasResult = true
	b.totalFailure++
	b.lastFailure = now
	b.successes = 0
	b.halfOpenProbe = false
	switch b.state {
	case CircuitHalfOpen:
		b.openLocked(now)
	case CircuitClosed:
		b.failures++
		if b.failures >= b.config.FailureThreshold {
			b.openLocked(now)
		}
	case CircuitOpen:
		// A late result from a request started before opening must not move the
		// recovery window backwards unexpectedly; retain the original openedAt.
	}
}

func (b *CircuitBreaker) openLocked(now time.Time) {
	b.state = CircuitOpen
	b.openedAt = now
	b.rateUntil = time.Time{}
	b.halfOpenProbe = false
}

// Reset manually closes a circuit and clears its consecutive failure state.
// Lifetime counters remain available for diagnosis.
func (b *CircuitBreaker) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.state = CircuitClosed
	b.failures = 0
	b.successes = 0
	b.halfOpenProbe = false
	b.hasResult = false
	b.lastFailure = time.Time{}
	b.lastSuccess = time.Time{}
	b.openedAt = time.Time{}
	b.rateUntil = time.Time{}
	b.rateStreak = 0
}

func (b *CircuitBreaker) Snapshot() CircuitSnapshot {
	return b.SnapshotAt(time.Now())
}

func (b *CircuitBreaker) SnapshotAt(now time.Time) CircuitSnapshot {
	b.mu.Lock()
	defer b.mu.Unlock()
	state := b.state
	nextRetry := time.Time{}
	if state == CircuitOpen {
		nextRetry = b.retryAtLocked()
		if !nextRetry.After(now) {
			state = CircuitHalfOpen
		}
	}
	return CircuitSnapshot{
		State:                state,
		ConsecutiveFailures:  b.failures,
		ConsecutiveSuccesses: b.successes,
		TotalFailures:        b.totalFailure,
		TotalSuccesses:       b.totalSuccess,
		HasResult:            b.hasResult,
		LastFailureAt:        b.lastFailure,
		LastSuccessAt:        b.lastSuccess,
		OpenedAt:             b.openedAt,
		NextRetryAt:          nextRetry,
	}
}

func (b *CircuitBreaker) retryAtLocked() time.Time {
	if !b.rateUntil.IsZero() {
		return b.rateUntil
	}
	return b.openedAt.Add(b.config.RecoveryTimeout)
}

// RecordRateLimit opens immediately. Retry-After overrides exponential backoff.
// A late parallel success cannot close an open circuit.
func (b *CircuitBreaker) RecordRateLimit(retryAfter string) {
	b.RecordRateLimitAt(time.Now(), retryAfter)
}
func (b *CircuitBreaker) RecordRateLimitAt(now time.Time, retryAfter string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.hasResult = true
	b.totalFailure++
	b.failures++
	b.lastFailure = now
	b.rateStreak++
	delay := b.config.RateLimitInitial
	for i := 1; i < b.rateStreak && delay < b.config.RateLimitMax; i++ {
		if delay > b.config.RateLimitMax/2 {
			delay = b.config.RateLimitMax
		} else {
			delay *= 2
		}
	}
	if delay > b.config.RateLimitMax {
		delay = b.config.RateLimitMax
	}
	raw := strings.TrimSpace(retryAfter)
	if seconds, err := strconv.ParseInt(raw, 10, 64); err == nil && seconds >= 0 && seconds <= 86400 {
		delay = time.Duration(seconds) * time.Second
	} else if date, err := http.ParseTime(raw); err == nil && date.After(now) && date.Sub(now) <= 24*time.Hour {
		delay = date.Sub(now)
	}
	if delay < time.Second {
		delay = time.Second
	}
	until := now.Add(delay)
	if b.rateUntil.After(until) {
		until = b.rateUntil
	}
	b.openLocked(now)
	b.rateUntil = until
	b.successes = 0
}

// ReleaseProbe frees a reservation after client cancellation or a skipped call.
func (b *CircuitBreaker) ReleaseProbe() { b.mu.Lock(); defer b.mu.Unlock(); b.halfOpenProbe = false }
