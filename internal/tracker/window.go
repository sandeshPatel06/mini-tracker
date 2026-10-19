package tracker

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
)

// ErrNoActiveWindow is returned when X11 reports no focused window (lock screen,
// desktop shell or a logout session). Callers treat this as an expected idle state.
var ErrNoActiveWindow = errors.New("no active window")

// ActiveWindowResult holds information about the currently active window.
type ActiveWindowResult struct {
	Title string `json:"title"`

	// AppName is the native application identity from WM_CLASS (e.g. "Code",
	// "gedit", "Slack") so analytics do not depend on AI text guesses.
	AppName string `json:"app_name,omitempty"`
}

// GetActiveWindowTitle connects to the X11 server and fetches the title of the active window.
func GetActiveWindowTitle() (*ActiveWindowResult, error) {
	return GetActiveWindowInfo()
}

// GetActiveWindowInfo connects to the X11 server and fetches the title plus the
// WM_CLASS application identity of the focused window.
func GetActiveWindowInfo() (*ActiveWindowResult, error) {
	X, err := xgb.NewConn()
	if err != nil {
		log.Printf("[tracker] window tracker: could not connect to X11: %v", err)
		return nil, fmt.Errorf("could not connect to X11: %w", err)
	}
	defer X.Close()

	setup := xproto.Setup(X)
	root := setup.DefaultScreen(X).Root

	// Helper to get an atom by name
	getAtom := func(name string) xproto.Atom {
		reply, err := xproto.InternAtom(X, true, uint16(len(name)), name).Reply()
		if err != nil {
			return 0
		}
		return reply.Atom
	}

	activeWinAtom := getAtom("_NET_ACTIVE_WINDOW")
	nameAtom := getAtom("_NET_WM_NAME")
	classAtom := getAtom("WM_CLASS")

	if activeWinAtom == 0 {
		return nil, fmt.Errorf("required atoms not found")
	}

	activeWinReply, err := xproto.GetProperty(X, false, root, activeWinAtom, xproto.GetPropertyTypeAny, 0, (1<<32)-1).Reply()
	if err != nil || len(activeWinReply.Value) == 0 {
		return nil, fmt.Errorf("could not get active window")
	}

	activeWinID := xproto.Window(xgb.Get32(activeWinReply.Value))
	if activeWinID == 0 {
		return nil, ErrNoActiveWindow
	}

	res := &ActiveWindowResult{}

	// Try to get _NET_WM_NAME (UTF8 string)
	nameReply, err := xproto.GetProperty(X, false, activeWinID, nameAtom, xproto.GetPropertyTypeAny, 0, (1<<32)-1).Reply()

	// Fallback to WM_NAME (usually latin1/ascii string) if _NET_WM_NAME is missing or empty
	if err != nil || len(nameReply.Value) == 0 {
		wmNameAtom := getAtom("WM_NAME")
		if wmNameAtom != 0 {
			nameReply, err = xproto.GetProperty(X, false, activeWinID, wmNameAtom, xproto.GetPropertyTypeAny, 0, (1<<32)-1).Reply()
		}
	}

	if err == nil && len(nameReply.Value) > 0 {
		res.Title = string(nameReply.Value)
	}

	// WM_CLASS encodes "instance\0class\0" — the class field is the stable, native
	// application name. NUL bytes are stripped defensively in case the client
	// leaves the property unpadded.
	if classAtom != 0 {
		classReply, cerr := xproto.GetProperty(X, false, activeWinID, classAtom, xproto.GetPropertyTypeAny, 0, (1<<32)-1).Reply()
		if cerr == nil && len(classReply.Value) > 0 {
			parts := strings.Split(string(bytes.TrimRight(classReply.Value, "\x00")), "\x00")
			switch {
			case len(parts) >= 2 && parts[1] != "":
				res.AppName = strings.TrimSpace(parts[1])
			case len(parts) >= 1 && parts[0] != "":
				res.AppName = strings.TrimSpace(parts[0])
			}
		}
	}

	return res, nil
}
