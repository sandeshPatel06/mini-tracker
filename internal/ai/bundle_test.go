package ai

import (
	"strings"
	"testing"
	"time"
)

func TestBuildBundlePromptSplit(t *testing.T) {
	items := []PromptItem{{
		LogID:        1,
		WindowTitle:  "main.go - VS Code",
		EntropyScore: 12.5,
		TotalClicks:  4,
		AppName:      "Code",
		Timestamp:    time.Date(2026, 10, 19, 10, 2, 0, 0, time.Local),
		HasImage:     true,
	}}
	staticText, dynamicText := BuildBundlePromptSplit(items, "ship billing bug", "2026-10-19 10:00-10:05 (5-minute bundle)")

	for _, want := range []string{"5-minute", "\"score\"", "\"category\"", "TASK CONTEXT", "ship billing bug"} {
		if !strings.Contains(staticText, want) {
			t.Fatalf("static prompt missing %q:\n%s", want, staticText)
		}
	}
	if strings.Contains(staticText, "ITEMS:") {
		t.Fatalf("static prompt must not contain dynamic items:\n%s", staticText)
	}
	for _, want := range []string{"WINDOW: 2026-10-19 10:00-10:05 (5-minute bundle)", "ITEMS:", "main.go", "\"i\":0"} {
		if !strings.Contains(dynamicText, want) {
			t.Fatalf("dynamic prompt missing %q:\n%s", want, dynamicText)
		}
	}
}

func TestParseBundleJSON(t *testing.T) {
	tests := []struct {
		name       string
		raw        string
		wantScore  float64
		wantCat    string
		wantReason string
	}{
		{
			name:       "plain object",
			raw:        `{"score":82,"category":"Coding","app_name":"Code","app_category":"Development","confidence":0.9,"reason":"focused coding"}`,
			wantScore:  82,
			wantCat:    "Coding",
			wantReason: "focused coding",
		},
		{
			name:       "fenced with prose",
			raw:        "Here you go:\n```json\n{\"score\": 45, \"category\": \"Browsing\", \"reason\": \"research\"}\n```",
			wantScore:  45,
			wantCat:    "Browsing",
			wantReason: "research",
		},
		{
			name:       "clamps score and defaults missing fields",
			raw:        `{"score":140}`,
			wantScore:  100,
			wantCat:    "Other",
			wantReason: "no reason provided",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseBundleJSON(tc.raw)
			if err != nil {
				t.Fatalf("ParseBundleJSON error: %v", err)
			}
			if got.Score != tc.wantScore || got.Category != tc.wantCat || got.Reason != tc.wantReason {
				t.Fatalf("ParseBundleJSON = %+v, want score=%v cat=%q reason=%q", got, tc.wantScore, tc.wantCat, tc.wantReason)
			}
		})
	}
}

func TestParseBundleJSONRejectsGarbage(t *testing.T) {
	if _, err := ParseBundleJSON("no json here"); err == nil {
		t.Fatal("expected error for output without an object")
	}
}
