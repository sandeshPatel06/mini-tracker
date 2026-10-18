package ai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

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
		client:    &http.Client{Timeout: 90 * time.Second},
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
	if envModel := os.Getenv("GEMINI_MODEL"); envModel != "" {
		g.modelName = envModel
	}
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
		return nil, fmt.Errorf("list models request: %w", err)
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
		candidates = append(candidates, name)
	}

	if len(candidates) == 0 {
		return nil, fmt.Errorf("no models with generateContent support found")
	}

	// Rank models by user preference: strictly prioritize models like gemma-4-31b-it
	scoreModel := func(name string) int {
		lName := strings.ToLower(name)
		if strings.Contains(lName, "gemma-4-31b-it") {
			return 200
		}
		if strings.Contains(lName, "gemma-4") {
			return 180
		}
		if strings.Contains(lName, "gemma") {
			return 160
		}
		if strings.Contains(lName, "flash") {
			return 50
		}
		return 10
	}

	// Sort candidates descending by score
	for i := 0; i < len(candidates); i++ {
		for j := i + 1; j < len(candidates); j++ {
			if scoreModel(candidates[j]) > scoreModel(candidates[i]) {
				candidates[i], candidates[j] = candidates[j], candidates[i]
			}
		}
	}

	return candidates, nil
}

// SelectBestModel picks the environment-configured model, pre-configured model, or candidate model.
func (g *GeminiClient) SelectBestModel(ctx context.Context, exclude map[string]bool) (string, error) {
	// 1. If GEMINI_MODEL env var is set and not excluded, use it directly
	if envModel := os.Getenv("GEMINI_MODEL"); envModel != "" {
		if exclude == nil || !exclude[envModel] {
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
		if exclude == nil || !exclude[current] {
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
			g.mu.Lock()
			g.modelName = m
			g.mu.Unlock()
			log.Printf("[ai/gemini] Auto-selected model: %s (from API ListModels)", m)
			return m, nil
		}
	} else {
		log.Printf("[ai/gemini] Model listing warning: %v — falling back to candidate list", err)
	}

	// 4. Fallback candidates prioritizing gemma models
	fallbacks := []string{
		"gemma-4-31b-it",
		"gemma-2-27b-it",
	}

	for _, m := range fallbacks {
		if exclude != nil && exclude[m] {
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
// Returns partial results together with the last error when some chunks fail.
func (g *GeminiClient) AnalyzeBatch(ctx context.Context, items []BatchAnalysisItem) ([]BatchAnalysisResult, error) {
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
		results, err := g.analyzeChunk(ctx, items[start:end])
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
func (g *GeminiClient) analyzeChunk(ctx context.Context, items []BatchAnalysisItem) ([]BatchAnalysisResult, error) {
	activeTask := ""
	for _, item := range items {
		if strings.TrimSpace(item.UserTask) != "" {
			activeTask = strings.TrimSpace(item.UserTask)
			break
		}
	}

	promptItems := make([]PromptItem, len(items))
	for i, item := range items {
		promptItems[i] = PromptItem{
			LogID:        item.LogID,
			WindowTitle:  item.WindowTitle,
			EntropyScore: item.EntropyScore,
			TotalClicks:  item.TotalClicks,
			AppName:      item.AppName,
			Timestamp:    item.Timestamp,
		}
	}

	parts := []map[string]interface{}{
		{"text": BuildAnalysisPrompt(promptItems, activeTask)},
	}

	dataDir := g.GetDataDir()
	for i, item := range items {
		imgPath := item.ImagePath
		if imgPath == "" {
			continue
		}
		if dataDir != "" {
			imgPath = ThumbnailForImage(imgPath, dataDir)
		}
		data, err := os.ReadFile(imgPath)
		if err != nil {
			continue
		}
		parts = append(parts,
			map[string]interface{}{"text": fmt.Sprintf("IMAGE %d:", i)},
			map[string]interface{}{
				"inlineData": map[string]interface{}{
					"mimeType": mimeTypeForPath(imgPath),
					"data":     base64.StdEncoding.EncodeToString(data),
				},
			},
		)
	}

	maxOutputTokens := 128 + 96*len(items)
	if maxOutputTokens > 2048 {
		maxOutputTokens = 2048
	}

	reqPayload := map[string]interface{}{
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

	exclude := map[string]bool{}
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

		rawText, usage, err := g.postGenerateContentWithRetry(ctx, model, reqPayload)
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

		log.Printf("[ai/gemini] analyzed %d items with model %s (tokens: prompt=%d output=%d)", len(items), model, usage.PromptTokenCount, usage.CandidatesTokenCount)
		return mapChunkResults(items, parsed, usage), nil
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("no working Gemini models available")
	}
	return nil, lastErr
}

// postGenerateContentWithRetry performs a single generateContent call with retry,
// honoring Retry-After and backing off on 429/5xx/network errors. If the model
// rejects responseSchema with a 400, the payload is retried once without it.
func (g *GeminiClient) postGenerateContentWithRetry(ctx context.Context, model string, reqPayload map[string]interface{}) (string, UsageMetadata, error) {
	cleanModel := strings.TrimPrefix(model, "models/")
	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s", cleanModel, g.apiKey)

	payload := reqPayload
	schemaDropped := false
	var rawText string
	var usage UsageMetadata

	policy := RetryPolicy{MaxAttempts: 4, BaseDelay: time.Second, MaxDelay: 20 * time.Second}
	err := DoWithRetry(ctx, policy, func(ctx context.Context) (error, bool, time.Duration) {
		reqBody, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err), false, 0
		}

		req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(reqBody))
		if err != nil {
			return err, false, 0
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := g.client.Do(req)
		if err != nil {
			return fmt.Errorf("gemini request: %w", err), true, 0
		}
		defer resp.Body.Close()

		respData, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))

		if resp.StatusCode == http.StatusOK {
			text, meta, perr := extractResponseText(respData)
			if perr != nil {
				return perr, false, 0
			}
			rawText, usage = text, meta
			return nil, false, 0
		}

		if resp.StatusCode == http.StatusBadRequest && !schemaDropped &&
			bytes.Contains(respData, []byte("schema")) {
			schemaDropped = true
			payload = withoutResponseSchema(reqPayload)
			return fmt.Errorf("gemini 400 (schema unsupported): %s", truncateString(string(respData), 200)), true, 0
		}

		return fmt.Errorf("gemini error %d: %s", resp.StatusCode, truncateString(string(respData), 300)),
			IsRetryableStatus(resp.StatusCode),
			RetryAfterFromHeader(resp.Header)
	})
	if err != nil {
		return "", UsageMetadata{}, err
	}
	return rawText, usage, nil
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
	maxModelCandidates = 3
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
		return nil, fmt.Errorf("gemini request: %w", err)
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
