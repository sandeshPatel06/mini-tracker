package tracker

import (
	"bytes"
	"fmt"
	"log"
	"strings"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
)

// WatcherEventType marks the application lifecycle event kind.
type WatcherEventType string

const (
	// WatcherOpen marks a window appearing in _NET_CLIENT_LIST (app launched).
	WatcherOpen WatcherEventType = "open"
	// WatcherClose marks a window disappearing from _NET_CLIENT_LIST (app closed).
	WatcherClose WatcherEventType = "close"
	// WatcherFocus marks the user focusing a window (_NET_ACTIVE_WINDOW change).
	WatcherFocus WatcherEventType = "focus"
)

// TrackerEvent is one application lifecycle observation on the desktop.
type TrackerEvent struct {
	Kind        WatcherEventType `json:"kind"`
	AppName     string           `json:"app_name"`
	WindowTitle string           `json:"window_title"`
}

// windowCacheEntry caches the identity of a window so "close" events can still
// name the application without re-querying a (now-gone) X window.
type windowCacheEntry struct {
	app   string
	title string
}

// StartAppWatcher runs an event-driven X11 application watcher on a dedicated
// connection. It consumes zero CPU while nothing changes on the desktop (the
// event read blocks until the X server delivers one), so it is dramatically
// cheaper than polling and precise down to the moment an app opens, closes or
// receives focus.
//
// The watcher loop re-tries (e.g. once per session reconnect) from the caller
// side when the connection dies, if desired.
func StartAppWatcher(stopCh <-chan struct{}, out chan<- TrackerEvent) error {
	X, err := xgb.NewConn()
	if err != nil {
		return err
	}
	defer X.Close()

	setup := xproto.Setup(X)
	root := setup.DefaultScreen(X).Root

	getAtom := func(name string) xproto.Atom {
		reply, err := xproto.InternAtom(X, true, uint16(len(name)), name).Reply()
		if err != nil {
			return 0
		}
		return reply.Atom
	}

	activeAtom := getAtom("_NET_ACTIVE_WINDOW")
	clientsAtom := getAtom("_NET_CLIENT_LIST")
	nameAtom := getAtom("_NET_WM_NAME")
	wmNameAtom := getAtom("WM_NAME")
	classAtom := getAtom("WM_CLASS")

	if activeAtom == 0 || clientsAtom == 0 {
		log.Printf("[tracker] app watcher: required atoms not found")
		return fmt.Errorf("required atoms not found")
	}

	// Subscribe to root property changes once; every event afterwards is pushed
	// to us by the server — no polling anywhere.
	if err := xproto.ChangeWindowAttributesChecked(X, root, xproto.CwEventMask,
		[]uint32{xproto.EventMaskPropertyChange}).Check(); err != nil {
		return err
	}

	// Prime the initial client list + active window so startup does not emit a
	// flood of historical "open" events for every window that existed already.
	lastClients := loadClientSet(X, clientsAtom, root)
	windowCache := cacheWindowIdentities(X, nameAtom, wmNameAtom, classAtom, lastClients)
	var lastActive xproto.Window
	if reply, err := fetchProperty(X, root, activeAtom); err == nil && len(reply.Value) >= 4 {
		lastActive = xproto.Window(xgb.Get32(reply.Value[:4]))
	}

	for {
		select {
		case <-stopCh:
			return nil
		default:
		}

		ev, xerr := X.WaitForEvent()
		if xerr != nil || ev == nil {
			// X error / connection issue — return; the caller loop can reconnect.
			if xerr != nil {
				return fmt.Errorf("x11 event read failed: %v", xerr)
			}
			return fmt.Errorf("x11 event read closed")
		}
		select {
		case <-stopCh:
			return nil
		default:
		}

		propEv, ok := ev.(*xproto.PropertyNotifyEvent)
		if !ok || propEv.Window != root {
			continue
		}

		switch propEv.Atom {
		case activeAtom:
			if reply, err := fetchProperty(X, root, activeAtom); err == nil && len(reply.Value) >= 4 {
				id := xproto.Window(xgb.Get32(reply.Value[:4]))
				if id != lastActive {
					lastActive = id
					info := windowCache[id]
					if info.app == "" && info.title == "" {
						info = queryWindowInfo(X, nameAtom, wmNameAtom, classAtom, id)
						windowCache[id] = info
					}
					emitEvent(out, TrackerEvent{Kind: WatcherFocus, AppName: info.app, WindowTitle: info.title})
				}
			}

		case clientsAtom:
			current := loadClientSet(X, clientsAtom, root)

			// Opened windows
			for id := range current {
				if _, exists := lastClients[id]; !exists {
					info := queryWindowInfo(X, nameAtom, wmNameAtom, classAtom, id)
					windowCache[id] = info
					emitEvent(out, TrackerEvent{Kind: WatcherOpen, AppName: info.app, WindowTitle: info.title})
				}
			}

			// Closed windows (identity pulled from the cache)
			for id := range lastClients {
				if _, exists := current[id]; !exists {
					info := windowCache[id]
					emitEvent(out, TrackerEvent{Kind: WatcherClose, AppName: info.app, WindowTitle: info.title})
					delete(windowCache, id)
				}
			}

			// Trim the cache to the current set so long sessions never grow it.
			for id := range windowCache {
				if _, exists := current[id]; !exists {
					delete(windowCache, id)
				}
			}

			lastClients = current

		default:
			// unrelated root property change — ignore
		}
	}
}

func emitEvent(out chan<- TrackerEvent, ev TrackerEvent) {
	select {
	case out <- ev:
	default:
		// Watching is advisory; drop rather than block the X event loop.
		log.Printf("[tracker] app watcher: event buffer full, dropped %s event (%s)", ev.Kind, ev.AppName)
	}
}

// fetchProperty is a tiny helper for root property reads.
func fetchProperty(X *xgb.Conn, window xproto.Window, atom xproto.Atom) (xproto.GetPropertyReply, error) {
	reply, err := xproto.GetProperty(X, false, window, atom, xproto.GetPropertyTypeAny, 0, (1<<32)-1).Reply()
	return *reply, err
}

// loadClientSet returns the current managed window id set.
func loadClientSet(X *xgb.Conn, clientsAtom xproto.Atom, root xproto.Window) map[xproto.Window]bool {
	set := map[xproto.Window]bool{}
	reply, err := xproto.GetProperty(X, false, root, clientsAtom, xproto.GetPropertyTypeAny, 0, (1<<32)-1).Reply()
	if err != nil {
		return set
	}
	val := reply.Value
	for len(val) >= 4 {
		set[xproto.Window(xgb.Get32(val[:4]))] = true
		val = val[4:]
	}
	return set
}

// cacheWindowIdentities resolves app/title info for every window in the set.
func cacheWindowIdentities(X *xgb.Conn, nameAtom, wmNameAtom, classAtom xproto.Atom, windows map[xproto.Window]bool) map[xproto.Window]windowCacheEntry {
	cache := map[xproto.Window]windowCacheEntry{}
	for id := range windows {
		cache[id] = queryWindowInfo(X, nameAtom, wmNameAtom, classAtom, id)
	}
	return cache
}

// queryWindowInfo resolves title + WM_CLASS app identity of a window.
func queryWindowInfo(X *xgb.Conn, nameAtom, wmNameAtom, classAtom xproto.Atom, id xproto.Window) windowCacheEntry {
	info := windowCacheEntry{}

	if reply, err := xproto.GetProperty(X, false, id, nameAtom, xproto.GetPropertyTypeAny, 0, (1<<32)-1).Reply(); err == nil && len(reply.Value) > 0 {
		info.title = string(reply.Value)
	}
	if info.title == "" {
		if reply, err := xproto.GetProperty(X, false, id, wmNameAtom, xproto.GetPropertyTypeAny, 0, (1<<32)-1).Reply(); err == nil && len(reply.Value) > 0 {
			info.title = string(reply.Value)
		}
	}

	if reply, err := xproto.GetProperty(X, false, id, classAtom, xproto.GetPropertyTypeAny, 0, (1<<32)-1).Reply(); err == nil && len(reply.Value) > 0 {
		parts := strings.SplitN(string(bytes.TrimRight(reply.Value, "\x00")), "\x00", 2)
		switch {
		case len(parts) >= 2 && parts[1] != "":
			info.app = strings.TrimSpace(parts[1])
		case len(parts) >= 1:
			info.app = strings.TrimSpace(parts[0])
		}
	}
	return info
}
