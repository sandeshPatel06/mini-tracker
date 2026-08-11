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

// MouseStats holds mouse interaction metrics for one flush interval.
type MouseStats struct {
	TotalClicks   int     `json:"total_clicks"`
	MouseDistance float64 `json:"mouse_distance"`
}

// MouseTracker monitors mouse/pointer input devices and aggregates click/movement metrics.
type MouseTracker struct {
	mu            sync.Mutex
	stopOnce      sync.Once
	clicks        int
	distanceSqAcc float64 // accumulated squared-distance components; sqrt taken only at flush
	stopCh        chan struct{}
	interval      time.Duration
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
	dx, dy  int32 // accumulated relative/delta values for one SYN_REPORT packet
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

			var (
				currX, currY         float64
				pendingDX, pendingDY int32
				hasPending           bool
			)

			flushMove := func() {
				if hasPending && (pendingDX != 0 || pendingDY != 0) {
					select {
					case eventCh <- mouseEvent{dx: pendingDX, dy: pendingDY}:
					default:
					}
					pendingDX, pendingDY = 0, 0
				}
				hasPending = false
			}

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

				switch ev.Type {
				case evdev.EV_SYN:
					// SYN_REPORT = end of event packet; flush accumulated movement.
					flushMove()

				case evdev.EV_KEY:
					if ev.Value == 1 && isMouseButton(ev.Code) {
						flushMove()
						select {
						case eventCh <- mouseEvent{isClick: true}:
						default:
						}
					}

				case evdev.EV_REL:
					switch ev.Code {
					case evdev.REL_X:
						pendingDX += ev.Value
					case evdev.REL_Y:
						pendingDY += ev.Value
					}
					hasPending = true

				case evdev.EV_ABS:
					if ev.Code == evdev.ABS_X {
						newVal := float64(ev.Value)
						if currX > 0 {
							pendingDX += int32(math.Round(newVal - currX))
							hasPending = true
						}
						currX = newVal
					} else if ev.Code == evdev.ABS_Y {
						newVal := float64(ev.Value)
						if currY > 0 {
							pendingDY += int32(math.Round(newVal - currY))
							hasPending = true
						}
						currY = newVal
					}
				}
			}
		}(path)
	}

	// Event aggregator — single goroutine, no mutex contention on hot path.
	go func() {
		for {
			select {
			case ev := <-eventCh:
				m.mu.Lock()
				if ev.isClick {
					m.clicks++
				} else {
					dx := float64(ev.dx)
					dy := float64(ev.dy)
					// Accumulate squared distance — only take sqrt once at flush time.
					m.distanceSqAcc += dx*dx + dy*dy
				}
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

// RecordMouseActivity records mouse activity submitted via the frontend (zero-sudo mode).
func (m *MouseTracker) RecordMouseActivity(clicks int, distance float64) {
	m.mu.Lock()
	m.clicks += clicks
	// Frontend reports straight pixel distance; square it to match flush math.
	m.distanceSqAcc += distance * distance
	m.mu.Unlock()
}

// Stop signals all tracker goroutines to exit. Safe to call multiple times.
func (m *MouseTracker) Stop() {
	m.stopOnce.Do(func() { close(m.stopCh) })
}

// flush atomically reads and resets the counters, computing final distance only here.
func (m *MouseTracker) flush() MouseStats {
	m.mu.Lock()
	clicks := m.clicks
	sqAcc := m.distanceSqAcc
	m.clicks = 0
	m.distanceSqAcc = 0
	m.mu.Unlock()

	dist := 0.0
	if sqAcc > 0 {
		dist = math.Round(math.Sqrt(sqAcc)*10) / 10
	}
	return MouseStats{
		TotalClicks:   clicks,
		MouseDistance: dist,
	}
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
