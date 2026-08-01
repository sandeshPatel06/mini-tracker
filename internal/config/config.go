package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// Config holds application configuration.
type Config struct {
	GeminiAPIKey       string        `json:"gemini_api_key"`
	ScreenshotInterval time.Duration `json:"screenshot_interval_seconds"`
	AIAnalysisInterval time.Duration `json:"ai_analysis_interval_seconds"`
	DataDir            string        `json:"data_dir"`
	BackendPort        int           `json:"backend_port"`
}

// Load reads config from env vars first, then falls back to
// ~/.config/mini-tracker/config.json, then uses sane defaults.
func Load() (*Config, error) {
	cfg := &Config{
		ScreenshotInterval: 30 * time.Second,
		AIAnalysisInterval: 3 * time.Hour,
		BackendPort:        8080,
	}

	// Config file path
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	cfgPath := filepath.Join(home, ".config", "mini-tracker", "config.json")

	// Try loading from file
	if data, err := os.ReadFile(cfgPath); err == nil {
		_ = json.Unmarshal(data, cfg)
	}

	// Load .env files if present (current working directory and config directory)
	loadDotEnv(".env")
	loadDotEnv(filepath.Join(home, ".config", "mini-tracker", ".env"))

	// Env overrides
	if v := os.Getenv("GEMINI_API_KEY"); v != "" {
		cfg.GeminiAPIKey = v
	}
	if v := os.Getenv("AI_ANALYSIS_INTERVAL"); v != "" {
		if dur, err := time.ParseDuration(v); err == nil {
			cfg.AIAnalysisInterval = dur
		}
	}

	// Data directory
	if cfg.DataDir == "" {
		cfg.DataDir = filepath.Join(home, ".local", "share", "mini-tracker")
	}

	// Ensure directories exist
	if err := os.MkdirAll(cfg.DataDir, 0755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0755); err != nil {
		return nil, err
	}

	return cfg, nil
}

// Save writes the config to ~/.config/mini-tracker/config.json.
func Save(cfg *Config) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	cfgDir := filepath.Join(home, ".config", "mini-tracker")
	if err := os.MkdirAll(cfgDir, 0755); err != nil {
		return err
	}
	cfgPath := filepath.Join(cfgDir, "config.json")
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(cfgPath, data, 0600)
}

// loadDotEnv reads a simple .env file without overriding existing env vars.
func loadDotEnv(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	lines := splitLines(string(data))
	for _, line := range lines {
		line = trimSpace(line)
		if line == "" || line[0] == '#' {
			continue
		}
		eq := indexOf(line, '=')
		if eq > 0 {
			key := trimSpace(line[:eq])
			val := trimSpace(line[eq+1:])
			// Strip quotes if present
			if len(val) >= 2 && ((val[0] == '"' && val[len(val)-1] == '"') || (val[0] == '\'' && val[len(val)-1] == '\'')) {
				val = val[1 : len(val)-1]
			}
			if os.Getenv(key) == "" {
				os.Setenv(key, val)
			}
		}
	}
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			line := s[start:i]
			if len(line) > 0 && line[len(line)-1] == '\r' {
				line = line[:len(line)-1]
			}
			lines = append(lines, line)
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

func trimSpace(s string) string {
	start, end := 0, len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t' || s[start] == '\r' || s[start] == '\n') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\r' || s[end-1] == '\n') {
		end--
	}
	return s[start:end]
}

func indexOf(s string, char byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == char {
			return i
		}
	}
	return -1
}

