package config

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// Config holds application configuration managed by Viper.
type Config struct {
	GeminiAPIKey       string        `mapstructure:"gemini_api_key" json:"gemini_api_key"`
	GeminiModel        string        `mapstructure:"gemini_model" json:"gemini_model"`
	ScreenshotInterval time.Duration `mapstructure:"screenshot_interval_seconds" json:"screenshot_interval_seconds"`
	AIAnalysisInterval time.Duration `mapstructure:"ai_analysis_interval_seconds" json:"ai_analysis_interval_seconds"`
	DataDir            string        `mapstructure:"data_dir" json:"data_dir"`
	BackendPort        int           `mapstructure:"backend_port" json:"backend_port"`
	BackendEndpoint    string        `mapstructure:"backend_endpoint" json:"backend_endpoint"`
	DatabaseURL        string        `mapstructure:"database_url" json:"database_url"`
	GoogleClientID     string        `mapstructure:"google_client_id" json:"google_client_id"`
	GoogleClientSecret string        `mapstructure:"google_client_secret" json:"google_client_secret"`
	AzureClientID      string        `mapstructure:"azure_client_id" json:"azure_client_id"`
	AzureClientSecret string        `mapstructure:"azure_client_secret" json:"azure_client_secret"`
	AzureTenantID     string        `mapstructure:"azure_tenant_id" json:"azure_tenant_id"`
}

// Load loads configuration using Viper from config.yaml, .env files, and environment variables.
func Load() (*Config, error) {
	v := viper.New()

	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	// 1. Set default values
	v.SetDefault("gemini_model", "models/gemma-4-31b-it")
	v.SetDefault("screenshot_interval_seconds", 30*time.Second)
	v.SetDefault("ai_analysis_interval_seconds", 3*time.Hour)
	v.SetDefault("backend_port", 8080)
	v.SetDefault("backend_endpoint", "http://localhost:8080")
	v.SetDefault("data_dir", filepath.Join(home, ".local", "share", "get-hike"))
	v.SetDefault("azure_tenant_id", "common")

	// 2. Load .env files if present (current working directory and config directory)
	loadDotEnv(".env")
	loadDotEnv(filepath.Join(home, ".config", "get-hike", ".env"))

	// 3. Environment Variable Auto-Binding
	v.AutomaticEnv()
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	// Bind key environment variable names directly into Viper
	_ = v.BindEnv("database_url", "DATABASE_URL")
	_ = v.BindEnv("gemini_api_key", "GEMINI_API_KEY")
	_ = v.BindEnv("gemini_model", "GEMINI_MODEL")
	_ = v.BindEnv("backend_port", "PORT")
	_ = v.BindEnv("backend_endpoint", "BACKEND_URL")
	_ = v.BindEnv("google_client_id", "GOOGLE_CLIENT_ID")
	_ = v.BindEnv("google_client_secret", "GOOGLE_CLIENT_SECRET")
	_ = v.BindEnv("azure_client_id", "AZURE_CLIENT_ID")
	_ = v.BindEnv("azure_client_secret", "AZURE_CLIENT_SECRET")
	_ = v.BindEnv("azure_tenant_id", "AZURE_TENANT_ID")

	// 4. Config file locations for config.yaml / config.json
	v.SetConfigName("config")
	v.SetConfigType("yaml")
	v.AddConfigPath(".")
	v.AddConfigPath(filepath.Join(home, ".config", "get-hike"))
	v.AddConfigPath(home)

	// Attempt reading YAML configuration
	if err := v.ReadInConfig(); err != nil {
		// Fallback to JSON if yaml is not found
		v.SetConfigType("json")
		_ = v.ReadInConfig()
	}

	var cfg Config
	cfg.GeminiAPIKey = v.GetString("gemini_api_key")
	cfg.GeminiModel = v.GetString("gemini_model")
	cfg.DataDir = v.GetString("data_dir")
	cfg.BackendPort = v.GetInt("backend_port")
	cfg.DatabaseURL = v.GetString("database_url")
	cfg.GoogleClientID = v.GetString("google_client_id")
	cfg.GoogleClientSecret = v.GetString("google_client_secret")
	cfg.AzureClientID = v.GetString("azure_client_id")
	cfg.AzureClientSecret = v.GetString("azure_client_secret")
	cfg.AzureTenantID = v.GetString("azure_tenant_id")
	if cfg.AzureTenantID == "" {
		cfg.AzureTenantID = "common"
	}

	// Helper duration getters with fallback
	cfg.ScreenshotInterval = v.GetDuration("screenshot_interval_seconds")
	if cfg.ScreenshotInterval <= 0 {
		if sec := v.GetInt("screenshot_interval_seconds"); sec > 0 {
			cfg.ScreenshotInterval = time.Duration(sec) * time.Second
		} else {
			cfg.ScreenshotInterval = 30 * time.Second
		}
	}

	cfg.AIAnalysisInterval = v.GetDuration("ai_analysis_interval_seconds")
	if cfg.AIAnalysisInterval <= 0 {
		cfg.AIAnalysisInterval = 3 * time.Hour
	}

	// Ensure DataDir fallback
	if cfg.DataDir == "" {
		cfg.DataDir = filepath.Join(home, ".local", "share", "get-hike")
	}

	// Ensure required directories exist
	_ = os.MkdirAll(cfg.DataDir, 0755)
	_ = os.MkdirAll(filepath.Join(home, ".config", "get-hike"), 0755)

	return &cfg, nil
}

// Save writes the config to ~/.config/get-hike/config.yaml using Viper.
func Save(cfg *Config) error {
	v := viper.New()
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	cfgDir := filepath.Join(home, ".config", "get-hike")
	_ = os.MkdirAll(cfgDir, 0755)

	v.Set("gemini_api_key", cfg.GeminiAPIKey)
	v.Set("gemini_model", cfg.GeminiModel)
	v.Set("screenshot_interval_seconds", cfg.ScreenshotInterval.String())
	v.Set("ai_analysis_interval_seconds", cfg.AIAnalysisInterval.String())
	v.Set("data_dir", cfg.DataDir)
	v.Set("backend_port", cfg.BackendPort)
	v.Set("backend_endpoint", cfg.BackendEndpoint)
	v.Set("database_url", cfg.DatabaseURL)

	v.SetConfigName("config")
	v.SetConfigType("yaml")
	v.SetConfigFile(filepath.Join(cfgDir, "config.yaml"))
	return v.WriteConfig()
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
