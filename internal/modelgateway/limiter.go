package modelgateway

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

// providerLimiter enforces a smoothed RPM budget plus a hard concurrency cap per provider.
type providerLimiter struct {
	mu       sync.Mutex
	tokens   float64
	capacity float64
	refill   float64
	last     time.Time
	slots    chan struct{}
}

func newProviderLimiter(rpm, concurrency int) *providerLimiter {
	if rpm <= 0 && concurrency <= 0 {
		return nil
	}
	limiter := &providerLimiter{last: time.Now()}
	if rpm > 0 {
		limiter.capacity = float64(rpm) / 6
		if limiter.capacity < 1 {
			limiter.capacity = 1
		}
		limiter.tokens = limiter.capacity
		limiter.refill = float64(rpm) / 60
	}
	if concurrency > 0 {
		limiter.slots = make(chan struct{}, concurrency)
	}
	return limiter
}

func (l *providerLimiter) acquire(ctx context.Context) error {
	if l == nil {
		return nil
	}
	if l.slots != nil {
		select {
		case l.slots <- struct{}{}:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if err := l.waitToken(ctx); err != nil {
		l.release()
		return err
	}
	return nil
}

func (l *providerLimiter) release() {
	if l == nil || l.slots == nil {
		return
	}
	select {
	case <-l.slots:
	default:
	}
}

func (l *providerLimiter) waitToken(ctx context.Context) error {
	if l == nil || l.refill <= 0 {
		return nil
	}
	for {
		l.mu.Lock()
		now := time.Now()
		l.tokens += now.Sub(l.last).Seconds() * l.refill
		if l.tokens > l.capacity {
			l.tokens = l.capacity
		}
		l.last = now
		if l.tokens >= 1 {
			l.tokens--
			l.mu.Unlock()
			return nil
		}
		wait := time.Duration((1 - l.tokens) / l.refill * float64(time.Second))
		l.mu.Unlock()
		if wait < time.Millisecond {
			wait = time.Millisecond
		}
		timer := time.NewTimer(wait)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		}
	}
}

// ProviderError is returned for non-2xx provider responses and carries retry guidance.
type ProviderError struct {
	Provider   string
	StatusCode int
	RetryAfter time.Duration
	Message    string
}

func (e *ProviderError) Error() string {
	if e.RetryAfter > 0 {
		return fmt.Sprintf("provider %s returned HTTP %d (retry after %s): %s", e.Provider, e.StatusCode, e.RetryAfter, e.Message)
	}
	return fmt.Sprintf("provider %s returned HTTP %d: %s", e.Provider, e.StatusCode, e.Message)
}

// ErrQuotaExceeded marks a provider account quota exhaustion; retrying cannot help.
var ErrQuotaExceeded = errors.New("provider quota exceeded")

// Retryable reports whether a technical retry may succeed. Auth, parameter and policy errors are terminal.
func Retryable(err error) bool {
	if errors.Is(err, ErrProviderUnsupported) || errors.Is(err, ErrProviderUnconfigured) || errors.Is(err, ErrQuotaExceeded) {
		return false
	}
	var providerErr *ProviderError
	if !errors.As(err, &providerErr) {
		return true
	}
	return providerErr.StatusCode == 408 || providerErr.StatusCode == 429 || providerErr.StatusCode >= 500
}

func parseRetryAfter(value string) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(value); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	if when, err := time.Parse(time.RFC1123, value); err == nil {
		if duration := time.Until(when); duration > 0 {
			return duration
		}
	}
	return 0
}
