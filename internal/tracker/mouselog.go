package tracker

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/holoplot/go-evdev"
)

// MouseStats holds mouse click metrics for one flush interval.
type MouseStats struct {
	TotalClicks int `json:"total_clicks"`
}

// MouseTracker monitors mouse/pointer input devices and aggregates click metrics.
type MouseTracker struct {
	mu       sync.Mutex
	stopOnce sync.Once
	clicks   int
	stopCh   chan struct{}
	interval time.Duration
}

// NewMouseTracker creates a new MouseTracker with the given flush interval.
func NewMouseTracker(interval time.Duration) *MouseTracker {
	return &MouseTracker{
		stopCh:   make(chan struct{}),
		interval: interval,
	}
}

// mouseEvent is a lightweight tagged union sent from device readers to the aggregator.
type mouseEvent struct {
	isClick bool
}

// Start launches goroutines for all discovered pointer/mouse devices and a flush ticker.
func (m *MouseTracker) Start() (<-chan MouseStats, error) {
	devices, err := discoverMice()
	if err != nil || len(devices) == 0 {
		log.Printf("[tracker] Zero-Sudo Mode Active for Mouse: Web frontend / API mouse tracking enabled.")
	} else {
		log.Printf("[tracker] Found %d native evdev mouse/pointer device(s)", len(devices))
	}

	// Buffered: drops events when aggregator is busy — never blocks the reader goroutine.
	eventCh := make(chan mouseEvent, 256)

	for _, path := range devices {
		go func(devPath string) {
			dev, err := evdev.Open(devPath)
			if err != nil {
				log.Printf("[tracker] cannot open mouse dev %s: %v", devPath, err)
				return
			}
			defer dev.Close()
			log.Printf("[tracker] listening on mouse dev %s", devPath)

			for {
				// BLOCKING read — goroutine sleeps until the kernel delivers an event.
				// No select/default spin-loop → zero idle CPU.
				ev, err := dev.ReadOne()
				if err != nil {
					return
				}

				select {
				case <-m.stopCh:
					return
				default:
				}

				if ev.Type == evdev.EV_KEY && ev.Value == 1 && isMouseButton(ev.Code) {
					select {
					case eventCh <- mouseEvent{isClick: true}:
					default:
					}
				}
			}
		}(path)
	}

	// Event aggregator — single goroutine, no mutex contention on hot path.
	go func() {
		for {
			select {
			case <-eventCh:
				m.mu.Lock()
				m.clicks++
				m.mu.Unlock()
			case <-m.stopCh:
				return
			}
		}
	}()

	statsCh := make(chan MouseStats, 4)

	go func() {
		ticker := time.NewTicker(m.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				stats := m.flush()
				select {
				case statsCh <- stats:
				default:
				}
			case <-m.stopCh:
				return
			}
		}
	}()

	return statsCh, nil
}

// RecordMouseActivity records mouse clicks submitted via the frontend (zero-sudo mode).
func (m *MouseTracker) RecordMouseActivity(clicks int) {
	m.mu.Lock()
	m.clicks += clicks
	m.mu.Unlock()
}

// Stop signals all tracker goroutines to exit. Safe to call multiple times.
func (m *MouseTracker) Stop() {
	m.stopOnce.Do(func() { close(m.stopCh) })
}

// flush atomically reads and resets the click counter.
func (m *MouseTracker) flush() MouseStats {
	m.mu.Lock()
	clicks := m.clicks
	m.clicks = 0
	m.mu.Unlock()

	return MouseStats{TotalClicks: clicks}
}

// isMouseButton checks if an evdev code is a mouse button (BTN_LEFT … BTN_TASK).
func isMouseButton(code evdev.EvCode) bool {
	return code >= 272 && code <= 279
}

// discoverMice returns all /dev/input/event* device paths that report pointer events.
// Scans only /dev/input directly (not by-id/by-path which are symlinks to the same nodes).
func discoverMice() ([]string, error) {
	entries, err := os.ReadDir("/dev/input")
	if err != nil {
		return nil, err
	}

	seen := make(map[string]bool)
	var mice []string

	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "event") {
			continue
		}
		path := filepath.Join("/dev/input", name)

		// Resolve symlinks so we never open the same hardware device twice.
		real, err := filepath.EvalSymlinks(path)
		if err != nil {
			real = path
		}
		if seen[real] {
			continue
		}

		dev, err := evdev.Open(path)
		if err != nil {
			continue
		}
		caps := dev.CapableTypes()
		isPointer := false
		for _, t := range caps {
			if t == evdev.EV_REL || t == evdev.EV_ABS {
				isPointer = true
				break
			}
		}
		dev.Close()

		if isPointer {
			mice = append(mice, path)
			seen[real] = true
		}
	}
	return mice, nil
}
