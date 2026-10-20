package appname

import "testing"

func TestCanonical(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"empty", "", ""},
		{"whitespace", "   ", ""},
		{"chrome space", "Google Chrome", "google chrome"},
		{"chrome hyphen", "Google-chrome", "google chrome"},
		{"chrome underscore", "google_chrome", "google chrome"},
		{"mixed case", "Gnome-Terminal", "terminal"},
		{"terminal alias", "Gnome-terminal", "terminal"},
		{"terminal plain", "Terminal", "terminal"},
		{"vscode alias", "code", "visual studio code"},
		{"vscode full", "Visual Studio Code", "visual studio code"},
		{"get hike", "Get-Hike", "get-hike"},
		{"oc title", "OC | Import session from session-ses_eab3.md", "oc"},
		{"path", "ui-ux-pro-max-skill/.claude/skills/SKILL.md", ""},
		{"sentence", "this is a very long window title that should be rejected", ""},
		{"middle dot", "OC · build.yml me Windows", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Canonical(tc.input); got != tc.want {
				t.Fatalf("Canonical(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestCanonicalIsStable(t *testing.T) {
	variants := []string{"Google Chrome", "google-chrome", "GOOGLE_CHROME", "google chrome"}
	want := Canonical(variants[0])
	for _, variant := range variants[1:] {
		if got := Canonical(variant); got != want {
			t.Fatalf("Canonical(%q) = %q, want %q", variant, got, want)
		}
	}
}
