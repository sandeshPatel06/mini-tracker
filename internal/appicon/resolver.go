package appicon

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/rkoesters/xdg/desktop"
)

const (
	defaultMaxIconBytes = 512 << 10
	indexTTL            = 10 * time.Minute
	missTTL             = 5 * time.Minute
	maxAppNameLen       = 160
	maxIconNameLen      = 128
)

type Icon struct {
	Data     []byte
	MIMEType string
}

func (i Icon) DataURL() string {
	return "data:" + i.MIMEType + ";base64," + base64.StdEncoding.EncodeToString(i.Data)
}

type Config struct {
	AppDirs      []string
	ThemeLookup  func(name string) (string, bool)
	MaxIconBytes int64
}

type Resolver struct {
	mu      sync.Mutex
	cfg     Config
	index   *appIndex
	indexAt time.Time
	icons   map[string]Icon
	misses  map[string]time.Time
	now     func() time.Time
}

func NewResolver() *Resolver {
	return newResolver(defaultConfig())
}

func newResolver(cfg Config) *Resolver {
	if cfg.MaxIconBytes <= 0 {
		cfg.MaxIconBytes = defaultMaxIconBytes
	}
	return &Resolver{
		cfg:    cfg,
		icons:  make(map[string]Icon),
		misses: make(map[string]time.Time),
		now:    time.Now,
	}
}

func (r *Resolver) Warm() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ensureIndexLocked(r.now())
}

func (r *Resolver) Lookup(appName string) (Icon, bool) {
	if len(appName) > maxAppNameLen {
		appName = appName[:maxAppNameLen]
	}
	cleaned := cleanAppName(appName)
	key := normalize(cleaned)
	if key == "" {
		return Icon{}, false
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	now := r.now()
	if at, ok := r.misses[key]; ok && now.Sub(at) < missTTL {
		return Icon{}, false
	}

	r.ensureIndexLocked(now)
	if r.index == nil {
		r.misses[key] = now
		return Icon{}, false
	}

	if entry := r.index.find(key); entry != nil {
		if icon, ok := r.iconForEntryLocked(entry); ok {
			return icon, true
		}
	}

	seen := make(map[string]bool)
	for _, candidates := range [][]string{aliasTargets[key], iconNameCandidates(cleaned)} {
		for _, candidate := range candidates {
			if seen[candidate] {
				continue
			}
			seen[candidate] = true
			if path, ok := r.themeIconLocked(candidate); ok {
				if icon, ok := r.readIconLocked(path); ok {
					return icon, true
				}
			}
		}
	}

	r.misses[key] = now
	return Icon{}, false
}

func (r *Resolver) ensureIndexLocked(now time.Time) {
	if r.index != nil && now.Sub(r.indexAt) < indexTTL {
		return
	}
	r.index = buildIndex(r.cfg.AppDirs)
	r.indexAt = now
	for key := range r.misses {
		delete(r.misses, key)
	}
}

func (r *Resolver) iconForEntryLocked(entry *appEntry) (Icon, bool) {
	if entry.icon != "" {
		if filepath.IsAbs(entry.icon) {
			if icon, ok := r.readIconLocked(entry.icon); ok {
				return icon, true
			}
		} else if path, ok := r.themeIconLocked(entry.icon); ok {
			if icon, ok := r.readIconLocked(path); ok {
				return icon, true
			}
		}
	}
	for _, candidate := range iconNameCandidates(entry.name) {
		if path, ok := r.themeIconLocked(candidate); ok {
			if icon, ok := r.readIconLocked(path); ok {
				return icon, true
			}
		}
	}
	return Icon{}, false
}

func (r *Resolver) themeIconLocked(name string) (string, bool) {
	if r.cfg.ThemeLookup == nil {
		return "", false
	}
	name = strings.TrimSpace(name)
	if !safeIconName(name) {
		return "", false
	}
	return r.cfg.ThemeLookup(name)
}

func (r *Resolver) readIconLocked(path string) (Icon, bool) {
	path = expandHome(path)
	if !filepath.IsAbs(path) {
		return Icon{}, false
	}
	path = filepath.Clean(path)

	candidates := []string{path}
	if filepath.Ext(path) == "" {
		candidates = []string{path + ".png", path + ".svg"}
	}

	for _, candidate := range candidates {
		mimeType := iconMIMEType(candidate)
		if mimeType == "" {
			continue
		}
		if icon, ok := r.icons[candidate]; ok {
			return icon, true
		}
		info, err := os.Stat(candidate)
		if err != nil || info.IsDir() || info.Size() > r.cfg.MaxIconBytes {
			continue
		}
		data, err := os.ReadFile(candidate)
		if err != nil || len(data) == 0 || int64(len(data)) > r.cfg.MaxIconBytes {
			continue
		}
		icon := Icon{Data: data, MIMEType: mimeType}
		r.icons[candidate] = icon
		return icon, true
	}
	return Icon{}, false
}

type appIndex struct {
	byName    map[string]*appEntry
	byID      map[string]*appEntry
	byWMClass map[string]*appEntry
	byExec    map[string]*appEntry
	byIcon    map[string]*appEntry
	byGeneric map[string]*appEntry
}

type appEntry struct {
	name      string
	id        string
	icon      string
	wmClass   string
	exec      string
	generic   string
	noDisplay bool
	priority  int
}

func newAppIndex() *appIndex {
	return &appIndex{
		byName:    make(map[string]*appEntry),
		byID:      make(map[string]*appEntry),
		byWMClass: make(map[string]*appEntry),
		byExec:    make(map[string]*appEntry),
		byIcon:    make(map[string]*appEntry),
		byGeneric: make(map[string]*appEntry),
	}
}

func buildIndex(dirs []string) *appIndex {
	index := newAppIndex()
	seen := make(map[string]bool)

	for i, dir := range dirs {
		dir = expandHome(dir)
		if dir == "" {
			continue
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, file := range entries {
			if file.IsDir() || !strings.HasSuffix(strings.ToLower(file.Name()), ".desktop") {
				continue
			}
			full := filepath.Join(dir, file.Name())
			if abs, err := filepath.Abs(full); err == nil {
				full = abs
			}
			if seen[full] {
				continue
			}
			seen[full] = true

			f, err := os.Open(full)
			if err != nil {
				continue
			}
			entry, err := desktop.New(f)
			f.Close()
			if err != nil || entry.Hidden || entry.Name == "" || entry.Icon == "" {
				continue
			}

			index.add(&appEntry{
				name:      entry.Name,
				id:        strings.TrimSuffix(file.Name(), ".desktop"),
				icon:      strings.TrimSpace(entry.Icon),
				wmClass:   entry.StartupWMClass,
				exec:      execBase(entry.Exec),
				generic:   entry.GenericName,
				noDisplay: entry.NoDisplay,
				priority:  len(dirs) - i,
			})
		}
	}
	return index
}

func (ix *appIndex) add(entry *appEntry) {
	ix.insert(ix.byName, normalize(entry.name), entry)
	ix.insert(ix.byID, normalize(entry.id), entry)
	ix.insert(ix.byWMClass, normalize(entry.wmClass), entry)
	ix.insert(ix.byExec, normalize(entry.exec), entry)
	ix.insert(ix.byGeneric, normalize(entry.generic), entry)
	if !filepath.IsAbs(entry.icon) {
		ix.insert(ix.byIcon, normalize(entry.icon), entry)
	}
}

func (ix *appIndex) insert(target map[string]*appEntry, key string, entry *appEntry) {
	if key == "" {
		return
	}
	if existing, ok := target[key]; !ok || entry.score() > existing.score() {
		target[key] = entry
	}
}

func (e *appEntry) score() int {
	score := e.priority
	if !e.noDisplay {
		score += 1000
	}
	return score
}

func (ix *appIndex) find(key string) *appEntry {
	if entry := ix.lookup(key); entry != nil {
		return entry
	}
	for _, alias := range aliasTargets[key] {
		if entry := ix.lookup(normalize(alias)); entry != nil {
			return entry
		}
	}
	return ix.fuzzy(key)
}

func (ix *appIndex) lookup(key string) *appEntry {
	for _, target := range []map[string]*appEntry{
		ix.byName,
		ix.byID,
		ix.byWMClass,
		ix.byExec,
		ix.byGeneric,
		ix.byIcon,
	} {
		if entry := target[key]; entry != nil {
			return entry
		}
	}
	return nil
}

func (ix *appIndex) fuzzy(key string) *appEntry {
	if len([]rune(key)) < 4 {
		return nil
	}
	var best *appEntry
	bestScore := 0.0
	for _, target := range []map[string]*appEntry{ix.byName, ix.byGeneric} {
		for candidate, entry := range target {
			short, long := key, candidate
			if len(short) > len(long) {
				short, long = long, short
			}
			if !strings.Contains(long, short) {
				continue
			}
			ratio := float64(len(short)) / float64(len(long))
			if ratio < 0.6 {
				continue
			}
			score := ratio*100 + float64(entry.score())/10000
			if best == nil || score > bestScore {
				best = entry
				bestScore = score
			}
		}
	}
	return best
}

func cleanAppName(name string) string {
	name = strings.TrimSpace(name)
	name = strings.Trim(name, "\"'`")
	for _, separator := range []string{" | ", " — ", " – ", " :: "} {
		if at := strings.Index(name, separator); at >= 2 {
			name = name[:at]
			break
		}
	}
	if at := strings.IndexAny(name, ":："); at >= 2 {
		name = name[:at]
	}
	name = strings.TrimSpace(name)
	if strings.HasSuffix(strings.ToLower(name), ".exe") {
		name = name[:len(name)-4]
	}
	return strings.TrimSpace(name)
}

func normalize(value string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(value) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func iconNameCandidates(name string) []string {
	cleaned := strings.ToLower(cleanAppName(name))
	if cleaned == "" {
		return nil
	}
	values := []string{
		cleaned,
		strings.ReplaceAll(cleaned, " ", "-"),
		strings.ReplaceAll(cleaned, " ", ""),
		normalize(cleaned),
	}
	seen := make(map[string]bool)
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if !safeIconName(value) || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func safeIconName(name string) bool {
	if name == "" || len(name) > maxIconNameLen || strings.Contains(name, "..") {
		return false
	}
	for _, r := range name {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' || r == '.' {
			continue
		}
		return false
	}
	return true
}

func iconMIMEType(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png":
		return "image/png"
	case ".svg":
		return "image/svg+xml"
	default:
		return ""
	}
}

func execBase(exec string) string {
	fields := strings.Fields(exec)
	if len(fields) == 0 {
		return ""
	}
	return strings.Trim(filepath.Base(fields[0]), "\"'")
}

func expandHome(path string) string {
	if path == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
	}
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

var aliasTargets = map[string][]string{
	"vscode":           {"code", "vscode", "visualstudiocode"},
	"visualstudiocode": {"code", "vscode", "visualstudiocode"},
	"code":             {"code", "vscode"},
	"vscodium":         {"vscodium", "codium"},
	"googlechrome":     {"google-chrome", "googlechrome", "chrome"},
	"chrome":           {"google-chrome", "googlechrome", "chrome"},
	"chromium":         {"chromium", "chromium-browser"},
	"firefox":          {"firefox", "firefox-esr"},
	"brave":            {"brave-browser", "brave"},
	"bravebrowser":     {"brave-browser", "brave"},
	"microsoftedge":    {"microsoft-edge", "microsoftedge"},
	"edge":             {"microsoft-edge", "microsoftedge"},
	"antigravity":      {"antigravity"},
	"antigravityide":   {"antigravity"},
	"terminal":         {"org.gnome.Terminal", "gnome-terminal", "utilities-terminal"},
	"gnometerminal":    {"org.gnome.Terminal", "gnome-terminal", "utilities-terminal"},
	"bash":             {"org.gnome.Terminal", "utilities-terminal", "bash"},
	"zsh":              {"org.gnome.Terminal", "utilities-terminal"},
	"shell":            {"utilities-terminal"},
	"console":          {"utilities-terminal"},
	"opencode":         {"opencode", "org.gnome.Terminal", "utilities-terminal"},
	"oc":               {"opencode", "org.gnome.Terminal", "utilities-terminal"},
	"files":            {"org.gnome.Nautilus", "nautilus", "system-file-manager"},
	"nautilus":         {"org.gnome.Nautilus", "nautilus"},
	"filemanager":      {"org.gnome.Nautilus", "nautilus", "system-file-manager"},
	"slack":            {"slack"},
	"discord":          {"discord"},
	"notion":           {"notion"},
	"obsidian":         {"obsidian"},
	"spotify":          {"spotify"},
	"telegram":         {"telegram", "telegramdesktop"},
	"whatsapp":         {"whatsapp", "whatsapp-desktop"},
	"zoom":             {"zoom"},
	"figma":            {"figma", "figma-linux"},
	"postman":          {"postman"},
	"bruno":            {"bruno"},
	"goland":           {"goland", "jetbrains-goland"},
	"intellijidea":     {"intellij-idea-community", "intellij-idea-ultimate", "idea"},
	"pycharm":          {"pycharm-community", "pycharm-professional", "pycharm"},
	"webstorm":         {"webstorm"},
	"androidstudio":    {"android-studio", "androidstudio"},
	"dockerdesktop":    {"docker-desktop", "docker"},
	"githubdesktop":    {"github-desktop", "github-desktop-bin"},
	"trello":           {"trello"},
	"jira":             {"jira"},
	"obs":              {"obs", "obs-studio"},
	"vlc":              {"vlc"},
	"libreoffice":      {"libreoffice", "libreoffice-startcenter"},
	"thunderbird":      {"thunderbird"},
	"gimp":             {"gimp"},
	"inkscape":         {"inkscape"},
	"blender":          {"blender"},
	"steam":            {"steam"},
}
