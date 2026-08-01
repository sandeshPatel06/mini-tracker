package ai

import (
	"strings"
	"testing"
)

func TestParseGeminiResponse(t *testing.T) {
	jsonResp := `{
		"candidates": [
			{
				"content": {
					"parts": [
						{
							"text": "{\"category\": \"Coding\", \"productive\": true, \"confidence\": 0.9, \"brief_reason\": \"Writing Go code\"}"
						}
					]
				}
			}
		]
	}`

	res, err := parseGeminiResponse(strings.NewReader(jsonResp))
	if err != nil {
		t.Fatalf("parseGeminiResponse failed: %v", err)
	}

	if res.Category != "Coding" {
		t.Errorf("expected category Coding, got %s", res.Category)
	}
	if !res.Productive {
		t.Errorf("expected productive true, got false")
	}
	if res.Confidence != 0.9 {
		t.Errorf("expected confidence 0.9, got %f", res.Confidence)
	}
	if res.Reason != "Writing Go code" {
		t.Errorf("expected reason 'Writing Go code', got %s", res.Reason)
	}
}

func TestBuildGeminiRequest(t *testing.T) {
	req := buildGeminiRequest("test prompt", "base64data")
	contents, ok := req["contents"].([]map[string]interface{})
	if !ok || len(contents) == 0 {
		t.Fatalf("invalid request contents structure")
	}
}
