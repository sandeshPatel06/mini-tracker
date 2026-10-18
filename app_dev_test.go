package main

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/reak/get-hike/internal/logger"
)

func TestMaskSecret(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", "not set"},
		{"whitespace only", "   ", "not set"},
		{"short", "short", "set"},
		{"exactly ten", "1234567890", "set"},
		{"long", "AIzaSyD1234567890ab12", "AIza...ab12"},
		{"long with spaces", "  AIzaSyD1234567890ab12  ", "AIza...ab12"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := maskSecret(tc.in); got != tc.want {
				t.Fatalf("maskSecret(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestTailLogLines(t *testing.T) {
	lines := []string{"1", "2", "3", "4", "5"}

	got := tailLogLines(lines, 3)
	if strings.Join(got, ",") != "3,4,5" {
		t.Fatalf("tailLogLines(5 lines, 3) = %v, want [3 4 5]", got)
	}

	if got := tailLogLines(lines, 0); len(got) != len(lines) {
		t.Fatalf("tailLogLines(5 lines, 0) length = %d, want %d", len(got), len(lines))
	}
	if got := tailLogLines(lines, -10); len(got) != len(lines) {
		t.Fatalf("tailLogLines(5 lines, -10) length = %d, want %d", len(got), len(lines))
	}
	if got := tailLogLines(lines, 100); len(got) != len(lines) {
		t.Fatalf("tailLogLines(5 lines, 100) length = %d, want %d", len(got), len(lines))
	}

	big := make([]string, 6000)
	for i := range big {
		big[i] = "line"
	}
	if got := tailLogLines(big, 999999); len(got) != maxLogTailLines {
		t.Fatalf("tailLogLines(6000 lines, 999999) length = %d, want %d", len(got), maxLogTailLines)
	}
}

func TestResolveAppLogPathTraversalGuard(t *testing.T) {
	dir := t.TempDir()

	rejected := []string{
		"../../etc/passwd",
		"/etc/passwd",
		"..",
		".",
		"notes.txt",
		"app_2026-10-18.txt",
		"other_2026-10-18.log",
		"today.log",
	}
	for _, name := range rejected {
		if _, err := resolveAppLogPath(dir, name); err == nil {
			t.Fatalf("resolveAppLogPath(%q) succeeded, want error", name)
		}
	}

	got, err := resolveAppLogPath(dir, "app_2026-10-18.log")
	if err != nil {
		t.Fatalf("resolveAppLogPath(valid) error = %v", err)
	}
	if want := filepath.Join(dir, "app_2026-10-18.log"); got != want {
		t.Fatalf("resolveAppLogPath(valid) = %q, want %q", got, want)
	}

	got, err = resolveAppLogPath(dir, "../../app_2026-10-18.log")
	if err != nil {
		t.Fatalf("resolveAppLogPath(nested valid) error = %v", err)
	}
	if filepath.Dir(got) != dir {
		t.Fatalf("resolveAppLogPath(nested valid) escaped dir: %q", got)
	}

	got, err = resolveAppLogPath(dir, "")
	if err != nil {
		t.Fatalf("resolveAppLogPath(empty) error = %v", err)
	}
	wantToday := filepath.Join(dir, "app_"+time.Now().Format("2006-01-02")+".log")
	if got != wantToday {
		t.Fatalf("resolveAppLogPath(empty) = %q, want %q", got, wantToday)
	}
}

func TestGetAppLogsRejectsWithoutConfig(t *testing.T) {
	a := &App{}
	if _, err := a.GetAppLogs("../../etc/passwd", 10); err == nil {
		t.Fatal("GetAppLogs() with nil config succeeded, want error")
	}
}

func TestSeedDemoLogsCountGuard(t *testing.T) {
	a := &App{}

	if n, err := a.SeedDemoLogs(0); err == nil || n != 0 {
		t.Fatalf("SeedDemoLogs(0) = (%d, %v), want (0, error)", n, err)
	}
	if n, err := a.SeedDemoLogs(51); err == nil || n != 0 {
		t.Fatalf("SeedDemoLogs(51) = (%d, %v), want (0, error)", n, err)
	}
	if n, err := a.SeedDemoLogs(-1); err == nil || n != 0 {
		t.Fatalf("SeedDemoLogs(-1) = (%d, %v), want (0, error)", n, err)
	}
}

func TestGetDevDiagnosticsNilSafe(t *testing.T) {
	a := &App{}
	diag := a.GetDevDiagnostics()

	required := []string{
		"app_mode", "headless", "go_version", "os", "arch", "wails_buildtype",
		"data_dir", "db_path", "db_size_bytes", "logs_dir", "today_log_file",
		"log_files_count", "screenshot_dir", "screenshots_count", "screenshots_bytes",
		"tracking_active", "tracked_seconds_today", "daemon_running", "daemon_pid",
		"ai_configured", "ai_model", "ai_key_masked", "pending_ai_logs",
		"backend_endpoint", "sync_last_at", "demo_logs_count", "version",
	}
	for _, key := range required {
		if _, ok := diag[key]; !ok {
			t.Fatalf("GetDevDiagnostics() missing key %q", key)
		}
	}
	if diag["app_mode"] != "desktop" {
		t.Fatalf("app_mode = %v, want desktop", diag["app_mode"])
	}
	if diag["ai_key_masked"] != "not set" {
		t.Fatalf("ai_key_masked = %v, want not set", diag["ai_key_masked"])
	}
}

func TestLoggerSetLevel(t *testing.T) {
	previous := logger.Log.GetLevel()
	defer logger.Log.SetLevel(previous)

	if err := logger.SetLevel("debug"); err != nil {
		t.Fatalf("SetLevel(debug) error = %v", err)
	}
	if err := logger.SetLevel("warning"); err != nil {
		t.Fatalf("SetLevel(warning) error = %v", err)
	}
	if err := logger.SetLevel("bogus"); err == nil {
		t.Fatal("SetLevel(bogus) succeeded, want error")
	}
}
