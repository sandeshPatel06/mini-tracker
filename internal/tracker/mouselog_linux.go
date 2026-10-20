//go:build linux

package tracker

import (
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/holoplot/go-evdev"
)

// startNativeMouseCapture launches one reader goroutine per discovered pointer
// device. It is Linux-only because it relies on evdev.
func startNativeMouseCapture(eventCh chan<- mouseEvent, stopCh <-chan struct{}) {
	devices, err := discoverMice()
	if err != nil || len(devices) == 0 {
		log.Printf("[tracker] Zero-Sudo Mode Active for Mouse: Web frontend / API mouse tracking enabled.")
	} else {
		log.Printf("[tracker] Found %d native evdev mouse/pointer device(s)", len(devices))
	}

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
				case <-stopCh:
					return
				default:
				}

				if ev.Type == evdev.EV_KEY && ev.Value == 1 && isMouseButton(int(ev.Code)) {
					select {
					case eventCh <- mouseEvent{isClick: true}:
					default:
					}
				}
			}
		}(path)
	}
}

// isMouseButton checks if an evdev code is a mouse button (BTN_LEFT … BTN_TASK).
func isMouseButton(code int) bool {
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
