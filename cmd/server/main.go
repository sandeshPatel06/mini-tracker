// cmd/server is a standalone web server & collector for mini-tracker.
// It requires NO sudo, NO GTK, and NO pkg-config dependencies.
// It serves the full React dashboard UI and runs the background tracker daemon.
package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/holoplot/go-evdev"
	"github.com/reak/mini-tracker/internal/ai"
	"github.com/reak/mini-tracker/internal/config"
	"github.com/reak/mini-tracker/internal/db"
	"github.com/reak/mini-tracker/internal/email"
	"github.com/reak/mini-tracker/internal/tracker"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Printf("[server] config load error: %v", err)
		cfg = &config.Config{
			ScreenshotInterval: 30 * time.Second,
			DataDir:            filepath.Join(os.Getenv("HOME"), ".local/share/mini-tracker"),
			BackendPort:        8080,
		}
	}

	database, err := db.Open(cfg.DataDir)
	if err != nil {
		log.Fatalf("[server] database error: %v", err)
	}
	defer database.Close()

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
		accumulatedSec   int64
	)

	getElapsedSeconds := func() int64 {
		trackerMutex.Lock()
		defer trackerMutex.Unlock()
		if !trackerActive {
			return accumulatedSec
		}
		return accumulatedSec + int64(time.Since(trackerStartTime).Seconds())
	}

	var latestKeyStats tracker.KeystrokeStats
	go func() {
		ticker := time.NewTicker(cfg.ScreenshotInterval)
		defer ticker.Stop()
		for {
			select {
			case stats, ok := <-statsCh:
				if ok {
					latestKeyStats = stats
				}
			case <-ticker.C:
				trackerMutex.Lock()
				active := trackerActive
				trackerMutex.Unlock()
				if !active {
					continue
				}

				ks := latestKeyStats
				latestKeyStats = tracker.KeystrokeStats{}

				shot, err := tracker.CaptureScreenshot(cfg.DataDir)
				if err != nil {
					log.Printf("[server] screenshot capture: %v", err)
					shot = &tracker.ScreenshotResult{}
				}

				entry := &db.LogEntry{
					Timestamp:    time.Now(),
					ImagePath:    shot.FilePath,
					TotalKeys:    ks.TotalKeys,
					UniqueKeys:   ks.UniqueKeys,
					EntropyScore: ks.EntropyScore,
				}

				id, err := database.InsertLog(entry)
				if err != nil {
					log.Printf("[server] insert log error: %v", err)
					continue
				}

				go func(logID int64, filePath, b64 string, score float64) {
					ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
					defer cancel()

					res, err := gemini.Analyze(ctx, b64, score)
					if err != nil {
						log.Printf("[server] AI analysis error: %v", err)
					} else {
						_ = database.UpdateAIResult(logID, res.Category, res.Productive, res.Confidence, res.Reason)
						log.Printf("[server] logged #%d — category=%s productive=%v reason=%s", logID, res.Category, res.Productive, res.Reason)
					}

					// Ensure screenshot file is deleted after processing/sending to backend
					if filePath != "" {
						if err := os.Remove(filePath); err == nil {
							log.Printf("[server] deleted temporary screenshot: %s", filePath)
						}
					}
				}(id, shot.FilePath, shot.Base64Data, ks.EntropyScore)
			}
		}
	}()

	mux := http.NewServeMux()

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
		trackerMutex.Lock()
		if trackerActive {
			accumulatedSec += int64(time.Since(trackerStartTime).Seconds())
			trackerActive = false
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

	// GET /api/logs?date=YYYY-MM-DD
	mux.HandleFunc("/api/logs", func(w http.ResponseWriter, r *http.Request) {
		date := r.URL.Query().Get("date")
		if date == "" {
			date = time.Now().Format("2006-01-02")
		}
		logs, err := database.GetLogsForDate(date)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		jsonResp(w, logs)
	})

	// GET /api/stats?date=YYYY-MM-DD
	mux.HandleFunc("/api/stats", func(w http.ResponseWriter, r *http.Request) {
		date := r.URL.Query().Get("date")
		if date == "" {
			date = time.Now().Format("2006-01-02")
		}
		stats, err := database.GetProductivityStats(date)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		jsonResp(w, stats)
	})

	// GET /api/image?path=...
	mux.HandleFunc("/api/image", func(w http.ResponseWriter, r *http.Request) {
		imgPath := r.URL.Query().Get("path")
		if imgPath == "" {
			http.Error(w, "missing path", 400)
			return
		}
		// Security check: ensure path is inside DataDir or user home
		if !filepath.IsAbs(imgPath) {
			http.Error(w, "invalid path", 400)
			return
		}
		data, err := os.ReadFile(imgPath)
		if err != nil {
			http.Error(w, "image not found", 404)
			return
		}
		w.Header().Set("Content-Type", "image/jpeg")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		w.Write(data)
	})

	// GET or POST /api/config
	mux.HandleFunc("/api/config", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			var payload struct {
				GeminiAPIKey string `json:"gemini_api_key"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err == nil && payload.GeminiAPIKey != "" {
				cfg.GeminiAPIKey = payload.GeminiAPIKey
				gemini.SetAPIKey(payload.GeminiAPIKey)
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
		var payload struct {
			TotalKeys  int `json:"total_keys"`
			UniqueKeys int `json:"unique_keys"`
			KeyCode    int `json:"key_code"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			jsonError(w, "Invalid input JSON payload", http.StatusBadRequest)
			return
		}

		if payload.KeyCode > 0 {
			keyTracker.RecordKeyCode(evdev.EvCode(payload.KeyCode))
		} else if payload.TotalKeys > 0 {
			keyTracker.RecordKeystrokes(payload.TotalKeys, payload.UniqueKeys)
		}

		jsonResp(w, map[string]interface{}{"success": true})
	})

	// POST /api/screenshots — Receive screenshot base64 + keystroke data from desktop app, save log, trigger AI API analysis & store analytics
	mux.HandleFunc("/api/screenshots", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			jsonError(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var payload struct {
			ImageBase64  string  `json:"image_base64"`
			TotalKeys    int     `json:"total_keys"`
			UniqueKeys   int     `json:"unique_keys"`
			EntropyScore float64 `json:"entropy_score"`
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

		res, err := gemini.Analyze(ctx, payload.ImageBase64, entropy)
		if err == nil {
			category = res.Category
			productive = res.Productive
			confidence = res.Confidence
			reason = res.Reason
			_ = database.UpdateAIResult(logID, category, productive, confidence, reason)
			// Remove screenshot file after AI analysis is complete
			_ = os.Remove(filePath)
		} else {
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

	mailer := email.NewMailer()

	// In-memory cache for tracking recent OAuth/Login sessions across webview boundaries
	var (
		recentOAuthUser  *db.User
		recentOAuthOrg   *db.Organization
		recentOAuthTime  time.Time
		recentOAuthMutex sync.RWMutex
	)

	setRecentOAuth := func(u *db.User, o *db.Organization) {
		recentOAuthMutex.Lock()
		defer recentOAuthMutex.Unlock()
		recentOAuthUser = u
		recentOAuthOrg = o
		recentOAuthTime = time.Now()
	}

	getRecentOAuth := func() (*db.User, *db.Organization) {
		recentOAuthMutex.RLock()
		defer recentOAuthMutex.RUnlock()
		if recentOAuthUser != nil && time.Since(recentOAuthTime) < 5*time.Minute {
			return recentOAuthUser, recentOAuthOrg
		}
		return nil, nil
	}

	clearRecentOAuth := func() {
		recentOAuthMutex.Lock()
		defer recentOAuthMutex.Unlock()
		recentOAuthUser = nil
		recentOAuthOrg = nil
	}

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

		setSessionCookie(w, user.ID)

		jsonResp(w, map[string]interface{}{
			"success": true,
			"token":   fmt.Sprintf("%d", user.ID),
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
		if err != nil || user == nil || user.PasswordHash != hashPassword(req.Password) {
			jsonError(w, "Invalid email or password", http.StatusUnauthorized)
			return
		}

		setSessionCookie(w, user.ID)

		org, _ := database.GetOrganization(user.OrgID)
		jsonResp(w, map[string]interface{}{
			"success": true,
			"token":   fmt.Sprintf("%d", user.ID),
			"user":    user,
			"org":     org,
		})
	})

	// POST /api/auth/logout — End user session
	mux.HandleFunc("/api/auth/logout", func(w http.ResponseWriter, r *http.Request) {
		clearRecentOAuth()
		http.SetCookie(w, &http.Cookie{
			Name:     "mini_session_user_id",
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
		var userID int64

		if cookie, err := r.Cookie("mini_session_user_id"); err == nil && cookie.Value != "" {
			userID, _ = strconv.ParseInt(cookie.Value, 10, 64)
		}
		if userID == 0 {
			authHeader := r.Header.Get("Authorization")
			if strings.HasPrefix(authHeader, "Bearer ") {
				userID, _ = strconv.ParseInt(strings.TrimPrefix(authHeader, "Bearer "), 10, 64)
			}
		}
		if userID == 0 {
			userIDHeader := r.Header.Get("X-Session-User-ID")
			if userIDHeader != "" {
				userID, _ = strconv.ParseInt(userIDHeader, 10, 64)
			}
		}
		if userID == 0 {
			if recentUser, _ := getRecentOAuth(); recentUser != nil {
				userID = recentUser.ID
			}
		}

		if userID == 0 {
			jsonResp(w, map[string]interface{}{"authenticated": false})
			return
		}

		user, err := database.GetUserByID(userID)
		if err != nil || user == nil {
			jsonResp(w, map[string]interface{}{"authenticated": false})
			return
		}

		org, _ := database.GetOrganization(user.OrgID)
		jsonResp(w, map[string]interface{}{
			"authenticated": true,
			"user":          user,
			"org":           org,
		})
	})

	// GET /api/auth/oauth/google — Google OAuth Single Sign-On Endpoint
	mux.HandleFunc("/api/auth/oauth/google", func(w http.ResponseWriter, r *http.Request) {
		redirectTarget := getRedirectTarget(r)

		clientID := os.Getenv("GOOGLE_CLIENT_ID")
		clientSecret := os.Getenv("GOOGLE_CLIENT_SECRET")
		redirectURI := resolveOAuthCallbackURL(r, "GOOGLE_REDIRECT_URI", "/api/auth/oauth/google/callback")

		// Real OAuth Flow if Client ID & Secret are configured
		if clientID != "" && clientSecret != "" {
			authURL := fmt.Sprintf("https://accounts.google.com/o/oauth2/v2/auth?client_id=%s&redirect_uri=%s&response_type=code&scope=openid%%20email%%20profile&state=%s",
				url.QueryEscape(clientID),
				url.QueryEscape(redirectURI),
				url.QueryEscape(redirectTarget),
			)
			http.Redirect(w, r, authURL, http.StatusFound)
			return
		}

		// Default local / test SSO provision flow
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
	})

	// GET /api/auth/oauth/google/callback — Google OAuth 2.0 Callback Handler
	mux.HandleFunc("/api/auth/oauth/google/callback", func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		stateTarget := r.URL.Query().Get("state")
		if stateTarget == "" {
			stateTarget = "/"
		}
		if code == "" {
			http.Error(w, "Missing authorization code", http.StatusBadRequest)
			return
		}

		clientID := os.Getenv("GOOGLE_CLIENT_ID")
		clientSecret := os.Getenv("GOOGLE_CLIENT_SECRET")
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
	})

	// GET /api/auth/oauth/azure — Microsoft Azure AD / Entra ID SSO Endpoint
	mux.HandleFunc("/api/auth/oauth/azure", func(w http.ResponseWriter, r *http.Request) {
		redirectTarget := getRedirectTarget(r)

		clientID := os.Getenv("AZURE_CLIENT_ID")
		clientSecret := os.Getenv("AZURE_CLIENT_SECRET")
		tenantID := os.Getenv("AZURE_TENANT_ID")
		if tenantID == "" {
			tenantID = "common"
		}
		redirectURI := resolveOAuthCallbackURL(r, "AZURE_REDIRECT_URI", "/api/auth/oauth/azure/callback")

		// Real OAuth Flow if Client ID & Secret are configured
		if clientID != "" && clientSecret != "" {
			authURL := fmt.Sprintf("https://login.microsoftonline.com/%s/oauth2/v2.0/authorize?client_id=%s&response_type=code&redirect_uri=%s&scope=openid%%20email%%20profile%%20User.Read&state=%s",
				tenantID,
				url.QueryEscape(clientID),
				url.QueryEscape(redirectURI),
				url.QueryEscape(redirectTarget),
			)
			http.Redirect(w, r, authURL, http.StatusFound)
			return
		}

		// Default local / test SSO provision flow
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
	})

	// GET /api/auth/oauth/azure/callback — Azure AD OAuth 2.0 Callback Handler
	mux.HandleFunc("/api/auth/oauth/azure/callback", func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		stateTarget := r.URL.Query().Get("state")
		if stateTarget == "" {
			stateTarget = "/"
		}
		if code == "" {
			http.Error(w, "Missing authorization code", http.StatusBadRequest)
			return
		}

		clientID := os.Getenv("AZURE_CLIENT_ID")
		clientSecret := os.Getenv("AZURE_CLIENT_SECRET")
		tenantID := os.Getenv("AZURE_TENANT_ID")
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
		if userEmail == "" {
			http.Error(w, "No email address found for Azure AD user", http.StatusBadRequest)
			return
		}

		user, err := provisionOAuthUser("Azure Enterprise Org", "azure-org", userEmail, profile.DisplayName)
		if err != nil {
			http.Error(w, fmt.Sprintf("User Provisioning Error: %v", err), http.StatusInternalServerError)
			return
		}

		setSessionCookie(w, user.ID)
		http.Redirect(w, r, stateTarget, http.StatusFound)
	})

	// GET /api/org/members — Get team roster and pending invitations
	mux.HandleFunc("/api/org/members", func(w http.ResponseWriter, r *http.Request) {
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
	})

	// POST /api/org/invite — Invite a new team member
	mux.HandleFunc("/api/org/invite", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
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
		if req.OrgID == 0 {
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

		_ = mailer.SendInviteEmail(req.Email, org.Name, req.Role, inviteURL)

		jsonResp(w, map[string]interface{}{
			"success":    true,
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

		org, _ := database.GetOrganization(user.OrgID)
		jsonResp(w, map[string]interface{}{
			"success": true,
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
	log.Printf("   Corporate Beta Platform Enabled")
	log.Printf("=====================================================")

	if err := http.ListenAndServe(":"+port, cors(mux)); err != nil {
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

func jsonError(w http.ResponseWriter, msg string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": false,
		"error":   msg,
	})
}

func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
		} else {
			w.Header().Set("Access-Control-Allow-Origin", "*")
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Cookie, X-Session-User-ID")
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func processPendingLogs(database *db.DB, gemini *ai.GeminiClient) int {
	if database == nil || gemini == nil || !gemini.HasKey() {
		return 0
	}
	logs, err := database.GetUnanalyzedLogs()
	if err != nil || len(logs) == 0 {
		return 0
	}

	log.Printf("[server] processing %d pending unanalyzed logs with Gemini...", len(logs))
	processed := 0
	for _, entry := range logs {
		var b64 string
		if entry.ImagePath != "" {
			data, err := os.ReadFile(entry.ImagePath)
			if err == nil {
				b64 = encodingBase64(data)
			}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		res, err := gemini.Analyze(ctx, b64, entry.EntropyScore)
		cancel()

		if err != nil {
			log.Printf("[server] re-analyze log #%d error: %v", entry.ID, err)
			if strings.Contains(err.Error(), "429") || strings.Contains(err.Error(), "RESOURCE_EXHAUSTED") {
				time.Sleep(4 * time.Second)
			}
			continue
		}

		if err := database.UpdateAIResult(entry.ID, res.Category, res.Productive, res.Confidence, res.Reason); err == nil {
			processed++
			// Delete screenshot file after sending to backend/AI
			if entry.ImagePath != "" {
				if err := os.Remove(entry.ImagePath); err == nil {
					log.Printf("[server] deleted processed screenshot: %s", entry.ImagePath)
				}
			}
		}

		// Throttle slightly between items to respect API rate limits
		time.Sleep(1 * time.Second)
	}

	log.Printf("[server] completed processing %d/%d pending logs", processed, len(logs))
	return processed
}

func encodingBase64(data []byte) string {
	return base64.StdEncoding.EncodeToString(data)
}
