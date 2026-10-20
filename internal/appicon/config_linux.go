//go:build linux

package appicon

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/codelif/xdgicons"
)

func defaultConfig() Config {
	return Config{
		AppDirs:      applicationDirs(),
		ThemeLookup:  newThemeLookup(),
		MaxIconBytes: defaultMaxIconBytes,
	}
}

func applicationDirs() []string {
	home, _ := os.UserHomeDir()
	var dirs []string
	add := func(dir string) {
		dir = expandHome(dir)
		if dir == "" {
			return
		}
		for _, existing := range dirs {
			if existing == dir {
				return
			}
		}
		dirs = append(dirs, dir)
	}

	if home != "" {
		add(filepath.Join(home, ".local", "share", "applications"))
	}

	dataHome := os.Getenv("XDG_DATA_HOME")
	if dataHome == "" && home != "" {
		dataHome = filepath.Join(home, ".local", "share")
	}
	if dataHome != "" {
		add(filepath.Join(dataHome, "applications"))
	}

	dataDirs := os.Getenv("XDG_DATA_DIRS")
	if dataDirs == "" {
		dataDirs = "/usr/local/share:/usr/share"
	}
	for _, dir := range strings.Split(dataDirs, ":") {
		if dir != "" {
			add(filepath.Join(dir, "applications"))
		}
	}

	if home != "" {
		add(filepath.Join(home, ".local", "share", "flatpak", "exports", "share", "applications"))
	}
	add("/var/lib/flatpak/exports/share/applications")
	add("/var/lib/snapd/desktop/applications")

	return dirs
}

func newThemeLookup() func(string) (string, bool) {
	lookups := []*xdgicons.IconLookup{
		xdgicons.NewIconLookupWithConfig(xdgicons.LookupConfig{
			FallbackTheme: "Adwaita",
			Extensions:    []string{"png", "svg"},
			DefaultSize:   48,
		}),
		xdgicons.NewIconLookupWithConfig(xdgicons.LookupConfig{
			Theme:         "Adwaita",
			FallbackTheme: "hicolor",
			Extensions:    []string{"png", "svg"},
			DefaultSize:   48,
		}),
	}

	return func(name string) (string, bool) {
		for _, lookup := range lookups {
			icon, err := lookup.FindIcon(name, 48, 1)
			if err == nil && icon.Path != "" {
				return icon.Path, true
			}
		}
		return "", false
	}
}
