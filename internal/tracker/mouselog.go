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
	mu         sync.Mutex
	clicks     int
	distance   float64
	stopCh     chan struct{}
	interval   time.Duration
	lastX      float64
	lastY      float64
	hasLastPos bool
}

// NewMouseTracker creates a new MouseTracker with the given flush interval.
func NewMouseTracker(interval time.Duration) *MouseTracker {
	return &MouseTracker{
		stopCh:   make(chan struct{}),
		interval: interval,
	}
}

// Start launches goroutines for all discovered pointer/mouse devices and a flush ticker.
func (m *MouseTracker) Start() (<-chan MouseStats, error) {
	devices, err := discoverMice()
	if err != nil || len(devices) == 0 {
		log.Printf("[tracker] Zero-Sudo Mode Active for Mouse: Web frontend / API mouse tracking enabled.")
	} else {
		log.Printf("[tracker] Found %d native evdev mouse/pointer device(s)", len(devices))
	}

	type mouseEvent struct {
		isClick bool
		relX    float64
		relY    float64
	}

	eventCh := make(chan mouseEvent, 512)

	for _, path := range devices {
		go func(devPath string) {
			dev, err := evdev.Open(devPath)
			if err != nil {
				log.Printf("[tracker] cannot open mouse dev %s: %v", devPath, err)
				return
			}
			defer dev.Close()
			log.Printf("[tracker] listening on mouse dev %s", devPath)

			var currX, currY float64

			for {
				select {
				case <-m.stopCh:
					return
				default:
					ev, err := dev.ReadOne()
					if err != nil {
						return
					}

					// Mouse click events (EV_KEY: BTN_LEFT, BTN_RIGHT, BTN_MIDDLE, BTN_SIDE, BTN_EXTRA)
					if ev.Type == evdev.EV_KEY && ev.Value == 1 {
						if isMouseButton(ev.Code) {
							select {
							case eventCh <- mouseEvent{isClick: true}:
							default:
							}
						}
					}

					// Relative mouse movement (EV_REL: REL_X, REL_Y)
					if ev.Type == evdev.EV_REL {
						var rx, ry float64
						if ev.Code == evdev.REL_X {
							rx = float64(ev.Value)
						} else if ev.Code == evdev.REL_Y {
							ry = float64(ev.Value)
						}
						if rx != 0 || ry != 0 {
							select {
							case eventCh <- mouseEvent{isClick: false, relX: rx, relY: ry}:
							default:
							}
						}
					}

					// Absolute positioning (EV_ABS: ABS_X, ABS_Y e.g. touchpad/drawing tablet)
					if ev.Type == evdev.EV_ABS {
						if ev.Code == evdev.ABS_X || ev.Code == evdev.ABS_Y {
							newVal := float64(ev.Value)
							var rx, ry float64
							if ev.Code == evdev.ABS_X {
								if currX > 0 {
									rx = math.Abs(newVal - currX)
								}
								currX = newVal
							} else if ev.Code == evdev.ABS_Y {
								if currY > 0 {
									ry = math.Abs(newVal - currY)
								}
								currY = newVal
							}
							if rx > 0 || ry > 0 {
								select {
								case eventCh <- mouseEvent{isClick: false, relX: rx, relY: ry}:
								default:
								}
							}
						}
					}
				}
			}
		}(path)
	}

	// Event aggregator
	go func() {
		for {
			select {
			case ev := <-eventCh:
				m.mu.Lock()
				if ev.isClick {
					m.clicks++
				} else {
					dist := math.Sqrt(ev.relX*ev.relX + ev.relY*ev.relY)
					m.distance += dist
				}
				m.mu.Unlock()
			case <-m.stopCh:
				return
			}
		}
	}()

	statsCh := make(chan MouseStats, 4)

	// Flush ticker
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

// RecordMouseActivity records mouse activity submitted via API or desktop frontend.
func (m *MouseTracker) RecordMouseActivity(clicks int, distance float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.clicks += clicks
	m.distance += distance
}

// Stop signals all tracker goroutines to exit.
func (m *MouseTracker) Stop() {
	close(m.stopCh)
}

// flush atomically reads and resets the counters.
func (m *MouseTracker) flush() MouseStats {
	m.mu.Lock()
	clicks := m.clicks
	dist := m.distance
	m.clicks = 0
	m.distance = 0
	m.mu.Unlock()

	return MouseStats{
		TotalClicks:   clicks,
		MouseDistance: math.Round(dist*10) / 10,
	}
}

// isMouseButton checks if an evdev code corresponds to a mouse button.
func isMouseButton(code evdev.EvCode) bool {
	// BTN_LEFT = 0x110 (272), BTN_RIGHT = 0x111 (273), BTN_MIDDLE = 0x112 (274), BTN_SIDE = 0x113, BTN_EXTRA = 0x114
	return code >= 272 && code <= 279
}

// discoverMice returns all /dev/input pointer/mouse device paths.
func discoverMice() ([]string, error) {
	dirsToScan := []string{"/dev/input"}
	if entries, err := os.ReadDir("/dev/input/by-id"); err == nil && len(entries) > 0 {
		dirsToScan = append(dirsToScan, "/dev/input/by-id")
	}
	if entries, err := os.ReadDir("/dev/input/by-path"); err == nil && len(entries) > 0 {
		dirsToScan = append(dirsToScan, "/dev/input/by-path")
	}

	seen := make(map[string]bool)
	var mice []string

	for _, dir := range dirsToScan {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			name := e.Name()
			if !strings.Contains(name, "event") && !strings.Contains(name, "mouse") {
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
				seen[path] = true
			}
		}
	}
	return mice, nil
}
