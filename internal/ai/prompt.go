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
	ImagePath    string
	HasImage     bool
}

// SessionEvent is one observed application lifecycle event ("open", "close",
// "focus") provided to the analysis prompt so the model sees the broader
// app-switch narrative instead of isolated one-minute snapshots.
type SessionEvent struct {
	Time  string `json:"time"`
	Kind  string `json:"kind"`
	App   string `json:"app"`
	Title string `json:"title"`
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
// keys i,t,e,c,a,ts,img, the JSON output contract, screenshot ordering rules, and
// (optionally) a chronological SESSION CONTEXT of app lifecycle events.
func BuildAnalysisPrompt(items []PromptItem, activeTask string, sessionEvents ...SessionEvent) string {
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
			Img: item.HasImage,
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
	b.WriteString("You are a desktop activity analyst. Classify and score each item.\n\n")
	fmt.Fprintf(&b, "JSON OUTPUT CONTRACT (STRICT):\n1. Exactly %d objects, one per item, exact input order.\n2. First char '[' and last char ']'.\n3. Compact single-line JSON. No markdown, fences, prose, comments, trailing commas, or text outside the array.\n4. Never stop mid-array; shorten reasons instead.\n5. Keys: \"i\" (input index), \"cat\" (one of: %s), \"a\", \"ac\", \"p\" 0-100, \"c\" 0.0-1.0, \"r\" max 10 words.\n\n", len(items), strings.Join(AnalysisCategories, ", "))
	b.WriteString("SCORING RUBRIC: on-task 70-100; related docs/research 45-70; neutral browsing/messaging 25-50; leisure/idle 0-25. Entropy e>20 adds 10; e<8 subtracts 10; clicks c>20 adds 5. Never default to 100.\n\n")
	b.WriteString("SCREENSHOTS: each img=true item is followed by \"IMAGE <i>\" in input order. img=false = window unchanged since the previous item — reuse its screenshot; never claim new visual details for it.\n\n")
	b.WriteString(taskLine)

	if len(sessionEvents) > 0 {
		b.WriteString("SESSION CONTEXT (chronological app activity): ")
		for _, ev := range sessionEvents {
			fmt.Fprintf(&b, "%s %s %s", ev.Time, ev.Kind, SanitizeTitle(ev.App, 40))
			if title := SanitizeTitle(ev.Title, 40); title != "" {
				fmt.Fprintf(&b, " (%s)", title)
			}
			b.WriteString("; ")
		}
		b.WriteString("judge app-switch cadence with it (rapid toggling between a work app and chat = notification checks; sustained single app = focused work; app opened right before a stretch = likely the main task).\n\n")
	}

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

// salvageAnalysisObjects scans a possibly truncated JSON array and decodes every
// complete analysis object it can find, skipping malformed fragments instead of
// stopping at the first broken boundary. Objects behind a truncated fragment are
// still recovered so maximal partial results are left for later retries.
func salvageAnalysisObjects(raw string) []AnalysisResultItem {
	if !strings.Contains(raw, "{") {
		return nil
	}

	inString := false
	escaped := false
	depth := 0
	var objStart = -1

	var out []AnalysisResultItem
	appendCandidate := func(candidate string) {
		var item AnalysisResultItem
		if err := json.Unmarshal([]byte(candidate), &item); err != nil {
			return
		}
		if item.Category == "" && item.AppName == "" && item.Reason == "" && item.Score == 0 {
			return
		}
		for _, existing := range out {
			if existing.Index == item.Index {
				return
			}
		}
		out = append(out, item)
	}

	for i := 0; i < len(raw); i++ {
		ch := raw[i]
		if inString {
			if escaped {
				escaped = false
			} else if ch == '\\' {
				escaped = true
			} else if ch == '"' {
				inString = false
			}
			continue
		}
		switch ch {
		case '"':
			inString = true
		case '{':
			if depth == 0 {
				objStart = i
			}
			depth++
		case '}':
			if depth == 0 {
				continue
			}
			depth--
			if depth == 0 && objStart >= 0 {
				appendCandidate(raw[objStart : i+1])
				objStart = -1
			}
		}
	}
	return out
}
