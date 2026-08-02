package main

import (
	"context"
	"encoding/base64"
	"log"
	"os"
	"time"

	"github.com/reak/mini-tracker/internal/ai"
	"github.com/reak/mini-tracker/internal/config"
	"github.com/reak/mini-tracker/internal/db"
	"github.com/reak/mini-tracker/internal/tracker"
)


// App is the Wails application struct — all public methods are bound to the frontend.
type App struct {
	ctx    context.Context
	cancel context.CancelFunc

	cfg        *config.Config
	database   *db.DB
	gemini     *ai.GeminiClient
	keyTracker *tracker.KeystrokeTracker

	// latest keystroke stats (updated by the tracker goroutine)
	latestKeyStats tracker.KeystrokeStats
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
		cfg = &config.Config{ScreenshotInterval: 30 * time.Second, DataDir: "/tmp/mini-tracker"}
	}
	a.cfg = cfg

	// Open database
	dbConn, err := db.Open(cfg.DataDir)
	if err != nil {
		log.Printf("[app] db error: %v", err)
	} else {
		a.database = dbConn
	}

	// Gemini client
	a.gemini = ai.NewGeminiClient(cfg.GeminiAPIKey, cfg.GeminiModel)

	// Start keystroke tracker
	a.keyTracker = tracker.NewKeystrokeTracker(cfg.ScreenshotInterval)
	statsCh, err := a.keyTracker.Start()
	if err != nil {
		log.Printf("[app] keystroke tracker error: %v", err)
		statsCh = nil
	}

	// Main collection loop — fires every ScreenshotInterval
	go func() {
		ticker := time.NewTicker(cfg.ScreenshotInterval)
		defer ticker.Stop()

		for {
			select {
			case <-a.ctx.Done():
				return
			case stats, ok := <-statsCh:
				if ok {
					a.latestKeyStats = stats
				}
			case <-ticker.C:
				a.collect()
			}
		}
	}()

	// Auto-process any pending/unanalyzed logs if Gemini key is set
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

	log.Printf("[app] started — screenshot interval: %s", cfg.ScreenshotInterval)
}

// collect captures a screenshot + uses latest keystroke stats, persists the
// entry, then fires async Gemini analysis.
func (a *App) collect() {
	keyStats := a.latestKeyStats
	a.latestKeyStats = tracker.KeystrokeStats{} // reset

	// Screenshot
	shot, err := tracker.CaptureScreenshot(a.cfg.DataDir)
	if err != nil {
		log.Printf("[app] screenshot error: %v", err)
		shot = &tracker.ScreenshotResult{}
	}

	entry := &db.LogEntry{
		Timestamp:    time.Now(),
		ImagePath:    shot.FilePath,
		TotalKeys:    keyStats.TotalKeys,
		UniqueKeys:   keyStats.UniqueKeys,
		EntropyScore: keyStats.EntropyScore,
	}

	if a.database == nil {
		return
	}

	id, err := a.database.InsertLog(entry)
	if err != nil {
		log.Printf("[app] insert log error: %v", err)
		return
	}

	// Fire async AI analysis
	go func(logID int64, filePath, b64Data string, score float64) {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()

		result, err := a.gemini.Analyze(ctx, b64Data, score)
		if err != nil {
			log.Printf("[app] AI analysis error: %v", err)
		} else {
			if err := a.database.UpdateAIResult(logID, result.Category, result.Productive, result.Confidence, result.Reason); err != nil {
				log.Printf("[app] update AI result error: %v", err)
			}
			log.Printf("[app] logged #%d — category=%s productive=%v",
				logID, result.Category, result.Productive)
		}

		// Ensure screenshot file is deleted after processing/sending to backend
		if filePath != "" {
			if err := os.Remove(filePath); err == nil {
				log.Printf("[app] deleted temporary screenshot: %s", filePath)
			}
		}
	}(id, shot.FilePath, shot.Base64Data, keyStats.EntropyScore)
}

// shutdown is called when the app terminates.
func (a *App) shutdown(_ context.Context) {
	if a.cancel != nil {
		a.cancel()
	}
	if a.keyTracker != nil {
		a.keyTracker.Stop()
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

// GetStats returns productivity stats for today.
func (a *App) GetStats(date string) (*db.ProductivityStats, error) {
	if a.database == nil {
		return nil, nil
	}
	return a.database.GetProductivityStats(date)
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

// GetImageBase64 reads an image file from disk and returns it as a data URL.
func (a *App) GetImageBase64(imagePath string) (string, error) {
	if imagePath == "" {
		return "", nil
	}
	data, err := os.ReadFile(imagePath)
	if err != nil {
		return "", err
	}
	return "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(data), nil
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
			log.Printf("[app] re-analyze log #%d error: %v", entry.ID, err)
			continue
		}

		if err := a.database.UpdateAIResult(entry.ID, res.Category, res.Productive, res.Confidence, res.Reason); err == nil {
			processed++
			// Delete screenshot file after sending to backend/AI
			if entry.ImagePath != "" {
				if err := os.Remove(entry.ImagePath); err == nil {
					log.Printf("[app] deleted processed screenshot: %s", entry.ImagePath)
				}
			}
		}
	}

	log.Printf("[app] completed processing %d/%d pending logs", processed, len(logs))
	return processed, nil
}

