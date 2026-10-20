package ai

import (
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestSanitizeTitle(t *testing.T) {
	tests := []struct {
		name  string
		title string
		max   int
		want  string
	}{
		{"strips http url", "Check https://example.com/path?q=1 now", 100, "Check now"},
		{"strips bare url", "https://example.com/path?q=1", 100, ""},
		{"strips email", "Mail user@example.com today", 100, "Mail today"},
		{"collapses whitespace", "a \n\t b", 100, "a b"},
		{"truncates to maxChars", strings.Repeat("x", 300), 100, strings.Repeat("x", 100) + "…"},
		{"keeps short title", "short", 100, "short"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := SanitizeTitle(tc.title, tc.max); got != tc.want {
				t.Fatalf("SanitizeTitle(%q, %d) = %q, want %q", tc.title, tc.max, got, tc.want)
			}
		})
	}
}

func TestSanitizeTitleUnicodeSafe(t *testing.T) {
	input := strings.Repeat("日本語", 200)
	got := SanitizeTitle(input, 100)
	if !utf8.ValidString(got) {
		t.Fatalf("SanitizeTitle produced invalid UTF-8: %q", got)
	}
	if n := utf8.RuneCountInString(got); n != 101 {
		t.Fatalf("rune count = %d, want 101 (100 runes + ellipsis)", n)
	}
	if want := string([]rune(input)[:100]) + "…"; got != want {
		t.Fatalf("unicode truncation = %q, want %q", got, want)
	}
}

func TestBuildAnalysisPrompt(t *testing.T) {
	base := time.Date(2026, 10, 18, 9, 5, 0, 0, time.UTC)
	items := []PromptItem{
		{LogID: 1, WindowTitle: "main.go - Visual Studio Code", EntropyScore: 12.34, TotalClicks: 25, AppName: "VS Code", Timestamp: base, HasImage: true},
		{LogID: 2, WindowTitle: "user@example.com - Inbox", EntropyScore: 4.06, TotalClicks: 3, AppName: "Mail", Timestamp: base.Add(5 * time.Minute), HasImage: true},
		{LogID: 3, WindowTitle: "See https://news.example.com/x now", EntropyScore: 0, TotalClicks: 1, AppName: "Chrome", Timestamp: base.Add(10 * time.Minute), HasImage: true},
	}

	got := BuildAnalysisPrompt(items, "Finish the quarterly report")

	if !strings.Contains(got, "ITEMS:") {
		t.Fatalf("prompt missing ITEMS section")
	}
	i0 := strings.Index(got, `"i":0`)
	i1 := strings.Index(got, `"i":1`)
	i2 := strings.Index(got, `"i":2`)
	if i0 == -1 || i1 == -1 || i2 == -1 || !(i0 < i1 && i1 < i2) {
		t.Fatalf("items not serialized in order with indexes 0,1,2: %q", got)
	}
	if !strings.Contains(got, "IMAGE") {
		t.Errorf("prompt missing IMAGE screenshot contract")
	}
	if !strings.Contains(got, `"t":"main.go - Visual Studio Code"`) {
		t.Errorf("prompt missing sanitized title for item 0")
	}
	if !strings.Contains(got, `"t":"- Inbox"`) {
		t.Errorf("prompt missing sanitized title for item 1")
	}
	if !strings.Contains(got, `"t":"See now"`) {
		t.Errorf("prompt missing sanitized title for item 2")
	}
	if !strings.Contains(got, `"e":12.3`) {
		t.Errorf("entropy not rounded to one decimal")
	}
	if !strings.Contains(got, `"ts":"09:05"`) {
		t.Errorf("timestamp not formatted as HH:MM")
	}
	if !strings.Contains(got, `"img":true`) {
		t.Errorf("img flag missing")
	}
	if strings.Contains(got, "user@example.com") || strings.Contains(got, "https://news.example.com") {
		t.Errorf("prompt leaked raw URL or email")
	}
	if !strings.Contains(got, "Finish the quarterly report") {
		t.Errorf("prompt missing active task")
	}

	withoutTask := BuildAnalysisPrompt(items, "")
	if !strings.Contains(withoutTask, "No active task provided") {
		t.Errorf("prompt missing absent-task message")
	}
}

func TestBuildAnalysisPromptInstructionWordLimit(t *testing.T) {
	items := []PromptItem{
		{WindowTitle: "a.go - Code", EntropyScore: 1, TotalClicks: 1, AppName: "Code", Timestamp: time.Now()},
	}
	longTask := strings.TrimSpace(strings.Repeat("review ", 25))

	for _, task := range []string{"", "Fix the parser", longTask} {
		prompt := BuildAnalysisPrompt(items, task)
		idx := strings.Index(prompt, "ITEMS:")
		if idx == -1 {
			t.Fatalf("prompt missing ITEMS: marker")
		}
		if words := len(strings.Fields(prompt[:idx])); words > 250 {
			t.Errorf("instruction section has %d words, want <= 250 (task %q)", words, task)
		}
	}
}

func TestBuildPromptItemsSuppressesIdleScreenshots(t *testing.T) {
	base := time.Date(2026, 10, 19, 10, 0, 0, 0, time.UTC)
	mk := func(mins int, title string, keys, clicks int, img bool) BatchAnalysisItem {
		it := BatchAnalysisItem{
			LogID:       int64(mins),
			WindowTitle: title,
			AppName:     "Code",
			TotalKeys:   keys,
			TotalClicks: clicks,
			Timestamp:   base.Add(time.Duration(mins) * time.Minute),
		}
		if img {
			it.ImagePath = "/tmp/x.webp"
		}
		return it
	}
	items := []BatchAnalysisItem{
		mk(0, "main.go - VS Code", 3, 0, true),   // image kept
		mk(1, "main.go - VS Code", 0, 0, true),   // idle, same window → suppressed
		mk(2, "main.go - VS Code", 0, 0, true),   // still idle → suppressed
		mk(3, "main.go - VS Code", 12, 0, true),  // keystrokes → fresh image
		mk(4, "Slack - #general", 0, 2, true),    // window change → fresh image
		mk(5, "Slack - #general", 0, 0, true),    // idle, same window → suppressed
		mk(20, "Slack - #general", 0, 0, true),   // 15min gap exceeded → fresh
		mk(21, "main.go - VS Code", 0, 0, false), // no image at all (stays imageless)
	}

	got := buildPromptItems(items)
	if !got[0].HasImage {
		t.Errorf("item 0 should keep its image")
	}
	for i := 1; i <= 2; i++ {
		if got[i].HasImage {
			t.Errorf("item %d should have a suppressed image (img=false)", i)
		}
	}
	if !got[3].HasImage {
		t.Errorf("item 3 (activity) should keep a fresh image")
	}
	if !got[4].HasImage {
		t.Errorf("item 4 (window change) should keep a fresh image")
	}
	if got[5].HasImage {
		t.Errorf("item 5 should be suppressed (idle same window)")
	}
	if !got[6].HasImage {
		t.Errorf("item 6 (gap > %v) should keep a fresh image", maxIdleStreakGap)
	}
	if got[7].HasImage {
		t.Errorf("item 7 has no image file and must stay imageless")
	}

	// Prompt flags must mirror HasImage exactly.
	prompt := BuildAnalysisPrompt(
		[]PromptItem{{WindowTitle: "x", Timestamp: base, HasImage: false}},
		"",
	)
	if !strings.Contains(prompt, `"img":false`) {
		t.Errorf("prompt missing img:false flag for suppressed item")
	}
}

func TestAnalysisResponseSchema(t *testing.T) {
	schema := AnalysisResponseSchema()
	if schema["type"] != "ARRAY" {
		t.Fatalf("root type = %v, want ARRAY", schema["type"])
	}
	items, ok := schema["items"].(map[string]interface{})
	if !ok {
		t.Fatalf("items is %T, want object", schema["items"])
	}
	if items["type"] != "OBJECT" {
		t.Errorf("items type = %v, want OBJECT", items["type"])
	}
	props, ok := items["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("properties is %T, want object", items["properties"])
	}
	wantTypes := map[string]string{
		"i": "INTEGER", "cat": "STRING", "a": "STRING", "ac": "STRING",
		"p": "INTEGER", "c": "NUMBER", "r": "STRING",
	}
	for key, want := range wantTypes {
		prop, ok := props[key].(map[string]interface{})
		if !ok {
			t.Errorf("missing property %q", key)
			continue
		}
		if prop["type"] != want {
			t.Errorf("property %q type = %v, want %s", key, prop["type"], want)
		}
	}
	cat, ok := props["cat"].(map[string]interface{})
	if !ok {
		t.Fatal("missing cat property")
	}
	enum, ok := cat["enum"].([]string)
	if !ok {
		t.Fatalf("cat enum is %T, want []string", cat["enum"])
	}
	if !reflect.DeepEqual(enum, AnalysisCategories) {
		t.Errorf("cat enum = %v, want %v", enum, AnalysisCategories)
	}
	required, ok := items["required"].([]string)
	if !ok {
		t.Fatalf("required is %T, want []string", items["required"])
	}
	have := map[string]bool{}
	for _, r := range required {
		have[r] = true
	}
	for _, key := range []string{"i", "cat", "p"} {
		if !have[key] {
			t.Errorf("required missing %q", key)
		}
	}
}

func TestParseAnalysisJSON(t *testing.T) {
	plain := `[{"i":0,"cat":"Coding","a":"VS Code","ac":"IDE","t":"main.go","p":85,"c":0.9,"r":"writing code"},{"i":1,"cat":"Idle","p":3}]`

	t.Run("fenced", func(t *testing.T) {
		got, err := ParseAnalysisJSON("```json\n"+plain+"\n```", 2)
		if err != nil {
			t.Fatalf("ParseAnalysisJSON error: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("len = %d, want 2", len(got))
		}
		if got[0].Index != 0 || got[0].Category != "Coding" || got[0].AppName != "VS Code" || got[0].Score != 85 || got[0].Confidence != 0.9 {
			t.Errorf("unexpected first item: %+v", got[0])
		}
		if got[1].Index != 1 || got[1].Category != "Idle" {
			t.Errorf("unexpected second item: %+v", got[1])
		}
	})

	t.Run("plain", func(t *testing.T) {
		got, err := ParseAnalysisJSON(plain, 2)
		if err != nil {
			t.Fatalf("ParseAnalysisJSON error: %v", err)
		}
		if len(got) != 2 || got[0].Title != "main.go" {
			t.Fatalf("unexpected result: %+v", got)
		}
	})

	t.Run("truncated", func(t *testing.T) {
		truncated := `[{"i":0,"cat":"Coding","a":"VS Code","p":85,"c":0.9,"r":"writing code"},{"i":1,"cat":"Idle","p":3,"c":0.`
		got, err := ParseAnalysisJSON(truncated, 2)
		if err != nil {
			t.Fatalf("ParseAnalysisJSON error: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("len = %d, want 1 repaired item", len(got))
		}
		if got[0].Category != "Coding" {
			t.Errorf("unexpected repaired item: %+v", got[0])
		}
	})

	t.Run("garbage", func(t *testing.T) {
		for _, raw := range []string{"", "hello world", "[not json]", "```json\nnot json\n```", "{"} {
			if _, err := ParseAnalysisJSON(raw, 1); err == nil {
				t.Errorf("ParseAnalysisJSON(%q) error = nil, want error", raw)
			}
		}
	})
}
