// cmd/server is a standalone web server & collector for mini-tracker.
// It requires NO sudo, NO GTK, and NO pkg-config dependencies.
// It serves the full React dashboard UI and runs the background tracker daemon.
package main

import (
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/reak/get-hike/internal/ai"
	"github.com/reak/get-hike/internal/appicon"
	"github.com/reak/get-hike/internal/config"
	"github.com/reak/get-hike/internal/db"
	"github.com/reak/get-hike/internal/email"
	"github.com/reak/get-hike/internal/tracker"
)

var (
	appIconsOnce sync.Once
	appIcons     *appicon.Resolver
)

func appIconResolver() *appicon.Resolver {
	appIconsOnce.Do(func() {
		appIcons = appicon.NewResolver()
		appIcons.Warm()
	})
	return appIcons
}

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Printf("[server] config load error: %v", err)
		cfg = &config.Config{
			ScreenshotInterval: 30 * time.Second,
			DataDir:            filepath.Join(os.Getenv("HOME"), ".local/share/get-hike"),
			BackendPort:        8080,
		}
	}

	database, err := db.Open(cfg.DataDir, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("[server] database error: %v", err)
	}
	defer database.Close()

	if updated, err := database.NormalizeAppNames(); err != nil {
		log.Printf("[server] app name normalisation error: %v", err)
	} else if updated > 0 {
		log.Printf("[server] normalised %d legacy app name rows", updated)
	}

	gemini := ai.NewGeminiClient(cfg.GeminiAPIKey, cfg.GeminiModel)
	if gemini.HasKey() {
		go processPendingLogs(database, gemini)
	}

	// Schedule periodic background AI batch analysis (configurable via config / env, defaults to 3 hours)
	go func() {
		ticker := time.NewTicker(cfg.AIAnalysisInterval)
		defer ticker.Stop()
		log.Printf("[server] scheduled periodic AI batch analysis cron every %v", cfg.AIAnalysisInterval)
		for range ticker.C {
			if gemini.HasKey() {
				log.Printf("[server] running scheduled periodic AI batch analysis...")
				processPendingLogs(database, gemini)
			}
		}
	}()

	keyTracker := tracker.NewKeystrokeTracker(cfg.ScreenshotInterval)

	statsCh, err := keyTracker.Start()
	if err != nil {
		log.Printf("[server] keystroke tracker info: %v (continuing with screenshot-only capture)", err)
		statsCh = nil
	} else {
		defer keyTracker.Stop()
	}

	// Background collector loop state
	var (
		trackerMutex     sync.Mutex
		trackerActive    = true
		trackerStartTime = time.Now()
		accumulatedSec   = database.GetTodayTrackedSeconds(int64(cfg.ScreenshotInterval.Seconds()))
	)

	getElapsedSeconds := func() int64 {
		trackerMutex.Lock()
		defer trackerMutex.Unlock()
		if !trackerActive {
			return accumulatedSec
		}
		return accumulatedSec + int64(time.Since(trackerStartTime).Seconds())
	}

	serverTickerResetCh := make(chan time.Duration, 1)

	var (
		latestKeyStats   tracker.KeystrokeStats
		latestMouseStats tracker.MouseStats
	)

	// Native mouse click capture (evdev when permitted; /api/tracker/input total_clicks as fallback)
	mouseTracker := tracker.NewMouseTracker(cfg.ScreenshotInterval)
	if mouseStatsCh, err := mouseTracker.Start(); err != nil {
		log.Printf("[server] mouse tracker info: %v (continuing without native click capture)", err)
	} else {
		defer mouseTracker.Stop()
		go func() {
			for ms := range mouseStatsCh {
				trackerMutex.Lock()
				latestMouseStats = ms
				trackerMutex.Unlock()
			}
		}()
	}

	go func() {
		dur := cfg.ScreenshotInterval
		ticker := time.NewTicker(dur)
		defer ticker.Stop()
		for {
			select {
			case newDur := <-serverTickerResetCh:
				ticker.Stop()
				dur = newDur
				ticker = time.NewTicker(dur)
				log.Printf("[server] background collector ticker reset to %v", dur)
			case stats, ok := <-statsCh:
				if ok {
					trackerMutex.Lock()
					latestKeyStats = stats
					trackerMutex.Unlock()
				}
			case <-ticker.C:
				trackerMutex.Lock()
				active := trackerActive
				trackerMutex.Unlock()
				if !active {
					continue
				}

				trackerMutex.Lock()
				ks := latestKeyStats
				latestKeyStats = tracker.KeystrokeStats{}
				ms := latestMouseStats
				latestMouseStats = tracker.MouseStats{}
				trackerMutex.Unlock()

				winRes, err := tracker.GetActiveWindowTitle()
				windowTitle := ""
				if err != nil {
					log.Printf("[server] window title error: %v", err)
				} else if winRes != nil {
					windowTitle = winRes.Title
				}

				imagePath := ""
				if res, err := tracker.CaptureScreenshot(cfg.DataDir); err != nil {
					log.Printf("[server] screenshot capture error: %v", err)
				} else if res != nil {
					imagePath = res.FilePath
				}

				entry := &db.LogEntry{
					Timestamp:    time.Now(),
					ImagePath:    imagePath,
					WindowTitle:  windowTitle,
					TotalKeys:    ks.TotalKeys,
					UniqueKeys:   ks.UniqueKeys,
					EntropyScore: ks.EntropyScore,
					TotalClicks:  ms.TotalClicks,
				}

				id, err := database.InsertLog(entry)
				if err != nil {
					log.Printf("[server] insert log error: %v", err)
					continue
				}

				go func(logID int64, title string, score float64, clicks int) {
					ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
					defer cancel()

					res, err := gemini.Analyze(ctx, title, score, clicks)
					if err != nil {
						log.Printf("[server] AI analysis error for #%d: %v — incrementing retry count", logID, err)
						_ = database.IncrementAIRetryCount(logID)
					} else {
						_ = database.UpdateLogAnalysis(logID, res.Category, res.AppName, res.AppCategory, res.WindowTitle, 0, "", res.Productive, res.ProductiveScore, res.Confidence, res.Reason)
						log.Printf("[server] logged #%d — app=%s category=%s productive_score=%.0f%% reason=%s", logID, res.AppName, res.Category, res.ProductiveScore, res.Reason)
					}
				}(id, windowTitle, ks.EntropyScore, ms.TotalClicks)
			}
		}
	}()

	// Background cleanup routine: Retain screenshots for 7 days (168 hours)
	go func() {
		ticker := time.NewTicker(6 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			imgDir := filepath.Join(cfg.DataDir, "images")
			cutoff := time.Now().Add(-7 * 24 * time.Hour)
			_ = filepath.Walk(imgDir, func(path string, info os.FileInfo, err error) error {
				if err == nil && !info.IsDir() && info.ModTime().Before(cutoff) {
					if strings.HasSuffix(path, ".png") || strings.HasSuffix(path, ".jpg") || strings.HasSuffix(path, ".jpeg") {
						_ = os.Remove(path)
						log.Printf("[server] 7-day retention cleanup: deleted old screenshot %s", path)
					}
				}
				return nil
			})
		}
	}()

	mux := http.NewServeMux()

	// GET /wizard — Self-contained floating tracker widget (opens as standalone OS window via wizard-launch.sh)
	mux.HandleFunc("/wizard", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, wizardHTML)
	})

	// GET & POST /api/tracker/status & /api/tracker/toggle
	mux.HandleFunc("/api/tracker/status", func(w http.ResponseWriter, r *http.Request) {
		trackerMutex.Lock()
		active := trackerActive
		trackerMutex.Unlock()
		jsonResp(w, map[string]interface{}{
			"active":          active,
			"elapsed_seconds": getElapsedSeconds(),
		})
	})

	mux.HandleFunc("/api/tracker/toggle", func(w http.ResponseWriter, r *http.Request) {
		if getSessionUser(r, database) == nil && !isLoopbackRequest(r) {
			jsonError(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		trackerMutex.Lock()
		if trackerActive {
			accumulatedSec += int64(time.Since(trackerStartTime).Seconds())
			trackerActive = false
			latestKeyStats = tracker.KeystrokeStats{}
			latestMouseStats = tracker.MouseStats{}
		} else {
			trackerStartTime = time.Now()
			trackerActive = true
		}
		active := trackerActive
		trackerMutex.Unlock()

		jsonResp(w, map[string]interface{}{
			"active":          active,
			"elapsed_seconds": getElapsedSeconds(),
		})
	})

	// Helper to resolve and enforce multi-tenant user & organization data isolation:
	// 1. Standard team members ('member') are hard-restricted to viewing ONLY their own user_id and org_id.
	// 2. Admins/Owners ('admin', 'owner') can view their own logs or logs of team members in their org.
	// 3. If requested user_id belongs to a different organization, returns HTTP 403 Forbidden.
	getAuthContext := func(r *http.Request, database *db.DB) (userID int64, orgID int64, statusCode int, errMsg string) {
		sessUser := getSessionUser(r, database)

		reqUserIDStr := r.URL.Query().Get("user_id")
		var reqUserID int64
		if reqUserIDStr != "" {
			reqUserID, _ = strconv.ParseInt(reqUserIDStr, 10, 64)
		}

		if sessUser == nil {
			if !isLoopbackRequest(r) {
				return 0, 0, http.StatusUnauthorized, "Unauthorized"
			}
			// Unauthenticated fallback for desktop standalone app mode (loopback only)
			if reqUserID > 0 {
				return reqUserID, 1, http.StatusOK, ""
			}
			return 0, 1, http.StatusOK, ""
		}

		// 1. Standard member: strictly isolated to own user_id & org_id
		if sessUser.Role != "owner" && sessUser.Role != "admin" {
			return sessUser.ID, sessUser.OrgID, http.StatusOK, ""
		}

		// 2. Admin / Owner: if specific user_id requested, verify same organization
		if reqUserID > 0 {
			targetUser, err := database.GetUserByID(reqUserID)
			if err != nil || targetUser == nil {
				return 0, 0, http.StatusNotFound, "Requested user not found"
			}
			if targetUser.OrgID != sessUser.OrgID {
				return 0, 0, http.StatusForbidden, "Forbidden: Requested user belongs to another organization"
			}
			return reqUserID, sessUser.OrgID, http.StatusOK, ""
		}

		// 3. Admin / Owner: viewing org-wide activity
		return 0, sessUser.OrgID, http.StatusOK, ""
	}

	// GET /api/logs?date=YYYY-MM-DD&start_date=...&end_date=...&user_id=...&page=1&limit=50
	mux.HandleFunc("/api/logs", func(w http.ResponseWriter, r *http.Request) {
		userID, orgID, status, errMsg := getAuthContext(r, database)
		if status != http.StatusOK {
			jsonError(w, errMsg, status)
			return
		}

		date := r.URL.Query().Get("date")
		startDate := r.URL.Query().Get("start_date")
		endDate := r.URL.Query().Get("end_date")
		pageStr := r.URL.Query().Get("page")
		limitStr := r.URL.Query().Get("limit")

		if startDate == "" && endDate == "" {
			if date == "" {
				date = time.Now().Format("2006-01-02")
			}
			startDate = date
			endDate = date
		}

		if pageStr != "" || limitStr != "" {
			page, _ := strconv.Atoi(pageStr)
			limit, _ := strconv.Atoi(limitStr)
			res, err := database.GetLogsFilteredPaginated(userID, orgID, startDate, endDate, page, limit)
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			jsonResp(w, res)
			return
		}

		logs, err := database.GetLogsFiltered(userID, orgID, startDate, endDate)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		if logs == nil {
			logs = []db.LogEntry{}
		}
		jsonResp(w, logs)
	})

	// POST /api/ai/log-result — Persist a client-side (web guest) AI analysis result
	// so results survive refreshes/new tabs instead of being re-analyzed every load.
	mux.HandleFunc("/api/ai/log-result", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		userID, orgID, status, errMsg := getAuthContext(r, database)
		if status != http.StatusOK {
			jsonError(w, errMsg, status)
			return
		}

		var req struct {
			ID              int64   `json:"id"`
			Category        string  `json:"category"`
			AppName         string  `json:"app_name"`
			AppCategory     string  `json:"app_category"`
			WindowTitle     string  `json:"window_title"`
			IsProductive    bool    `json:"is_productive"`
			ProductiveScore float64 `json:"productive_score"`
			Confidence      float64 `json:"ai_confidence"`
			Reason          string  `json:"ai_reason"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ID <= 0 || req.Category == "" {
			http.Error(w, "Invalid analysis result", http.StatusBadRequest)
			return
		}

		entry, err := database.GetLogByID(req.ID)
		if err != nil || entry == nil {
			http.Error(w, "Log not found", http.StatusNotFound)
			return
		}
		// Authenticated callers may only persist results for their own logs;
		// the unauthenticated fallback is already restricted to loopback.
		if userID > 0 && (entry.UserID != userID || (orgID > 0 && entry.OrgID != orgID)) {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}

		if err := database.UpdateLogAnalysis(req.ID, req.Category, req.AppName, req.AppCategory, req.WindowTitle, 0, "", req.IsProductive, req.ProductiveScore, req.Confidence, req.Reason); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		jsonResp(w, map[string]interface{}{"success": true})
	})

	// POST /api/ai/analyze-batch — Server-side AI analysis proxy (avoids exposing Gemini API key to browsers)
	mux.HandleFunc("/api/ai/analyze-batch", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		userID, orgID, status, errMsg := getAuthContext(r, database)
		if status != http.StatusOK {
			jsonError(w, errMsg, status)
			return
		}

		var req struct {
			LogIDs   []int64 `json:"log_ids"`
			UserTask string  `json:"user_task"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.LogIDs) == 0 {
			jsonError(w, "Invalid request or empty log_ids", http.StatusBadRequest)
			return
		}

		effKey, effModel, keySource, err := database.ResolveEffectiveGeminiKey(userID, orgID, cfg.GeminiAPIKey, cfg.GeminiModel)
		if err != nil || effKey == "" {
			jsonError(w, "No Gemini API key configured on server", http.StatusBadRequest)
			return
		}

		batchItems := make([]ai.BatchAnalysisItem, 0, len(req.LogIDs))
		for _, id := range req.LogIDs {
			entry, err := database.GetLogByID(id)
			if err != nil || entry == nil {
				continue
			}
			if userID > 0 && (entry.UserID != userID || (orgID > 0 && entry.OrgID != orgID)) {
				continue
			}
			batchItems = append(batchItems, ai.BatchAnalysisItem{
				LogID:        entry.ID,
				WindowTitle:  entry.WindowTitle,
				EntropyScore: entry.EntropyScore,
				ImagePath:    entry.ImagePath,
				TotalKeys:    entry.TotalKeys,
				TotalClicks:  entry.TotalClicks,
				AppName:      entry.AppName,
				Timestamp:    entry.Timestamp,
			})
		}

		if len(batchItems) == 0 {
			jsonError(w, "No accessible logs found for analysis", http.StatusBadRequest)
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
		defer cancel()

		client := ai.NewGeminiClient(effKey, effModel)
		results, err := client.AnalyzeBatch(ctx, batchItems)
		if err != nil {
			log.Printf("[server] AI proxy AnalyzeBatch error: %v", err)
			jsonError(w, fmt.Sprintf("AI analysis failed: %v", err), http.StatusBadGateway)
			return
		}

		var totalTokens int
		for _, res := range results {
			_ = database.UpdateLogAnalysis(res.LogID, res.Category, res.AppName, res.AppCategory, res.WindowTitle, 0, "", res.Productive, res.ProductiveScore, res.Confidence, res.Reason)
			totalTokens += res.Usage.TotalTokenCount
			if res.Usage.TotalTokenCount > 0 {
				_ = database.RecordAPIUsage(orgID, userID, keySource, res.Usage.PromptTokenCount, res.Usage.CandidatesTokenCount, res.Usage.TotalTokenCount, effModel)
			}
		}

		jsonResp(w, map[string]interface{}{
			"success":      true,
			"results":      results,
			"key_source":   keySource,
			"model":        effModel,
			"total_tokens": totalTokens,
		})
	})

	// GET /api/ai/models — Server-side model listing using configured effective key
	mux.HandleFunc("/api/ai/models", func(w http.ResponseWriter, r *http.Request) {
		userID, orgID, status, errMsg := getAuthContext(r, database)
		if status != http.StatusOK {
			jsonError(w, errMsg, status)
			return
		}

		effKey, effModel, _, err := database.ResolveEffectiveGeminiKey(userID, orgID, cfg.GeminiAPIKey, cfg.GeminiModel)
		if err != nil || effKey == "" {
			jsonError(w, "No Gemini API key configured on server", http.StatusBadRequest)
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()

		client := ai.NewGeminiClient(effKey, effModel)
		models, err := client.FetchAvailableModels(ctx)
		if err != nil {
			jsonError(w, fmt.Sprintf("Failed to fetch models: %v", err), http.StatusBadGateway)
			return
		}

		type ModelItem struct {
			Name        string `json:"name"`
			DisplayName string `json:"displayName"`
		}
		resList := make([]ModelItem, 0, len(models))
		for _, m := range models {
			clean := strings.TrimPrefix(m, "models/")
			resList = append(resList, ModelItem{
				Name:        m,
				DisplayName: clean,
			})
		}

		jsonResp(w, map[string]interface{}{
			"success": true,
			"models":  resList,
		})
	})

	// POST /api/ai/test — Test Gemini key / connection securely server-side
	mux.HandleFunc("/api/ai/test", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		userID, orgID, status, errMsg := getAuthContext(r, database)
		if status != http.StatusOK {
			jsonError(w, errMsg, status)
			return
		}

		var req struct {
			APIKey string `json:"api_key"`
			Model  string `json:"model"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)

		testKey := strings.TrimSpace(req.APIKey)
		testModel := strings.TrimSpace(req.Model)

		if testKey == "" {
			effKey, effModel, _, err := database.ResolveEffectiveGeminiKey(userID, orgID, cfg.GeminiAPIKey, cfg.GeminiModel)
			if err != nil || effKey == "" {
				jsonError(w, "No API key provided or configured on server", http.StatusBadRequest)
				return
			}
			testKey = effKey
			if testModel == "" {
				testModel = effModel
			}
		}

		if testModel == "" {
			testModel = "models/gemini-3.5-flash-lite"
		}

		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()

		client := ai.NewGeminiClient(testKey, testModel)
		models, err := client.FetchAvailableModels(ctx)
		if err != nil {
			jsonError(w, fmt.Sprintf("API test failed: %v", err), http.StatusBadRequest)
			return
		}

		type ModelItem struct {
			Name        string `json:"name"`
			DisplayName string `json:"displayName"`
		}
		resList := make([]ModelItem, 0, len(models))
		for _, m := range models {
			clean := strings.TrimPrefix(m, "models/")
			resList = append(resList, ModelItem{
				Name:        m,
				DisplayName: clean,
			})
		}

		jsonResp(w, map[string]interface{}{
			"success": true,
			"message": "Connection verified successfully",
			"models":  resList,
		})
	})

	// GET /api/stats?date=YYYY-MM-DD&start_date=...&end_date=...&user_id=...
	mux.HandleFunc("/api/stats", func(w http.ResponseWriter, r *http.Request) {
		userID, orgID, status, errMsg := getAuthContext(r, database)
		if status != http.StatusOK {
			jsonError(w, errMsg, status)
			return
		}

		date := r.URL.Query().Get("date")
		startDate := r.URL.Query().Get("start_date")
		endDate := r.URL.Query().Get("end_date")

		if startDate == "" && endDate == "" {
			if date == "" {
				date = time.Now().Format("2006-01-02")
			}
			startDate = date
			endDate = date
		}

		stats, err := database.GetProductivityStatsFiltered(userID, orgID, startDate, endDate)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		jsonResp(w, stats)
	})

	// GET /api/work-sessions?date=YYYY-MM-DD
	mux.HandleFunc("/api/work-sessions", func(w http.ResponseWriter, r *http.Request) {
		userID, orgID, status, errMsg := getAuthContext(r, database)
		if status != http.StatusOK {
			jsonError(w, errMsg, status)
			return
		}

		date := r.URL.Query().Get("date")
		startDate := r.URL.Query().Get("start_date")
		endDate := r.URL.Query().Get("end_date")

		if startDate == "" && endDate == "" {
			if date == "" {
				date = time.Now().Format("2006-01-02")
			}
			startDate = date
			endDate = date
		}

		sessions, err := database.GetWorkSessionsFiltered(userID, orgID, startDate, endDate)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		if sessions == nil {
			sessions = []db.WorkSession{}
		}
		jsonResp(w, sessions)
	})

	// GET /api/image?path=...
	mux.HandleFunc("/api/image", func(w http.ResponseWriter, r *http.Request) {
		imgPath := r.URL.Query().Get("path")
		if imgPath == "" {
			http.Error(w, "missing path", 400)
			return
		}
		if getSessionUser(r, database) == nil && !isLoopbackRequest(r) {
			http.Error(w, "unauthorized", 401)
			return
		}
		cleanPath := filepath.Clean(imgPath)
		if !filepath.IsAbs(cleanPath) {
			http.Error(w, "invalid path", 400)
			return
		}
		// Security check: only allow files contained in DataDir (screenshots/images)
		dataDirClean := filepath.Clean(cfg.DataDir)
		relPath, relErr := filepath.Rel(dataDirClean, cleanPath)
		if relErr != nil || filepath.IsAbs(relPath) || relPath == ".." || strings.HasPrefix(relPath, ".."+string(os.PathSeparator)) {
			http.Error(w, "forbidden path", 403)
			return
		}
		data, err := os.ReadFile(cleanPath)
		if err != nil {
			http.Error(w, "image not found", 404)
			return
		}
		w.Header().Set("Content-Type", "image/jpeg")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		w.Write(data)
	})

	// GET /api/app-icon?name=...
	mux.HandleFunc("/api/app-icon", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "method not allowed", 405)
			return
		}
		if getSessionUser(r, database) == nil && !isLoopbackRequest(r) {
			http.Error(w, "unauthorized", 401)
			return
		}
		name := strings.TrimSpace(r.URL.Query().Get("name"))
		if name == "" || len(name) > 160 || strings.ContainsAny(name, "\x00\r\n") {
			http.Error(w, "invalid name", 400)
			return
		}
		icon, ok := appIconResolver().Lookup(name)
		if !ok {
			http.Error(w, "icon not found", 404)
			return
		}
		w.Header().Set("Content-Type", icon.MIMEType)
		w.Header().Set("Cache-Control", "private, max-age=86400")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Write(icon.Data)
	})

	// GET or POST /api/config
	mux.HandleFunc("/api/config", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			if getSessionUser(r, database) == nil && !isLoopbackRequest(r) {
				jsonError(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			var payload struct {
				GeminiAPIKey              string  `json:"gemini_api_key"`
				AIModel                   string  `json:"ai_model"`
				ScreenshotIntervalSeconds float64 `json:"screenshot_interval_seconds"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err == nil {
				if payload.GeminiAPIKey != "" {
					cfg.GeminiAPIKey = payload.GeminiAPIKey
					gemini.SetAPIKey(payload.GeminiAPIKey)
				}
				if payload.AIModel != "" {
					cfg.GeminiModel = payload.AIModel
					gemini.SetModel(payload.AIModel)
				}
				if payload.ScreenshotIntervalSeconds >= 5 {
					cfg.ScreenshotInterval = time.Duration(payload.ScreenshotIntervalSeconds) * time.Second
					select {
					case serverTickerResetCh <- cfg.ScreenshotInterval:
					default:
					}
				}
				_ = config.Save(cfg)
				go processPendingLogs(database, gemini)
			}
		}

		jsonResp(w, map[string]interface{}{
			"screenshot_interval_seconds": cfg.ScreenshotInterval.Seconds(),
			"data_dir":                    cfg.DataDir,
			"ai_configured":               gemini.HasKey(),
		})
	})

	// POST /api/process-pending — Trigger cron background AI analysis manually or via scheduler
	mux.HandleFunc("/api/process-pending", func(w http.ResponseWriter, r *http.Request) {
		count := processPendingLogs(database, gemini)
		jsonResp(w, map[string]interface{}{
			"processed": count,
		})
	})

	// POST /api/tracker/input — Ingest keystroke events from desktop app / frontend listeners
	mux.HandleFunc("/api/tracker/input", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			jsonError(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if getSessionUser(r, database) == nil && !isLoopbackRequest(r) {
			jsonError(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		trackerMutex.Lock()
		active := trackerActive
		trackerMutex.Unlock()
		if !active {
			jsonResp(w, map[string]interface{}{"active": false, "success": false, "message": "Tracker is paused"})
			return
		}
		var payload struct {
			TotalKeys   int `json:"total_keys"`
			UniqueKeys  int `json:"unique_keys"`
			KeyCode     int `json:"key_code"`
			TotalClicks int `json:"total_clicks"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			jsonError(w, "Invalid input JSON payload", http.StatusBadRequest)
			return
		}

		if payload.KeyCode > 0 {
			keyTracker.RecordKeyCode(payload.KeyCode)
		} else if payload.TotalKeys > 0 {
			keyTracker.RecordKeystrokes(payload.TotalKeys, payload.UniqueKeys)
		}
		if payload.TotalClicks > 0 && mouseTracker != nil {
			mouseTracker.RecordMouseActivity(payload.TotalClicks)
		}

		jsonResp(w, map[string]interface{}{"success": true})
	})

	// POST /api/screenshots — Receive screenshot base64 + keystroke data from desktop app, save log, trigger AI API analysis & store analytics
	mux.HandleFunc("/api/screenshots", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			jsonError(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if getSessionUser(r, database) == nil && !isLoopbackRequest(r) {
			jsonError(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		trackerMutex.Lock()
		active := trackerActive
		trackerMutex.Unlock()
		if !active {
			jsonResp(w, map[string]interface{}{"active": false, "success": false, "message": "Tracker is paused"})
			return
		}

		var payload struct {
			ImageBase64  string  `json:"image_base64"`
			TotalKeys    int     `json:"total_keys"`
			UniqueKeys   int     `json:"unique_keys"`
			EntropyScore float64 `json:"entropy_score"`
			TotalClicks  int     `json:"total_clicks"`
			WindowTitle  string  `json:"window_title"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			jsonError(w, "Invalid screenshot payload", http.StatusBadRequest)
			return
		}

		if payload.ImageBase64 == "" {
			jsonError(w, "Image base64 data required", http.StatusBadRequest)
			return
		}

		// Save screenshot image to disk
		screensDir := filepath.Join(cfg.DataDir, "screenshots")
		_ = os.MkdirAll(screensDir, 0755)
		filePath := filepath.Join(screensDir, fmt.Sprintf("ss_%d.jpg", time.Now().UnixNano()))

		rawImg, err := base64.StdEncoding.DecodeString(payload.ImageBase64)
		if err != nil {
			jsonError(w, "Invalid base64 encoding", http.StatusBadRequest)
			return
		}

		if err := os.WriteFile(filePath, rawImg, 0644); err != nil {
			jsonError(w, "Failed to save screenshot file", http.StatusInternalServerError)
			return
		}

		entropy := payload.EntropyScore
		if entropy == 0 && payload.TotalKeys > 0 {
			entropy = tracker.ComputeEntropyScore(payload.TotalKeys, payload.UniqueKeys)
		}

		entry := &db.LogEntry{
			Timestamp:    time.Now(),
			ImagePath:    filePath,
			TotalKeys:    payload.TotalKeys,
			UniqueKeys:   payload.UniqueKeys,
			EntropyScore: entropy,
			TotalClicks:  payload.TotalClicks,
			WindowTitle:  payload.WindowTitle,
		}

		logID, err := database.InsertLog(entry)
		if err != nil {
			jsonError(w, "Database error", http.StatusInternalServerError)
			return
		}

		// Perform AI API analysis
		ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
		defer cancel()

		var category, reason string
		var productive bool
		var confidence float64

		batchResults, err := gemini.AnalyzeBatch(ctx, []ai.BatchAnalysisItem{{
			LogID:        logID,
			WindowTitle:  payload.WindowTitle,
			EntropyScore: entropy,
			ImagePath:    filePath,
			TotalClicks:  payload.TotalClicks,
		}})
		if err == nil && len(batchResults) > 0 {
			res := batchResults[0]
			category = res.Category
			productive = res.Productive
			confidence = res.Confidence
			reason = res.Reason
			_ = database.UpdateLogAnalysis(logID, res.Category, res.AppName, res.AppCategory, res.WindowTitle, 0, "", res.Productive, res.ProductiveScore, res.Confidence, res.Reason)
		} else {
			if err == nil {
				err = fmt.Errorf("no batch analysis result returned")
			}
			log.Printf("[server] API screenshot upload AI processing deferred for log #%d: %v", logID, err)
		}

		entry.ID = logID
		entry.AICategory = category
		entry.IsProductive = productive
		entry.AIConfidence = confidence
		entry.AIReason = reason

		jsonResp(w, map[string]interface{}{
			"success": true,
			"log":     entry,
		})
	})

	// POST /api/telemetry/push — Authenticated telemetry upload endpoint
	mux.HandleFunc("/api/telemetry/push", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			jsonError(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		trackerMutex.Lock()
		active := trackerActive
		trackerMutex.Unlock()
		if !active {
			jsonResp(w, map[string]interface{}{"active": false, "message": "Tracker is paused"})
			return
		}

		userID, orgID, status, errMsg := getAuthContext(r, database)
		if status != http.StatusOK {
			jsonError(w, errMsg, status)
			return
		}

		var payload struct {
			LocalID      int64   `json:"local_id"`
			Timestamp    string  `json:"timestamp"`
			ImageBase64  string  `json:"image_base64"`
			TotalKeys    int     `json:"total_keys"`
			UniqueKeys   int     `json:"unique_keys"`
			EntropyScore float64 `json:"entropy_score"`
			TotalClicks  int     `json:"total_clicks"`
			AppName      string  `json:"app_name"`
			WindowTitle  string  `json:"window_title"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			jsonError(w, "Invalid telemetry payload", http.StatusBadRequest)
			return
		}

		ts, err := time.Parse(time.RFC3339, payload.Timestamp)
		if err != nil {
			ts = time.Now()
		}

		filePath := ""
		if payload.ImageBase64 != "" {
			todayDir := filepath.Join(cfg.DataDir, "images", ts.Format("2006-01-02"))
			_ = os.MkdirAll(todayDir, 0755)
			filePath = filepath.Join(todayDir, fmt.Sprintf("ss_%d.jpg", time.Now().UnixNano()))
			if rawImg, err := base64.StdEncoding.DecodeString(payload.ImageBase64); err == nil {
				_ = os.WriteFile(filePath, rawImg, 0644)
			}
		}

		entropy := payload.EntropyScore
		if entropy == 0 && payload.TotalKeys > 0 {
			entropy = tracker.ComputeEntropyScore(payload.TotalKeys, payload.UniqueKeys)
		}

		entry := &db.LogEntry{
			OrgID:        orgID,
			UserID:       userID,
			Timestamp:    ts,
			ImagePath:    filePath,
			TotalKeys:    payload.TotalKeys,
			UniqueKeys:   payload.UniqueKeys,
			EntropyScore: entropy,
			TotalClicks:  payload.TotalClicks,
			AppName:      payload.AppName,
			WindowTitle:  payload.WindowTitle,
		}

		logID, err := database.InsertLog(entry)
		if err != nil {
			jsonError(w, "Database error saving telemetry", http.StatusInternalServerError)
			return
		}

		// Resolve effective Gemini API Key based on Hierarchy:
		// 1. Org Admin Key in DB -> 2. User Personal Key in DB -> 3. Global System Key
		go func(id int64, imgPath, title string, score float64, clicks int, uID, oID int64) {
			effKey, effModel, keySource, err := database.ResolveEffectiveGeminiKey(uID, oID, cfg.GeminiAPIKey, cfg.GeminiModel)
			if err != nil || effKey == "" {
				log.Printf("[server] telemetry AI processing skipped for log #%d (no valid key found, source=%s)", id, keySource)
				return
			}

			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()

			tempGemini := ai.NewGeminiClient(effKey, effModel)
			results, err := tempGemini.AnalyzeBatch(ctx, []ai.BatchAnalysisItem{{
				LogID:        id,
				WindowTitle:  title,
				EntropyScore: score,
				ImagePath:    imgPath,
				TotalClicks:  clicks,
			}})
			if err == nil && len(results) > 0 {
				res := results[0]
				_ = database.UpdateLogAnalysis(id, res.Category, res.AppName, res.AppCategory, res.WindowTitle, 0, "", res.Productive, res.ProductiveScore, res.Confidence, res.Reason)
				_ = database.RecordAPIUsage(oID, uID, keySource, res.Usage.PromptTokenCount, res.Usage.CandidatesTokenCount, res.Usage.TotalTokenCount, effModel)
				log.Printf("[server] processed telemetry #%d via AI key source [%s] — category=%s productive=%v tokens=%d", id, keySource, res.Category, res.Productive, res.Usage.TotalTokenCount)
			} else {
				if err == nil {
					err = fmt.Errorf("no batch analysis result returned")
				}
				log.Printf("[server] telemetry AI error for log #%d (source=%s): %v", id, keySource, err)
			}
		}(logID, filePath, payload.WindowTitle, entropy, payload.TotalClicks, userID, orgID)

		jsonResp(w, map[string]interface{}{
			"success":   true,
			"local_id":  payload.LocalID,
			"remote_id": logID,
		})
	})

	// GET /api/telemetry/pull — Download newly analyzed AI logs for desktop client sync
	mux.HandleFunc("/api/telemetry/pull", func(w http.ResponseWriter, r *http.Request) {
		userID, orgID, status, errMsg := getAuthContext(r, database)
		if status != http.StatusOK {
			jsonError(w, errMsg, status)
			return
		}

		sinceStr := r.URL.Query().Get("since")
		startDate := time.Now().Add(-24 * time.Hour).Format("2006-01-02")
		endDate := time.Now().Format("2006-01-02")

		if sinceStr != "" {
			if t, err := time.Parse(time.RFC3339, sinceStr); err == nil {
				startDate = t.Format("2006-01-02")
			}
		}

		logs, err := database.GetLogsFiltered(userID, orgID, startDate, endDate)
		if err != nil {
			jsonError(w, err.Error(), http.StatusInternalServerError)
			return
		}

		jsonResp(w, logs)
	})

	// GET /api/org/usage — Admin-Only endpoint to track organization key usage & member token stats
	mux.HandleFunc("/api/org/usage", func(w http.ResponseWriter, r *http.Request) {
		user := getSessionUser(r, database)
		if user == nil {
			if !isLoopbackRequest(r) {
				jsonError(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			// Guest / Standalone fallback: return local guest database usage summary (loopback only)
			summary, err := database.GetUserUsageSummary(0)
			if err != nil {
				jsonError(w, err.Error(), http.StatusInternalServerError)
				return
			}
			jsonResp(w, summary)
			return
		}

		if user.Role != "owner" && user.Role != "admin" {
			jsonError(w, "Forbidden: Only Organization Admins or Owners can view organization-wide usage metrics", http.StatusForbidden)
			return
		}

		summary, err := database.GetOrgUsageSummary(user.OrgID)
		if err != nil {
			jsonError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		jsonResp(w, summary)
	})

	// POST /api/org/usage/reset — Reset usage logs when Google quota resets or admin manually resets
	mux.HandleFunc("/api/org/usage/reset", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			jsonError(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		user := getSessionUser(r, database)
		if user == nil {
			jsonError(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		if user.Role != "owner" && user.Role != "admin" {
			jsonError(w, "Forbidden: Only Organization Admins can reset org usage", http.StatusForbidden)
			return
		}
		if err := database.ResetAPIUsage(user.OrgID, user.ID); err != nil {
			jsonError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		log.Printf("[server] API usage counter reset for org #%d", user.OrgID)
		jsonResp(w, map[string]interface{}{"success": true})
	})

	// GET /api/user/usage — Individual user token usage endpoint
	mux.HandleFunc("/api/user/usage", func(w http.ResponseWriter, r *http.Request) {
		user := getSessionUser(r, database)
		if user == nil && !isLoopbackRequest(r) {
			jsonError(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		var targetUserID int64
		if user != nil {
			targetUserID = user.ID
		}
		summary, err := database.GetUserUsageSummary(targetUserID)
		if err != nil {
			jsonError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		jsonResp(w, summary)
	})

	// POST /api/org/settings — Save Organization Admin Gemini API key in database
	mux.HandleFunc("/api/org/settings", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			jsonError(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		user := getSessionUser(r, database)
		if user == nil {
			jsonError(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		if user.Role != "owner" && user.Role != "admin" {
			jsonError(w, "Forbidden: Only Org Admins or Owners can configure Org API keys", http.StatusForbidden)
			return
		}

		var payload struct {
			GeminiAPIKey string `json:"gemini_api_key"`
			GeminiModel  string `json:"gemini_model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			jsonError(w, "Invalid payload", http.StatusBadRequest)
			return
		}

		if payload.GeminiModel == "" {
			payload.GeminiModel = "models/gemini-3.5-flash-lite"
		}

		if err := database.SetOrgGeminiConfig(user.OrgID, payload.GeminiAPIKey, payload.GeminiModel); err != nil {
			jsonError(w, fmt.Sprintf("Failed to save org key: %v", err), http.StatusInternalServerError)
			return
		}

		log.Printf("[server] updated Org #%d Gemini API key in DB by admin User #%d", user.OrgID, user.ID)
		jsonResp(w, map[string]interface{}{"success": true})
	})

	// POST /api/user/settings — Save Personal Gemini API key for solo accounts
	mux.HandleFunc("/api/user/settings", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			jsonError(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		user := getSessionUser(r, database)
		if user == nil {
			jsonError(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		var payload struct {
			PersonalGeminiAPIKey string `json:"personal_gemini_api_key"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			jsonError(w, "Invalid payload", http.StatusBadRequest)
			return
		}

		if err := database.SetUserPersonalKey(user.ID, payload.PersonalGeminiAPIKey); err != nil {
			jsonError(w, fmt.Sprintf("Failed to save personal key: %v", err), http.StatusInternalServerError)
			return
		}

		log.Printf("[server] updated personal Gemini API key in DB for User #%d", user.ID)
		jsonResp(w, map[string]interface{}{"success": true})
	})

	mailer := email.NewMailer()

	// Helper to set session cookie securely
	setSessionCookie := func(w http.ResponseWriter, userID int64) {
		http.SetCookie(w, &http.Cookie{
			Name:     "mini_session_user_id",
			Value:    fmt.Sprintf("%d", userID),
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
			Expires:  time.Now().Add(30 * 24 * time.Hour),
		})
		if u, err := database.GetUserByID(userID); err == nil && u != nil {
			o, _ := database.GetOrganization(u.OrgID)
			setRecentOAuth(u, o)
		}
	}

	// Helper to resolve redirect URL after OAuth completion
	getRedirectTarget := func(r *http.Request) string {
		if target := r.URL.Query().Get("redirect"); target != "" {
			if strings.HasPrefix(target, "wails://") || target == "/" || target == "/auth-success" {
				return "/auth-success"
			}
			return target
		}
		return "/auth-success"
	}

	// GET /auth-success — Clean confirmation page displayed in external browser post-OAuth
	mux.HandleFunc("/auth-success", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Authentication Successful - Mini Tracker</title>
    <style>
        body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Helvetica, Arial, sans-serif; background: #0a0b12; color: #e2e8f0; display: flex; align-items: center; justify-content: center; height: 100vh; margin: 0; }
        .card { background: #0f1120; border: 1px solid rgba(99, 102, 241, 0.25); border-radius: 20px; padding: 40px 32px; text-align: center; max-width: 400px; box-shadow: 0 20px 50px rgba(0,0,0,0.6); }
        .icon { font-size: 48px; margin-bottom: 16px; display: block; }
        h2 { margin: 0 0 8px; color: #10b981; font-size: 22px; font-weight: 700; }
        p { color: #94a3b8; font-size: 14px; line-height: 1.5; margin: 0 0 24px; }
        .badge { display: inline-block; background: rgba(99, 102, 241, 0.15); color: #818cf8; border: 1px solid rgba(99, 102, 241, 0.3); padding: 8px 18px; border-radius: 99px; font-weight: 600; font-size: 13px; }
    </style>
</head>
<body>
    <div class="card">
        <span class="icon">✨</span>
        <h2>Authentication Successful!</h2>
        <p>You have logged in successfully. You may close this browser tab and return to the Mini Tracker application.</p>
        <div class="badge">Session Active</div>
    </div>
    <script>
        setTimeout(function() { window.close(); }, 3000);
    </script>
</body>
</html>`)
	})

	// Helper to resolve backend callback URL dynamically via env vars (BACKEND_URL, BACKEND_ENDPOINT, APP_URL)
	resolveOAuthCallbackURL := func(r *http.Request, envURIKey, path string) string {
		if uri := os.Getenv(envURIKey); uri != "" {
			return uri
		}
		backendURL := os.Getenv("BACKEND_URL")
		if backendURL == "" {
			backendURL = os.Getenv("BACKEND_ENDPOINT")
		}
		if backendURL == "" {
			backendURL = os.Getenv("APP_URL")
		}
		if backendURL == "" {
			backendURL = fmt.Sprintf("http://%s", r.Host)
		}
		return fmt.Sprintf("%s%s", strings.TrimRight(backendURL, "/"), path)
	}

	// Helper to safely provision or retrieve an OAuth user without crashing/panicking
	provisionOAuthUser := func(orgName, orgSlug, userEmail, userFullName string) (*db.User, error) {
		if userEmail == "" {
			return nil, fmt.Errorf("user email cannot be empty")
		}
		if userFullName == "" {
			userFullName = "OAuth User"
		}

		// 1. Check or create organization safely
		org, err := database.GetOrganizationBySlug(orgSlug)
		if err != nil || org == nil {
			newOrg, createErr := database.CreateOrganization(orgName, orgSlug)
			if createErr != nil {
				// Re-attempt fetch in case org was created concurrently
				org, err = database.GetOrganizationBySlug(orgSlug)
				if err != nil || org == nil {
					return nil, fmt.Errorf("organization lookup/creation failed: %v", createErr)
				}
			} else {
				org = newOrg
			}
		}

		// 2. Check or create user safely
		user, err := database.GetUserByEmail(userEmail)
		if err != nil || user == nil {
			passHash := hashPassword("sso-oauth-account-" + userEmail)
			newUser, createErr := database.CreateUser(org.ID, userEmail, passHash, userFullName, "member")
			if createErr != nil {
				// Re-attempt fetch in case user exists under another organization
				user, err = database.GetUserByEmail(userEmail)
				if err != nil || user == nil {
					return nil, fmt.Errorf("user lookup/creation failed: %v", createErr)
				}
			} else {
				user = newUser
			}
		}

		return user, nil
	}

	// POST /api/org/register — Create a new organization & owner user
	mux.HandleFunc("/api/org/register", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			jsonError(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Name     string `json:"name"`
			Email    string `json:"email"`
			Password string `json:"password"`
			FullName string `json:"full_name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" || req.Email == "" || req.Password == "" {
			jsonError(w, "Invalid request payload. Name, email, and password are required.", http.StatusBadRequest)
			return
		}

		slug := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(req.Name), " ", "-"))
		org, err := database.CreateOrganization(req.Name, slug)
		if err != nil {
			org, _ = database.GetOrganizationBySlug(slug)
			if org == nil {
				jsonError(w, fmt.Sprintf("Organization creation failed: %v", err), http.StatusBadRequest)
				return
			}
		}

		passHash := hashPassword(req.Password)
		fullName := req.FullName
		if fullName == "" {
			fullName = "Admin"
		}
		user, err := database.CreateUser(org.ID, req.Email, passHash, fullName, "owner")
		if err != nil {
			jsonError(w, fmt.Sprintf("User creation failed (email may already exist): %v", err), http.StatusBadRequest)
			return
		}

		token := setJWTTokenCookie(w, user)

		jsonResp(w, map[string]interface{}{
			"success": true,
			"token":   token,
			"org":     org,
			"user":    user,
		})
	})

	// POST /api/auth/login — Authenticate user
	mux.HandleFunc("/api/auth/login", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			jsonError(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Email    string `json:"email"`
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Email == "" || req.Password == "" {
			jsonError(w, "Email and password are required", http.StatusBadRequest)
			return
		}

		user, err := database.GetUserByEmail(req.Email)
		if err != nil || user == nil {
			jsonError(w, "Invalid email or password", http.StatusUnauthorized)
			return
		}

		matched, needsRehash := db.CheckPassword(user.PasswordHash, req.Password)
		if !matched {
			jsonError(w, "Invalid email or password", http.StatusUnauthorized)
			return
		}

		// Transparently upgrade legacy SHA-256 hashes to bcrypt on successful login
		if needsRehash {
			newHash := db.HashPassword(req.Password)
			_ = database.UpdateUserPassword(user.ID, newHash)
			user.PasswordHash = newHash
			log.Printf("[server] upgraded legacy password hash for user #%d to bcrypt", user.ID)
		}

		token := setJWTTokenCookie(w, user)

		org, _ := database.GetOrganization(user.OrgID)
		jsonResp(w, map[string]interface{}{
			"success": true,
			"token":   token,
			"user":    user,
			"org":     org,
		})
	})

	// POST /api/auth/logout — End user session
	mux.HandleFunc("/api/auth/logout", func(w http.ResponseWriter, r *http.Request) {
		clearRecentOAuth()
		http.SetCookie(w, &http.Cookie{
			Name:     "mini_session_jwt",
			Value:    "",
			Path:     "/",
			HttpOnly: true,
			MaxAge:   -1,
			Expires:  time.Unix(0, 0),
		})
		jsonResp(w, map[string]interface{}{"success": true})
	})

	// GET /api/auth/me — Get active session state
	mux.HandleFunc("/api/auth/me", func(w http.ResponseWriter, r *http.Request) {
		user := getSessionUser(r, database)
		if user == nil {
			jsonResp(w, map[string]interface{}{"authenticated": false})
			return
		}

		dbUser, err := database.GetUserByID(user.ID)
		if err != nil || dbUser == nil {
			jsonResp(w, map[string]interface{}{"authenticated": false})
			return
		}

		org, _ := database.GetOrganization(dbUser.OrgID)
		jsonResp(w, map[string]interface{}{
			"authenticated": true,
			"user":          dbUser,
			"org":           org,
		})
	})

	// GET /api/auth/oauth/google & /api/auth/google/login — Google OAuth Single Sign-On Endpoint
	handleGoogleLogin := func(w http.ResponseWriter, r *http.Request) {
		redirectTarget := getRedirectTarget(r)

		clientID := cfg.GoogleClientID
		if clientID == "" {
			clientID = os.Getenv("GOOGLE_CLIENT_ID")
		}
		redirectURI := resolveOAuthCallbackURL(r, "GOOGLE_REDIRECT_URI", "/api/auth/oauth/google/callback")

		// If explicit mock query parameter ?mock=true or ?test=true is supplied, perform local test login bypass
		if os.Getenv("ALLOW_MOCK_OAUTH") == "true" && (r.URL.Query().Get("mock") == "true" || r.URL.Query().Get("test") == "true") {
			emailParam := r.URL.Query().Get("email")
			if emailParam == "" {
				emailParam = "google.user@company.com"
			}
			nameParam := r.URL.Query().Get("name")
			if nameParam == "" {
				nameParam = "Google Workspace User"
			}

			user, err := provisionOAuthUser("Google Workspace Org", "google-org", emailParam, nameParam)
			if err != nil {
				http.Error(w, fmt.Sprintf("Google OAuth Authentication Error: %v", err), http.StatusInternalServerError)
				return
			}

			setSessionCookie(w, user.ID)
			http.Redirect(w, r, redirectTarget, http.StatusFound)
			return
		}

		effectiveClientID := clientID
		if effectiveClientID == "" {
			effectiveClientID = "YOUR_GOOGLE_CLIENT_ID"
		}

		authURL := fmt.Sprintf("https://accounts.google.com/o/oauth2/v2/auth?client_id=%s&redirect_uri=%s&response_type=code&scope=openid%%20email%%20profile&state=%s",
			url.QueryEscape(effectiveClientID),
			url.QueryEscape(redirectURI),
			url.QueryEscape(redirectTarget),
		)

		if r.URL.Query().Get("json") == "true" || strings.Contains(r.Header.Get("Accept"), "application/json") {
			jsonResp(w, map[string]interface{}{
				"provider":                    "google",
				"url":                         authURL,
				"google_client_id_configured": clientID != "",
				"redirect_uri":                redirectURI,
			})
			return
		}

		http.Redirect(w, r, authURL, http.StatusFound)
	}

	mux.HandleFunc("/api/auth/oauth/google", handleGoogleLogin)
	mux.HandleFunc("/api/auth/google/login", handleGoogleLogin)

	// GET /api/auth/oauth/google/callback & /api/auth/google/callback — Google OAuth 2.0 Callback Handler
	handleGoogleCallback := func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		stateTarget := r.URL.Query().Get("state")
		if stateTarget == "" {
			stateTarget = "/"
		}
		if code == "" {
			http.Error(w, "Missing authorization code", http.StatusBadRequest)
			return
		}

		clientID := cfg.GoogleClientID
		if clientID == "" {
			clientID = os.Getenv("GOOGLE_CLIENT_ID")
		}
		clientSecret := cfg.GoogleClientSecret
		if clientSecret == "" {
			clientSecret = os.Getenv("GOOGLE_CLIENT_SECRET")
		}
		redirectURI := resolveOAuthCallbackURL(r, "GOOGLE_REDIRECT_URI", "/api/auth/oauth/google/callback")

		resp, err := http.PostForm("https://oauth2.googleapis.com/token", url.Values{
			"code":          {code},
			"client_id":     {clientID},
			"client_secret": {clientSecret},
			"redirect_uri":  {redirectURI},
			"grant_type":    {"authorization_code"},
		})
		if err != nil || resp.StatusCode != http.StatusOK {
			http.Error(w, "Failed to exchange OAuth code with Google", http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()

		var tokenResp struct {
			AccessToken string `json:"access_token"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil || tokenResp.AccessToken == "" {
			http.Error(w, "Invalid token response from Google", http.StatusBadGateway)
			return
		}

		req, _ := http.NewRequest("GET", "https://www.googleapis.com/oauth2/v2/userinfo", nil)
		req.Header.Set("Authorization", "Bearer "+tokenResp.AccessToken)
		userResp, err := http.DefaultClient.Do(req)
		if err != nil || userResp.StatusCode != http.StatusOK {
			http.Error(w, "Failed to fetch user profile from Google", http.StatusBadGateway)
			return
		}
		defer userResp.Body.Close()

		var profile struct {
			Email string `json:"email"`
			Name  string `json:"name"`
		}
		if err := json.NewDecoder(userResp.Body).Decode(&profile); err != nil || profile.Email == "" {
			http.Error(w, "Invalid user profile response from Google", http.StatusBadGateway)
			return
		}

		user, err := provisionOAuthUser("Google Workspace Org", "google-org", profile.Email, profile.Name)
		if err != nil {
			http.Error(w, fmt.Sprintf("User Provisioning Error: %v", err), http.StatusInternalServerError)
			return
		}

		setSessionCookie(w, user.ID)
		http.Redirect(w, r, stateTarget, http.StatusFound)
	}

	mux.HandleFunc("/api/auth/oauth/google/callback", handleGoogleCallback)
	mux.HandleFunc("/api/auth/google/callback", handleGoogleCallback)

	// GET /api/auth/oauth/azure & /api/auth/azure/login — Microsoft Azure AD / Entra ID SSO Endpoint
	handleAzureLogin := func(w http.ResponseWriter, r *http.Request) {
		redirectTarget := getRedirectTarget(r)

		clientID := cfg.AzureClientID
		if clientID == "" {
			clientID = os.Getenv("AZURE_CLIENT_ID")
		}
		tenantID := cfg.AzureTenantID
		if tenantID == "" {
			tenantID = os.Getenv("AZURE_TENANT_ID")
		}
		if tenantID == "" {
			tenantID = "common"
		}
		redirectURI := resolveOAuthCallbackURL(r, "AZURE_REDIRECT_URI", "/api/auth/oauth/azure/callback")

		// If explicit mock query parameter ?mock=true or ?test=true is supplied, perform local test login bypass
		if os.Getenv("ALLOW_MOCK_OAUTH") == "true" && (r.URL.Query().Get("mock") == "true" || r.URL.Query().Get("test") == "true") {
			emailParam := r.URL.Query().Get("email")
			if emailParam == "" {
				emailParam = "azure.user@company.com"
			}
			nameParam := r.URL.Query().Get("name")
			if nameParam == "" {
				nameParam = "Azure AD User"
			}

			user, err := provisionOAuthUser("Azure Enterprise Org", "azure-org", emailParam, nameParam)
			if err != nil {
				http.Error(w, fmt.Sprintf("Azure OAuth Authentication Error: %v", err), http.StatusInternalServerError)
				return
			}

			setSessionCookie(w, user.ID)
			http.Redirect(w, r, redirectTarget, http.StatusFound)
			return
		}

		effectiveClientID := clientID
		if effectiveClientID == "" {
			effectiveClientID = "YOUR_AZURE_CLIENT_ID"
		}

		authURL := fmt.Sprintf("https://login.microsoftonline.com/%s/oauth2/v2.0/authorize?client_id=%s&response_type=code&redirect_uri=%s&scope=openid%%20email%%20profile%%20User.Read&state=%s",
			tenantID,
			url.QueryEscape(effectiveClientID),
			url.QueryEscape(redirectURI),
			url.QueryEscape(redirectTarget),
		)

		if r.URL.Query().Get("json") == "true" || strings.Contains(r.Header.Get("Accept"), "application/json") {
			jsonResp(w, map[string]interface{}{
				"provider":                   "azure",
				"url":                        authURL,
				"azure_client_id_configured": clientID != "",
				"redirect_uri":               redirectURI,
			})
			return
		}

		http.Redirect(w, r, authURL, http.StatusFound)
	}

	mux.HandleFunc("/api/auth/oauth/azure", handleAzureLogin)
	mux.HandleFunc("/api/auth/azure/login", handleAzureLogin)

	// GET /api/auth/oauth/azure/callback & /api/auth/azure/callback — Azure AD OAuth 2.0 Callback Handler
	handleAzureCallback := func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		stateTarget := r.URL.Query().Get("state")
		if stateTarget == "" {
			stateTarget = "/"
		}
		if code == "" {
			http.Error(w, "Missing authorization code", http.StatusBadRequest)
			return
		}

		clientID := cfg.AzureClientID
		if clientID == "" {
			clientID = os.Getenv("AZURE_CLIENT_ID")
		}
		clientSecret := cfg.AzureClientSecret
		if clientSecret == "" {
			clientSecret = os.Getenv("AZURE_CLIENT_SECRET")
		}
		tenantID := cfg.AzureTenantID
		if tenantID == "" {
			tenantID = os.Getenv("AZURE_TENANT_ID")
		}
		if tenantID == "" {
			tenantID = "common"
		}
		redirectURI := resolveOAuthCallbackURL(r, "AZURE_REDIRECT_URI", "/api/auth/oauth/azure/callback")

		tokenEndpoint := fmt.Sprintf("https://login.microsoftonline.com/%s/oauth2/v2.0/token", tenantID)
		resp, err := http.PostForm(tokenEndpoint, url.Values{
			"code":          {code},
			"client_id":     {clientID},
			"client_secret": {clientSecret},
			"redirect_uri":  {redirectURI},
			"grant_type":    {"authorization_code"},
		})
		if err != nil || resp.StatusCode != http.StatusOK {
			http.Error(w, "Failed to exchange OAuth code with Azure AD", http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()

		var tokenResp struct {
			AccessToken string `json:"access_token"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil || tokenResp.AccessToken == "" {
			http.Error(w, "Invalid token response from Azure AD", http.StatusBadGateway)
			return
		}

		req, _ := http.NewRequest("GET", "https://graph.microsoft.com/v1.0/me", nil)
		req.Header.Set("Authorization", "Bearer "+tokenResp.AccessToken)
		userResp, err := http.DefaultClient.Do(req)
		if err != nil || userResp.StatusCode != http.StatusOK {
			http.Error(w, "Failed to fetch user profile from Azure AD", http.StatusBadGateway)
			return
		}
		defer userResp.Body.Close()

		var profile struct {
			UserPrincipalName string `json:"userPrincipalName"`
			Mail              string `json:"mail"`
			DisplayName       string `json:"displayName"`
		}
		if err := json.NewDecoder(userResp.Body).Decode(&profile); err != nil {
			http.Error(w, "Invalid user profile response from Azure AD", http.StatusBadGateway)
			return
		}

		userEmail := profile.Mail
		if userEmail == "" {
			userEmail = profile.UserPrincipalName
		}
		userName := profile.DisplayName
		if userName == "" {
			userName = "Azure AD User"
		}

		user, err := provisionOAuthUser("Azure Enterprise Org", "azure-org", userEmail, userName)
		if err != nil {
			http.Error(w, fmt.Sprintf("User Provisioning Error: %v", err), http.StatusInternalServerError)
			return
		}

		setSessionCookie(w, user.ID)
		http.Redirect(w, r, stateTarget, http.StatusFound)
	}

	mux.HandleFunc("/api/auth/oauth/azure/callback", handleAzureCallback)
	mux.HandleFunc("/api/auth/azure/callback", handleAzureCallback)

	// GET /api/org/members — Get team roster and pending invitations
	mux.HandleFunc("/api/org/members", func(w http.ResponseWriter, r *http.Request) {
		sessUser := getSessionUser(r, database)
		if sessUser == nil {
			if !isLoopbackRequest(r) {
				jsonError(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			// Unauthenticated fallback: allowed in standalone guest desktop mode for default org 1 (loopback only)
			orgIDStr := r.URL.Query().Get("org_id")
			orgID, _ := strconv.ParseInt(orgIDStr, 10, 64)
			if orgID == 0 {
				orgID = 1
			}
			members, err := database.GetOrgMembers(orgID)
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			invitations, _ := database.GetPendingInvitations(orgID)
			org, _ := database.GetOrganization(orgID)
			jsonResp(w, map[string]interface{}{
				"org":         org,
				"members":     members,
				"invitations": invitations,
			})
			return
		}

		if sessUser.Role == "member" {
			jsonError(w, "Forbidden: Only organization admins can access member roster", http.StatusForbidden)
			return
		}

		orgID := sessUser.OrgID
		members, err := database.GetOrgMembers(orgID)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}

		invitations, _ := database.GetPendingInvitations(orgID)
		org, _ := database.GetOrganization(orgID)

		jsonResp(w, map[string]interface{}{
			"org":         org,
			"members":     members,
			"invitations": invitations,
		})
	})

	// POST /api/org/invite — Invite a new team member
	mux.HandleFunc("/api/org/invite", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		sessUser := getSessionUser(r, database)
		if sessUser == nil && !isLoopbackRequest(r) {
			jsonError(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		if sessUser != nil && sessUser.Role == "member" {
			jsonError(w, "Forbidden: Only organization admins can send invitations", http.StatusForbidden)
			return
		}
		var req struct {
			OrgID int64  `json:"org_id"`
			Email string `json:"email"`
			Role  string `json:"role"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Email == "" {
			http.Error(w, "Missing email address", http.StatusBadRequest)
			return
		}
		if sessUser != nil {
			req.OrgID = sessUser.OrgID
		} else if req.OrgID == 0 {
			req.OrgID = 1
		}
		if req.Role == "" {
			req.Role = "member"
		}

		org, err := database.GetOrganization(req.OrgID)
		if err != nil {
			http.Error(w, "Organization not found", http.StatusNotFound)
			return
		}

		token := generateToken()
		expiresAt := time.Now().Add(48 * time.Hour)

		inv, err := database.CreateInvitation(req.OrgID, req.Email, req.Role, token, expiresAt)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		host := r.Host
		if host == "" {
			host = "localhost:8080"
		}
		inviteURL := fmt.Sprintf("%s://%s/#/accept-invite?token=%s", scheme, host, token)

		emailErr := mailer.SendInviteEmail(req.Email, org.Name, req.Role, inviteURL)
		if emailErr != nil {
			log.Printf("[server] invite email delivery failed for %s: %v", req.Email, emailErr)
		}

		jsonResp(w, map[string]interface{}{
			"success":    true,
			"email_sent": emailErr == nil,
			"invitation": inv,
			"invite_url": inviteURL,
		})
	})

	// GET /api/org/invite-info?token=... — Fetch invite details
	mux.HandleFunc("/api/org/invite-info", func(w http.ResponseWriter, r *http.Request) {
		token := r.URL.Query().Get("token")
		if token == "" {
			http.Error(w, "Missing invitation token", http.StatusBadRequest)
			return
		}

		inv, err := database.GetInvitationByToken(token)
		if err != nil {
			jsonResp(w, map[string]interface{}{"valid": false, "error": "Invalid or expired invitation token"})
			return
		}

		org, _ := database.GetOrganization(inv.OrgID)
		orgName := "Company Organization"
		if org != nil {
			orgName = org.Name
		}

		jsonResp(w, map[string]interface{}{
			"valid":      inv.Status == "pending",
			"status":     inv.Status,
			"email":      inv.Email,
			"role":       inv.Role,
			"org_name":   orgName,
			"expires_at": inv.ExpiresAt,
		})
	})

	// POST /api/org/accept-invite — Onboard user from invite link
	mux.HandleFunc("/api/org/accept-invite", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Token    string `json:"token"`
			FullName string `json:"full_name"`
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Token == "" || req.Password == "" {
			http.Error(w, "Invalid registration details", http.StatusBadRequest)
			return
		}

		passHash := hashPassword(req.Password)
		user, err := database.AcceptInvitation(req.Token, req.FullName, passHash)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// Sign the new member in immediately so no separate login step is needed.
		token := setJWTTokenCookie(w, user)

		org, _ := database.GetOrganization(user.OrgID)
		jsonResp(w, map[string]interface{}{
			"success": true,
			"token":   token,
			"user":    user,
			"org":     org,
		})
	})

	// Serve React Web Dashboard static assets from frontend/dist
	execPath, _ := os.Executable()
	execDir := filepath.Dir(execPath)
	homeDir, _ := os.UserHomeDir()

	candidates := []string{
		"frontend/dist",
		filepath.Join(execDir, "frontend/dist"),
		filepath.Join(execDir, "../frontend/dist"),
		filepath.Join(homeDir, ".local/share/mini-tracker/app/frontend/dist"),
		"/home/reak/git/mini-tracker/frontend/dist",
	}

	var validFrontendDir string
	for _, dir := range candidates {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			validFrontendDir = dir
			break
		}
	}

	if validFrontendDir != "" {
		log.Printf("[server] Serving frontend static assets from: %s", validFrontendDir)
		fileServer := http.FileServer(http.Dir(validFrontendDir))
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/auth-success" {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				fmt.Fprint(w, `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Authentication Successful - Mini Tracker</title>
    <style>
        body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Helvetica, Arial, sans-serif; background: #0a0b12; color: #e2e8f0; display: flex; align-items: center; justify-content: center; height: 100vh; margin: 0; }
        .card { background: #0f1120; border: 1px solid rgba(99, 102, 241, 0.25); border-radius: 20px; padding: 40px 32px; text-align: center; max-width: 400px; box-shadow: 0 20px 50px rgba(0,0,0,0.6); }
        .icon { font-size: 48px; margin-bottom: 16px; display: block; }
        h2 { margin: 0 0 8px; color: #10b981; font-size: 22px; font-weight: 700; }
        p { color: #94a3b8; font-size: 14px; line-height: 1.5; margin: 0 0 24px; }
        .badge { display: inline-block; background: rgba(99, 102, 241, 0.15); color: #818cf8; border: 1px solid rgba(99, 102, 241, 0.3); padding: 8px 18px; border-radius: 99px; font-weight: 600; font-size: 13px; }
    </style>
</head>
<body>
    <div class="card">
        <span class="icon">✨</span>
        <h2>Authentication Successful!</h2>
        <p>You have logged in successfully. You may close this browser tab and return to the Mini Tracker application.</p>
        <div class="badge">Session Active</div>
    </div>
    <script>
        setTimeout(function() { window.close(); }, 3000);
    </script>
</body>
</html>`)
				return
			}
			if strings.HasPrefix(r.URL.Path, "/api/") {
				http.NotFound(w, r)
				return
			}
			path := filepath.Join(validFrontendDir, r.URL.Path)
			info, err := os.Stat(path)
			if os.IsNotExist(err) || info.IsDir() {
				http.ServeFile(w, r, filepath.Join(validFrontendDir, "index.html"))
				return
			}
			fileServer.ServeHTTP(w, r)
		})
	} else {
		log.Printf("[server] WARNING: frontend static directory not found in candidates!")
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("=====================================================")
	log.Printf("📍 Mini Tracker Web Application Running!")
	log.Printf("   Dashboard UI: http://localhost:%s", port)
	log.Printf("=====================================================")

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           gzipMiddleware(cors(mux)),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

func hashPassword(password string) string {
	return db.HashPassword(password)
}

func generateToken() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func jsonResp(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

type gzipResponseWriter struct {
	io.Writer
	http.ResponseWriter
}

func (w gzipResponseWriter) Write(b []byte) (int, error) {
	return w.Writer.Write(b)
}

func gzipMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Content-Encoding", "gzip")
		gz := gzip.NewWriter(w)
		defer gz.Close()
		next.ServeHTTP(gzipResponseWriter{Writer: gz, ResponseWriter: w}, r)
	})
}

func jsonError(w http.ResponseWriter, msg string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": false,
		"error":   msg,
	})
}

func isLoopbackRequest(r *http.Request) bool {
	host := r.RemoteAddr
	if i := strings.LastIndex(host, ":"); i >= 0 {
		host = host[:i]
	}
	host = strings.Trim(host, "[]")
	return host == "localhost" || host == "::1" || strings.HasPrefix(host, "127.")
}

func isLoopbackOrigin(origin string) bool {
	return strings.Contains(origin, "://localhost") || strings.Contains(origin, "://127.") || strings.Contains(origin, "://[::1]")
}

func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && isLoopbackOrigin(origin) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
		} else {
			w.Header().Set("Access-Control-Allow-Origin", "*")
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Bundle scoring: one completed 5-minute capture window becomes one Gemini
// request whose single score is written back to every capture in the window.
const (
	aiBundleWindow     = 5 * time.Minute
	aiMaxBundleItems   = 12
	aiMaxBundlesPerRun = 6
)

// bundleWindowStart returns the local wall-clock 5-minute window start for t.
func bundleWindowStart(t time.Time) time.Time {
	minute := t.Minute() - t.Minute()%int(aiBundleWindow/time.Minute)
	return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), minute, 0, 0, t.Location())
}

// bundleWindowLabel renders a human-readable window description for the prompt.
func bundleWindowLabel(start time.Time) string {
	end := start.Add(aiBundleWindow)
	return fmt.Sprintf("%s %s-%s (5-minute bundle)", start.Format("2006-01-02"), start.Format("15:04"), end.Format("15:04"))
}

// aiBundle is one completed 5-minute window with the captures it contains.
type aiBundle struct {
	start time.Time
	logs  []db.LogEntry
}

// groupCompletedBundles buckets timestamp-ascending entries into 5-minute
// wall-clock windows and returns only fully elapsed windows, so the active
// window stays pending until it is complete.
func groupCompletedBundles(entries []db.LogEntry, now time.Time) []aiBundle {
	var bundles []aiBundle
	index := make(map[string]int)
	for _, entry := range entries {
		start := bundleWindowStart(entry.Timestamp)
		if entry.Timestamp.IsZero() {
			start = bundleWindowStart(now).Add(-aiBundleWindow)
		}
		if !start.Add(aiBundleWindow).Before(now) {
			break
		}
		key := fmt.Sprintf("%s|%d|%d", start.Format(time.RFC3339), entry.OrgID, entry.UserID)
		pos, ok := index[key]
		if !ok {
			pos = len(bundles)
			index[key] = pos
			bundles = append(bundles, aiBundle{start: start})
		}
		bundles[pos].logs = append(bundles[pos].logs, entry)
	}
	// Freshest windows first so a large historical backlog cannot delay recent
	// scores when a run submits only a limited number of requests.
	for i, j := 0, len(bundles)-1; i < j; i, j = i+1, j-1 {
		bundles[i], bundles[j] = bundles[j], bundles[i]
	}
	return bundles
}

func processPendingLogs(database *db.DB, gemini *ai.GeminiClient) int {
	processPendingMu.Lock()
	defer processPendingMu.Unlock()

	if database == nil || gemini == nil || !gemini.HasKey() {
		return 0
	}
	logs, err := database.GetUnanalyzedLogs()
	if err != nil || len(logs) == 0 {
		return 0
	}

	bundles := groupCompletedBundles(logs, time.Now())
	if len(bundles) == 0 {
		return 0
	}

	log.Printf("[server] processing %d pending logs in %d completed 5-minute bundles...", len(logs), len(bundles))
	processed := 0
	requests := 0

bundleLoop:
	for _, bundle := range bundles {
		for i := 0; i < len(bundle.logs); i += aiMaxBundleItems {
			end := i + aiMaxBundleItems
			if end > len(bundle.logs) {
				end = len(bundle.logs)
			}
			chunk := bundle.logs[i:end]

			if requests >= aiMaxBundlesPerRun {
				log.Printf("[server] bundle run budget reached (%d requests) — remaining bundles stay pending", requests)
				break bundleLoop
			}

			batchItems := make([]ai.BatchAnalysisItem, 0, len(chunk))
			for _, entry := range chunk {
				batchItems = append(batchItems, ai.BatchAnalysisItem{
					LogID:        entry.ID,
					WindowTitle:  entry.WindowTitle,
					EntropyScore: entry.EntropyScore,
					ImagePath:    entry.ImagePath,
					TotalKeys:    entry.TotalKeys,
					TotalClicks:  entry.TotalClicks,
					AppName:      entry.AppName,
					Timestamp:    entry.Timestamp,
				})
			}

			if ai.IsClearlyIdleBundle(batchItems) {
				for _, entry := range chunk {
					if err := database.UpdateLogAnalysis(entry.ID, "Idle", "", "Idle", entry.WindowTitle, 0, "", false, 5, 0.9, "no active window content"); err == nil {
						processed++
					}
				}
				log.Printf("[server] bundle skipped (clearly idle): %s, %d logs scored locally", bundleWindowLabel(bundle.start), len(chunk))
				continue
			}

			requests++
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			result, usage, bundleErr := gemini.AnalyzeSessionBundle(ctx, batchItems, bundleWindowLabel(bundle.start))
			cancel()

			if bundleErr != nil {
				log.Printf("[server] bundle AI analysis error for %d logs: %v — incrementing retry counts", len(chunk), bundleErr)
				for _, entry := range chunk {
					_ = database.IncrementAIRetryCount(entry.ID)
				}
				continue
			}

			ai.ApplyBundleActivityGuard(result, batchItems, nil)
			for _, entry := range chunk {
				if err := database.UpdateLogAnalysis(entry.ID, result.Category, result.AppName, result.AppCategory, entry.WindowTitle, 0, "", result.Score >= 50, result.Score, result.Confidence, result.Reason); err == nil {
					processed++
				}
			}
			if usage.TotalTokenCount > 0 {
				_ = database.RecordAPIUsage(0, 0, "global_system", usage.PromptTokenCount, usage.CandidatesTokenCount, usage.TotalTokenCount, gemini.GetModel())
			}

			time.Sleep(1 * time.Second)
		}
	}

	log.Printf("[server] completed processing %d/%d pending logs in %d bundle requests", processed, len(logs), requests)
	return processed
}

var processPendingMu sync.Mutex

func encodingBase64(data []byte) string {
	return base64.StdEncoding.EncodeToString(data)
}

var jwtSecret = loadJWTSecret()

// loadJWTSecret returns the JWT signing key: JWT_SECRET env var when set,
// otherwise an unpredictable per-process key (sessions invalidate on restart).
func loadJWTSecret() []byte {
	if s := os.Getenv("JWT_SECRET"); s != "" {
		return []byte(s)
	}
	host, _ := os.Hostname()
	seed := fmt.Sprintf("get-hike-jwt-%s-%d", host, time.Now().UnixNano())
	sum := sha256.Sum256([]byte(seed))
	log.Printf("[server] WARNING: JWT_SECRET not set — using an ephemeral signing key; web sessions will expire on server restart")
	return sum[:]
}

type JWTClaims struct {
	UserID int64  `json:"user_id"`
	Email  string `json:"email"`
	Role   string `json:"role"`
	OrgID  int64  `json:"org_id"`
	jwt.RegisteredClaims
}

func generateJWT(user *db.User) string {
	now := time.Now()
	claims := JWTClaims{
		UserID: user.ID,
		Email:  user.Email,
		Role:   user.Role,
		OrgID:  user.OrgID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(7 * 24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			Subject:   strconv.FormatInt(user.ID, 10),
			Issuer:    "get-hike-tracker",
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenStr, err := token.SignedString(jwtSecret)
	if err != nil {
		log.Printf("[server] error signing JWT: %v", err)
		return ""
	}
	return tokenStr
}

func parseJWT(tokenStr string) *JWTClaims {
	if strings.TrimSpace(tokenStr) == "" {
		return nil
	}
	var claims JWTClaims
	token, err := jwt.ParseWithClaims(tokenStr, &claims, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return jwtSecret, nil
	}, jwt.WithLeeway(5*time.Second))

	if err != nil || !token.Valid {
		return nil
	}

	return &claims
}

func setJWTTokenCookie(w http.ResponseWriter, user *db.User) string {
	jwtToken := generateJWT(user)
	http.SetCookie(w, &http.Cookie{
		Name:     "mini_session_jwt",
		Value:    jwtToken,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   7 * 24 * 3600,
	})
	return jwtToken
}

func getSessionUser(r *http.Request, database *db.DB) *db.User {
	var tokenStr string

	if cookie, err := r.Cookie("mini_session_jwt"); err == nil && cookie.Value != "" {
		tokenStr = cookie.Value
	}
	if tokenStr == "" {
		authHeader := r.Header.Get("Authorization")
		if strings.HasPrefix(authHeader, "Bearer ") {
			tokenStr = strings.TrimPrefix(authHeader, "Bearer ")
		}
	}

	if tokenStr != "" {
		claims := parseJWT(tokenStr)
		if claims != nil {
			return &db.User{
				ID:    claims.UserID,
				Email: claims.Email,
				Role:  claims.Role,
				OrgID: claims.OrgID,
			}
		}
	}

	return nil
}

var (
	recentOAuthUser  *db.User
	recentOAuthOrg   *db.Organization
	recentOAuthTime  time.Time
	recentOAuthMutex sync.RWMutex
)

func setRecentOAuth(u *db.User, o *db.Organization) {
	recentOAuthMutex.Lock()
	defer recentOAuthMutex.Unlock()
	recentOAuthUser = u
	recentOAuthOrg = o
	recentOAuthTime = time.Now()
}

func getRecentOAuth() (*db.User, *db.Organization) {
	recentOAuthMutex.RLock()
	defer recentOAuthMutex.RUnlock()
	if recentOAuthUser != nil && time.Since(recentOAuthTime) < 5*time.Minute {
		return recentOAuthUser, recentOAuthOrg
	}
	return nil, nil
}

func clearRecentOAuth() {
	recentOAuthMutex.Lock()
	defer recentOAuthMutex.Unlock()
	recentOAuthUser = nil
	recentOAuthOrg = nil
}

// wizardHTML — floating tracker window served at GET /wizard.
// Opened via window.open() from the main app — uses the same dark indigo/teal
// color palette as the desktop app (style.css tokens).
const wizardHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>get-Hike · Time Tracker</title>
<link rel="preconnect" href="https://fonts.googleapis.com">
<link href="https://fonts.googleapis.com/css2?family=Inter:wght@400;500;600;700&family=JetBrains+Mono:wght@400;500&display=swap" rel="stylesheet">
<style>
  *, *::before, *::after { box-sizing: border-box; margin: 0; padding: 0; }

  :root {
    --bg-base:       #06120e;
    --bg-surface:    #0b1c17;
    --bg-card:       #112720;
    --bg-elevated:   #18352c;
    --border-subtle: rgba(16,185,129,0.14);
    --border-medium: rgba(16,185,129,0.28);
    --border-glow:   rgba(16,185,129,0.45);
    --accent:        #10b981;
    --accent2:       #059669;
    --teal:          #34d399;
    --green:         #10b981;
    --red:           #ef4444;
    --amber:         #f59e0b;
    --text-primary:  #f0fdf4;
    --text-secondary:#a7f3d0;
    --text-muted:    #6ee7b7;
    --mono:          'JetBrains Mono', monospace;
  }

  html, body {
    width: 300px;
    background: var(--bg-base);
    font-family: 'Inter', system-ui, sans-serif;
    font-size: 13px;
    color: var(--text-primary);
    overflow: hidden;
    user-select: none;
    -webkit-font-smoothing: antialiased;
  }

  /* ── Window chrome ─────────────────────────── */
  #titlebar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 0 12px;
    height: 38px;
    background: var(--bg-surface);
    border-bottom: 1px solid var(--border-subtle);
    cursor: grab;
  }
  #titlebar:active { cursor: grabbing; }
  .tb-dots { display: flex; gap: 6px; }
  .tb-dot {
    width: 11px; height: 11px; border-radius: 50%; cursor: pointer;
    transition: filter 0.15s;
  }
  .tb-dot:hover { filter: brightness(1.2); }
  .tb-dot.close  { background: #ff5f57; }
  .tb-dot.min    { background: #febc2e; }
  .tb-dot.zoom   { background: #28c840; }
  .tb-brand {
    font-size: 11px; font-weight: 600; color: var(--text-muted);
    letter-spacing: 0.04em; text-transform: uppercase;
  }
  .tb-spacer { width: 52px; }

  /* ── Project strip ─────────────────────────── */
  #project-strip {
    padding: 10px 14px;
    background: var(--bg-card);
    border-bottom: 1px solid var(--border-subtle);
  }
  #project-name {
    font-size: 13px; font-weight: 700;
    color: var(--accent);
    margin-bottom: 2px;
  }
  #org-name { font-size: 11px; color: var(--text-muted); }

  /* ── Session timer ─────────────────────────── */
  #session {
    padding: 14px 14px 12px;
    border-bottom: 1px solid var(--border-subtle);
    background: var(--bg-surface);
  }
  #session-top {
    display: flex; justify-content: space-between; align-items: center; margin-bottom: 4px;
  }
  #session-lbl { font-size: 10px; color: var(--text-muted); text-transform: uppercase; letter-spacing: 0.07em; }
  #online-pill { font-size: 10px; font-weight: 600; display: flex; align-items: center; gap: 4px; }
  #online-dot  { width: 6px; height: 6px; border-radius: 50%; transition: background 0.3s, box-shadow 0.3s; }
  #clock {
    font-family: var(--mono);
    font-size: 32px; font-weight: 700; color: var(--text-primary);
    letter-spacing: -0.5px; line-height: 1; margin-bottom: 12px;
    transition: color 0.3s;
  }
  #clock.running { color: var(--teal); }

  /* ON/OFF toggle row */
  #toggle-row { display: flex; align-items: center; justify-content: space-between; }
  #toggle-hint { font-size: 11px; color: var(--text-muted); }
  /* pill switch */
  .pill-switch { position: relative; width: 46px; height: 24px; cursor: pointer; }
  .pill-switch input { opacity: 0; width: 0; height: 0; }
  .pill-track {
    position: absolute; inset: 0; border-radius: 12px;
    background: var(--bg-elevated); border: 1px solid var(--border-medium);
    transition: background 0.2s, border-color 0.2s;
  }
  .pill-thumb {
    position: absolute; top: 3px; left: 3px;
    width: 18px; height: 18px; border-radius: 50%;
    background: var(--text-muted);
    transition: transform 0.2s, background 0.2s;
    box-shadow: 0 1px 4px rgba(0,0,0,0.4);
  }

  /* ── Stats grid ────────────────────────────── */
  #stats {
    display: grid; grid-template-columns: 1fr 1fr;
    gap: 1px; background: var(--border-subtle);
    border-bottom: 1px solid var(--border-subtle);
  }
  .stat {
    background: var(--bg-card); padding: 10px 13px;
  }
  .stat-lbl {
    font-size: 9px; text-transform: uppercase; letter-spacing: 0.07em;
    color: var(--text-muted); margin-bottom: 3px; font-weight: 600;
  }
  .stat-val { font-size: 15px; font-weight: 700; color: var(--text-primary); }

  /* ── Memo ──────────────────────────────────── */
  #memo-wrap {
    padding: 10px 13px; border-bottom: 1px solid var(--border-subtle);
    background: var(--bg-surface);
  }
  #memo {
    width: 100%; background: var(--bg-card); border: 1px solid var(--border-subtle);
    border-radius: 8px; color: var(--text-primary); font-size: 12px;
    font-family: 'Inter', sans-serif; padding: 7px 10px; resize: none; outline: none;
    transition: border-color 0.15s; box-sizing: border-box;
  }
  #memo:focus { border-color: var(--border-glow); }
  #memo::placeholder { color: var(--text-muted); }

  /* ── Activity bars ─────────────────────────── */
  #activity { padding: 10px 13px 12px; background: var(--bg-surface); }
  #act-hdr  { display: flex; justify-content: space-between; margin-bottom: 7px; }
  #act-lbl  { font-size: 10px; color: var(--text-muted); text-transform: uppercase; letter-spacing: 0.06em; }
  #act-pct  { font-size: 11px; font-weight: 700; }
  #bars     { display: flex; gap: 3px; height: 20px; align-items: flex-end; }
  .bar      { flex: 1; border-radius: 2px 2px 0 0; transition: background 0.4s; }

  /* progress line */
  #prog-wrap {
    height: 3px; background: var(--border-subtle);
    margin: 0 13px; border-radius: 99px; overflow: hidden;
  }
  #prog-bar {
    height: 100%; border-radius: 99px;
    background: var(--accent);
    transition: width 0.8s ease;
  }

  /* ── Footer ────────────────────────────────── */
  #footer {
    padding: 8px 13px;
    display: flex; align-items: center; justify-content: space-between;
    background: var(--bg-base);
    border-top: 1px solid var(--border-subtle);
  }
  #footer-brand {
    font-size: 11px; font-weight: 600;
    color: var(--accent);
  }
  .foot-btn {
    background: none; border: none; color: var(--text-muted); cursor: pointer;
    font-size: 14px; padding: 2px 4px; transition: color 0.15s;
  }
  .foot-btn:hover { color: var(--text-primary); }

  @keyframes slideIn {
    from { opacity:0; transform: scale(0.97) translateY(6px); }
    to   { opacity:1; transform: scale(1)    translateY(0); }
  }
  body { animation: slideIn 0.22s cubic-bezier(0.16,1,0.3,1); }
</style>
</head>
<body>

<!-- Title bar (drag handle) -->
<div id="titlebar">
  <div class="tb-dots">
    <div class="tb-dot close"  id="btn-close" title="Close"></div>
    <div class="tb-dot min"    title="Minimize (click window controls)"></div>
    <div class="tb-dot zoom"   title="Maximize"></div>
  </div>
  <span class="tb-brand">⏱ Time Tracker</span>
  <span class="tb-spacer"></span>
</div>

<!-- Project -->
<div id="project-strip">
  <div id="project-name">get-Hike Productivity</div>
  <div id="org-name">Your Organization</div>
</div>

<!-- Session -->
<div id="session">
  <div id="session-top">
    <span id="session-lbl">Current Session</span>
    <span id="online-pill">
      <span id="online-dot"></span>
      <span id="online-text">Offline</span>
    </span>
  </div>
  <div id="clock">00:00:00</div>
  <div id="toggle-row">
    <span id="toggle-hint">Tracker paused</span>
    <label class="pill-switch" title="Toggle tracking">
      <input type="checkbox" id="toggle-input">
      <div class="pill-track" id="pill-track"></div>
      <div class="pill-thumb" id="pill-thumb"></div>
    </label>
  </div>
</div>

<!-- Stats -->
<div id="stats">
  <div class="stat">
    <div class="stat-lbl">Today</div>
    <div class="stat-val" id="sv-today">—</div>
  </div>
  <div class="stat">
    <div class="stat-lbl">Productive</div>
    <div class="stat-val" id="sv-prod">—</div>
  </div>
  <div class="stat">
    <div class="stat-lbl">Score</div>
    <div class="stat-val" id="sv-score">—</div>
  </div>
  <div class="stat">
    <div class="stat-lbl">Unproductive</div>
    <div class="stat-val" id="sv-unprod">—</div>
  </div>
</div>

<!-- Memo -->
<div id="memo-wrap">
  <textarea id="memo" rows="2" placeholder="What are you working on?"></textarea>
</div>

<!-- Activity -->
<div id="activity">
  <div id="act-hdr">
    <span id="act-lbl">Activity Level</span>
    <span id="act-pct" style="color:var(--text-muted)">—</span>
  </div>
  <div id="bars"></div>
</div>

<!-- Progress line -->
<div id="prog-wrap"><div id="prog-bar" style="width:0%"></div></div>

<!-- Footer -->
<div id="footer">
  <span id="footer-brand">✦ get-Hike</span>
  <div style="display:flex;gap:4px">
    <button class="foot-btn" id="btn-refresh" title="Refresh">↻</button>
    <button class="foot-btn" id="btn-close2"  title="Close">✕</button>
  </div>
</div>

<script>
(function () {
  let isActive = false, elapsed = 0;

  const clock     = document.getElementById('clock');
  const hint      = document.getElementById('toggle-hint');
  const oDot      = document.getElementById('online-dot');
  const oText     = document.getElementById('online-text');
  const actPct    = document.getElementById('act-pct');
  const barsEl    = document.getElementById('bars');
  const progBar   = document.getElementById('prog-bar');
  const pillTrack = document.getElementById('pill-track');
  const pillThumb = document.getElementById('pill-thumb');
  const togInput  = document.getElementById('toggle-input');

  /* helpers */
  const pad    = n => String(n).padStart(2,'0');
  const fmtClk = s => pad(Math.floor(s/3600))+':'+pad(Math.floor((s%3600)/60))+':'+pad(s%60);
  const fmtMin = m => { const h=Math.floor(m/60),mm=m%60; return h?h+'h '+(mm?mm+'m':''):mm+'m'; };
  const colFor = s => s>=70?'#2dd4bf':s>=40?'#f59e0b':'#ef4444';

  function applyStatus(active) {
    isActive = active;
    clock.className = active ? 'running' : '';
    oDot.style.background  = active ? '#10b981' : '#64748b';
    oDot.style.boxShadow   = active ? '0 0 7px #10b981' : 'none';
    oText.textContent      = active ? 'Online' : 'Offline';
    hint.textContent       = active ? 'Tracking active' : 'Tracker paused';
    togInput.checked       = active;
    pillTrack.style.background   = active ? 'rgba(99,102,241,0.25)' : '';
    pillTrack.style.borderColor  = active ? '#6366f1' : '';
    pillThumb.style.background   = active ? '#6366f1' : '';
    pillThumb.style.transform    = active ? 'translateX(22px)' : 'translateX(0)';
  }

  function renderBars(score) {
    const SEGS=10, fill=Math.round(score/100*SEGS), col=colFor(score);
    barsEl.innerHTML='';
    for(let i=0;i<SEGS;i++){
      const d=document.createElement('div'); d.className='bar';
      const h=i<fill?(40+Math.round(i/SEGS*60)):20;
      d.style.height=h+'%';
      d.style.background=i<fill?col:'rgba(99,102,241,0.12)';
      barsEl.appendChild(d);
    }
    actPct.textContent=score+'%'; actPct.style.color=col;
    progBar.style.width=Math.min(score,100)+'%';
    document.getElementById('sv-score').style.color=col;
    document.getElementById('sv-score').textContent=score+'%';
    document.getElementById('sv-prod').style.color=col;
  }

  const authHdr=()=>{const t=localStorage.getItem('mini_jwt_token');return t?{'Authorization':'Bearer '+t}:{};};

  async function fetchStatus(){
    try{
      const r=await fetch('/api/tracker/status',{headers:authHdr()});
      if(!r.ok) throw 0;
      const d=await r.json();
      elapsed=d.elapsed_seconds||0;
      clock.textContent=fmtClk(elapsed);
      applyStatus(!!d.active);
    }catch{
      oText.textContent='Offline'; oDot.style.background='#ef4444';
    }
  }

  async function fetchStats(){
    try{
      const today=new Date().toLocaleDateString('en-CA');
      const r=await fetch('/api/stats?date='+today,{headers:authHdr()});
      if(!r.ok) return;
      const d=await r.json();
      const prod=d.productive_minutes||0, unprod=d.unproductive_minutes||0;
      const total=d.total_minutes||(prod+unprod);
      const score=total>0?Math.round(prod/total*100):0;
      document.getElementById('sv-today').textContent=fmtMin(total);
      document.getElementById('sv-prod').textContent=fmtMin(prod);
      document.getElementById('sv-unprod').textContent=unprod>0?fmtMin(unprod):'—';
      renderBars(score);
    }catch{}
  }

  async function doToggle(){
    try{
      const r=await fetch('/api/tracker/toggle',{method:'POST',headers:authHdr()});
      if(!r.ok) return;
      const d=await r.json();
      elapsed=d.elapsed_seconds||elapsed;
      applyStatus(!!d.active);
    }catch{}
  }

  /* init */
  const urlParams = new URLSearchParams(window.location.search);
  const urlToken = urlParams.get('token');
  if (urlToken) {
    localStorage.setItem('mini_jwt_token', urlToken);
    window.history.replaceState({}, document.title, "/wizard");
  }
  
  renderBars(0);
  fetchStatus(); fetchStats();
  setInterval(()=>{if(isActive){elapsed++;clock.textContent=fmtClk(elapsed);}},1000);
  setInterval(fetchStatus,10000);
  setInterval(fetchStats,60000);

  /* events */
  togInput.addEventListener('change', doToggle);
  document.getElementById('btn-close').addEventListener('click',  ()=>window.close());
  document.getElementById('btn-close2').addEventListener('click', ()=>window.close());
  document.getElementById('btn-refresh').addEventListener('click', ()=>{
    fetchStatus(); fetchStats();
    const b=document.getElementById('btn-refresh');
    b.textContent='✓'; setTimeout(()=>b.textContent='↻',800);
  });

  /* drag (works in window.open popup) */
  let drag=false,ox=0,oy=0;
  document.getElementById('titlebar').addEventListener('mousedown',e=>{
    if(e.target.closest('.tb-dot')) return;
    drag=true; ox=e.screenX; oy=e.screenY;
  });
  document.addEventListener('mousemove',e=>{
    if(!drag) return;
    try{window.moveTo(window.screenX+e.screenX-ox,window.screenY+e.screenY-oy);}catch{}
    ox=e.screenX; oy=e.screenY;
  });
  document.addEventListener('mouseup',()=>drag=false);
})();
</script>
</body>
</html>`
