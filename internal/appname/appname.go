// Package appname normalises application identities so that the same app is
// never listed twice.
//
// The tracker records the application name from two different sources: the
// X11 WM_CLASS of the focused window (for example "google-chrome", "code",
// "Gnome-terminal") and the Gemini analysis, which returns a human readable
// name (for example "Google Chrome", "Visual Studio Code", "Terminal").
// Both spellings refer to the same application, so they are folded into one
// canonical, lower-cased identity before they are stored or displayed.
package appname

import "strings"

// aliases maps an already normalised name to its canonical spelling. Keys are
// lower-cased with separators collapsed into single spaces.
var aliases = map[string]string{
	"gnome terminal":        "terminal",
	"gnome terminal server": "terminal",
	"xfce4 terminal":        "terminal",
	"code":                  "visual studio code",
	"code oss":              "visual studio code",
	"vscodium":              "visual studio code",
	"get hike":              "get-hike",
	"gethike":               "get-hike",
}

// Canonical returns the canonical identity of an application name.
//
// The name is trimmed, cut at a window-title separator, lower-cased and its
// separators are collapsed into single spaces so that "Google Chrome",
// "google-chrome" and "Google_Chrome" all resolve to the same identity.
// Known spellings are folded into their canonical form.
//
// An empty string is returned when the value does not look like an
// application name (empty, a file path or a copied window title).
func Canonical(name string) string {
	raw := strings.TrimSpace(name)
	if raw == "" {
		return ""
	}

	// The AI frequently answers with the window title ("OC | Import session
	// from ..."). Keep the leading identity and drop the title part.
	if at := strings.Index(raw, " | "); at > 0 {
		raw = strings.TrimSpace(raw[:at])
	}
	if !plausible(raw) {
		return ""
	}

	cleaned := strings.ToLower(raw)
	cleaned = strings.Map(func(r rune) rune {
		switch r {
		case '-', '_', '+', ',', '\t', '\n', '\r':
			return ' '
		}
		return r
	}, cleaned)
	cleaned = strings.Join(strings.Fields(cleaned), " ")
	if cleaned == "" {
		return ""
	}
	if canonical, ok := aliases[cleaned]; ok {
		return canonical
	}
	return cleaned
}

// plausible reports whether a value looks like an application name instead of
// a file path or a sentence copied from the screen.
func plausible(name string) bool {
	if name == "" || len(name) > 48 {
		return false
	}
	if strings.ContainsAny(name, "/\\") {
		return false
	}
	if strings.Contains(name, " · ") {
		return false
	}
	if len(strings.Fields(name)) > 4 {
		return false
	}
	return true
}
