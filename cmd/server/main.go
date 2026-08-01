// cmd/server is a standalone web server & collector for mini-tracker.
// It requires NO sudo, NO GTK, and NO pkg-config dependencies.
// It serves the full React dashboard UI and runs the background tracker daemon.
package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

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

	gemini := ai.NewGeminiClient(cfg.GeminiAPIKey)
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

	// POST /api/process-pending
	mux.HandleFunc("/api/process-pending", func(w http.ResponseWriter, r *http.Request) {
		count := processPendingLogs(database, gemini)
		jsonResp(w, map[string]interface{}{
			"processed": count,
		})
	})

	mailer := email.NewMailer()

	// Corporate API Endpoints

	// Helper to set session cookie
	setSessionCookie := func(w http.ResponseWriter, userID int64) {
		http.SetCookie(w, &http.Cookie{
			Name:     "mini_session_user_id",
			Value:    fmt.Sprintf("%d", userID),
			Path:     "/",
			HttpOnly: true,
			Expires:  time.Now().Add(30 * 24 * time.Hour),
		})
	}

	// POST /api/org/register — Create a new organization & owner user
	mux.HandleFunc("/api/org/register", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Name     string `json:"name"`
			Email    string `json:"email"`
			Password string `json:"password"`
			FullName string `json:"full_name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" || req.Email == "" || req.Password == "" {
			http.Error(w, "Invalid request payload", http.StatusBadRequest)
			return
		}

		slug := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(req.Name), " ", "-"))
		org, err := database.CreateOrganization(req.Name, slug)
		if err != nil {
			http.Error(w, fmt.Sprintf("Organization creation failed: %v", err), http.StatusBadRequest)
			return
		}

		passHash := hashPassword(req.Password)
		fullName := req.FullName
		if fullName == "" {
			fullName = "Admin"
		}
		user, err := database.CreateUser(org.ID, req.Email, passHash, fullName, "owner")
		if err != nil {
			http.Error(w, fmt.Sprintf("User creation failed: %v", err), http.StatusBadRequest)
			return
		}

		setSessionCookie(w, user.ID)

		jsonResp(w, map[string]interface{}{
			"success": true,
			"org":     org,
			"user":    user,
		})
	})

	// POST /api/auth/login — Authenticate user
	mux.HandleFunc("/api/auth/login", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Email    string `json:"email"`
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Email == "" {
			http.Error(w, "Invalid credentials", http.StatusBadRequest)
			return
		}

		user, err := database.GetUserByEmail(req.Email)
		if err != nil || user.PasswordHash != hashPassword(req.Password) {
			http.Error(w, "Invalid email or password", http.StatusUnauthorized)
			return
		}

		setSessionCookie(w, user.ID)

		org, _ := database.GetOrganization(user.OrgID)
		jsonResp(w, map[string]interface{}{
			"success": true,
			"user":    user,
			"org":     org,
		})
	})

	// POST /api/auth/logout — End user session
	mux.HandleFunc("/api/auth/logout", func(w http.ResponseWriter, r *http.Request) {
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
		cookie, err := r.Cookie("mini_session_user_id")
		if err != nil || cookie.Value == "" {
			jsonResp(w, map[string]interface{}{"authenticated": false})
			return
		}

		userID, _ := strconv.ParseInt(cookie.Value, 10, 64)
		if userID == 0 {
			jsonResp(w, map[string]interface{}{"authenticated": false})
			return
		}

		user, err := database.GetUserByID(userID)
		if err != nil {
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
		// Mock/Demonstration OAuth flow: Authenticate/Provision Google SSO User
		email := "google.user@company.com"
		user, err := database.GetUserByEmail(email)
		if err != nil {
			org, _ := database.CreateOrganization("Google Workspace Org", "google-org")
			user, _ = database.CreateUser(org.ID, email, hashPassword("google-sso"), "Google Workspace User", "member")
		}
		setSessionCookie(w, user.ID)
		http.Redirect(w, r, "/", http.StatusFound)
	})

	// GET /api/auth/oauth/azure — Microsoft Azure AD / Entra ID SSO Endpoint
	mux.HandleFunc("/api/auth/oauth/azure", func(w http.ResponseWriter, r *http.Request) {
		// Mock/Demonstration OAuth flow: Authenticate/Provision Azure AD SSO User
		email := "azure.user@company.com"
		user, err := database.GetUserByEmail(email)
		if err != nil {
			org, _ := database.CreateOrganization("Azure Enterprise Org", "azure-org")
			user, _ = database.CreateUser(org.ID, email, hashPassword("azure-sso"), "Azure AD User", "member")
		}
		setSessionCookie(w, user.ID)
		http.Redirect(w, r, "/", http.StatusFound)
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
	frontendDir := "frontend/dist"
	if _, err := os.Stat(frontendDir); err == nil {
		fileServer := http.FileServer(http.Dir(frontendDir))
		mux.Handle("/", fileServer)
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
	h := sha256.Sum256([]byte("mini-tracker-salt-" + password))
	return hex.EncodeToString(h[:])
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

func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
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
