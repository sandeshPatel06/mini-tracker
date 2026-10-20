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
		TotalKeys:    7,
		TotalClicks:  4,
		AppName:      "Code",
		Timestamp:    time.Date(2026, 10, 19, 10, 2, 0, 0, time.Local),
		HasImage:     true,
	}}
	staticText, dynamicText := BuildBundlePromptSplit(items, "ship billing bug", "2026-10-19 10:00-10:05 (5-minute bundle)", SessionEvent{Time: "10:01", Kind: "focus", App: "Chrome", Title: "docs"})

	for _, want := range []string{"5-minute", "\"score\"", "\"category\"", "TASK CONTEXT", "ship billing bug", "Reading/studying", "Zero input is NOT idle"} {
		if !strings.Contains(staticText, want) {
			t.Fatalf("static prompt missing %q:\n%s", want, staticText)
		}
	}
	if strings.Contains(staticText, "ITEMS:") {
		t.Fatalf("static prompt must not contain dynamic items:\n%s", staticText)
	}
	for _, want := range []string{"WINDOW: 2026-10-19 10:00-10:05 (5-minute bundle)", "ITEMS:", "main.go", "\"i\":0", "\"k\":7", "WINDOW ACTIVITY", "keys=7", "focus_changes=1"} {
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

func TestIsClearlyIdleBundle(t *testing.T) {
	if IsClearlyIdleBundle(nil) {
		t.Fatal("empty bundle must not be considered idle")
	}
	if !IsClearlyIdleBundle([]BatchAnalysisItem{{AppName: " ", WindowTitle: ""}, {AppName: "", WindowTitle: "  "}}) {
		t.Fatal("blank app and title must be clearly idle")
	}
	if IsClearlyIdleBundle([]BatchAnalysisItem{{AppName: "", WindowTitle: "main.go"}}) {
		t.Fatal("window with a title is not idle")
	}
}

func TestApplyBundleActivityGuard(t *testing.T) {
	reading := []BatchAnalysisItem{{AppName: "Google-chrome", WindowTitle: "Go docs", TotalKeys: 0, TotalClicks: 0}}

	result := &SessionBundleResult{Score: 90, Category: "Browsing", Confidence: 0.95}
	ApplyBundleActivityGuard(result, reading, nil)
	if result.Score != 75 || result.Confidence != 0.8 {
		t.Fatalf("static reading window = %+v, want score 75 confidence 0.8", result)
	}

	active := []BatchAnalysisItem{{AppName: "Google-chrome", WindowTitle: "Go docs", TotalKeys: 3}}
	result = &SessionBundleResult{Score: 90, Category: "Browsing", Confidence: 0.9}
	ApplyBundleActivityGuard(result, active, nil)
	if result.Score != 90 {
		t.Fatalf("active reading window score = %v, want 90", result.Score)
	}

	result = &SessionBundleResult{Score: 90, Category: "Browsing"}
	ApplyBundleActivityGuard(result, reading, []SessionEvent{{Kind: "focus", App: "Chrome"}})
	if result.Score != 90 {
		t.Fatalf("window with focus events score = %v, want 90", result.Score)
	}

	result = &SessionBundleResult{Score: 100, Category: "Other", AppName: "X", Confidence: 1}
	ApplyBundleActivityGuard(result, []BatchAnalysisItem{{AppName: "", WindowTitle: ""}}, nil)
	if result.Score != 5 || result.Category != "Idle" {
		t.Fatalf("clearly idle result = %+v, want Idle score 5", result)
	}
}
