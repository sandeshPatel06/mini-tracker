// Package tracker provides the Linux-native keystroke entropy monitor.
// It supports both direct /dev/input event monitoring and zero-sudo application/API input tracking.
// Zero-sudo tracking mode operates without root privileges, without 'sudo usermod -aG input $USER',
// and without requiring system logouts.
package tracker

import (
	"math"
	"sync"
	"time"
)

// KeystrokeStats holds the keystroke uniqueness metrics for one interval.
type KeystrokeStats struct {
	TotalKeys    int     `json:"total_keys"`
	UniqueKeys   int     `json:"unique_keys"`
	EntropyScore float64 `json:"entropy_score"` // 0–100
}

// KeystrokeTracker monitors keyboard devices and aggregates keystroke uniqueness metrics.
type KeystrokeTracker struct {
	mu       sync.Mutex
	stopOnce sync.Once
	total    int
	unique   map[int]struct{}
	stopCh   chan struct{}
	interval time.Duration
}

// NewKeystrokeTracker creates a tracker with the given flush interval.
func NewKeystrokeTracker(interval time.Duration) *KeystrokeTracker {
	return &KeystrokeTracker{
		unique:   make(map[int]struct{}),
		stopCh:   make(chan struct{}),
		interval: interval,
	}
}

// Start launches native key capture (where supported) and a flush ticker.
// The provided channel receives a KeystrokeStats every interval.
func (t *KeystrokeTracker) Start() (<-chan KeystrokeStats, error) {
	eventCh := make(chan int, 256)
	startNativeKeyCapture(eventCh, t.stopCh)

	// Aggregator
	go func() {
		for {
			select {
			case code := <-eventCh:
				t.mu.Lock()
				t.total++
				t.unique[code] = struct{}{}
				t.mu.Unlock()
			case <-t.stopCh:
				return
			}
		}
	}()

	statsCh := make(chan KeystrokeStats, 4)

	// Ticker flushes stats every interval
	go func() {
		ticker := time.NewTicker(t.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				stats := t.flush()
				select {
				case statsCh <- stats:
				default:
				}
			case <-t.stopCh:
				return
			}
		}
	}()

	return statsCh, nil
}

// RecordKeystrokes records keystroke activity submitted via API or desktop frontend.
func (t *KeystrokeTracker) RecordKeystrokes(total, unique int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.total += total
	if t.unique == nil {
		t.unique = make(map[int]struct{})
	}
	for i := 0; i < unique; i++ {
		t.unique[100+i] = struct{}{}
	}
}

// RecordKeyCode records a single keypress event code.
func (t *KeystrokeTracker) RecordKeyCode(code int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.total++
	if t.unique == nil {
		t.unique = make(map[int]struct{})
	}
	t.unique[code] = struct{}{}
}

// Stop signals all goroutines to exit. Safe to call multiple times.
func (t *KeystrokeTracker) Stop() {
	t.stopOnce.Do(func() { close(t.stopCh) })
}

// flush atomically reads and resets the counters, then computes the entropy score.
func (t *KeystrokeTracker) flush() KeystrokeStats {
	t.mu.Lock()
	total := t.total
	unique := len(t.unique)
	t.total = 0
	t.unique = make(map[int]struct{})
	t.mu.Unlock()

	score := ComputeEntropyScore(total, unique)
	return KeystrokeStats{
		TotalKeys:    total,
		UniqueKeys:   unique,
		EntropyScore: score,
	}
}

// ComputeEntropyScore returns a 0–100 score:
// high score = lots of distinct keys (real typing), low score = repetitive or idle.
func ComputeEntropyScore(total, unique int) float64 {
	if total == 0 {
		return 0
	}
	// Uniqueness ratio × log factor, capped at 100
	ratio := float64(unique) / float64(total)
	logFactor := math.Log2(float64(total) + 1)
	score := ratio * logFactor * 10
	if score > 100 {
		score = 100
	}
	return math.Round(score*10) / 10
}
