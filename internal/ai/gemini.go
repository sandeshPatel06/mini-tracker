package ai

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// redactAPIKey removes the ?key=... query parameter from URLs embedded in error
// messages so API keys never leak into logs.
var redactKeyPattern = regexp.MustCompile(`key=[^&\s"']+`)

func redactAPIKey(s string) string {
	return redactKeyPattern.ReplaceAllLiteralString(s, "key=REDACTED")
}

// UsageMetadata represents token usage returned from Gemini REST API.
type UsageMetadata struct {
	PromptTokenCount     int `json:"promptTokenCount"`
	CandidatesTokenCount int `json:"candidatesTokenCount"`
	TotalTokenCount      int `json:"totalTokenCount"`
}

// AnalysisResult is the structured response from Gemini.
type AnalysisResult struct {
	Category        string        `json:"category"`
	AppName         string        `json:"app_name"`
	AppCategory     string        `json:"app_category"`
	WindowTitle     string        `json:"window_title"`
	Productive      bool          `json:"productive"`
	ProductiveScore float64       `json:"productivity_score"`
	Confidence      float64       `json:"confidence"`
	Reason          string        `json:"brief_reason"`
	Usage           UsageMetadata `json:"usage_metadata"`
}

// GeminiModelInfo represents a model returned by ListModels API.
type GeminiModelInfo struct {
	Name                       string   `json:"name"`
	DisplayName                string   `json:"displayName"`
	SupportedGenerationMethods []string `json:"supportedGenerationMethods"`
}

type listModelsResponse struct {
	Models []GeminiModelInfo `json:"models"`
}

// GeminiClient wraps the Gemini REST API with dynamic model discovery.
type GeminiClient struct {
	apiKey    string
	modelName string
	dataDir   string
	client    *http.Client
	mu        sync.Mutex
	// disabled tracks models that returned a hard "unavailable/deprecated" error
	// within this session so model selection stops picking them.
	disabled map[string]bool
	// lastGoodModel remembers the most recent model that produced a parsed
	// answer so a broken configured/env model is not retried on every request.
	lastGoodModel string
	// Gemini explicit context cache (cachedContents) for the stable analysis
	// instructions. Batches can overlap, so state is guarded separately.
	cacheMu       sync.Mutex
	cacheName     string
	cacheKey      string
	cacheExpiry   time.Time
	cacheDisabled bool
}

// NewGeminiClient returns a new Gemini client using an optional initialModel or GEMINI_MODEL env var.
func NewGeminiClient(apiKey string, initialModel ...string) *GeminiClient {
	m := ""
	if len(initialModel) > 0 && initialModel[0] != "" {
		m = initialModel[0]
	} else if envModel := os.Getenv("GEMINI_MODEL"); envModel != "" {
		m = envModel
	}
	return &GeminiClient{
		apiKey:    apiKey,
		modelName: m,
		client:    &http.Client{Timeout: 60 * time.Second},
		disabled:  map[string]bool{},
	}
}

// SetModel explicitly sets the model name to use.
func (g *GeminiClient) SetModel(modelName string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.modelName = modelName
}

// SetAPIKey updates the API key at runtime and resets model selection.
func (g *GeminiClient) SetAPIKey(apiKey string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.apiKey = apiKey
	if g.disabled == nil {
		g.disabled = map[string]bool{}
	}
	if envModel := os.Getenv("GEMINI_MODEL"); envModel != "" {
		g.modelName = envModel
	}
}

// DisableModel marks a model permanently unavailable for this session.
func (g *GeminiClient) DisableModel(modelName string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.disabled == nil {
		g.disabled = map[string]bool{}
	}
	g.disabled[modelName] = true
}

// IsDisabled reports whether a model was marked unavailable this session.
func (g *GeminiClient) IsDisabled(modelName string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.disabled == nil {
		return false
	}
	return g.disabled[modelName]
}

// markModelHealthy records the model that just produced a usable answer so
// later requests can skip known-broken candidates.
func (g *GeminiClient) markModelHealthy(model string) {
	g.mu.Lock()
	g.lastGoodModel = model
	g.mu.Unlock()
}

// listDisabledModels returns a snapshot of session-disabled model names.
func (g *GeminiClient) listDisabledModels() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	names := make([]string, 0, len(g.disabled))
	for name := range g.disabled {
		names = append(names, name)
	}
	return names
}

// contextCacheTTL is the lifetime of a Gemini cachedContents resource. Bundles
// share the same static instructions, so a short TTL keeps the cache warm
// without paying for stale storage.
const contextCacheTTL = 10 * time.Minute

// contextCachingEnabled reports whether explicit Gemini context caching is on.
// Disable with GEMINI_CONTEXT_CACHE=0/false/off.
func contextCachingEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("GEMINI_CONTEXT_CACHE"))) {
	case "0", "false", "off", "no":
		return false
	default:
		return true
	}
}

// ensureContextCache returns a cachedContents resource name for the static
// analysis instructions, creating (or refreshing) it on demand. It returns ""
// when caching is disabled, unsupported by the model, or the instructions are
// below the provider's minimum token count — callers then send the full prompt.
func (g *GeminiClient) ensureContextCache(ctx context.Context, model, staticPrompt string) string {
	if staticPrompt == "" || !contextCachingEnabled() {
		return ""
	}
	cleanModel := strings.TrimPrefix(model, "models/")
	sum := sha256.Sum256([]byte(staticPrompt))
	key := cleanModel + "|" + fmt.Sprintf("%x", sum)

	g.cacheMu.Lock()
	if g.cacheDisabled {
		g.cacheMu.Unlock()
		return ""
	}
	if g.cacheName != "" && g.cacheKey == key && time.Now().Before(g.cacheExpiry) {
		name := g.cacheName
		g.cacheMu.Unlock()
		return name
	}
	previous := g.cacheName
	g.cacheMu.Unlock()

	cacheURL := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/cachedContents?key=%s", g.apiKey)
	body, err := json.Marshal(map[string]interface{}{
		"model":       "models/" + cleanModel,
		"displayName": "get-hike-analysis-context",
		"ttl":         fmt.Sprintf("%ds", int(contextCacheTTL/time.Second)),
		"contents": []map[string]interface{}{
			{"role": "user", "parts": []map[string]interface{}{{"text": staticPrompt}}},
		},
	})
	if err != nil {
		return ""
	}
	req, err := http.NewRequestWithContext(ctx, "POST", cacheURL, bytes.NewReader(body))
	if err != nil {
		return ""
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := g.client.Do(req)
	if err != nil {
		log.Printf("[ai/gemini] context cache create failed: %s", redactAPIKey(err.Error()))
		return ""
	}
	defer resp.Body.Close()
	respData, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		msg := strings.ToLower(string(respData))
		switch {
		case strings.Contains(msg, "too small") || strings.Contains(msg, "minimum"):
			g.cacheMu.Lock()
			g.cacheDisabled = true
			g.cacheMu.Unlock()
			log.Printf("[ai/gemini] context cache unavailable (instructions below model minimum) — continuing uncached")
		case strings.Contains(msg, "not supported") || strings.Contains(msg, "unsupported"):
			g.cacheMu.Lock()
			g.cacheDisabled = true
			g.cacheMu.Unlock()
			log.Printf("[ai/gemini] context cache not supported by %s — continuing uncached", cleanModel)
		default:
			log.Printf("[ai/gemini] context cache create error %d: %s", resp.StatusCode, truncateString(string(respData), 200))
		}
		return ""
	}

	var created struct {
		Name       string `json:"name"`
		ExpireTime string `json:"expireTime"`
	}
	if err := json.Unmarshal(respData, &created); err != nil || created.Name == "" {
		return ""
	}
	expiry := time.Now().Add(contextCacheTTL)
	if parsed, perr := time.Parse(time.RFC3339, created.ExpireTime); perr == nil {
		expiry = parsed
	}

	g.cacheMu.Lock()
	g.cacheName = created.Name
	g.cacheKey = key
	g.cacheExpiry = expiry
	g.cacheMu.Unlock()
	log.Printf("[ai/gemini] context cache ready: %s (model=%s, ttl=%s)", created.Name, cleanModel, contextCacheTTL)

	if previous != "" && previous != created.Name {
		g.deleteCachedContent(previous)
	}
	return created.Name
}

// clearContextCache drops the in-memory cache handle and deletes the remote
// resource (best effort).
func (g *GeminiClient) clearContextCache() {
	g.cacheMu.Lock()
	name := g.cacheName
	g.cacheName, g.cacheKey, g.cacheExpiry = "", "", time.Time{}
	g.cacheMu.Unlock()
	if name != "" {
		g.deleteCachedContent(name)
	}
}

// deleteCachedContent removes a cachedContents resource to stop storage billing.
func (g *GeminiClient) deleteCachedContent(name string) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	deleteURL := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/%s?key=%s", name, g.apiKey)
	req, err := http.NewRequestWithContext(ctx, "DELETE", deleteURL, nil)
	if err != nil {
		return
	}
	resp, err := g.client.Do(req)
	if err != nil {
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	_ = resp.Body.Close()
}

// isContextCacheError reports whether a generateContent failure is caused by a
// stale/expired cached content resource, so the caller retries uncached.
func isContextCacheError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "cachedcontent") ||
		strings.Contains(msg, "cached content") ||
		strings.Contains(msg, "cached_content")
}

// HasKey checks if an API key is currently set.
func (g *GeminiClient) HasKey() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.apiKey != ""
}

// GetModel returns current active model name.
func (g *GeminiClient) GetModel() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.modelName
}

// SetDataDir sets the base directory used for cached AI thumbnails.
func (g *GeminiClient) SetDataDir(dir string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.dataDir = dir
}

// GetDataDir returns the base directory used for cached AI thumbnails.
func (g *GeminiClient) GetDataDir() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.dataDir
}

// FetchAvailableModels queries Gemini API for models supporting generateContent,
// ordered by cheapness and efficiency (preferring Flash models).
func (g *GeminiClient) FetchAvailableModels(ctx context.Context) ([]string, error) {
	if g.apiKey == "" {
		return nil, fmt.Errorf("no API key set")
	}

	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models?key=%s", g.apiKey)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := g.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("list models request: %s", redactAPIKey(err.Error()))
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("list models API error %d: %s", resp.StatusCode, string(data))
	}

	var listResp listModelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&listResp); err != nil {
		return nil, fmt.Errorf("decode list models: %w", err)
	}

	var candidates []string
	for _, m := range listResp.Models {
		// Ensure generateContent is supported
		supportsGen := false
		for _, method := range m.SupportedGenerationMethods {
			if method == "generateContent" {
				supportsGen = true
				break
			}
		}
		if !supportsGen {
			continue
		}

		// Normalize name (remove "models/" prefix if present)
		name := strings.TrimPrefix(m.Name, "models/")

		// Skip models known to be deprecated/retired: ListModels still reports
		// them but generateContent rejects 404 ("no longer available to new users").
		if deprecatedModels[name] {
			continue
		}

		candidates = append(candidates, name)
	}

	if len(candidates) == 0 {
		return nil, fmt.Errorf("no models with generateContent support found")
	}

	return rankCandidateModels(candidates), nil
}

// nonTextModelKeywords marks ListModels entries that cannot be used for
// screenshot analysis (speech, image generation, embeddings, research agents,
// robotics/computer-use models). They advertise generateContent but reject or
// ignore the multimodal analysis payload.
var nonTextModelKeywords = []string{
	"-tts", "tts-", "embedding", "image", "imagen", "veo", "lyria",
	"robotics", "computer-use", "antigravity", "deep-research", "transcribe",
	"nano-banana", "omni",
}

// isNonTextModel reports whether a model should be excluded from batch analysis.
func isNonTextModel(name string) bool {
	lName := strings.ToLower(name)
	for _, keyword := range nonTextModelKeywords {
		if strings.Contains(lName, keyword) {
			return true
		}
	}
	return false
}

// modelPreferenceScore ranks models for screenshot batch analysis. Flash-lite
// models are preferred because they accept responseSchema, handle inline images
// and are the cheapest tier; gemma models reject structured output schemas, so
// they are kept only as a last-resort fallback.
func modelPreferenceScore(name string) int {
	lName := strings.ToLower(name)

	score := 0
	switch {
	case strings.Contains(lName, "flash-lite"):
		score = 50
	case strings.Contains(lName, "flash"):
		score = 40
	case strings.Contains(lName, "pro"):
		score = 20
	case strings.Contains(lName, "gemma"):
		score = 5
	case strings.Contains(lName, "gemini"):
		score = 15
	}

	if strings.Contains(lName, "gemini") {
		score += 10
	}
	if strings.Contains(lName, "preview") {
		score -= 3
	}
	return score + geminiVersionScore(lName)
}

// geminiVersionScore returns a small bonus for newer Gemini versions parsed from
// names such as "gemini-3.5-flash-lite" (3.5 -> 6). Version bumps must not
// outrank the family preference (e.g. an unknown gemini-4 model still ranks
// below flash-lite).
func geminiVersionScore(name string) int {
	const prefix = "gemini-"
	idx := strings.Index(name, prefix)
	if idx < 0 {
		return 0
	}
	rest := name[idx+len(prefix):]
	var major, minor int
	if n, _ := fmt.Sscanf(rest, "%d.%d", &major, &minor); n == 0 {
		return 0
	}
	return major*2 + minor/10
}

// rankCandidateModels filters non-text models and sorts the remainder by batch
// suitability (highest first). Sorting is stable so ListModels order is kept
// within equal scores.
func rankCandidateModels(candidates []string) []string {
	ranked := make([]string, 0, len(candidates))
	for _, name := range candidates {
		if isNonTextModel(name) {
			continue
		}
		ranked = append(ranked, name)
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		return modelPreferenceScore(ranked[i]) > modelPreferenceScore(ranked[j])
	})
	return ranked
}

// SelectBestModel picks the environment-configured model, pre-configured model, or candidate model.
func (g *GeminiClient) SelectBestModel(ctx context.Context, exclude map[string]bool) (string, error) {
	// 0. Reuse the model that last answered successfully. Without this, a broken
	// environment/pre-configured model (e.g. one returning 500) would be retried
	// first on every single bundle before failing over again.
	g.mu.Lock()
	good := g.lastGoodModel
	g.mu.Unlock()
	if good != "" && (exclude == nil || !exclude[good]) && !g.IsDisabled(good) {
		log.Printf("[ai/gemini] Reusing last healthy model: %s", good)
		return good, nil
	}

	// 1. If GEMINI_MODEL env var is set and not excluded, use it directly
	if envModel := os.Getenv("GEMINI_MODEL"); envModel != "" {
		if (exclude == nil || !exclude[envModel]) && !g.IsDisabled(envModel) {
			g.mu.Lock()
			g.modelName = envModel
			g.mu.Unlock()
			log.Printf("[ai/gemini] Using environment-configured model: %s", envModel)
			return envModel, nil
		}
	}

	// 2. If client has a pre-configured modelName not excluded, use it
	g.mu.Lock()
	current := g.modelName
	g.mu.Unlock()
	if current != "" {
		if (exclude == nil || !exclude[current]) && !g.IsDisabled(current) {
			log.Printf("[ai/gemini] Using pre-configured model: %s", current)
			return current, nil
		}
	}

	// 3. Try online auto-discovery first if no explicit model is configured
	models, err := g.FetchAvailableModels(ctx)
	if err == nil {
		for _, m := range models {
			if exclude != nil && exclude[m] {
				continue
			}
			// Skip models explicitly disabled during this session (hard 404/unavailable).
			if g.IsDisabled(m) {
				continue
			}
			g.mu.Lock()
			g.modelName = m
			g.mu.Unlock()
			log.Printf("[ai/gemini] Auto-selected model: %s (from API ListModels)", m)
			return m, nil
		}
	} else {
		log.Printf("[ai/gemini] Model listing warning: %v — falling back to candidate list", redactAPIKey(err.Error()))
	}

	// 4. Fallback candidates when ListModels is unreachable
	for _, m := range fallbackModels {
		if exclude != nil && exclude[m] {
			continue
		}
		if g.IsDisabled(m) {
			continue
		}
		g.mu.Lock()
		g.modelName = m
		g.mu.Unlock()
		log.Printf("[ai/gemini] Selected fallback model: %s", m)
		return m, nil
	}

	return "", fmt.Errorf("no working Gemini models available")
}

// deprecatedModels are retired model IDs still listed by ListModels but rejected
// by generateContent. Filtered from auto-selection and never used as fallbacks.
var deprecatedModels = map[string]bool{
	"gemma-2-27b-it":   true,
	"gemini-2.5-flash": true,
}

// fallbackModels are well-known model IDs used when ListModels is unreachable.
// Flash-lite first (schema + vision capable), gemma last (schema-averse; the
// payload ladder drops the schema before trying them).
var fallbackModels = []string{
	"gemini-3.5-flash-lite",
	"gemini-flash-lite-latest",
	"gemini-3.6-flash",
	"gemini-3.8-flash",
	"gemma-4-31b-it",
	"gemma-4-26b-a4b-it",
}

// Analyze sends window title and telemetry to Gemini by wrapping it in AnalyzeBatch.
func (g *GeminiClient) Analyze(ctx context.Context, windowTitle string, entropyScore float64, totalClicks int) (*AnalysisResult, error) {
	batchResults, err := g.AnalyzeBatch(ctx, []BatchAnalysisItem{
		{
			WindowTitle:  windowTitle,
			EntropyScore: entropyScore,
			TotalClicks:  totalClicks,
		},
	})
	if err != nil {
		return nil, err
	}
	if len(batchResults) == 0 {
		return nil, fmt.Errorf("empty result from batch analysis")
	}

	res := batchResults[0]
	return &AnalysisResult{
		Category:        res.Category,
		AppName:         res.AppName,
		AppCategory:     res.AppCategory,
		WindowTitle:     res.WindowTitle,
		Productive:      res.Productive,
		ProductiveScore: res.ProductiveScore,
		Confidence:      res.Confidence,
		Reason:          res.Reason,
		Usage:           res.Usage,
	}, nil
}

// BatchAnalysisItem represents a single screenshot item in a batch request.
type BatchAnalysisItem struct {
	LogID        int64
	WindowTitle  string
	EntropyScore float64
	ImagePath    string
	TotalKeys    int
	TotalClicks  int
	UserTask     string
	AppName      string
	Timestamp    time.Time
}

// BatchAnalysisResult represents the result for a single item in a batch.
type BatchAnalysisResult struct {
	LogID           int64
	Category        string        `json:"category"`
	AppName         string        `json:"app_name"`
	AppCategory     string        `json:"app_category"`
	WindowTitle     string        `json:"window_title"`
	Productive      bool          `json:"productive"`
	ProductiveScore float64       `json:"productivity_score"`
	Confidence      float64       `json:"confidence"`
	Reason          string        `json:"reason"`
	Usage           UsageMetadata `json:"usage_metadata"`
}

// AnalyzeBatch sends multiple items in grouped Gemini requests. Chunks of up to
// maxItemsPerRequest are analyzed independently so one failure never blocks the rest.
// Optional sessionEvents give the model context about app switches. Returns partial
// results together with the last error when some chunks fail.
func (g *GeminiClient) AnalyzeBatch(ctx context.Context, items []BatchAnalysisItem, sessionEvents ...SessionEvent) ([]BatchAnalysisResult, error) {
	if !g.HasKey() || len(items) == 0 {
		return nil, fmt.Errorf("no API key set or empty batch")
	}

	var all []BatchAnalysisResult
	var lastErr error
	for start := 0; start < len(items); start += maxItemsPerRequest {
		if err := ctx.Err(); err != nil {
			if len(all) > 0 {
				return all, err
			}
			return nil, err
		}
		end := start + maxItemsPerRequest
		if end > len(items) {
			end = len(items)
		}
		results, err := g.analyzeChunk(ctx, items[start:end], sessionEvents)
		if err != nil {
			lastErr = err
			continue
		}
		all = append(all, results...)
	}

	if len(all) == 0 {
		if lastErr != nil {
			return nil, lastErr
		}
		return nil, fmt.Errorf("gemini batch returned no results")
	}
	return all, lastErr
}

// analyzeChunk builds a compact prompt (plus thumbnails) for one group of items,
// posts it with retry/backoff and model failover, and maps the parsed results back
// to the input items. Items without a model result are omitted so callers can retry them.
// maxIdleStreakGap caps how long a quiet same-window stretch can keep reusing a
// single screenshot: beyond this gap a fresh screenshot is always sent.
const maxIdleStreakGap = 15 * time.Minute

// buildPromptItems maps batch items to prompt items, suppressing duplicate
// screenshots for quiet same-window stretches. Images are by far the largest
// token cost in a batch request and consecutive captures of an unchanged,
// unattended window look identical, so only the newest screenshot of each
// stretch is sent — later items flag img=false and the prompt instructs the
// model to reuse the previous image. A window change, any keystroke/mouse
// activity, or an oversized time gap always ends the stretch with a fresh
// visual check.
func buildPromptItems(items []BatchAnalysisItem) []PromptItem {
	var lastKept *BatchAnalysisItem
	promptItems := make([]PromptItem, len(items))
	for i, item := range items {
		hasImage := item.ImagePath != ""
		suppressed := false
		if hasImage && lastKept != nil &&
			item.AppName == lastKept.AppName &&
			item.WindowTitle == lastKept.WindowTitle &&
			item.Timestamp.Sub(lastKept.Timestamp) <= maxIdleStreakGap &&
			item.TotalKeys == 0 && item.TotalClicks == 0 {
			hasImage = false
			suppressed = true
		}
		if hasImage {
			lastKept = &items[i]
		}
		if suppressed && os.Getenv("MINI_AI_SUPPRESS_DEBUG") != "" {
			log.Printf("[ai/gemini] suppressed duplicate idle screenshot for item %d (%s)", item.LogID, item.WindowTitle)
		}
		promptItems[i] = PromptItem{
			LogID:        item.LogID,
			WindowTitle:  item.WindowTitle,
			EntropyScore: item.EntropyScore,
			TotalKeys:    item.TotalKeys,
			TotalClicks:  item.TotalClicks,
			AppName:      item.AppName,
			Timestamp:    item.Timestamp,
			ImagePath:    item.ImagePath,
			HasImage:     hasImage,
		}
	}
	return promptItems
}

func (g *GeminiClient) analyzeChunk(ctx context.Context, items []BatchAnalysisItem, sessionEvents []SessionEvent) ([]BatchAnalysisResult, error) {
	activeTask := ""
	for _, item := range items {
		if strings.TrimSpace(item.UserTask) != "" {
			activeTask = strings.TrimSpace(item.UserTask)
			break
		}
	}

	// Token savings: images are by far the largest cost in a batch request.
	// Consecutive captures of the same window with no input activity look
	// identical, so only the newest screenshot of each quiet stretch is sent —
	// later items flag img=false and the prompt tells the model to reuse the
	// previous image. Nothing is suppressed when the window changes or input
	// happens, so real changes always get a fresh visual check.
	promptItems := buildPromptItems(items)

	// Split the prompt into a stable instruction block (cacheable) and the
	// dynamic session/items block. Gemini explicit caching stores the static
	// half once and reuses it across requests, cutting repeated input tokens.
	staticPrompt, dynamicPrompt := BuildAnalysisPromptSplit(promptItems, activeTask, sessionEvents...)
	fullPrompt := staticPrompt + dynamicPrompt

	imageParts := buildImageParts(promptItems, g.GetDataDir())

	// Output budget: roomy floor so pretty-printed or long-reason outputs never
	// truncate mid-array; 160 tokens per plenty for compact objects.
	maxOutputTokens := 1280 + 160*len(items)
	if maxOutputTokens > 4096 {
		maxOutputTokens = 4096
	}

	// Thinking tokens count against maxOutputTokens on thinking-capable models
	// and burning through the budget truncates structured answers, so the
	// request ladder disables them per model (see payloadVariants). Not every
	// model accepts thinkingConfig (gemma and lite models reject it), which is
	// why the base payload omits it.
	buildPayload := func(cacheName string) map[string]interface{} {
		promptText := fullPrompt
		if cacheName != "" {
			promptText = dynamicPrompt
		}
		parts := make([]map[string]interface{}, 0, 1+len(imageParts))
		parts = append(parts, map[string]interface{}{"text": promptText})
		parts = append(parts, imageParts...)
		payload := map[string]interface{}{
			"contents": []map[string]interface{}{
				{"parts": parts},
			},
			"generationConfig": map[string]interface{}{
				"temperature":      0.1,
				"maxOutputTokens":  maxOutputTokens,
				"responseMimeType": "application/json",
				"responseSchema":   AnalysisResponseSchema(),
			},
		}
		if cacheName != "" {
			payload["cachedContent"] = cacheName
		}
		return payload
	}
	uncachedPayload := buildPayload("")

	exclude := map[string]bool{}
	// Seed the exclusion set with models disabled earlier this session so
	// selection starts at a healthy candidate instead of repeating 404s.
	for _, name := range g.listDisabledModels() {
		exclude[name] = true
	}
	var lastErr error
	for attempt := 0; attempt < maxModelCandidates; attempt++ {
		model, err := g.SelectBestModel(ctx, exclude)
		if err != nil {
			if lastErr == nil {
				lastErr = err
			}
			break
		}
		exclude[model] = true

		cacheName := g.ensureContextCache(ctx, model, staticPrompt)
		rawText, usage, err := g.postGenerateContentWithRetry(ctx, model, buildPayload(cacheName))
		if err != nil && cacheName != "" && isContextCacheError(err) {
			g.clearContextCache()
			log.Printf("[ai/gemini] cached context rejected for %s (%v) — retrying without cache", model, err)
			rawText, usage, err = g.postGenerateContentWithRetry(ctx, model, uncachedPayload)
		}
		if err != nil {
			lastErr = err
			log.Printf("[ai/gemini] model %s failed: %v — trying next model", model, err)
			continue
		}

		parsed, err := ParseAnalysisJSON(rawText, len(items))
		if err != nil {
			lastErr = err
			log.Printf("[ai/gemini] model %s returned unparsable output: %v — trying next model", model, err)
			continue
		}

		g.markModelHealthy(model)
		log.Printf("[ai/gemini] analyzed %d items with model %s (tokens: prompt=%d output=%d)", len(items), model, usage.PromptTokenCount, usage.CandidatesTokenCount)
		return mapChunkResults(items, parsed, usage), nil
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("no working Gemini models available")
	}
	return nil, lastErr
}

// errPayloadRejected marks a request whose payload shape the model refused
// (responseSchema or thinkingConfig unsupported, or empty output under a rigid
// schema). The caller walks the payload ladder instead of retrying the body.
var errPayloadRejected = errors.New("gemini payload rejected")

// modelQuotaDisableThreshold is how long a server-provided quota reset must be
// before the model is disabled for the session. Long resets are daily quotas;
// retrying every batch only burns time and keeps the batch pending forever.
const modelQuotaDisableThreshold = 5 * time.Minute

// postGenerateContentWithRetry performs a single generateContent call, walking a
// ladder of progressively simpler payloads:
//
//	schema + thinkingConfig -> schema only -> free-form JSON
//
// Transient failures (429/5xx/network) are retried per rung with backoff; a
// shape rejection immediately drops to the next rung. Models reporting a long
// quota reset (e.g. "retry in 15h") or a hard 404 are disabled for the session
// so later batches start from a healthy candidate. Models that answer with
// empty text under a schema (gemma-class) still get a free-form retry.
func (g *GeminiClient) postGenerateContentWithRetry(ctx context.Context, model string, reqPayload map[string]interface{}) (string, UsageMetadata, error) {
	cleanModel := strings.TrimPrefix(model, "models/")
	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s", cleanModel, g.apiKey)

	policy := RetryPolicy{MaxAttempts: 3, BaseDelay: 2 * time.Second, MaxDelay: 15 * time.Second}
	variants := buildPayloadVariants(model, reqPayload)
	var lastErr error

	for i, payload := range variants {
		var rawText string
		var usage UsageMetadata

		err := DoWithRetry(ctx, policy, func(ctx context.Context) (error, bool, time.Duration) {
			reqBody, err := json.Marshal(payload)
			if err != nil {
				return fmt.Errorf("marshal request: %w", err), false, 0
			}

			req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(reqBody))
			if err != nil {
				return fmt.Errorf("build gemini request: %s", redactAPIKey(err.Error())), false, 0
			}
			req.Header.Set("Content-Type", "application/json")

			resp, err := g.client.Do(req)
			if err != nil {
				return fmt.Errorf("gemini request: %s", redactAPIKey(err.Error())), true, 0
			}
			defer resp.Body.Close()

			respData, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))

			if resp.StatusCode == http.StatusOK {
				text, meta, perr := extractResponseText(respData)
				if perr != nil {
					return perr, false, 0
				}
				if strings.TrimSpace(text) == "" {
					return fmt.Errorf("%w: returned empty output", errPayloadRejected), false, 0
				}
				rawText, usage = text, meta
				return nil, false, 0
			}

			if resp.StatusCode == http.StatusBadRequest && looksLikeAuthError(respData) {
				return fmt.Errorf("gemini error %d: %s", resp.StatusCode, truncateString(string(respData), 300)), false, 0
			}

			if resp.StatusCode == http.StatusNotFound && isHardUnavailable(respData) {
				g.DisableModel(model)
				log.Printf("[ai/gemini] model %s is retired/unavailable — disabled for this session", model)
				return fmt.Errorf("gemini error %d: %s", resp.StatusCode, truncateString(string(respData), 300)), false, 0
			}

			if isPayloadRejection(resp.StatusCode, respData, payload) {
				return fmt.Errorf("%w: gemini error %d: %s", errPayloadRejected, resp.StatusCode, truncateString(string(respData), 300)), false, 0
			}

			if resp.StatusCode == http.StatusTooManyRequests {
				if delay := retryDelayFromBody(respData); delay > modelQuotaDisableThreshold {
					g.DisableModel(model)
					log.Printf("[ai/gemini] model %s quota exhausted (server retry in %s) — disabled for this session", model, delay.Round(time.Second))
					return fmt.Errorf("gemini quota exhausted for %s (retry in %s)", model, delay.Round(time.Second)), false, 0
				}
			}

			return fmt.Errorf("gemini error %d: %s", resp.StatusCode, truncateString(string(respData), 300)),
				IsRetryableStatus(resp.StatusCode),
				RetryAfterFromHeader(resp.Header)
		})
		if err == nil {
			return rawText, usage, nil
		}
		lastErr = err

		if errors.Is(err, errPayloadRejected) && i < len(variants)-1 {
			log.Printf("[ai/gemini] model %s rejected payload variant %d/%d: %v — retrying with a simpler payload", model, i+1, len(variants), err)
			continue
		}
		return "", UsageMetadata{}, err
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("no working Gemini payload for %s", model)
	}
	return "", UsageMetadata{}, lastErr
}

// buildPayloadVariants returns the request payloads to try for a model, from
// richest to simplest. thinkingConfig is only offered to models that accept it;
// gemma and lite models reject it with a 400.
func buildPayloadVariants(model string, base map[string]interface{}) []map[string]interface{} {
	variants := make([]map[string]interface{}, 0, 3)
	if supportsThinkingBudget(model) {
		variants = append(variants, withThinkingConfig(base, 0))
	}
	variants = append(variants, base)
	if _, ok := generationConfigValue(base, "responseSchema"); ok {
		variants = append(variants, withoutResponseSchema(base))
	}
	return variants
}

// supportsThinkingBudget reports whether a model accepts thinkingConfig.
func supportsThinkingBudget(model string) bool {
	lModel := strings.ToLower(strings.TrimPrefix(model, "models/"))
	if strings.Contains(lModel, "lite") || strings.Contains(lModel, "gemma") {
		return false
	}
	return strings.Contains(lModel, "gemini")
}

// generationConfigValue returns a generationConfig entry from a request payload.
func generationConfigValue(payload map[string]interface{}, key string) (interface{}, bool) {
	gc, ok := payload["generationConfig"].(map[string]interface{})
	if !ok {
		return nil, false
	}
	v, ok := gc[key]
	return v, ok
}

// hasThinkingConfig reports whether a payload carries adaptive-thinking controls.
func hasThinkingConfig(payload map[string]interface{}) bool {
	_, ok := generationConfigValue(payload, "thinkingConfig")
	return ok
}

// looksLikeAuthError reports whether a 400 body is an API-key/permission error,
// which no payload change can fix.
func looksLikeAuthError(body []byte) bool {
	lBody := bytes.ToLower(body)
	return bytes.Contains(lBody, []byte("api key")) ||
		bytes.Contains(lBody, []byte("api_key")) ||
		bytes.Contains(lBody, []byte("permission denied")) ||
		bytes.Contains(lBody, []byte("billing"))
}

// isPayloadRejection reports whether an error response means the payload shape
// is unsupported by this model, so the next (simpler) variant should be tried
// instead of retrying the same body.
func isPayloadRejection(status int, body []byte, payload map[string]interface{}) bool {
	if status != http.StatusBadRequest && status != http.StatusInternalServerError {
		return false
	}
	lBody := bytes.ToLower(body)
	if hasThinkingConfig(payload) && bytes.Contains(lBody, []byte("thinking")) {
		return true
	}
	if _, hasSchema := generationConfigValue(payload, "responseSchema"); !hasSchema {
		return false
	}
	for _, marker := range [][]byte{
		[]byte("schema"),
		[]byte("invalid argument"),
		[]byte("internal error"),
		[]byte("unsupported"),
		[]byte("not supported"),
		[]byte("thinking"),
		[]byte("budget"),
	} {
		if bytes.Contains(lBody, marker) {
			return true
		}
	}
	return false
}

var (
	retryDelayJSONPattern = regexp.MustCompile(`"retryDelay"\s*:\s*"([^"]+)"`)
	retryInPattern        = regexp.MustCompile(`(?i)retry in ([0-9hms.]+)`)
)

// retryDelayFromBody extracts the server-provided quota reset from a 429 body.
// Gemini returns either {"retryDelay":"15h17m48s"} or "Please retry in 15h17m48s".
func retryDelayFromBody(body []byte) time.Duration {
	for _, re := range []*regexp.Regexp{retryDelayJSONPattern, retryInPattern} {
		m := re.FindSubmatch(body)
		if len(m) != 2 {
			continue
		}
		raw := strings.TrimRight(strings.TrimSpace(string(m[1])), ".")
		if d, err := time.ParseDuration(raw); err == nil {
			return d
		}
	}
	return 0
}

// isHardUnavailable reports whether a 404 response body means the model itself is
// gone for this API key ("is not found for API version..." / "no longer available
// to new users") rather than a transient routing problem.
func isHardUnavailable(body []byte) bool {
	return bytes.Contains(body, []byte("is not found for API version")) ||
		bytes.Contains(body, []byte("no longer available to new users")) ||
		bytes.Contains(body, []byte("is not supported for generateContent"))
}

// extractResponseText pulls the last non-thought text part and usage metadata
// from a generateContent response body.
func extractResponseText(respData []byte) (string, UsageMetadata, error) {
	var geminiResp struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text    string `json:"text"`
					Thought bool   `json:"thought"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
		UsageMetadata UsageMetadata `json:"usageMetadata"`
	}
	if err := json.Unmarshal(respData, &geminiResp); err != nil {
		return "", UsageMetadata{}, fmt.Errorf("decode gemini response: %w", err)
	}
	if len(geminiResp.Candidates) == 0 || len(geminiResp.Candidates[0].Content.Parts) == 0 {
		return "", geminiResp.UsageMetadata, fmt.Errorf("invalid gemini response format")
	}

	respParts := geminiResp.Candidates[0].Content.Parts
	rawText := ""
	for i := len(respParts) - 1; i >= 0; i-- {
		if !respParts[i].Thought && strings.TrimSpace(respParts[i].Text) != "" {
			rawText = strings.TrimSpace(respParts[i].Text)
			break
		}
	}
	if rawText == "" {
		rawText = strings.TrimSpace(respParts[0].Text)
	}
	return rawText, geminiResp.UsageMetadata, nil
}

// mapChunkResults maps parsed model output back to input items by index,
// falling back to positional mapping when the model omits indexes.
func mapChunkResults(items []BatchAnalysisItem, parsed []AnalysisResultItem, usage UsageMetadata) []BatchAnalysisResult {
	byIndex := make(map[int]AnalysisResultItem, len(parsed))
	for _, r := range parsed {
		if _, exists := byIndex[r.Index]; !exists {
			byIndex[r.Index] = r
		}
	}
	positional := len(parsed) == len(items)

	var results []BatchAnalysisResult
	for i, item := range items {
		r, ok := byIndex[i]
		if !ok && positional {
			r, ok = parsed[i], true
		}
		if !ok {
			continue
		}

		score := r.Score
		if score < 0 {
			score = 0
		}
		if score > 100 {
			score = 100
		}

		resItem := BatchAnalysisResult{
			LogID:           item.LogID,
			Category:        r.Category,
			AppName:         r.AppName,
			AppCategory:     r.AppCategory,
			WindowTitle:     item.WindowTitle,
			Productive:      score >= 50,
			ProductiveScore: score,
			Confidence:      r.Confidence,
			Reason:          r.Reason,
		}
		if resItem.Category == "" {
			resItem.Category = "Unknown"
		}
		if resItem.Confidence <= 0 || resItem.Confidence > 1 {
			resItem.Confidence = 0.5
		}
		if resItem.Reason == "" {
			resItem.Reason = "AI response incomplete"
		}
		results = append(results, resItem)
	}

	if len(results) > 0 {
		results[0].Usage = usage
	}
	return results
}

// withThinkingConfig returns a copy of the payload with a thinking budget added
// to generationConfig (0 disables thinking so it cannot eat the output budget).
func withThinkingConfig(payload map[string]interface{}, budget int) map[string]interface{} {
	cloned := make(map[string]interface{}, len(payload))
	for k, v := range payload {
		cloned[k] = v
	}
	if gcRaw, ok := cloned["generationConfig"].(map[string]interface{}); ok {
		gc := make(map[string]interface{}, len(gcRaw)+1)
		for k, v := range gcRaw {
			gc[k] = v
		}
		gc["thinkingConfig"] = map[string]interface{}{"thinkingBudget": budget}
		cloned["generationConfig"] = gc
	}
	return cloned
}

// withoutResponseSchema returns a copy of the payload with responseSchema removed
// from generationConfig (for models that reject structured output schemas).
func withoutResponseSchema(payload map[string]interface{}) map[string]interface{} {
	cloned := make(map[string]interface{}, len(payload))
	for k, v := range payload {
		cloned[k] = v
	}
	if gcRaw, ok := cloned["generationConfig"].(map[string]interface{}); ok {
		gc := make(map[string]interface{}, len(gcRaw))
		for k, v := range gcRaw {
			gc[k] = v
		}
		delete(gc, "responseSchema")
		cloned["generationConfig"] = gc
	}
	return cloned
}

// mimeTypeForPath maps an image extension to a MIME type.
func mimeTypeForPath(path string) string {
	lower := strings.ToLower(path)
	switch {
	case strings.HasSuffix(lower, ".png"):
		return "image/png"
	case strings.HasSuffix(lower, ".jpg"), strings.HasSuffix(lower, ".jpeg"):
		return "image/jpeg"
	default:
		return "image/webp"
	}
}

const (
	maxItemsPerRequest = 10
	maxModelCandidates = 4
)

func (g *GeminiClient) executeAnalysis(ctx context.Context, modelName string, reqBody []byte) (*AnalysisResult, error) {
	cleanModelName := strings.TrimPrefix(modelName, "models/")
	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s", cleanModelName, g.apiKey)

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := g.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gemini request: %s", redactAPIKey(err.Error()))
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("gemini error %d: %s", resp.StatusCode, string(data))
	}

	return parseGeminiResponse(resp.Body)
}

// buildGeminiRequest constructs the Gemini API request body with JSON response enforcement.
func buildGeminiRequest(prompt, windowTitle string) map[string]interface{} {
	parts := []map[string]interface{}{
		{"text": prompt},
	}
	if windowTitle != "" {
		parts = append(parts, map[string]interface{}{
			"text": fmt.Sprintf("Window Title: %q", windowTitle),
		})
	}

	return map[string]interface{}{
		"contents": []map[string]interface{}{
			{"parts": parts},
		},
		"generationConfig": map[string]interface{}{
			"temperature":      0.1,
			"maxOutputTokens":  1536,
			"responseMimeType": "application/json",
		},
	}
}

// parseGeminiResponse extracts the AnalysisResult from the Gemini response with fallback search and sanitization.
func parseGeminiResponse(body io.Reader) (*AnalysisResult, error) {
	var resp struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text    string `json:"text"`
					Thought bool   `json:"thought"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
		UsageMetadata UsageMetadata `json:"usageMetadata"`
	}
	if err := json.NewDecoder(body).Decode(&resp); err != nil {
		return nil, fmt.Errorf("decode gemini response: %w", err)
	}
	if len(resp.Candidates) == 0 || len(resp.Candidates[0].Content.Parts) == 0 {
		return nil, fmt.Errorf("empty gemini response")
	}

	parts := resp.Candidates[0].Content.Parts
	var lastRaw string
	var lastErr error

	// Search parts (preferring non-thought parts from end to beginning)
	for i := len(parts) - 1; i >= 0; i-- {
		p := parts[i]
		if p.Thought {
			continue
		}
		rawText := strings.TrimSpace(p.Text)
		if rawText == "" {
			continue
		}
		lastRaw = rawText

		// 1. Direct unmarshal attempt
		var direct AnalysisResult
		if err := json.Unmarshal([]byte(rawText), &direct); err == nil && direct.Category != "" {
			direct.Usage = resp.UsageMetadata
			return &direct, nil
		}

		// 2. Search all JSON object substrings '{' ... '}' or attempt auto-closing broken/truncated JSON
		starts := allIndices(rawText, "{")
		for k := len(starts) - 1; k >= 0; k-- {
			start := starts[k]
			end := strings.LastIndex(rawText, "}")
			var candidate string
			if end > start {
				candidate = sanitizeJSONString(rawText[start : end+1])
			} else {
				// Attempt to auto-repair truncated JSON object missing closing brace
				candidate = sanitizeJSONString(rawText[start:]) + `"}`
			}
			var res AnalysisResult
			if err := json.Unmarshal([]byte(candidate), &res); err == nil && res.Category != "" {
				res.Usage = resp.UsageMetadata
				return &res, nil
			}
		}
		lastErr = fmt.Errorf("could not parse valid JSON object from candidate text")
	}

	if lastRaw != "" {
		return nil, fmt.Errorf("parse AI JSON (%q): %v", truncateString(lastRaw, 120), lastErr)
	}
	return nil, fmt.Errorf("empty text in gemini candidates")
}

func sanitizeJSONString(s string) string {
	s = strings.ReplaceAll(s, ", ...}", "}")
	s = strings.ReplaceAll(s, ",...}", "}")
	s = strings.ReplaceAll(s, "...", "")
	return s
}

func allIndices(s, substr string) []int {
	var indices []int
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			indices = append(indices, i)
		}
	}
	return indices
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func extractJSONArrayString(rawText string) string {
	rawText = strings.TrimSpace(rawText)
	// Strip markdown code fences if present
	if strings.Contains(rawText, "```") {
		clean := rawText
		if idx := strings.Index(clean, "```json"); idx != -1 {
			clean = clean[idx+7:]
		} else if idx := strings.Index(clean, "```"); idx != -1 {
			clean = clean[idx+3:]
		}
		if endIdx := strings.LastIndex(clean, "```"); endIdx != -1 {
			clean = clean[:endIdx]
		}
		rawText = strings.TrimSpace(clean)
	}

	start := strings.Index(rawText, "[")
	if start >= 0 {
		end := strings.LastIndex(rawText, "]")
		if end > start {
			return rawText[start : end+1]
		}
		// Truncated array missing closing bracket: attempt auto-repair by finding last complete object
		sub := rawText[start:]
		lastObj := strings.LastIndex(sub, "}")
		if lastObj != -1 {
			return sub[:lastObj+1] + "]"
		}
	}
	return rawText
}
