package tracker

import (
	"fmt"
	"log"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
)

// ActiveWindowResult holds information about the currently active window.
type ActiveWindowResult struct {
	Title string `json:"title"`
}

// GetActiveWindowTitle connects to the X11 server and fetches the title of the active window.
func GetActiveWindowTitle() (*ActiveWindowResult, error) {
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

	if activeWinAtom == 0 || nameAtom == 0 {
		return nil, fmt.Errorf("required atoms not found")
	}

	activeWinReply, err := xproto.GetProperty(X, false, root, activeWinAtom, xproto.GetPropertyTypeAny, 0, (1<<32)-1).Reply()
	if err != nil || len(activeWinReply.Value) == 0 {
		return nil, fmt.Errorf("could not get active window")
	}

	activeWinID := xproto.Window(xgb.Get32(activeWinReply.Value))
	if activeWinID == 0 {
		return nil, fmt.Errorf("no active window")
	}

	// Try to get _NET_WM_NAME (UTF8 string)
	nameReply, err := xproto.GetProperty(X, false, activeWinID, nameAtom, xproto.GetPropertyTypeAny, 0, (1<<32)-1).Reply()
	
	// Fallback to WM_NAME (usually latin1/ascii string) if _NET_WM_NAME is missing or empty
	if err != nil || len(nameReply.Value) == 0 {
		wmNameAtom := getAtom("WM_NAME")
		if wmNameAtom != 0 {
			nameReply, err = xproto.GetProperty(X, false, activeWinID, wmNameAtom, xproto.GetPropertyTypeAny, 0, (1<<32)-1).Reply()
		}
	}

	title := ""
	if err == nil && len(nameReply.Value) > 0 {
		title = string(nameReply.Value)
	}

	return &ActiveWindowResult{
		Title: title,
	}, nil
}
