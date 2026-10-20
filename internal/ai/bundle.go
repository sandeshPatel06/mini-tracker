package ai

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
)

// SessionBundleResult is the single score the model returns for one 5-minute
// bundle of consecutive capture items.
type SessionBundleResult struct {
	Score       float64 `json:"score"`
	Category    string  `json:"category"`
	AppName     string  `json:"app_name"`
	AppCategory string  `json:"app_category"`
	Confidence  float64 `json:"confidence"`
	Reason      string  `json:"reason"`
}

// bundleInstructions returns the stable instructions for bundle scoring. The
// text only depends on the shape of the request (item count, task), which makes
// it a good candidate for a Gemini cached content shared by every bundle.
func bundleInstructions(itemCount int, activeTask string) string {
	taskLine := "TASK CONTEXT: No active task provided; assess general work relevance."
	if task := sanitizePromptTask(activeTask); task != "" {
		taskLine = fmt.Sprintf("TASK CONTEXT: The active user task is %q; judge how directly this window advances it.", task)
	}

	var b strings.Builder
	b.WriteString("You are a desktop activity analyst. Score ONE work-session bundle: consecutive captures of a single 5-minute window.\n\n")
	fmt.Fprintf(&b, "JSON OUTPUT CONTRACT (STRICT):\n1. Exactly one JSON object covering all %d items, no per-item output.\n2. First char '{' and last char '}'.\n3. Compact single-line JSON. No markdown, fences, prose, comments, trailing commas, or text outside the object. Never stop mid-object; shorten the reason instead.\n4. Keys: \"score\" (0-100 for the whole window), \"category\" (one of: %s), \"app_name\" (dominant app in the window), \"app_category\", \"confidence\" 0.0-1.0, \"reason\" max 12 words.\n\n", itemCount, strings.Join(AnalysisCategories, ", "))
	b.WriteString("SCORING RUBRIC: focused on-task work 70-100; related docs/research 45-70; neutral browsing/messaging 25-50; leisure/idle 0-25. Rapid app toggling between work and chat lowers the score; sustained work in one app raises it. Entropy and click counts are weak signals only. Never default to 100.\n\n")
	b.WriteString("SCREENSHOTS: each img=true item is followed by \"IMAGE <i>\" in input order. img=false = window unchanged since the previous item — reuse its screenshot; never claim new visual details for it.\n\n")
	b.WriteString(taskLine)
	return b.String()
}

// BuildBundlePromptSplit returns the stable bundle instructions separately from
// the dynamic window/session/items text so the stable half can be cached.
func BuildBundlePromptSplit(items []PromptItem, activeTask, windowLabel string, sessionEvents ...SessionEvent) (staticText, dynamicText string) {
	staticText = bundleInstructions(len(items), activeTask)
	var b strings.Builder
	if label := strings.TrimSpace(windowLabel); label != "" {
		fmt.Fprintf(&b, "WINDOW: %s\n\n", label)
	}
	b.WriteString(sessionContextText(sessionEvents))
	b.WriteString("ITEMS: ")
	b.WriteString(encodePromptItems(items))
	return staticText, b.String()
}

// BundleResponseSchema returns the responseSchema for one bundle score object.
func BundleResponseSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "OBJECT",
		"properties": map[string]interface{}{
			"score":        map[string]interface{}{"type": "INTEGER"},
			"category":     map[string]interface{}{"type": "STRING", "enum": AnalysisCategories},
			"app_name":     map[string]interface{}{"type": "STRING"},
			"app_category": map[string]interface{}{"type": "STRING"},
			"confidence":   map[string]interface{}{"type": "NUMBER"},
			"reason":       map[string]interface{}{"type": "STRING"},
		},
		"required": []string{"score", "category", "reason"},
	}
}

// ParseBundleJSON extracts and validates the single JSON object answer.
func ParseBundleJSON(raw string) (*SessionBundleResult, error) {
	text := strings.TrimSpace(raw)
	if strings.Contains(text, "```") {
		if idx := strings.Index(text, "```json"); idx != -1 {
			text = text[idx+7:]
		} else if idx := strings.Index(text, "```"); idx != -1 {
			text = text[idx+3:]
		}
		if end := strings.LastIndex(text, "```"); end != -1 {
			text = text[:end]
		}
	}
	text = sanitizeJSONString(strings.TrimSpace(text))
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start < 0 || end <= start {
		return nil, fmt.Errorf("parse bundle JSON: no object in %q", truncateString(text, 120))
	}

	var result SessionBundleResult
	if err := json.Unmarshal([]byte(text[start:end+1]), &result); err != nil {
		return nil, fmt.Errorf("parse bundle JSON: %w", err)
	}
	if result.Score < 0 {
		result.Score = 0
	}
	if result.Score > 100 {
		result.Score = 100
	}
	if strings.TrimSpace(result.Category) == "" {
		result.Category = "Other"
	}
	if strings.TrimSpace(result.Reason) == "" {
		result.Reason = "no reason provided"
	}
	if result.Confidence <= 0 || result.Confidence > 1 {
		result.Confidence = 0.5
	}
	return &result, nil
}

// buildImageParts renders inline thumbnail payloads for the prompt items that
// kept their screenshot.
func buildImageParts(promptItems []PromptItem, dataDir string) []map[string]interface{} {
	imageParts := make([]map[string]interface{}, 0, len(promptItems)*2)
	for i := range promptItems {
		imgPath := promptItems[i].ImagePath
		if !promptItems[i].HasImage || imgPath == "" {
			continue
		}
		if dataDir != "" {
			imgPath = ThumbnailForImage(imgPath, dataDir)
		}
		data, err := os.ReadFile(imgPath)
		if err != nil {
			continue
		}
		imageParts = append(imageParts,
			map[string]interface{}{"text": fmt.Sprintf("IMAGE %d:", i)},
			map[string]interface{}{
				"inlineData": map[string]interface{}{
					"mimeType": mimeTypeForPath(imgPath),
					"data":     base64.StdEncoding.EncodeToString(data),
				},
			},
		)
	}
	return imageParts
}

// AnalyzeSessionBundle scores one 5-minute bundle with a single request: the
// static instructions go through the Gemini context cache (when available),
// the items/session text is sent as the dynamic part, and the model returns one
// score object for the whole window. Model failover and the payload ladder
// behave exactly like AnalyzeBatch.
func (g *GeminiClient) AnalyzeSessionBundle(ctx context.Context, items []BatchAnalysisItem, windowLabel string, sessionEvents ...SessionEvent) (*SessionBundleResult, UsageMetadata, error) {
	if !g.HasKey() || len(items) == 0 {
		return nil, UsageMetadata{}, fmt.Errorf("no API key set or empty bundle")
	}

	activeTask := ""
	for _, item := range items {
		if strings.TrimSpace(item.UserTask) != "" {
			activeTask = strings.TrimSpace(item.UserTask)
			break
		}
	}

	promptItems := buildPromptItems(items)
	staticPrompt, dynamicPrompt := BuildBundlePromptSplit(promptItems, activeTask, windowLabel, sessionEvents...)
	fullPrompt := staticPrompt + dynamicPrompt
	imageParts := buildImageParts(promptItems, g.GetDataDir())

	maxOutputTokens := 512

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
				"responseSchema":   BundleResponseSchema(),
			},
		}
		if cacheName != "" {
			payload["cachedContent"] = cacheName
		}
		return payload
	}
	uncachedPayload := buildPayload("")

	exclude := map[string]bool{}
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
			log.Printf("[ai/gemini] cached context rejected for %s (%v) — retrying bundle without cache", model, err)
			rawText, usage, err = g.postGenerateContentWithRetry(ctx, model, uncachedPayload)
		}
		if err != nil {
			lastErr = err
			log.Printf("[ai/gemini] bundle model %s failed: %v — trying next model", model, err)
			continue
		}

		result, err := ParseBundleJSON(rawText)
		if err != nil {
			lastErr = err
			log.Printf("[ai/gemini] bundle model %s returned unparsable output: %v — trying next model", model, err)
			continue
		}

		g.markModelHealthy(model)
		log.Printf("[ai/gemini] scored %d-item bundle with model %s: %.0f%% %s (tokens: prompt=%d output=%d)", len(items), model, result.Score, result.Category, usage.PromptTokenCount, usage.CandidatesTokenCount)
		return result, usage, nil
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("no working Gemini models available")
	}
	return nil, UsageMetadata{}, lastErr
}
