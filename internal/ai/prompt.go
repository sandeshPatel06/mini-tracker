package ai

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// PromptItem is a single window-activity item embedded into an analysis prompt.
type PromptItem struct {
	LogID        int64
	WindowTitle  string
	EntropyScore float64
	TotalClicks  int
	AppName      string
	Timestamp    time.Time
}

// AnalysisCategories is the fixed set of activity categories the model may output.
var AnalysisCategories = []string{
	"Coding",
	"Writing",
	"Browsing",
	"Document Editing",
	"Communication",
	"Design",
	"Social Media",
	"Video/Entertainment",
	"Idle",
	"Other",
}

var (
	titleURLPattern   = regexp.MustCompile(`https?://\S+`)
	titleEmailPattern = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)
)

// SanitizeTitle strips http(s) URLs and emails, collapses whitespace, trims, and
// truncates rune-safely to maxChars with "…".
func SanitizeTitle(title string, maxChars int) string {
	if maxChars <= 0 {
		return ""
	}
	clean := titleURLPattern.ReplaceAllString(title, " ")
	clean = titleEmailPattern.ReplaceAllString(clean, " ")
	clean = strings.Join(strings.Fields(clean), " ")
	runes := []rune(clean)
	if len(runes) <= maxChars {
		return clean
	}
	return string(runes[:maxChars]) + "…"
}

type promptItemJSON struct {
	I   int         `json:"i"`
	T   string      `json:"t"`
	E   json.Number `json:"e"`
	C   int         `json:"c"`
	A   string      `json:"a"`
	TS  string      `json:"ts"`
	Img bool        `json:"img"`
}

const (
	promptTaskMaxRunes = 160
	promptTaskMaxWords = 16
)

func sanitizePromptTask(task string) string {
	clean := SanitizeTitle(task, promptTaskMaxRunes)
	words := strings.Fields(clean)
	if len(words) > promptTaskMaxWords {
		clean = strings.Join(words[:promptTaskMaxWords], " ") + "…"
	}
	return clean
}

// BuildAnalysisPrompt returns a compact prompt embedding items as a JSON array with
// keys i,t,e,c,a,ts,img, the JSON output contract, and screenshot ordering rules.
func BuildAnalysisPrompt(items []PromptItem, activeTask string) string {
	encoded := make([]promptItemJSON, 0, len(items))
	for i, item := range items {
		entropy := math.Round(item.EntropyScore*10) / 10
		if math.IsNaN(entropy) || math.IsInf(entropy, 0) {
			entropy = 0
		}
		encoded = append(encoded, promptItemJSON{
			I:   i,
			T:   SanitizeTitle(item.WindowTitle, 100),
			E:   json.Number(strconv.FormatFloat(entropy, 'f', 1, 64)),
			C:   item.TotalClicks,
			A:   item.AppName,
			TS:  item.Timestamp.Format("15:04"),
			Img: true,
		})
	}
	itemsJSON, err := json.Marshal(encoded)
	if err != nil {
		itemsJSON = []byte("[]")
	}

	taskLine := "TASK CONTEXT: No active task provided; assess general work relevance."
	if task := sanitizePromptTask(activeTask); task != "" {
		taskLine = fmt.Sprintf("TASK CONTEXT: The active user task is %q; score each item by how directly it advances this task.", task)
	}

	var b strings.Builder
	b.WriteString("You are a desktop activity productivity analyst. Classify each item and score its productivity.\n\n")
	fmt.Fprintf(&b, "JSON OUTPUT CONTRACT: Respond ONLY with a valid JSON array, no markdown, no prose. One object per item in exact input order with keys: i (integer index), cat (one of: %s), a (app name), ac (app category), p (productivity score 0-100), c (confidence 0.0-1.0), r (reason, at most 12 words).\n\n", strings.Join(AnalysisCategories, ", "))
	b.WriteString("SCORING RUBRIC: focused on-task work 70-100; related docs or research 45-70; neutral browsing or messaging 25-50; leisure, social media, or idle 0-25. Keystroke entropy e>20 adds 10; e<8 subtracts 10; clicks c>20 adds 5. Never default to 100.\n\n")
	b.WriteString("SCREENSHOTS: Each screenshot follows in item order, preceded by a text part \"IMAGE <i>:\" where i is the item index; use image and telemetry together.\n\n")
	b.WriteString(taskLine)
	b.WriteString("\n\nITEMS: ")
	b.WriteString(string(itemsJSON))
	return b.String()
}

// AnalysisResponseSchema returns the OpenAPI-subset responseSchema for a JSON array
// of analysis result objects.
func AnalysisResponseSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "ARRAY",
		"items": map[string]interface{}{
			"type": "OBJECT",
			"properties": map[string]interface{}{
				"i":   map[string]interface{}{"type": "INTEGER"},
				"cat": map[string]interface{}{"type": "STRING", "enum": AnalysisCategories},
				"a":   map[string]interface{}{"type": "STRING"},
				"ac":  map[string]interface{}{"type": "STRING"},
				"p":   map[string]interface{}{"type": "INTEGER"},
				"c":   map[string]interface{}{"type": "NUMBER"},
				"r":   map[string]interface{}{"type": "STRING"},
			},
			"required": []string{"i", "cat", "p"},
		},
	}
}

// AnalysisResultItem is a single parsed analysis result.
type AnalysisResultItem struct {
	Index       int     `json:"i"`
	Category    string  `json:"cat"`
	AppName     string  `json:"a"`
	AppCategory string  `json:"ac"`
	Title       string  `json:"t"`
	Score       float64 `json:"p"`
	Confidence  float64 `json:"c"`
	Reason      string  `json:"r"`
}

// ParseAnalysisJSON extracts the JSON array (fences/truncation handled) and unmarshals;
// error if none. When the whole array fails to parse (e.g. truncated output), valid
// complete objects are salvaged individually so partial results are not discarded.
func ParseAnalysisJSON(raw string, expectedItems int) ([]AnalysisResultItem, error) {
	arrayStr := sanitizeJSONString(extractJSONArrayString(raw))
	var results []AnalysisResultItem
	if err := json.Unmarshal([]byte(arrayStr), &results); err != nil {
		salvaged := salvageAnalysisObjects(arrayStr)
		if len(salvaged) == 0 {
			return nil, fmt.Errorf("parse analysis JSON (expected %d items, got %q): %w", expectedItems, truncateString(arrayStr, 120), err)
		}
		results = salvaged
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("parse analysis JSON: empty array (expected %d items)", expectedItems)
	}
	return results, nil
}

// salvageAnalysisObjects decodes as many complete analysis objects as possible from
// a possibly truncated array, stopping at the first malformed object boundary.
func salvageAnalysisObjects(raw string) []AnalysisResultItem {
	if !strings.Contains(raw, "{") {
		return nil
	}
	var out []AnalysisResultItem
	dec := json.NewDecoder(strings.NewReader(raw))
	for {
		var item AnalysisResultItem
		if err := dec.Decode(&item); err != nil {
			break
		}
		out = append(out, item)
	}
	return out
}
