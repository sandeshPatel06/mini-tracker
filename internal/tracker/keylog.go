// Package tracker provides the Linux-native keystroke entropy monitor.
// It supports both direct /dev/input event monitoring and zero-sudo application/API input tracking.
// Zero-sudo tracking mode operates without root privileges, without 'sudo usermod -aG input $USER',
// and without requiring system logouts.
package tracker

import (
	"log"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/holoplot/go-evdev"
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
	unique   map[evdev.EvCode]struct{}
	stopCh   chan struct{}
	interval time.Duration
}

// NewKeystrokeTracker creates a tracker with the given flush interval.
func NewKeystrokeTracker(interval time.Duration) *KeystrokeTracker {
	return &KeystrokeTracker{
		unique:   make(map[evdev.EvCode]struct{}),
		stopCh:   make(chan struct{}),
		interval: interval,
	}
}

// Start launches goroutines for all discovered keyboard devices and a flush
// ticker. The provided channel receives a KeystrokeStats every interval.
func (t *KeystrokeTracker) Start() (<-chan KeystrokeStats, error) {
	devices, err := discoverKeyboards()
	if err != nil || len(devices) == 0 {
		log.Printf("[tracker] Zero-Sudo Mode Active: Application & API key input tracking enabled (no sudo or input group required).")
	} else {
		log.Printf("[tracker] Found %d native evdev keyboard device(s)", len(devices))
	}

	eventCh := make(chan evdev.EvCode, 256)

	// One goroutine per keyboard device
	for _, path := range devices {
		go func(devPath string) {
			dev, err := evdev.Open(devPath)
			if err != nil {
				log.Printf("[tracker] cannot open %s: %v", devPath, err)
				return
			}
			defer dev.Close()
			log.Printf("[tracker] listening on %s", devPath)

			for {
				select {
				case <-t.stopCh:
					return
				default:
					ev, err := dev.ReadOne()
					if err != nil {
						return
					}
					// Only capture key-press events (value==1), not releases or repeats
					if ev.Type == evdev.EV_KEY && ev.Value == 1 {
						select {
						case eventCh <- ev.Code:
						default:
						}
					}
				}
			}
		}(path)
	}

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
		t.unique = make(map[evdev.EvCode]struct{})
	}
	for i := 0; i < unique; i++ {
		t.unique[evdev.EvCode(100+i)] = struct{}{}
	}
}

// RecordKeyCode records a single keypress event code.
func (t *KeystrokeTracker) RecordKeyCode(code evdev.EvCode) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.total++
	if t.unique == nil {
		t.unique = make(map[evdev.EvCode]struct{})
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
	t.unique = make(map[evdev.EvCode]struct{})
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

// discoverKeyboards returns all /dev/input keyboard device paths.
func discoverKeyboards() ([]string, error) {
	dirsToScan := []string{"/dev/input"}
	if entries, err := os.ReadDir("/dev/input/by-id"); err == nil && len(entries) > 0 {
		dirsToScan = append(dirsToScan, "/dev/input/by-id")
	}
	if entries, err := os.ReadDir("/dev/input/by-path"); err == nil && len(entries) > 0 {
		dirsToScan = append(dirsToScan, "/dev/input/by-path")
	}

	seen := make(map[string]bool)
	var keyboards []string

	for _, dir := range dirsToScan {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			name := e.Name()
			if !strings.Contains(name, "event") && !strings.Contains(name, "kbd") {
				continue
			}
			path := filepath.Join(dir, name)
			if seen[path] {
				continue
			}

			dev, err := evdev.Open(path)
			if err != nil {
				continue
			}
			caps := dev.CapableTypes()
			for _, t := range caps {
				if t == evdev.EV_KEY {
					keyboards = append(keyboards, path)
					seen[path] = true
					break
				}
			}
			dev.Close()
		}
	}
	return keyboards, nil
}
