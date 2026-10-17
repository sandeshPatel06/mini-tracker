package ai

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"
)

func TestDoWithRetrySuccessFirstAttempt(t *testing.T) {
	calls := 0
	err := DoWithRetry(context.Background(), RetryPolicy{
		MaxAttempts: 4,
		BaseDelay:   time.Millisecond,
		MaxDelay:    5 * time.Millisecond,
	}, func(ctx context.Context) (error, bool, time.Duration) {
		calls++
		return nil, false, 0
	})

	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected 1 call, got %d", calls)
	}
}

func TestDoWithRetryRetryableThenSuccess(t *testing.T) {
	calls := 0
	err := DoWithRetry(context.Background(), RetryPolicy{
		MaxAttempts: 4,
		BaseDelay:   time.Millisecond,
		MaxDelay:    5 * time.Millisecond,
	}, func(ctx context.Context) (error, bool, time.Duration) {
		calls++
		if calls < 3 {
			return fmt.Errorf("transient %d", calls), true, 0
		}
		return nil, false, 0
	})

	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if calls != 3 {
		t.Fatalf("expected 3 calls, got %d", calls)
	}
}

func TestDoWithRetryNonRetryable(t *testing.T) {
	sentinel := errors.New("permanent")
	calls := 0
	err := DoWithRetry(context.Background(), RetryPolicy{
		MaxAttempts: 4,
		BaseDelay:   time.Millisecond,
		MaxDelay:    5 * time.Millisecond,
	}, func(ctx context.Context) (error, bool, time.Duration) {
		calls++
		return sentinel, false, 0
	})

	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel error, got %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected 1 call, got %d", calls)
	}
}

func TestDoWithRetryExhaustsAttempts(t *testing.T) {
	calls := 0
	var lastErr error
	err := DoWithRetry(context.Background(), RetryPolicy{
		MaxAttempts: 3,
		BaseDelay:   time.Millisecond,
		MaxDelay:    5 * time.Millisecond,
	}, func(ctx context.Context) (error, bool, time.Duration) {
		calls++
		lastErr = fmt.Errorf("attempt %d", calls)
		return lastErr, true, 0
	})

	if calls != 3 {
		t.Fatalf("expected 3 calls, got %d", calls)
	}
	if !errors.Is(err, lastErr) {
		t.Fatalf("expected last error %v, got %v", lastErr, err)
	}
}

func TestDoWithRetryHonorsRetryAfter(t *testing.T) {
	start := time.Now()
	calls := 0
	err := DoWithRetry(context.Background(), RetryPolicy{
		MaxAttempts: 2,
		BaseDelay:   time.Millisecond,
		MaxDelay:    5 * time.Millisecond,
	}, func(ctx context.Context) (error, bool, time.Duration) {
		calls++
		if calls == 1 {
			return errors.New("rate limited"), true, 30 * time.Millisecond
		}
		return nil, false, 0
	})

	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if calls != 2 {
		t.Fatalf("expected 2 calls, got %d", calls)
	}
	if elapsed := time.Since(start); elapsed < 30*time.Millisecond {
		t.Fatalf("expected wait of at least 30ms, got %v", elapsed)
	}
}

func TestDoWithRetryCancelDuringBackoff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	err := DoWithRetry(ctx, RetryPolicy{
		MaxAttempts: 4,
		BaseDelay:   200 * time.Millisecond,
		MaxDelay:    time.Second,
	}, func(ctx context.Context) (error, bool, time.Duration) {
		return errors.New("transient"), true, 0
	})
	elapsed := time.Since(start)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if elapsed >= 150*time.Millisecond {
		t.Fatalf("expected prompt cancellation, took %v", elapsed)
	}
}

func TestIsRetryableStatus(t *testing.T) {
	tests := []struct {
		code int
		want bool
	}{
		{http.StatusTooManyRequests, true},
		{http.StatusInternalServerError, true},
		{http.StatusBadGateway, true},
		{http.StatusServiceUnavailable, true},
		{http.StatusGatewayTimeout, true},
		{http.StatusOK, false},
		{http.StatusBadRequest, false},
		{http.StatusUnauthorized, false},
		{http.StatusNotFound, false},
		{http.StatusNotImplemented, false},
	}

	for _, tt := range tests {
		if got := IsRetryableStatus(tt.code); got != tt.want {
			t.Errorf("IsRetryableStatus(%d) = %v, want %v", tt.code, got, tt.want)
		}
	}
}

func TestRetryAfterFromHeader(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  time.Duration
	}{
		{"delta seconds", "2", 2 * time.Second},
		{"garbage", "not-a-number", 0},
		{"past http date", time.Now().Add(-time.Hour).UTC().Format(http.TimeFormat), 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := http.Header{}
			h.Set("Retry-After", tt.value)
			if got := RetryAfterFromHeader(h); got != tt.want {
				t.Errorf("RetryAfterFromHeader(%q) = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}
