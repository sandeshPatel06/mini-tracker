package ai

import (
	"bytes"
	"context"
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
		client:    &http.Client{Timeout: 60 * time.Second},
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
	g.modelName = os.Getenv("GEMINI_MODEL")
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

	// Rank models by cost/efficiency/capability (preferring Flash models for 1-2s response times)
	scoreModel := func(name string) int {
		lName := strings.ToLower(name)
		if strings.Contains(lName, "gemma-4-31b-it") || strings.Contains(lName, "2.5-flash") {
			return 150
		}
		if strings.Contains(lName, "gemini-2.0-flash") || strings.Contains(lName, "2.0-flash") {
			return 140
		}
		if strings.Contains(lName, "gemini-1.5-flash") || strings.Contains(lName, "1.5-flash") {
			return 130
		}
		if strings.Contains(lName, "flash") {
			return 120
		}
		if strings.Contains(lName, "gemma-4-31b-it") || strings.Contains(lName, "gemma-4") {
			return 20
		}
		if strings.Contains(lName, "gemma") {
			return 10
		}
		return 5
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

// SelectBestModel picks the environment-configured model, pre-configured model, or cheapest available model via discovery.
func (g *GeminiClient) SelectBestModel(ctx context.Context, exclude map[string]bool) (string, error) {
	// 1. If GEMINI_MODEL env var is set and not excluded, use it directly without API lookup
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

	// 4. Fallback candidates if API listing fails or returns no unexcluded models
	fallbacks := []string{
		"gemma-4-31b-it",
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

// Analyze sends a single screenshot and telemetry to Gemini by wrapping it in AnalyzeBatch.
func (g *GeminiClient) Analyze(ctx context.Context, base64Image string, entropyScore float64) (*AnalysisResult, error) {
	batchResults, err := g.AnalyzeBatch(ctx, []BatchAnalysisItem{
		{
			Base64Image:  base64Image,
			EntropyScore: entropyScore,
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
	LogID         int64
	Base64Image   string
	EntropyScore  float64
	ImagePath     string
	TotalClicks   int
	MouseDistance float64
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

// AnalyzeBatch sends multiple screenshots in a single Gemini API request.
func (g *GeminiClient) AnalyzeBatch(ctx context.Context, items []BatchAnalysisItem) ([]BatchAnalysisResult, error) {
	if !g.HasKey() || len(items) == 0 {
		return nil, fmt.Errorf("no API key set or empty batch")
	}

	// Build per-item telemetry summary for the prompt
	itemSummaries := ""
	for i, item := range items {
		itemSummaries += fmt.Sprintf("  Item %d: Keystroke Entropy=%.1f/100, Mouse Clicks=%d, Mouse Distance=%.0fpx\n",
			i+1, item.EntropyScore, item.TotalClicks, item.MouseDistance)
	}

	prompt := fmt.Sprintf(`You are a world-class developer productivity analyst inspecting a sequence of %d desktop screenshots captured from a Linux workstation in chronological order.

TELEMETRY CONTEXT (use this data to calibrate scores — do NOT ignore it):
%s

INSTRUCTIONS FOR EACH SCREENSHOT ITEM:
1. Multi-Monitor Grid: Each image may show multiple monitors side-by-side. Inspect ALL visible content.
2. Identify the primary application (app_name, e.g. VS Code, Terminal, Chrome, Slack, Spotify).
3. Identify the active window title or file path visible (window_title).
4. Classify the app_category (e.g. IDE / Code Editor, Terminal / CLI, Web Browser, Communication, Entertainment).
5. Select the primary category from: [Coding, Writing, Browsing, Document Editing, Communication, Social Media, Video/Entertainment, Idle, Other].
6. Compute productivity_score (integer 0-100) based on ALL signals below — NEVER default to 100 without evidence:
   - Keystroke Entropy > 20: Active typing — strong coding/writing indicator (+30 to score)
   - Keystroke Entropy 8-20: Moderate typing — reviewing, debugging (+15 to score)
   - Keystroke Entropy < 8: Reading/idle mode — reduces score unless heavy mouse usage
   - Mouse Distance > 5000px: Active navigation/design work (+10 to score)
   - Mouse Clicks > 20: Interactive workflow (+5 to score)
   - Visual context: Code editor with diff/code visible = 85-100; IDE with terminal = 80-95;
     browser on technical docs = 60-80; social media/YouTube = 0-25; idle desktop = 0-15.
   - Combined low entropy + low mouse + idle screen = score 0-20.
7. Set is_productive=true only if productivity_score >= 50.

Respond ONLY with a valid JSON array of %d objects in EXACT input order:
[
`, len(items), itemSummaries, len(items))

	for i, item := range items {
		prompt += fmt.Sprintf(`  {"item_index": %d, "app_name": "<detected app>", "app_category": "<detected category>", "window_title": "<detected title>", "category": "<category>", "productivity_score": <0-100 integer>, "is_productive": <true/false>, "confidence": <0.0-1.0>, "reason": "<1-sentence reason citing entropy %.1f and visible context>"}%s
`, i+1, item.EntropyScore, func() string {
			if i < len(items)-1 {
				return ","
			}
			return ""
		}())
	}
	prompt += `]`

	parts := []map[string]interface{}{
		{"text": prompt},
	}

	for i, item := range items {
		parts = append(parts, map[string]interface{}{
			"text": fmt.Sprintf("[Item %d | Entropy: %.1f/100 | Clicks: %d | Mouse Distance: %.0fpx]", i+1, item.EntropyScore, item.TotalClicks, item.MouseDistance),
		})
		if item.Base64Image != "" {
			parts = append(parts, map[string]interface{}{
				"inline_data": map[string]interface{}{
					"mime_type": "image/webp",
					"data":      item.Base64Image,
				},
			})
		}
	}

	reqPayload := map[string]interface{}{
		"contents": []map[string]interface{}{
			{"parts": parts},
		},
		"generationConfig": map[string]interface{}{
			"temperature":      0.15,
			"maxOutputTokens":  2048,
			"responseMimeType": "application/json",
		},
	}

	reqBody, err := json.Marshal(reqPayload)
	if err != nil {
		return nil, fmt.Errorf("marshal batch request: %w", err)
	}

	model := g.GetModel()
	if model == "" {
		m, err := g.SelectBestModel(ctx, nil)
		if err != nil {
			return nil, err
		}
		model = m
	}

	cleanModel := strings.TrimPrefix(model, "models/")
	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s", cleanModel, g.apiKey)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := g.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gemini batch request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("gemini batch error %d: %s", resp.StatusCode, string(data))
	}

	var geminiResp struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
		UsageMetadata UsageMetadata `json:"usageMetadata"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&geminiResp); err != nil || len(geminiResp.Candidates) == 0 || len(geminiResp.Candidates[0].Content.Parts) == 0 {
		return nil, fmt.Errorf("invalid gemini batch response format")
	}

	rawText := strings.TrimSpace(geminiResp.Candidates[0].Content.Parts[0].Text)
	jsonArrayStr := extractJSONArrayString(rawText)
	var rawResults []struct {
		ItemIndex         int     `json:"item_index"`
		AppName           string  `json:"app_name"`
		AppCategory       string  `json:"app_category"`
		WindowTitle       string  `json:"window_title"`
		Category          string  `json:"category"`
		ProductivityScore float64 `json:"productivity_score"`
		ProductiveScore   float64 `json:"productive_score"`
		IsProductive      bool    `json:"is_productive"`
		Productive        bool    `json:"productive"`
		Confidence        float64 `json:"confidence"`
		Reason            string  `json:"reason"`
	}

	if err := json.Unmarshal([]byte(jsonArrayStr), &rawResults); err != nil {
		return nil, fmt.Errorf("parse batch JSON (%q): %w", truncateString(rawText, 100), err)
	}

	var results []BatchAnalysisResult
	for i, item := range items {
		// Default: derive a conservative score from entropy rather than hard-coding 95%
		defaultScore := 0.0
		if item.EntropyScore > 20 {
			defaultScore = 70.0
		} else if item.EntropyScore > 8 {
			defaultScore = 40.0
		}
		resItem := BatchAnalysisResult{
			LogID:           item.LogID,
			Category:        "Unknown",
			Productive:      defaultScore >= 50,
			ProductiveScore: defaultScore,
			Confidence:      0.5,
			Reason:          "AI partial analysis — fallback based on entropy",
			Usage:           geminiResp.UsageMetadata,
		}
		if i < len(rawResults) {
			r := rawResults[i]
			if r.Category != "" {
				resItem.Category = r.Category
			}
			if r.AppName != "" {
				resItem.AppName = r.AppName
			}
			if r.AppCategory != "" {
				resItem.AppCategory = r.AppCategory
			}
			if r.WindowTitle != "" {
				resItem.WindowTitle = r.WindowTitle
			}
			score := r.ProductivityScore
			if score == 0 {
				score = r.ProductiveScore
			}
			// Only accept AI score if it's non-zero and not suspiciously round-100 with zero entropy
			if score > 0 && !(score == 100 && item.EntropyScore < 5 && item.TotalClicks < 3) {
				resItem.ProductiveScore = score
			}
			resItem.Productive = r.IsProductive || r.Productive || resItem.ProductiveScore >= 50
			if r.Confidence > 0 {
				resItem.Confidence = r.Confidence
			}
			if r.Reason != "" {
				resItem.Reason = r.Reason
			}
		}
		results = append(results, resItem)
	}

	return results, nil
}

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
func buildGeminiRequest(prompt, base64Image string) map[string]interface{} {
	parts := []map[string]interface{}{
		{"text": prompt},
	}
	if base64Image != "" {
		parts = append(parts, map[string]interface{}{
			"inline_data": map[string]interface{}{
				"mime_type": "image/webp",
				"data":      base64Image,
			},
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
	if strings.HasPrefix(rawText, "```") {
		lines := strings.Split(rawText, "\n")
		if len(lines) >= 2 {
			if strings.HasPrefix(lines[0], "```") {
				lines = lines[1:]
			}
			if len(lines) > 0 && strings.HasPrefix(lines[len(lines)-1], "```") {
				lines = lines[:len(lines)-1]
			}
			rawText = strings.Join(lines, "\n")
		}
	}
	rawText = strings.TrimSpace(rawText)

	start := strings.Index(rawText, "[")
	end := strings.LastIndex(rawText, "]")
	if start >= 0 && end > start {
		return rawText[start : end+1]
	}
	return rawText
}
