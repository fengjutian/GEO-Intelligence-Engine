package modelgateway

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestProviderLimiterEnforcesConcurrency(t *testing.T) {
	limiter := newProviderLimiter(0, 1)
	if err := limiter.acquire(context.Background()); err != nil {
		t.Fatal(err)
	}
	acquired := make(chan struct{})
	go func() {
		if err := limiter.acquire(context.Background()); err == nil {
			close(acquired)
		}
	}()
	select {
	case <-acquired:
		t.Fatal("second acquire must wait for the first slot")
	case <-time.After(50 * time.Millisecond):
	}
	limiter.release()
	select {
	case <-acquired:
	case <-time.After(time.Second):
		t.Fatal("second acquire did not proceed after release")
	}
	limiter.release()
}

func TestProviderLimiterTokenRefill(t *testing.T) {
	limiter := &providerLimiter{tokens: 0, capacity: 1, refill: 1000, last: time.Now()}
	start := time.Now()
	if err := limiter.waitToken(context.Background()); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("token refill waited too long: %s", elapsed)
	}
}

func TestRetryableClassifiesProviderErrors(t *testing.T) {
	cases := []struct {
		status int
		want   bool
	}{
		{408, true},
		{429, true},
		{500, true},
		{503, true},
		{400, false},
		{401, false},
		{403, false},
	}
	for _, test := range cases {
		err := &ProviderError{Provider: "deepseek", StatusCode: test.status}
		if got := Retryable(err); got != test.want {
			t.Fatalf("Retryable(%d) = %v, want %v", test.status, got, test.want)
		}
	}
	if !Retryable(errors.New("network down")) {
		t.Fatal("transport errors must be retryable")
	}
	if Retryable(ErrProviderUnconfigured) || Retryable(ErrProviderUnsupported) {
		t.Fatal("configuration errors must not be retried")
	}
}

func TestParseRetryAfter(t *testing.T) {
	if got := parseRetryAfter("2"); got != 2*time.Second {
		t.Fatalf("parseRetryAfter(2) = %s", got)
	}
	if got := parseRetryAfter(""); got != 0 {
		t.Fatalf("parseRetryAfter(empty) = %s", got)
	}
	if got := parseRetryAfter("not-a-delay"); got != 0 {
		t.Fatalf("parseRetryAfter(invalid) = %s", got)
	}
}
