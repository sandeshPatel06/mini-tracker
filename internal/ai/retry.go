package ai

import (
	"context"
	"math/rand"
	"net/http"
	"strconv"
	"time"
)

// RetryPolicy controls DoWithRetry behavior.
type RetryPolicy struct {
	MaxAttempts int           // total attempts including the first; <=0 => 4
	BaseDelay   time.Duration // <=0 => 1s
	MaxDelay    time.Duration // <=0 => 20s
}

// RetryableFunc is a single attempt. It reports whether the error is retryable
// and an optional server-provided delay to honor.
type RetryableFunc func(ctx context.Context) (err error, retryable bool, retryAfter time.Duration)

// DoWithRetry runs fn until success, until retryable=false, or attempts exhausted.
// Backoff = BaseDelay * 2^(attempt-1) capped at MaxDelay, plus +/-20% jitter;
// if retryAfter > backoff, retryAfter is used. No sleep after final attempt.
// Returns ctx.Err() promptly if the context is cancelled during a backoff wait.
func DoWithRetry(ctx context.Context, policy RetryPolicy, fn RetryableFunc) error {
	if policy.MaxAttempts <= 0 {
		policy.MaxAttempts = 4
	}
	if policy.BaseDelay <= 0 {
		policy.BaseDelay = time.Second
	}
	if policy.MaxDelay <= 0 {
		policy.MaxDelay = 20 * time.Second
	}

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

	var lastErr error
	for attempt := 1; attempt <= policy.MaxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}

		err, retryable, retryAfter := fn(ctx)
		if err == nil {
			return nil
		}
		lastErr = err
		if !retryable || attempt == policy.MaxAttempts {
			return err
		}

		backoff := policy.BaseDelay
		for i := 1; i < attempt; i++ {
			if backoff >= policy.MaxDelay {
				break
			}
			backoff *= 2
		}
		if backoff > policy.MaxDelay {
			backoff = policy.MaxDelay
		}

		jitter := 1 + 0.2*(rng.Float64()*2-1)
		delay := time.Duration(float64(backoff) * jitter)
		if delay < 0 {
			delay = 0
		}
		if retryAfter > delay {
			delay = retryAfter
		}

		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}

	return lastErr
}

// IsRetryableStatus reports whether an HTTP status should be retried (429, 500, 502, 503, 504).
func IsRetryableStatus(code int) bool {
	switch code {
	case http.StatusTooManyRequests,
		http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

// RetryAfterFromHeader parses Retry-After (delta seconds or HTTP-date); returns 0 when absent/invalid/past.
// Clamp to max 5 minutes.
func RetryAfterFromHeader(h http.Header) time.Duration {
	const maxRetryAfter = 5 * time.Minute

	if h == nil {
		return 0
	}
	v := h.Get("Retry-After")
	if v == "" {
		return 0
	}

	if secs, err := strconv.Atoi(v); err == nil {
		if secs <= 0 {
			return 0
		}
		if secs > int(maxRetryAfter/time.Second) {
			return maxRetryAfter
		}
		return time.Duration(secs) * time.Second
	}

	when, err := http.ParseTime(v)
	if err != nil {
		return 0
	}
	d := time.Until(when)
	if d <= 0 {
		return 0
	}
	if d > maxRetryAfter {
		return maxRetryAfter
	}
	return d
}
