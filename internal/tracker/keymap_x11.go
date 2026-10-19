package tracker

import (
	"log"
	"time"

	"github.com/holoplot/go-evdev"
	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
)

// x11KeyPollInterval controls how often the keyboard state snapshot is taken.
// 50ms misses very few human keystrokes (a pressed key is held for ~60-100ms)
// while being intentionally unobtrusive on CPU.
const x11KeyPollInterval = 50 * time.Millisecond

// pollX11Keymap runs a zero-sudo fallback keyboard monitor for systems where the
// /dev/input/event* nodes are unreadable (no input group). It polls the X11 full
// keyboard map (core QueryKeymap request — no special permissions beyond an X
// client connection) and forwards rising edges (key pushes) into the same event
// channel the evdev readers feed, so entropy accounting stays uniform. X keycodes
// are offset by 8 maps to evdev codes, but only uniqueness matters for scoring.
func pollX11Keymap(eventCh chan<- evdev.EvCode, stopCh <-chan struct{}) error {
	conn, err := xgb.NewConn()
	if err != nil {
		return err
	}
	defer conn.Close()

	log.Printf("[tracker] Zero-Sudo keyboard fallback active: X11 keymap polling (no /dev/input access needed)")

	var prev [32]byte
	ticker := time.NewTicker(x11KeyPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-stopCh:
			return nil
		case <-ticker.C:
		}

		reply, err := xproto.QueryKeymap(conn).Reply()
		if err != nil || len(reply.Keys) == 0 {
			// A dead X server ends the fallback; the caller can re-arm it.
			return err
		}

		for i := 0; i < len(reply.Keys) && i < len(prev); i++ {
			newly := reply.Keys[i] & ^prev[i]
			if newly == 0 {
				continue
			}
			for bit := 0; bit < 8; bit++ {
				if newly&(1<<bit) == 0 {
					continue
				}
				select {
				case eventCh <- evdev.EvCode(i*8 + bit + 8):
				default:
				}
			}
		}
		copy(prev[:], reply.Keys)
	}
}
