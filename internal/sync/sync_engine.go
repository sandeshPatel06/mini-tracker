package sync

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"sync"
	"time"

	"github.com/reak/get-hike/internal/db"
)

// SyncEngine manages background and manual telemetry pushes & analytics pulls
// between the desktop client's local SQLite database and the backend server.
type SyncEngine struct {
	database        *db.DB
	backendEndpoint string
	authToken       string

	mutex        sync.Mutex
	isSyncing    bool
	lastSyncedAt time.Time
}

// NewSyncEngine creates a new SyncEngine instance.
func NewSyncEngine(database *db.DB, endpoint string) *SyncEngine {
	if endpoint == "" {
		endpoint = "http://localhost:8080"
	}
	return &SyncEngine{
		database:        database,
		backendEndpoint: endpoint,
	}
}

// SetAuthToken updates the authentication token used for API requests.
func (s *SyncEngine) SetAuthToken(token string) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.authToken = token
}

// SetBackendEndpoint updates the remote server URL.
func (s *SyncEngine) SetBackendEndpoint(endpoint string) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if endpoint != "" {
		s.backendEndpoint = endpoint
	}
}

// PushTelemetry flushes pending local log entries to the backend server.
func (s *SyncEngine) PushTelemetry(ctx context.Context) (int, error) {
	if s.database == nil {
		return 0, nil
	}

	pendingLogs, err := s.database.GetPendingUploadLogs(50)
	if err != nil || len(pendingLogs) == 0 {
		return 0, err
	}

	s.mutex.Lock()
	endpoint := s.backendEndpoint
	token := s.authToken
	s.mutex.Unlock()

	client := &http.Client{Timeout: 30 * time.Second}
	var localIDs []int64
	var remoteIDs []int64

	for _, entry := range pendingLogs {
		var imgBase64 string
		if entry.ImagePath != "" {
			if data, err := os.ReadFile(entry.ImagePath); err == nil {
				imgBase64 = base64.StdEncoding.EncodeToString(data)
			}
		}

		payload := map[string]interface{}{
			"local_id":      entry.ID,
			"timestamp":     entry.Timestamp.Format(time.RFC3339),
			"image_base64":  imgBase64,
			"total_keys":    entry.TotalKeys,
			"unique_keys":   entry.UniqueKeys,
			"entropy_score": entry.EntropyScore,
			"app_name":      entry.AppName,
			"window_title":  entry.WindowTitle,
		}

		bodyBytes, err := json.Marshal(payload)
		if err != nil {
			continue
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"/api/telemetry/push", bytes.NewReader(bodyBytes))
		if err != nil {
			continue
		}
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}

		resp, err := client.Do(req)
		if err != nil {
			log.Printf("[sync] telemetry push request error: %v", err)
			break // Server offline, pause flush batch
		}

		if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated {
			var respData struct {
				Success  bool  `json:"success"`
				RemoteID int64 `json:"remote_id"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&respData); err == nil && respData.Success {
				localIDs = append(localIDs, entry.ID)
				remoteIDs = append(remoteIDs, respData.RemoteID)
			}
		}
		resp.Body.Close()
	}

	if len(localIDs) > 0 {
		_ = s.database.MarkLogsSynced(localIDs, remoteIDs)
		log.Printf("[sync] successfully pushed %d/%d pending telemetry logs to backend", len(localIDs), len(pendingLogs))
	}

	return len(localIDs), nil
}

// PullAnalytics pulls newly analyzed AI log entries from backend and updates local DB.
func (s *SyncEngine) PullAnalytics(ctx context.Context) (int, error) {
	if s.database == nil {
		return 0, nil
	}

	s.mutex.Lock()
	endpoint := s.backendEndpoint
	token := s.authToken
	sinceStr := s.lastSyncedAt.Format(time.RFC3339)
	s.mutex.Unlock()

	pullURL := fmt.Sprintf("%s/api/telemetry/pull?since=%s", endpoint, url.QueryEscape(sinceStr))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pullURL, nil)
	if err != nil {
		return 0, err
	}

	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[sync] pull analytics request error: %v", err)
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("pull failed with status %d: %s", resp.StatusCode, string(body))
	}

	var remoteLogs []db.LogEntry
	if err := json.NewDecoder(resp.Body).Decode(&remoteLogs); err != nil {
		return 0, fmt.Errorf("decode remote logs: %w", err)
	}

	if len(remoteLogs) == 0 {
		s.mutex.Lock()
		s.lastSyncedAt = time.Now()
		s.mutex.Unlock()
		return 0, nil
	}

	syncedCount := 0
	for _, remoteLog := range remoteLogs {
		if remoteLog.ID > 0 {
			// Update local log with AI analysis results
			_ = s.database.UpdateAIResult(
				remoteLog.ID,
				remoteLog.AICategory,
				remoteLog.IsProductive,
				remoteLog.AIConfidence,
				remoteLog.AIReason,
			)
			syncedCount++
		}
	}

	s.mutex.Lock()
	s.lastSyncedAt = time.Now()
	s.mutex.Unlock()

	log.Printf("[sync] pulled %d analyzed log records from backend server", syncedCount)
	return syncedCount, nil
}

// TriggerSyncNow performs an immediate manual Push + Pull cycle (e.g. via UI button).
func (s *SyncEngine) TriggerSyncNow(ctx context.Context) error {
	s.mutex.Lock()
	if s.isSyncing {
		s.mutex.Unlock()
		return nil // Already syncing
	}
	s.isSyncing = true
	s.mutex.Unlock()

	defer func() {
		s.mutex.Lock()
		s.isSyncing = false
		s.mutex.Unlock()
	}()

	log.Printf("[sync] initiating manual sync cycle...")
	_, pushErr := s.PushTelemetry(ctx)
	_, pullErr := s.PullAnalytics(ctx)

	if pushErr != nil && pullErr != nil {
		return fmt.Errorf("push err: %v; pull err: %v", pushErr, pullErr)
	}
	return nil
}

// StartBackgroundPullCron starts a ticker routine executing periodic sync (e.g., 3-hour pull window).
func (s *SyncEngine) StartBackgroundPullCron(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 3 * time.Hour
	}

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		log.Printf("[sync] background sync pull cron active (interval: %v)", interval)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				ctxTimeout, cancel := context.WithTimeout(ctx, 2*time.Minute)
				_ = s.TriggerSyncNow(ctxTimeout)
				cancel()
			}
		}
	}()
}
