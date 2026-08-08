package logger

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

var (
	// Log is the global Logrus logger instance for structured logging (server / backend).
	Log = logrus.New()

	// desktopFile holds active log file handle for desktop app file logging.
	desktopFile *os.File
	mu          sync.Mutex
)

func init() {
	// Configure Logrus default format
	Log.SetFormatter(&logrus.TextFormatter{
		FullTimestamp:   true,
		TimestampFormat: "2006-01-02 15:04:05.000",
		ForceColors:     true,
	})
	Log.SetOutput(os.Stdout)
	Log.SetLevel(logrus.InfoLevel)
}

// InitDesktopLogger initializes a file logger in dataDir/logs/ app.log with daily rotating files
// and automatically deletes log files older than 7 days.
func InitDesktopLogger(dataDir string) error {
	mu.Lock()
	defer mu.Unlock()

	logsDir := filepath.Join(dataDir, "logs")
	if err := os.MkdirAll(logsDir, 0755); err != nil {
		return fmt.Errorf("create logs dir: %w", err)
	}

	today := time.Now().Format("2006-01-02")
	logFilePath := filepath.Join(logsDir, fmt.Sprintf("app_%s.log", today))

	f, err := os.OpenFile(logFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("open desktop log file: %w", err)
	}

	if desktopFile != nil {
		_ = desktopFile.Close()
	}
	desktopFile = f

	// MultiWriter outputs logs to BOTH stdout and the local 7-day log file
	mw := io.MultiWriter(os.Stdout, f)
	log.SetOutput(mw)
	Log.SetOutput(mw)

	log.Printf("[logger] Desktop file logging initialized: %s (7-day file retention enabled)", logFilePath)

	// Run initial 7-day retention cleanup and schedule hourly cron
	cleanOldLogFiles(logsDir)
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			cleanOldLogFiles(logsDir)
		}
	}()

	return nil
}

// cleanOldLogFiles scans logsDir and removes app_YYYY-MM-DD.log files older than 7 days.
func cleanOldLogFiles(logsDir string) {
	entries, err := os.ReadDir(logsDir)
	if err != nil {
		return
	}

	cutoff := time.Now().AddDate(0, 0, -7)

	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".log" {
			info, err := entry.Info()
			if err != nil {
				continue
			}
			if info.ModTime().Before(cutoff) {
				fullPath := filepath.Join(logsDir, entry.Name())
				if err := os.Remove(fullPath); err == nil {
					log.Printf("[logger] 7-day log retention cleanup: removed old log file %s", entry.Name())
				}
			}
		}
	}
}
