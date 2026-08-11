package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/reak/get-hike/internal/ai"
	"github.com/reak/get-hike/internal/config"
	"github.com/reak/get-hike/internal/db"
	"github.com/reak/get-hike/internal/logger"
	syncp "github.com/reak/get-hike/internal/sync"
	"github.com/reak/get-hike/internal/tracker"
)

// App is the Wails application struct — all public methods are bound to the frontend.
type App struct {
	ctx    context.Context
	cancel context.CancelFunc

	cfg            *config.Config
	database       *db.DB
	gemini         *ai.GeminiClient
	keyTracker     *tracker.KeystrokeTracker
	mouseTracker   *tracker.MouseTracker
	syncEngine     *syncp.SyncEngine
	tickerResetCh  chan time.Duration

	isGuest   bool
	authToken string

	// latest input stats — guarded by statsMu against concurrent goroutine access
	statsMu          sync.Mutex
	latestKeyStats   tracker.KeystrokeStats
	latestMouseStats tracker.MouseStats
}

// NewApp creates a new App application struct.
func NewApp() *App {
	return &App{}
}

// startup is called when the app starts.
func (a *App) startup(ctx context.Context) {
	a.ctx, a.cancel = context.WithCancel(ctx)

	// Load config
	cfg, err := config.Load()
	if err != nil {
		log.Printf("[app] config load error: %v", err)
		cfg = &config.Config{ScreenshotInterval: 30 * time.Second, DataDir: "/tmp/get-hike"}
	}
	a.cfg = cfg

	// Initialize Desktop File Logger with automatic 7-day log retention
	_ = logger.InitDesktopLogger(cfg.DataDir)

	// Open database
	dbConn, err := db.Open(cfg.DataDir)
	if err != nil {
		log.Printf("[app] db error: %v", err)
	} else {
		a.database = dbConn
	}

	// Gemini client (used for Guest Mode or direct client-side requests)
	a.gemini = ai.NewGeminiClient(cfg.GeminiAPIKey, cfg.GeminiModel)

	// Sync Engine (used for Authenticated / Org Mode)
	a.syncEngine = syncp.NewSyncEngine(a.database, cfg.BackendEndpoint)

	// Start keystroke tracker
	a.keyTracker = tracker.NewKeystrokeTracker(cfg.ScreenshotInterval)
	statsCh, err := a.keyTracker.Start()
	if err != nil {
		log.Printf("[app] keystroke tracker error: %v", err)
		statsCh = nil
	}

	a.tickerResetCh = make(chan time.Duration, 1)

	// Start mouse tracker
	a.mouseTracker = tracker.NewMouseTracker(cfg.ScreenshotInterval)
	if mouseStatsCh, err := a.mouseTracker.Start(); err != nil {
		log.Printf("[app] mouse tracker error (non-fatal): %v", err)
	} else {
		// drain mouse stats into latestMouseStats (mutex-protected)
		go func() {
			for ms := range mouseStatsCh {
				a.statsMu.Lock()
				a.latestMouseStats = ms
				a.statsMu.Unlock()
			}
		}()
	}

	// Main collection loop — fires every ScreenshotInterval
	go func() {
		dur := cfg.ScreenshotInterval
		ticker := time.NewTicker(dur)
		defer ticker.Stop()

		for {
			select {
			case <-a.ctx.Done():
				return
			case newDur := <-a.tickerResetCh:
				ticker.Stop()
				dur = newDur
				ticker = time.NewTicker(dur)
				log.Printf("[app] screenshot collection loop reset to %v", dur)
			case stats, ok := <-statsCh:
				if ok {
					a.statsMu.Lock()
					a.latestKeyStats = stats
					a.statsMu.Unlock()
				}
			case <-ticker.C:
				a.collect()
			}
		}
	}()

	// Start 3-hour background pull cron if connected to backend
	a.syncEngine.StartBackgroundPullCron(a.ctx, 3*time.Hour)

	// Auto-process any pending/unanalyzed logs if Gemini key is set (Guest mode)
	if a.gemini.HasKey() {
		go func() {
			count, err := a.ProcessPendingLogs()
			if err != nil {
				log.Printf("[app] auto-process pending logs error: %v", err)
			} else if count > 0 {
				log.Printf("[app] auto-processed %d pending logs with Gemini", count)
			}
		}()
	}

	// 7-day Screenshot Retention Cron Routine (runs hourly to clean up files > 7 days old)
	go func() {
		a.cleanOldScreenshots()
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()

		for {
			select {
			case <-a.ctx.Done():
				return
			case <-ticker.C:
				a.cleanOldScreenshots()
			}
		}
	}()

	log.Printf("[app] started — screenshot interval: %s (7-day retention active)", cfg.ScreenshotInterval)
}

// cleanOldScreenshots scans dataDir/images/ and removes folders/files older than 7 days.
func (a *App) cleanOldScreenshots() {
	if a.cfg == nil || a.cfg.DataDir == "" {
		return
	}

	imagesDir := filepath.Join(a.cfg.DataDir, "images")
	entries, err := os.ReadDir(imagesDir)
	if err != nil {
		return
	}

	cutoff := time.Now().AddDate(0, 0, -7)

	for _, entry := range entries {
		if entry.IsDir() {
			// Date folders are named YYYY-MM-DD
			folderTime, err := time.Parse("2006-01-02", entry.Name())
			if err == nil {
				if folderTime.Before(cutoff) {
					fullPath := filepath.Join(imagesDir, entry.Name())
					if err := os.RemoveAll(fullPath); err == nil {
						log.Printf("[app] retention cron: deleted screenshots older than 7 days in %s", fullPath)
					}
				}
			}
		}
	}
}

// collect captures a screenshot + uses latest keystroke/mouse stats, persists the
// entry, then fires async Gemini analysis (in Guest mode) or queues for backend sync (in Auth mode).
func (a *App) collect() {
	a.statsMu.Lock()
	keyStats := a.latestKeyStats
	a.latestKeyStats = tracker.KeystrokeStats{}
	mouseStats := a.latestMouseStats
	a.latestMouseStats = tracker.MouseStats{}
	a.statsMu.Unlock()

	// Screenshot
	shot, err := tracker.CaptureScreenshot(a.cfg.DataDir)
	if err != nil {
		log.Printf("[app] screenshot error: %v", err)
		shot = &tracker.ScreenshotResult{}
	}

	syncStatus := "pending_upload"
	if a.isGuest {
		syncStatus = "local_only"
	}

	entry := &db.LogEntry{
		Timestamp:     time.Now(),
		ImagePath:     shot.FilePath,
		TotalKeys:     keyStats.TotalKeys,
		UniqueKeys:    keyStats.UniqueKeys,
		EntropyScore:  keyStats.EntropyScore,
		TotalClicks:   mouseStats.TotalClicks,
		MouseDistance: mouseStats.MouseDistance,
		SyncStatus:    syncStatus,
	}

	if a.database == nil {
		return
	}

	id, err := a.database.InsertLog(entry)
	if err != nil {
		log.Printf("[app] insert log error: %v", err)
		return
	}

	if a.isGuest {
		// Guest Mode: Perform direct client-side local Gemini AI analysis
		go func(logID int64, b64Data string, score float64) {
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()

			if result, err := a.gemini.Analyze(ctx, b64Data, score); err == nil {
				if err := a.database.UpdateAIResult(logID, result.Category, result.Productive, result.Confidence, result.Reason); err != nil {
					log.Printf("[app] update local AI result error: %v", err)
				}
				_ = a.database.RecordAPIUsage(0, 0, "guest", result.Usage.PromptTokenCount, result.Usage.CandidatesTokenCount, result.Usage.TotalTokenCount, a.gemini.GetModel())
				log.Printf("[app] guest logged #%d — category=%s productive=%v tokens=%d",
					logID, result.Category, result.Productive, result.Usage.TotalTokenCount)
			} else {
				log.Printf("[app] guest AI analysis offline/error for #%d: %v — applying local fallback", logID, err)
				fallbackReason := "Offline Mode (Local Log)"
				if !a.gemini.HasKey() {
					fallbackReason = "No Gemini API key set"
				}
				_ = a.database.UpdateAIResult(logID, "Browsing", true, 0.8, fallbackReason)
			}
		}(id, shot.Base64Data, keyStats.EntropyScore)
	} else {
		// Authenticated Mode: Push raw telemetry to backend
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			_, _ = a.syncEngine.PushTelemetry(ctx)
		}()
	}
}

// SetAuthSession updates active session authentication token and mode (Guest vs Authenticated).
func (a *App) SetAuthSession(token string, isGuest bool) {
	a.authToken = token
	a.isGuest = isGuest
	if a.syncEngine != nil {
		a.syncEngine.SetAuthToken(token)
	}
	log.Printf("[app] session updated — isGuest: %v", isGuest)
}

// TriggerSyncNow initiates an immediate Push + Pull cycle on demand.
func (a *App) TriggerSyncNow() (bool, error) {
	if a.isGuest {
		return true, nil // Guest mode is local-only
	}
	if a.syncEngine == nil {
		return false, fmt.Errorf("sync engine unavailable")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	err := a.syncEngine.TriggerSyncNow(ctx)
	if err != nil {
		return false, err
	}
	return true, nil
}

// SetUserPersonalKey sets personal Gemini API Key for solo users.
func (a *App) SetUserPersonalKey(apiKey string) (bool, error) {
	if a.cfg != nil {
		a.cfg.GeminiAPIKey = apiKey
		_ = config.Save(a.cfg)
	}
	if a.gemini != nil {
		a.gemini.SetAPIKey(apiKey)
	}
	return true, nil
}

// SetOrgGeminiKey updates the organization Gemini API key in backend DB.
func (a *App) SetOrgGeminiKey(orgID int64, apiKey string) (bool, error) {
	if a.database != nil && orgID > 0 {
		if err := a.database.SetOrgGeminiConfig(orgID, apiKey, "models/gemma-4-31b-it"); err != nil {
			return false, err
		}
	}
	return true, nil
}

// shutdown is called when the app terminates.
func (a *App) shutdown(_ context.Context) {
	// Flush any accumulated in-memory stats before stopping trackers.
	// This ensures activity counts from the current interval are saved to the DB
	// so daily totals are continuous across restarts.
	a.statsMu.Lock()
	keyStats := a.latestKeyStats
	mouseStats := a.latestMouseStats
	a.latestKeyStats = tracker.KeystrokeStats{}
	a.latestMouseStats = tracker.MouseStats{}
	a.statsMu.Unlock()

	if a.database != nil && (keyStats.TotalKeys > 0 || mouseStats.TotalClicks > 0 || mouseStats.MouseDistance > 0) {
		shot := &tracker.ScreenshotResult{} // no screenshot on shutdown flush
		syncStatus := "local_only"
		if !a.isGuest {
			syncStatus = "pending_upload"
		}
		if _, err := a.database.InsertLog(&db.LogEntry{
			OrgID:         0,
			UserID:        0,
			ImagePath:     shot.FilePath,
			TotalKeys:     keyStats.TotalKeys,
			UniqueKeys:    keyStats.UniqueKeys,
			EntropyScore:  keyStats.EntropyScore,
			TotalClicks:   mouseStats.TotalClicks,
			MouseDistance: mouseStats.MouseDistance,
			SyncStatus:    syncStatus,
			Timestamp:     time.Now(),
		}); err != nil {
			log.Printf("[app] shutdown flush error: %v", err)
		} else {
			log.Printf("[app] shutdown flush: saved %d keys / %d clicks to DB",
				keyStats.TotalKeys, mouseStats.TotalClicks)
		}
	}

	if a.cancel != nil {
		a.cancel()
	}
	if a.keyTracker != nil {
		a.keyTracker.Stop()
	}
	if a.mouseTracker != nil {
		a.mouseTracker.Stop()
	}
	if a.database != nil {
		_ = a.database.Close()
	}
}

// ---- Wails-bound frontend API ----

// GetTodayLogs returns all log entries for today.
func (a *App) GetTodayLogs() ([]db.LogEntry, error) {
	if a.database == nil {
		return nil, nil
	}
	return a.database.GetTodayLogs()
}

// GetLogsByDate returns all log entries for a given date (YYYY-MM-DD).
func (a *App) GetLogsByDate(date string) ([]db.LogEntry, error) {
	if a.database == nil {
		return nil, nil
	}
	return a.database.GetLogsForDate(date)
}

// GetTodayTrackedSeconds returns total accumulated tracked seconds for today from the DB.
func (a *App) GetTodayTrackedSeconds() int64 {
	if a.database == nil || a.cfg == nil {
		return 0
	}
	return a.database.GetTodayTrackedSeconds(int64(a.cfg.ScreenshotInterval.Seconds()))
}

// GetStats returns productivity stats for today.
func (a *App) GetStats(date string) (*db.ProductivityStats, error) {
	if a.database == nil {
		return nil, nil
	}
	return a.database.GetProductivityStats(date)
}

// RecordInputActivity allows the frontend desktop application to report user keystroke activity
// without requiring root/sudo/evdev input group permissions on Linux.
func (a *App) RecordInputActivity(totalKeys, uniqueKeys int) {
	if a.keyTracker != nil {
		a.keyTracker.RecordKeystrokes(totalKeys, uniqueKeys)
	}
}

// RecordMouseActivity allows the frontend desktop application to report mouse clicks and movement
// without requiring root/sudo/evdev input group permissions on Linux.
func (a *App) RecordMouseActivity(clicks int, distancePx float64) {
	if a.mouseTracker != nil {
		a.mouseTracker.RecordMouseActivity(clicks, distancePx)
	}
}

// ClearAllLocalData purges all local screenshot files and resets the SQLite database.
func (a *App) ClearAllLocalData() (bool, error) {
	if a.cfg == nil {
		return false, nil
	}
	imagesDir := filepath.Join(a.cfg.DataDir, "images")
	_ = os.RemoveAll(imagesDir)
	_ = os.MkdirAll(imagesDir, 0755)

	dbPath := filepath.Join(a.cfg.DataDir, "tracker.db")
	if a.database != nil {
		_ = a.database.Close()
	}
	_ = os.Remove(dbPath)
	_ = os.Remove(dbPath + "-wal")
	_ = os.Remove(dbPath + "-shm")

	newDb, err := db.Open(a.cfg.DataDir)
	if err == nil {
		a.database = newDb
	}
	return true, nil
}

// GetConfig returns non-sensitive config values for the UI.
func (a *App) GetConfig() map[string]interface{} {
	if a.cfg == nil {
		return nil
	}
	return map[string]interface{}{
		"screenshot_interval_seconds": a.cfg.ScreenshotInterval.Seconds(),
		"data_dir":                    a.cfg.DataDir,
		"ai_configured":               a.gemini != nil && a.gemini.HasKey(),
		"backend_endpoint":            a.cfg.BackendEndpoint,
	}
}

// UpdateGeminiAPIKey updates and persists the Gemini API key, then triggers re-analysis.
func (a *App) UpdateGeminiAPIKey(apiKey string) (bool, error) {
	if a.cfg == nil {
		return false, nil
	}
	a.cfg.GeminiAPIKey = apiKey
	if a.gemini != nil {
		a.gemini.SetAPIKey(apiKey)
	}
	if err := config.Save(a.cfg); err != nil {
		log.Printf("[app] save config error: %v", err)
	}
	go a.ProcessPendingLogs()
	return true, nil
}

// UpdateAIModel updates the active model name in Gemini client.
func (a *App) UpdateAIModel(modelName string) (bool, error) {
	if a.gemini != nil {
		a.gemini.SetModel(modelName)
		log.Printf("[app] Updated AI model to: %s", modelName)
	}
	return true, nil
}

// GetImageBase64 reads an image file from disk and returns it as a data URL.
func (a *App) GetImageBase64(imagePath string) (string, error) {
	if imagePath == "" {
		return "", nil
	}
	data, err := os.ReadFile(imagePath)
	if err != nil {
		return "", err
	}
	mimeType := "image/jpeg"
	ext := strings.ToLower(filepath.Ext(imagePath))
	if ext == ".webp" {
		mimeType = "image/webp"
	} else if ext == ".png" {
		mimeType = "image/png"
	}
	return fmt.Sprintf("data:%s;base64,%s", mimeType, base64.StdEncoding.EncodeToString(data)), nil
}

// UpdateScreenshotInterval updates and persists the screenshot interval, then resets the collection ticker.
func (a *App) UpdateScreenshotInterval(seconds int) (bool, error) {
	if seconds < 5 {
		seconds = 5
	}
	dur := time.Duration(seconds) * time.Second
	if a.cfg != nil {
		a.cfg.ScreenshotInterval = dur
		if err := config.Save(a.cfg); err != nil {
			log.Printf("[app] save config error: %v", err)
		}
	}
	if a.tickerResetCh != nil {
		select {
		case a.tickerResetCh <- dur:
		default:
		}
	}
	log.Printf("[app] Screenshot interval updated to %d seconds", seconds)
	return true, nil
}

// ProcessPendingLogs scans for unanalyzed logs and processes them with Gemini.
func (a *App) ProcessPendingLogs() (int, error) {
	if a.database == nil || a.gemini == nil || !a.gemini.HasKey() {
		return 0, nil
	}

	logs, err := a.database.GetUnanalyzedLogs()
	if err != nil || len(logs) == 0 {
		return 0, err
	}

	log.Printf("[app] processing %d pending unanalyzed logs...", len(logs))
	processed := 0

	for _, entry := range logs {
		var b64 string
		if entry.ImagePath != "" {
			data, err := os.ReadFile(entry.ImagePath)
			if err == nil {
				b64 = base64.StdEncoding.EncodeToString(data)
			}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		res, err := a.gemini.Analyze(ctx, b64, entry.EntropyScore)
		cancel()

		if err != nil {
			log.Printf("[app] re-analyze log #%d error: %v — applying offline fallback", entry.ID, err)
			_ = a.database.UpdateAIResult(entry.ID, "Browsing", true, 0.8, "Offline Mode (Local Log)")
			continue
		}

		if err := a.database.UpdateAIResult(entry.ID, res.Category, res.Productive, res.Confidence, res.Reason); err == nil {
			_ = a.database.RecordAPIUsage(entry.OrgID, entry.UserID, "guest", res.Usage.PromptTokenCount, res.Usage.CandidatesTokenCount, res.Usage.TotalTokenCount, a.gemini.GetModel())
			processed++
		}
	}

	log.Printf("[app] completed processing %d/%d pending logs", processed, len(logs))
	return processed, nil
}

// OpenTrackerWizard opens the wizard as a separate OS-level popup window.
func (a *App) OpenTrackerWizard(token string) error {
	url := fmt.Sprintf("http://127.0.0.1:8080/wizard?token=%s", token)
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "linux":
		cmd = exec.Command("google-chrome", "--app="+url, "--window-size=300,460")
		if err := cmd.Start(); err != nil {
			return exec.Command("xdg-open", url).Start()
		}
	case "darwin":
		cmd = exec.Command("open", "-n", "-a", "Google Chrome", "--args", "--app="+url, "--window-size=300,460")
		if err := cmd.Start(); err != nil {
			return exec.Command("open", url).Start()
		}
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", "chrome", "--app="+url, "--window-size=300,460")
		if err := cmd.Start(); err != nil {
			return exec.Command("cmd", "/c", "start", url).Start()
		}
	}
	return nil
}

