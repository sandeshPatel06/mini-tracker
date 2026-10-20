package appicon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCleanAppName(t *testing.T) {
	tests := map[string]string{
		"VS Code":                  "VS Code",
		"OC | Import session...":   "OC",
		"Google Chrome — Profile":  "Google Chrome",
		"Antigravity IDE: project": "Antigravity IDE",
		"Code.exe":                 "Code",
	}
	for input, want := range tests {
		if got := cleanAppName(input); got != want {
			t.Fatalf("cleanAppName(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestNormalize(t *testing.T) {
	tests := map[string]string{
		"VS Code":            "vscode",
		"Google-Chrome":      "googlechrome",
		"org.gnome.Terminal": "orggnometerminal",
	}
	for input, want := range tests {
		if got := normalize(input); got != want {
			t.Fatalf("normalize(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestResolverLookup(t *testing.T) {
	dir := t.TempDir()
	codeIcon := writeTestFile(t, dir, "code.png", []byte("png-code"))
	chromeIcon := writeTestFile(t, dir, "chrome.png", []byte("png-chrome"))
	terminalIcon := writeTestFile(t, dir, "terminal.svg", []byte("<svg/>"))

	writeDesktop(t, dir, "code.desktop", `
[Desktop Entry]
Type=Application
Name=Visual Studio Code
Icon=`+codeIcon+`
Exec=/usr/bin/code %F
StartupWMClass=Code
`)
	writeDesktop(t, dir, "terminal.desktop", `
[Desktop Entry]
Type=Application
Name=Terminal
Icon=org.gnome.Terminal
Exec=gnome-terminal
StartupWMClass=Gnome-terminal
`)
	writeDesktop(t, dir, "hidden.desktop", `
[Desktop Entry]
Type=Application
Name=Hidden App
Icon=`+codeIcon+`
Hidden=true
`)

	themes := map[string]string{
		"google-chrome":      chromeIcon,
		"chrome":             chromeIcon,
		"org.gnome.Terminal": terminalIcon,
		"org.gnome.terminal": terminalIcon,
		"utilities-terminal": terminalIcon,
	}
	resolver := newResolver(Config{
		AppDirs: []string{dir},
		ThemeLookup: func(name string) (string, bool) {
			path, ok := themes[name]
			return path, ok
		},
		MaxIconBytes: defaultMaxIconBytes,
	})

	tests := []struct {
		name     string
		wantMIME string
	}{
		{"VS Code", "image/png"},
		{"Code", "image/png"},
		{"OC | Import session...", "image/svg+xml"},
		{"Google Chrome", "image/png"},
		{"Unknown Application", ""},
		{"Hidden App", ""},
	}
	for _, test := range tests {
		icon, ok := resolver.Lookup(test.name)
		if test.wantMIME == "" {
			if ok {
				t.Fatalf("Lookup(%q) unexpectedly succeeded", test.name)
			}
			continue
		}
		if !ok {
			t.Fatalf("Lookup(%q) failed", test.name)
		}
		if icon.MIMEType != test.wantMIME {
			t.Fatalf("Lookup(%q) MIME = %q, want %q", test.name, icon.MIMEType, test.wantMIME)
		}
		if !strings.HasPrefix(icon.DataURL(), "data:"+test.wantMIME+";base64,") {
			t.Fatalf("Lookup(%q) data URL has unexpected prefix", test.name)
		}
	}
}

func TestResolverRejectsOversizedAndUnsafeIcons(t *testing.T) {
	dir := t.TempDir()
	largeIcon := writeTestFile(t, dir, "large.png", make([]byte, 1024))
	writeDesktop(t, dir, "large.desktop", `
[Desktop Entry]
Type=Application
Name=Large App
Icon=`+largeIcon+`
`)
	writeDesktop(t, dir, "unsafe.desktop", `
[Desktop Entry]
Type=Application
Name=Unsafe App
Icon=../../etc/passwd
`)

	unsafeLookupCalled := false
	resolver := newResolver(Config{
		AppDirs: []string{dir},
		ThemeLookup: func(name string) (string, bool) {
			if strings.Contains(name, "..") {
				unsafeLookupCalled = true
			}
			return "", false
		},
		MaxIconBytes: 32,
	})

	if _, ok := resolver.Lookup("Large App"); ok {
		t.Fatal("oversized icon should be rejected")
	}
	if _, ok := resolver.Lookup("Unsafe App"); ok {
		t.Fatal("unsafe icon should be rejected")
	}
	if unsafeLookupCalled {
		t.Fatal("theme lookup received an unsafe icon name")
	}
}

func writeTestFile(t *testing.T, dir, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeDesktop(t *testing.T, dir, name, content string) {
	t.Helper()
	writeTestFile(t, dir, name, []byte(content))
}
