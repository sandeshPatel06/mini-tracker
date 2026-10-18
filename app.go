package main

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"

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

	headless bool

	cfg           *config.Config
	database      *db.DB
	gemini        *ai.GeminiClient
	keyTracker    *tracker.KeystrokeTracker
	mouseTracker  *tracker.MouseTracker
	syncEngine    *syncp.SyncEngine
	tickerResetCh chan time.Duration

	isGuest   bool
	authToken string

	// latest input stats — guarded by statsMu against concurrent goroutine access
	statsMu          sync.Mutex
	latestKeyStats   tracker.KeystrokeStats
	latestMouseStats tracker.MouseStats
	activeTask       string

	isTrackingActive bool
	trackerStartTime time.Time
	accumulatedSec   int64

	// aiMu serializes batched AI analysis runs (single-flight).
	aiMu sync.Mutex

	// lastAIAttempt throttles automatic AI batch runs (guarded by statsMu).
	lastAIAttempt time.Time
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

	// Reclaim tracking from a detached background tracker spawned by a previous run.
	if !a.headless {
		stopBackgroundDaemon(cfg.DataDir)
	}

	// Open database
	dbConn, err := db.Open(cfg.DataDir)
	if err != nil {
		log.Printf("[app] db error: %v", err)
	} else {
		a.database = dbConn
	}

	// Gemini client (used for Guest Mode or direct client-side requests)
	a.gemini = ai.NewGeminiClient(cfg.GeminiAPIKey, cfg.GeminiModel)
	if a.gemini != nil {
		a.gemini.SetDataDir(cfg.DataDir)
	}

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

	a.isTrackingActive = true
	a.trackerStartTime = time.Now()

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
				a.statsMu.Lock()
				active := a.isTrackingActive
				a.statsMu.Unlock()
				if !active {
					continue
				}
				a.collect()
			}
		}
	}()

	// Start 3-hour background pull cron if connected to backend
	a.syncEngine.StartBackgroundPullCron(a.ctx, 3*time.Hour)

	// Fast pending-upload retry cron: after an outage, queued telemetry is pushed
	// as soon as connectivity and a session are back (at most ~5 minutes wait)
	// instead of waiting for the 3-hour pull cycle.
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-a.ctx.Done():
				return
			case <-ticker.C:
				if !a.syncActive() || a.database == nil {
					continue
				}
				pending, err := a.database.CountLogsBySyncStatus("pending_upload")
				if err != nil || pending == 0 {
					continue
				}
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
				if err := a.syncEngine.TriggerSyncNow(ctx); err != nil {
					log.Printf("[app] pending upload retry failed (%d queued): %v", pending, err)
				}
				cancel()
			}
		}
	}()

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

	// Periodic batched AI analysis — throttled between 2 and 15 minutes to minimize
	// API usage while keeping the dashboard fresh. Runs regardless of startup key so
	// a key added at runtime is picked up automatically; no-ops without a key.
	go func() {
		interval := cfg.AIAnalysisInterval
		if interval < 2*time.Minute {
			interval = 2 * time.Minute
		}
		if interval > 15*time.Minute {
			interval = 15 * time.Minute
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		log.Printf("[app] AI batch analysis cron interval: %v", interval)
		for {
			select {
			case <-a.ctx.Done():
				return
			case <-ticker.C:
				// Guest / solo / not-yet-authenticated modes analyze locally
				// (including the background daemon, which has no auth token).
				if a.isGuest || a.syncEngine == nil || !a.syncEngine.Enabled() {
					a.processPendingAIBatched()
				}
			}
		}
	}()

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

	winRes, err := tracker.GetActiveWindowTitle()
	windowTitle := ""
	if err != nil {
		log.Printf("[app] window title error: %v", err)
	} else if winRes != nil {
		windowTitle = winRes.Title
	}

	imagePath := ""
	if res, err := tracker.CaptureScreenshot(a.cfg.DataDir); err != nil {
		log.Printf("[app] screenshot capture error: %v", err)
	} else if res != nil {
		imagePath = res.FilePath
	}

	syncStatus := "pending_upload"
	if a.isGuest {
		syncStatus = "local_only"
	}

	entry := &db.LogEntry{
		Timestamp:    time.Now(),
		ImagePath:    imagePath,
		WindowTitle:  windowTitle,
		TotalKeys:    keyStats.TotalKeys,
		UniqueKeys:   keyStats.UniqueKeys,
		EntropyScore: keyStats.EntropyScore,
		TotalClicks:  mouseStats.TotalClicks,
		SyncStatus:   syncStatus,
	}

	if a.database == nil {
		return
	}

	if _, err := a.database.InsertLog(entry); err != nil {
		log.Printf("[app] insert log error: %v", err)
		return
	}

	if a.isGuest {
		// Guest Mode: queue for throttled batch analysis instead of one API call per log.
		go a.notifyPendingAI()
	} else if a.syncActive() {
		// Authenticated Mode: Push raw telemetry to backend (single-flight guarded).
		// Without a real session token (e.g. background daemon), skip the immediate
		// push — the periodic retry cron handles queued uploads once a token exists.
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			_ = a.syncEngine.TriggerSyncNow(ctx)
		}()
	}
}

// syncActive reports whether the app currently holds a usable backend session
// for telemetry sync (authenticated, non-guest, token set).
func (a *App) syncActive() bool {
	return a.syncEngine != nil && a.syncEngine.Enabled() && !a.isGuest
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
	// If tracking is still active, hand it off to a detached background process
	// so capture continues after this app exits.
	if !a.headless && a.cfg != nil {
		a.statsMu.Lock()
		active := a.isTrackingActive
		a.statsMu.Unlock()
		if active {
			if err := spawnBackgroundDaemon(a.cfg.DataDir); err != nil {
				log.Printf("[app] failed to start background tracker: %v", err)
			} else {
				log.Printf("[app] tracking will continue in background (pid %d)", readPIDFile(pidPath(a.cfg.DataDir)))
			}
		}
	}

	// Flush any accumulated in-memory stats before stopping trackers.
	// This ensures activity counts from the current interval are saved to the DB
	// so daily totals are continuous across restarts.
	a.statsMu.Lock()
	keyStats := a.latestKeyStats
	mouseStats := a.latestMouseStats
	a.latestKeyStats = tracker.KeystrokeStats{}
	a.latestMouseStats = tracker.MouseStats{}
	a.statsMu.Unlock()

	if a.database != nil && (keyStats.TotalKeys > 0 || mouseStats.TotalClicks > 0) {
		syncStatus := "local_only"
		if !a.isGuest {
			syncStatus = "pending_upload"
		}
		if _, err := a.database.InsertLog(&db.LogEntry{
			OrgID:        0,
			UserID:       0,
			ImagePath:    "",
			TotalKeys:    keyStats.TotalKeys,
			UniqueKeys:   keyStats.UniqueKeys,
			EntropyScore: keyStats.EntropyScore,
			TotalClicks:  mouseStats.TotalClicks,
			SyncStatus:   syncStatus,
			Timestamp:    time.Now(),
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

// ToggleTracking starts or pauses tracking session.
func (a *App) ToggleTracking() (map[string]interface{}, error) {
	a.statsMu.Lock()
	defer a.statsMu.Unlock()

	if a.isTrackingActive {
		if !a.trackerStartTime.IsZero() {
			a.accumulatedSec += int64(time.Since(a.trackerStartTime).Seconds())
		}
		a.isTrackingActive = false
		a.latestKeyStats = tracker.KeystrokeStats{}
		a.latestMouseStats = tracker.MouseStats{}
	} else {
		a.trackerStartTime = time.Now()
		a.isTrackingActive = true
	}

	elapsed := a.accumulatedSec
	if a.isTrackingActive && !a.trackerStartTime.IsZero() {
		elapsed += int64(time.Since(a.trackerStartTime).Seconds())
	}

	log.Printf("[app] tracking toggled — active: %v, elapsed: %ds", a.isTrackingActive, elapsed)
	return map[string]interface{}{
		"active":          a.isTrackingActive,
		"elapsed_seconds": elapsed,
	}, nil
}

// GetTrackingStatus returns current tracking state and accumulated elapsed time.
func (a *App) GetTrackingStatus() map[string]interface{} {
	a.statsMu.Lock()
	defer a.statsMu.Unlock()

	elapsed := a.accumulatedSec
	if a.isTrackingActive && !a.trackerStartTime.IsZero() {
		elapsed += int64(time.Since(a.trackerStartTime).Seconds())
	}

	return map[string]interface{}{
		"active":          a.isTrackingActive,
		"elapsed_seconds": elapsed,
	}
}

// GetTodayTrackedSeconds returns total accumulated tracked seconds for today.
func (a *App) GetTodayTrackedSeconds() int64 {
	a.statsMu.Lock()
	active := a.isTrackingActive
	startTime := a.trackerStartTime
	accum := a.accumulatedSec
	a.statsMu.Unlock()

	if active && !startTime.IsZero() {
		return accum + int64(time.Since(startTime).Seconds())
	}
	if accum > 0 {
		return accum
	}
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
	a.statsMu.Lock()
	active := a.isTrackingActive
	a.statsMu.Unlock()
	if !active {
		return
	}
	if a.keyTracker != nil {
		a.keyTracker.RecordKeystrokes(totalKeys, uniqueKeys)
	}
}

// RecordMouseActivity allows the frontend desktop application to report mouse clicks
// without requiring root/sudo/evdev input group permissions on Linux.
func (a *App) RecordMouseActivity(clicks int) {
	a.statsMu.Lock()
	active := a.isTrackingActive
	a.statsMu.Unlock()
	if !active {
		return
	}
	if a.mouseTracker != nil {
		a.mouseTracker.RecordMouseActivity(clicks)
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
	}
	if a.cfg != nil {
		a.cfg.GeminiModel = modelName
		if err := config.Save(a.cfg); err != nil {
			log.Printf("[app] save model error: %v", err)
		}
	}
	log.Printf("[app] Updated AI model to: %s", modelName)
	return true, nil
}

// SetActiveTask sets the current target user task (up to 20 words).
func (a *App) SetActiveTask(task string) string {
	a.statsMu.Lock()
	defer a.statsMu.Unlock()
	words := strings.Fields(strings.TrimSpace(task))
	if len(words) > 20 {
		words = words[:20]
	}
	a.activeTask = strings.Join(words, " ")
	log.Printf("[app] Active task updated to: %q", a.activeTask)
	return a.activeTask
}

// GetActiveTask returns the current target user task.
func (a *App) GetActiveTask() string {
	a.statsMu.Lock()
	defer a.statsMu.Unlock()
	return a.activeTask
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

const aiBatchSize = 10

// aiRetryMinGap is the minimum delay between automatic AI batch attempts.
// It keeps offline devices from hammering the API while still recovering quickly
// once connectivity returns.
const aiRetryMinGap = 2 * time.Minute

// isTransientAIError reports whether an AI batch failure is caused by connectivity
// problems or temporary rate-limit/server errors instead of a permanent fault.
// Transient failures must not consume the per-log retry budget: entries stay
// pending so they are analyzed once the device is back online.
func isTransientAIError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return true
	}
	if errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, syscall.ENETUNREACH) || errors.Is(err, syscall.EHOSTUNREACH) ||
		errors.Is(err, syscall.EPIPE) || errors.Is(err, syscall.ETIMEDOUT) {
		return true
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	msg := strings.ToLower(err.Error())
	for _, needle := range []string{
		"no such host", "server misbehaving", "network is unreachable",
		"connection refused", "connection reset", "broken pipe",
		"i/o timeout", "tls handshake timeout", "dial tcp", "lookup ",
		"proxyconnect", "temporary failure in name resolution",
		"gemini error 429", "gemini error 500", "gemini error 502",
		"gemini error 503", "gemini error 504",
	} {
		if strings.Contains(msg, needle) {
			return true
		}
	}
	return false
}

// markAIAttempt records the start time of an AI batch run for throttling.
func (a *App) markAIAttempt() {
	a.statsMu.Lock()
	a.lastAIAttempt = time.Now()
	a.statsMu.Unlock()
}

// aiAttemptAllowed reports whether enough time has passed since the last AI run.
func (a *App) aiAttemptAllowed() bool {
	a.statsMu.Lock()
	defer a.statsMu.Unlock()
	return time.Since(a.lastAIAttempt) >= aiRetryMinGap
}

// notifyPendingAI kicks off a single-flight batch analysis when enough logs are queued.
func (a *App) notifyPendingAI() {
	if a.database == nil || a.gemini == nil || !a.gemini.HasKey() {
		return
	}
	if !a.aiAttemptAllowed() {
		return
	}
	count, err := a.database.CountUnanalyzedLogs()
	if err != nil || count < aiBatchSize {
		return
	}
	a.processPendingAIBatched()
}

// processPendingAIBatched runs a batch analysis unless one is already in flight.
func (a *App) processPendingAIBatched() {
	if a.database == nil || a.gemini == nil || !a.gemini.HasKey() {
		return
	}
	if !a.aiMu.TryLock() {
		return
	}
	defer a.aiMu.Unlock()
	a.markAIAttempt()
	_, _ = a.processPendingAI()
}

// ProcessPendingLogs scans for unanalyzed logs and processes them in batched Gemini requests.
func (a *App) ProcessPendingLogs() (int, error) {
	a.aiMu.Lock()
	defer a.aiMu.Unlock()
	a.markAIAttempt()
	return a.processPendingAI()
}

// processPendingAI analyzes pending logs in groups of aiBatchSize using the compact
// prompt, thumbnail images, request-level retry and model failover. Partial results
// are applied immediately; items without results get their retry counter incremented.
func (a *App) processPendingAI() (int, error) {
	if a.database == nil || a.gemini == nil || !a.gemini.HasKey() {
		return 0, nil
	}

	logs, err := a.database.GetUnanalyzedLogs()
	if err != nil || len(logs) == 0 {
		return 0, err
	}

	log.Printf("[app] AI batch analysis: %d pending logs (%d per request, thumbnails, retry+failover)", len(logs), aiBatchSize)
	processed := 0

	for start := 0; start < len(logs); start += aiBatchSize {
		end := start + aiBatchSize
		if end > len(logs) {
			end = len(logs)
		}
		chunk := logs[start:end]

		items := make([]ai.BatchAnalysisItem, 0, len(chunk))
		byID := make(map[int64]db.LogEntry, len(chunk))
		for _, entry := range chunk {
			items = append(items, ai.BatchAnalysisItem{
				LogID:        entry.ID,
				WindowTitle:  entry.WindowTitle,
				EntropyScore: entry.EntropyScore,
				ImagePath:    entry.ImagePath,
				TotalClicks:  entry.TotalClicks,
				UserTask:     a.GetActiveTask(),
				AppName:      entry.AppName,
				Timestamp:    entry.Timestamp,
			})
			byID[entry.ID] = entry
		}

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		results, batchErr := a.gemini.AnalyzeBatch(ctx, items)
		cancel()

		if batchErr != nil && len(results) == 0 {
			if isTransientAIError(batchErr) {
				// Offline / temporary outage / rate limit: leave entries untouched so
				// they are analyzed on a later run (cron, next collect or reopen).
				log.Printf("[app] AI batch deferred (transient): %v — %d logs stay pending for retry", batchErr, len(chunk))
				break
			}
			log.Printf("[app] AI batch failed for %d logs: %v — incrementing retry counts", len(chunk), batchErr)
			for _, entry := range chunk {
				_ = a.database.IncrementAIRetryCount(entry.ID)
			}
			continue
		}
		if batchErr != nil {
			log.Printf("[app] AI batch partially failed (%v) — applying %d/%d results", batchErr, len(results), len(chunk))
		}

		got := make(map[int64]bool, len(results))
		for _, res := range results {
			entry, ok := byID[res.LogID]
			if !ok || got[res.LogID] {
				continue
			}
			got[res.LogID] = true
			updateErr := a.database.UpdateLogAnalysis(res.LogID, res.Category, res.AppName, res.AppCategory, entry.WindowTitle, 0, "", res.Productive, res.ProductiveScore, res.Confidence, res.Reason)
			if updateErr != nil {
				log.Printf("[app] update AI result for #%d error: %v", res.LogID, updateErr)
				continue
			}
			if res.Usage.TotalTokenCount > 0 {
				_ = a.database.RecordAPIUsage(entry.OrgID, entry.UserID, "guest", res.Usage.PromptTokenCount, res.Usage.CandidatesTokenCount, res.Usage.TotalTokenCount, a.gemini.GetModel())
			}
			processed++
		}

		for _, entry := range chunk {
			if !got[entry.ID] && !isTransientAIError(batchErr) {
				_ = a.database.IncrementAIRetryCount(entry.ID)
			}
		}

		if end < len(logs) {
			time.Sleep(500 * time.Millisecond)
		}
	}

	log.Printf("[app] AI batch analysis complete: %d/%d logs", processed, len(logs))
	return processed, nil
}

// ---- Desktop-only developer / diagnostics bindings ----

const (
	demoAppName         = "DEV-SIM"
	maxDiagnosticsBytes = 2 << 20
	defaultLogTailLines = 500
	maxLogTailLines     = 5000
)

// GetDevDiagnostics returns non-sensitive runtime diagnostics for the developer panel.
// It never includes API keys, tokens or JWTs (only a masked key hint).
func (a *App) GetDevDiagnostics() map[string]interface{} {
	diag := map[string]interface{}{
		"app_mode":              "desktop",
		"headless":              a.headless,
		"go_version":            runtime.Version(),
		"os":                    runtime.GOOS,
		"arch":                  runtime.GOARCH,
		"wails_buildtype":       "",
		"data_dir":              "",
		"db_path":               "",
		"db_size_bytes":         int64(0),
		"logs_dir":              "",
		"today_log_file":        "app_" + time.Now().Format("2006-01-02") + ".log",
		"log_files_count":       0,
		"screenshot_dir":        "",
		"screenshots_count":     0,
		"screenshots_bytes":     int64(0),
		"tracking_active":       false,
		"tracked_seconds_today": int64(0),
		"daemon_running":        false,
		"daemon_pid":            0,
		"ai_configured":         false,
		"ai_model":              "",
		"ai_key_masked":         maskSecret(""),
		"pending_ai_logs":       0,
		"backend_endpoint":      "",
		"sync_last_at":          "",
		"demo_logs_count":       0,
		"version":               appVersion(),
	}

	if a.ctx != nil {
		diag["wails_buildtype"] = wailsruntime.Environment(a.ctx).BuildType
	}

	dataDir := ""
	if a.cfg != nil {
		dataDir = a.cfg.DataDir
	}
	diag["data_dir"] = dataDir
	diag["backend_endpoint"] = ""
	if a.cfg != nil {
		diag["backend_endpoint"] = a.cfg.BackendEndpoint
	}

	if dataDir != "" {
		dbPath := filepath.Join(dataDir, "tracker.db")
		diag["db_path"] = dbPath
		var dbSize int64
		if info, err := os.Stat(dbPath); err == nil {
			dbSize = info.Size()
		}
		diag["db_size_bytes"] = dbSize

		logsDir := filepath.Join(dataDir, "logs")
		diag["logs_dir"] = logsDir
		diag["log_files_count"] = countAppLogFiles(logsDir)

		screenshotDir := filepath.Join(dataDir, "images")
		diag["screenshot_dir"] = screenshotDir
		scCount, scBytes := dirFileStats(screenshotDir)
		diag["screenshots_count"] = scCount
		diag["screenshots_bytes"] = scBytes

		daemonPid := readPIDFile(pidPath(dataDir))
		daemonRunning := daemonPid > 0 && processAlive(daemonPid)
		diag["daemon_running"] = daemonRunning
		diag["daemon_pid"] = daemonPid
	}

	a.statsMu.Lock()
	diag["tracking_active"] = a.isTrackingActive
	a.statsMu.Unlock()
	diag["tracked_seconds_today"] = a.GetTodayTrackedSeconds()

	aiConfigured := false
	aiModel := ""
	if a.gemini != nil {
		aiConfigured = a.gemini.HasKey()
		aiModel = a.gemini.GetModel()
	}
	apiKey := ""
	if a.cfg != nil {
		apiKey = a.cfg.GeminiAPIKey
	}
	if aiModel == "" && a.cfg != nil {
		aiModel = a.cfg.GeminiModel
	}
	diag["ai_configured"] = aiConfigured
	diag["ai_model"] = aiModel
	diag["ai_key_masked"] = maskSecret(apiKey)

	pendingAI := 0
	demoCount := 0
	if a.database != nil {
		if c, err := a.database.CountUnanalyzedLogs(); err == nil {
			pendingAI = c
		}
		if c, err := a.database.CountLogsByAppName(demoAppName); err == nil {
			demoCount = c
		}
	}
	diag["pending_ai_logs"] = pendingAI
	diag["demo_logs_count"] = demoCount

	if a.syncEngine != nil {
		if t := a.syncEngine.LastSyncedAt(); !t.IsZero() {
			diag["sync_last_at"] = t.UTC().Format(time.RFC3339)
		}
	}

	return diag
}

// GetAppLogs returns the last N lines of an app log file. fileName "" or "today"
// selects today's file; non-positive tailLines defaults to 500 (max 5000).
func (a *App) GetAppLogs(fileName string, tailLines int) (string, error) {
	logsDir, err := a.devLogsDir()
	if err != nil {
		return "", err
	}

	path, err := resolveAppLogPath(logsDir, fileName)
	if err != nil {
		return "", err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read app log %s: %w", filepath.Base(path), err)
	}

	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	lines = tailLogLines(lines, tailLines)
	if len(lines) == 0 {
		return "", nil
	}
	return strings.Join(lines, "\n") + "\n", nil
}

// ListAppLogFiles lists app_*.log files in the logs directory.
func (a *App) ListAppLogFiles() ([]map[string]interface{}, error) {
	logsDir, err := a.devLogsDir()
	if err != nil {
		return nil, err
	}

	matches, err := filepath.Glob(filepath.Join(logsDir, "app_*.log"))
	if err != nil {
		return nil, fmt.Errorf("glob app logs: %w", err)
	}

	files := make([]map[string]interface{}, 0, len(matches))
	for _, path := range matches {
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		files = append(files, map[string]interface{}{
			"name":       info.Name(),
			"size_bytes": info.Size(),
			"mod_time":   info.ModTime().UTC().Format(time.RFC3339),
		})
	}
	return files, nil
}

// ClearAppLogs deletes all app_*.log files in the logs directory.
func (a *App) ClearAppLogs() error {
	logsDir, err := a.devLogsDir()
	if err != nil {
		return err
	}

	matches, err := filepath.Glob(filepath.Join(logsDir, "app_*.log"))
	if err != nil {
		return fmt.Errorf("glob app logs: %w", err)
	}
	for _, path := range matches {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove app log %s: %w", filepath.Base(path), err)
		}
	}
	return nil
}

// OpenFolder opens one of the app folders ("logs"|"data"|"screenshots") in the
// OS file manager. Empty kind defaults to the data directory.
func (a *App) OpenFolder(kind string) error {
	if a.cfg == nil || a.cfg.DataDir == "" {
		return fmt.Errorf("config/data dir unavailable")
	}

	var dir string
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "logs":
		dir = filepath.Join(a.cfg.DataDir, "logs")
	case "screenshots":
		dir = filepath.Join(a.cfg.DataDir, "images")
	case "", "data":
		dir = a.cfg.DataDir
	default:
		return fmt.Errorf("unknown folder kind %q", kind)
	}

	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create folder %s: %w", dir, err)
	}
	if err := openFolderNative(dir); err != nil {
		return fmt.Errorf("open folder %s: %w", dir, err)
	}
	return nil
}

// SaveDiagnosticsBundle prompts for a path and writes the given content.
// Returns "" when the user cancels. Content is capped at 2 MB.
func (a *App) SaveDiagnosticsBundle(content string) (string, error) {
	if a.ctx == nil || a.headless {
		return "", fmt.Errorf("file dialogs unavailable (app not running in desktop mode)")
	}
	if len(content) > maxDiagnosticsBytes {
		content = content[:maxDiagnosticsBytes]
	}

	path, err := wailsruntime.SaveFileDialog(a.ctx, wailsruntime.SaveDialogOptions{
		DefaultFilename: fmt.Sprintf("get-hike-diagnostics-%s.md", time.Now().Format("20060102-150405")),
		Title:           "Save Diagnostics Bundle",
		Filters: []wailsruntime.FileFilter{
			{DisplayName: "Markdown (*.md)", Pattern: "*.md"},
		},
	})
	if err != nil {
		return "", fmt.Errorf("save diagnostics dialog: %w", err)
	}
	if path == "" {
		return "", nil
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return "", fmt.Errorf("write diagnostics bundle: %w", err)
	}
	return path, nil
}

// RunCollectNow triggers a single telemetry collection pass.
func (a *App) RunCollectNow() error {
	if a.cfg == nil {
		return fmt.Errorf("config unavailable")
	}
	if a.database == nil {
		return fmt.Errorf("database unavailable")
	}
	go a.collect()
	return nil
}

// StopTrackerDaemon stops the detached background tracker, if one is running.
func (a *App) StopTrackerDaemon() error {
	if a.cfg == nil {
		return fmt.Errorf("config unavailable")
	}
	stopBackgroundDaemon(a.cfg.DataDir)
	return nil
}

// GetSyncStatus returns the last sync time, enabled flag and backend endpoint.
func (a *App) GetSyncStatus() map[string]interface{} {
	status := map[string]interface{}{
		"last_synced_at":       "",
		"enabled":              false,
		"endpoint":             "",
		"pending_upload_count": 0,
	}
	if a.cfg != nil {
		status["endpoint"] = a.cfg.BackendEndpoint
	}
	if a.syncEngine != nil {
		status["enabled"] = !a.isGuest && a.syncEngine.Enabled()
		status["endpoint"] = a.syncEngine.Endpoint()
		if t := a.syncEngine.LastSyncedAt(); !t.IsZero() {
			status["last_synced_at"] = t.UTC().Format(time.RFC3339)
		}
	}
	if a.database != nil {
		if count, err := a.database.CountLogsBySyncStatus("pending_upload"); err == nil {
			status["pending_upload_count"] = count
		}
	}
	return status
}

// GetDBTableCounts returns row counts for the core local tables.
func (a *App) GetDBTableCounts() (map[string]int, error) {
	if a.database == nil {
		return nil, fmt.Errorf("database unavailable")
	}
	return a.database.TableCounts()
}

// BackupDatabase creates a SQLite backup and prompts for a destination path.
// Returns "" when the user cancels.
func (a *App) BackupDatabase() (string, error) {
	if a.database == nil {
		return "", fmt.Errorf("database unavailable")
	}
	if a.ctx == nil || a.headless {
		return "", fmt.Errorf("file dialogs unavailable (app not running in desktop mode)")
	}

	tmp, err := os.CreateTemp("", "get-hike-backup-*.db")
	if err != nil {
		return "", fmt.Errorf("create temp backup file: %w", err)
	}
	tmpPath := tmp.Name()
	_ = tmp.Close()
	// VACUUM INTO fails when the target already exists.
	_ = os.Remove(tmpPath)
	defer os.Remove(tmpPath)

	if err := a.database.BackupTo(tmpPath); err != nil {
		return "", fmt.Errorf("database backup: %w", err)
	}

	path, err := wailsruntime.SaveFileDialog(a.ctx, wailsruntime.SaveDialogOptions{
		DefaultFilename: fmt.Sprintf("get-hike-backup-%s.db", time.Now().Format("20060102-150405")),
		Title:           "Save Database Backup",
		Filters: []wailsruntime.FileFilter{
			{DisplayName: "SQLite Database (*.db)", Pattern: "*.db"},
		},
	})
	if err != nil {
		return "", fmt.Errorf("save backup dialog: %w", err)
	}
	if path == "" {
		return "", nil
	}
	if err := copyFileContents(tmpPath, path); err != nil {
		return "", fmt.Errorf("copy database backup: %w", err)
	}
	return path, nil
}

// GetScreenshotCacheStats returns the screenshot cache directory, file count and total bytes.
func (a *App) GetScreenshotCacheStats() map[string]interface{} {
	dir := ""
	if a.cfg != nil && a.cfg.DataDir != "" {
		dir = filepath.Join(a.cfg.DataDir, "images")
	}
	count, totalBytes := dirFileStats(dir)
	return map[string]interface{}{
		"dir":         dir,
		"count":       count,
		"total_bytes": totalBytes,
	}
}

// PurgeScreenshotCache deletes all files under the screenshots directory and
// returns the number of removed files.
func (a *App) PurgeScreenshotCache() (int, error) {
	if a.cfg == nil || a.cfg.DataDir == "" {
		return 0, fmt.Errorf("config/data dir unavailable")
	}
	dir := filepath.Join(a.cfg.DataDir, "images")

	removed := 0
	walkErr := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if d.IsDir() {
			return nil
		}
		if rmErr := os.Remove(path); rmErr != nil {
			if os.IsNotExist(rmErr) {
				return nil
			}
			return rmErr
		}
		removed++
		return nil
	})
	if walkErr != nil && !os.IsNotExist(walkErr) {
		return removed, fmt.Errorf("purge screenshots: %w", walkErr)
	}

	if entries, err := os.ReadDir(dir); err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				_ = os.Remove(filepath.Join(dir, entry.Name()))
			}
		}
	}
	return removed, nil
}

// RunAIDebugOnLatest analyzes the newest unanalyzed log once and returns the raw result.
func (a *App) RunAIDebugOnLatest() (map[string]interface{}, error) {
	result := map[string]interface{}{
		"log_id":       int64(0),
		"window_title": "",
		"model":        "",
		"category":     "",
		"productive":   false,
		"score":        float64(0),
		"reason":       "",
		"error":        "",
	}

	fail := func(err error) (map[string]interface{}, error) {
		result["error"] = err.Error()
		return result, err
	}

	if a.database == nil {
		return fail(fmt.Errorf("database unavailable"))
	}
	if a.gemini == nil || !a.gemini.HasKey() {
		return fail(fmt.Errorf("Gemini API key not configured"))
	}
	if !a.aiMu.TryLock() {
		return fail(fmt.Errorf("AI analysis already in progress"))
	}
	defer a.aiMu.Unlock()

	entry, err := a.database.GetLatestUnanalyzedLog()
	if err != nil {
		return fail(fmt.Errorf("load latest unanalyzed log: %w", err))
	}
	if entry == nil {
		return fail(fmt.Errorf("no unanalyzed logs available"))
	}

	result["log_id"] = entry.ID
	result["window_title"] = entry.WindowTitle

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	res, err := a.gemini.Analyze(ctx, entry.WindowTitle, entry.EntropyScore, entry.TotalClicks)
	if err != nil {
		return fail(fmt.Errorf("gemini analyze log #%d: %w", entry.ID, err))
	}

	result["model"] = a.gemini.GetModel()
	result["category"] = res.Category
	result["productive"] = res.Productive
	result["score"] = res.ProductiveScore
	result["reason"] = res.Reason

	appName := res.AppName
	if strings.TrimSpace(appName) == "" {
		appName = entry.AppName
	}
	appCategory := res.AppCategory
	if strings.TrimSpace(appCategory) == "" {
		appCategory = entry.AppCategory
	}

	updateErr := a.database.UpdateLogAnalysis(entry.ID, res.Category, appName, appCategory, entry.WindowTitle, entry.SessionID, entry.SessionTitle, res.Productive, res.ProductiveScore, res.Confidence, res.Reason)
	if updateErr != nil {
		return fail(fmt.Errorf("store AI result for log #%d: %w", entry.ID, updateErr))
	}
	if res.Usage.TotalTokenCount > 0 {
		_ = a.database.RecordAPIUsage(entry.OrgID, entry.UserID, "guest", res.Usage.PromptTokenCount, res.Usage.CandidatesTokenCount, res.Usage.TotalTokenCount, a.gemini.GetModel())
	}
	return result, nil
}

// ListAvailableModels returns the sorted unique Gemini models supporting generateContent.
func (a *App) ListAvailableModels() ([]string, error) {
	client := a.gemini
	key := ""
	if a.cfg != nil {
		key = strings.TrimSpace(a.cfg.GeminiAPIKey)
	}
	if client == nil {
		if key == "" {
			return nil, fmt.Errorf("no Gemini API key configured")
		}
		client = ai.NewGeminiClient(key)
	}
	if !client.HasKey() {
		if key == "" {
			return nil, fmt.Errorf("no Gemini API key configured")
		}
		client.SetAPIKey(key)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	models, err := client.FetchAvailableModels(ctx)
	if err != nil {
		return nil, fmt.Errorf("list Gemini models: %w", err)
	}

	sort.Strings(models)
	unique := models[:0]
	for i, model := range models {
		if i == 0 || model != unique[len(unique)-1] {
			unique = append(unique, model)
		}
	}
	return unique, nil
}

// SetLogLevel applies a runtime-only log level (debug|info|warn|warning|error).
func (a *App) SetLogLevel(level string) error {
	return logger.SetLevel(level)
}

// SeedDemoLogs inserts count synthetic DEV-SIM logs (1..50) spread over the last hour.
func (a *App) SeedDemoLogs(count int) (int, error) {
	if count < 1 || count > 50 {
		return 0, fmt.Errorf("demo log count must be between 1 and 50 (got %d)", count)
	}
	if a.database == nil {
		return 0, fmt.Errorf("database unavailable")
	}

	now := time.Now()
	spacing := 60 * time.Minute / time.Duration(count)
	inserted := 0
	for i := 0; i < count; i++ {
		entry := &db.LogEntry{
			Timestamp:    now.Add(-time.Duration(i) * spacing),
			WindowTitle:  "DEV-SIM Demo Log",
			TotalKeys:    80 + i,
			UniqueKeys:   20,
			EntropyScore: 40,
			TotalClicks:  5,
			AppName:      demoAppName,
			SyncStatus:   "synced",
		}
		id, err := a.database.InsertLog(entry)
		if err != nil {
			return inserted, fmt.Errorf("insert demo log %d: %w", i+1, err)
		}
		if err := a.database.UpdateLogAnalysis(id, "Coding", demoAppName, "Development", entry.WindowTitle, 0, "", true, 70, 0.9, "Demo seed entry"); err != nil {
			return inserted, fmt.Errorf("analyze demo log %d: %w", i+1, err)
		}
		if err := a.database.MarkLogsSynced([]int64{id}, nil); err != nil {
			return inserted, fmt.Errorf("mark demo log %d synced: %w", i+1, err)
		}
		inserted++
	}
	log.Printf("[app] seeded %d demo logs (app_name=%s)", inserted, demoAppName)
	return inserted, nil
}

// ClearDemoLogs deletes all synthetic DEV-SIM logs and returns the deleted count.
func (a *App) ClearDemoLogs() (int, error) {
	if a.database == nil {
		return 0, fmt.Errorf("database unavailable")
	}
	deleted, err := a.database.DeleteLogsByAppName(demoAppName)
	if err != nil {
		return 0, fmt.Errorf("clear demo logs: %w", err)
	}
	log.Printf("[app] cleared %d demo logs (app_name=%s)", deleted, demoAppName)
	return deleted, nil
}

// devLogsDir returns the app logs directory or an error when config is unavailable.
func (a *App) devLogsDir() (string, error) {
	if a.cfg == nil || a.cfg.DataDir == "" {
		return "", fmt.Errorf("config/data dir unavailable")
	}
	return filepath.Join(a.cfg.DataDir, "logs"), nil
}

// resolveAppLogPath converts a user-supplied log file name into a safe path inside
// logsDir. Traversal sequences are neutralized via filepath.Base and only names
// matching app_*.log are accepted.
func resolveAppLogPath(logsDir, fileName string) (string, error) {
	raw := strings.TrimSpace(fileName)
	if raw == "" || raw == "today" {
		raw = "app_" + time.Now().Format("2006-01-02") + ".log"
	}
	name := filepath.Base(raw)
	if !strings.HasPrefix(name, "app_") || !strings.HasSuffix(name, ".log") {
		return "", fmt.Errorf("invalid app log file name %q", fileName)
	}
	return filepath.Join(logsDir, name), nil
}

// tailLogLines clamps n to [1, maxLogTailLines] (defaulting to defaultLogTailLines)
// and returns the last n lines.
func tailLogLines(lines []string, n int) []string {
	if n <= 0 {
		n = defaultLogTailLines
	}
	if n > maxLogTailLines {
		n = maxLogTailLines
	}
	if len(lines) <= n {
		return lines
	}
	return lines[len(lines)-n:]
}

// maskSecret returns a masked hint for a secret value.
func maskSecret(secret string) string {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return "not set"
	}
	if len(secret) <= 10 {
		return "set"
	}
	return secret[:4] + "..." + secret[len(secret)-4:]
}

// countAppLogFiles counts app_*.log files in dir, returning 0 on any error.
func countAppLogFiles(dir string) int {
	if dir == "" {
		return 0
	}
	matches, err := filepath.Glob(filepath.Join(dir, "app_*.log"))
	if err != nil {
		return 0
	}
	return len(matches)
}

// dirFileStats counts regular files under dir and sums their sizes.
func dirFileStats(dir string) (int, int64) {
	if dir == "" {
		return 0, 0
	}
	count := 0
	var total int64
	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if info, infoErr := d.Info(); infoErr == nil {
			count++
			total += info.Size()
		}
		return nil
	})
	return count, total
}

// copyFileContents copies src to dst.
func copyFileContents(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

// appVersion returns the main module version with a short VCS revision when available.
func appVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}
	version := info.Main.Version
	if version == "" || version == "(devel)" {
		version = "dev"
	}
	for _, setting := range info.Settings {
		if setting.Key == "vcs.revision" && len(setting.Value) >= 7 {
			return version + "+" + setting.Value[:7]
		}
	}
	return version
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
