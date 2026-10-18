package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"syscall"
	"testing"

	syncp "github.com/reak/get-hike/internal/sync"
)

type timeoutNetErr struct{}

func (timeoutNetErr) Error() string   { return "i/o timeout" }
func (timeoutNetErr) Timeout() bool   { return true }
func (timeoutNetErr) Temporary() bool { return true }

func TestIsTransientAIError(t *testing.T) {
	cases := []struct {
		name      string
		err       error
		transient bool
	}{
		{"nil", nil, false},
		{"context deadline", context.DeadlineExceeded, true},
		{"context canceled", context.Canceled, true},
		{"connection refused", syscall.ECONNREFUSED, true},
		{"wrapped network unreachable", fmt.Errorf("gemini request: %w", syscall.ENETUNREACH), true},
		{"dns error", &net.DNSError{Err: "no such host", Name: "generativelanguage.googleapis.com"}, true},
		{"net timeout", fmt.Errorf("post failed: %w", timeoutNetErr{}), true},
		{"dial string", errors.New("dial tcp 142.250.1.1:443: connect: network is unreachable"), true},
		{"rate limited", errors.New("gemini error 429: quota exceeded"), true},
		{"server error", errors.New("gemini error 503: backend unavailable"), true},
		{"bad request", errors.New("gemini error 400: invalid payload"), false},
		{"parse failure", errors.New("gemini batch returned no results"), false},
		{"no models", errors.New("no working Gemini models available"), false},
	}

	for _, tc := range cases {
		if got := isTransientAIError(tc.err); got != tc.transient {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.transient)
		}
	}
}

func TestAIAttemptThrottle(t *testing.T) {
	a := NewApp()
	if !a.aiAttemptAllowed() {
		t.Fatal("expected first attempt to be allowed")
	}
	a.markAIAttempt()
	if a.aiAttemptAllowed() {
		t.Fatal("expected attempt to be throttled right after marking")
	}
}

func TestSyncActiveGating(t *testing.T) {
	a := NewApp()
	if a.syncActive() {
		t.Fatal("expected sync to be inactive without a sync engine")
	}

	a.syncEngine = syncp.NewSyncEngine(nil, "http://localhost:8080")
	a.syncEngine.SetAuthToken("token")
	a.isGuest = true
	if a.syncActive() {
		t.Fatal("expected guest mode to disable sync")
	}

	a.isGuest = false
	if !a.syncActive() {
		t.Fatal("expected sync to be active with token and non-guest mode")
	}

	a.syncEngine.SetAuthToken("")
	if a.syncActive() {
		t.Fatal("expected sync to be inactive without a token")
	}
}
