package config

import (
	"os"
	"testing"
	"time"
)

func TestLoadConfig(t *testing.T) {
	os.Setenv("GEMINI_API_KEY", "test-key-123")
	defer os.Unsetenv("GEMINI_API_KEY")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.GeminiAPIKey != "test-key-123" {
		t.Errorf("expected GeminiAPIKey to be test-key-123, got %s", cfg.GeminiAPIKey)
	}
	if cfg.ScreenshotInterval != 30*time.Second {
		t.Errorf("expected ScreenshotInterval to be 30s, got %v", cfg.ScreenshotInterval)
	}
}
