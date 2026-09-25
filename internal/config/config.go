// Package config loads process settings from a .env file and the environment.
package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/viper"
)

const (
	keyPort               = "PORT"
	keySQLitePath         = "SQLITE_PATH"
	keyUploadDir          = "UPLOAD_DIR"
	keyMaxUploadBytes     = "MAX_UPLOAD_BYTES"
	keyWorkerCount        = "WORKER_COUNT"
	keyTesseractLang      = "TESSERACT_LANG"
	keyTypeSafeAPIKey     = "TYPESAFE_API_KEY"
	keyTypeSafeBaseURL    = "TYPESAFE_BASE_URL"
	keyTypeSafeTimeout    = "TYPESAFE_TIMEOUT"
	keyOpenRouterAPIKey   = "OPENROUTER_API_KEY"
	keyOpenRouterBaseURL  = "OPENROUTER_BASE_URL"
	keyOpenRouterModel    = "OPENROUTER_MODEL"
	keyOpenRouterTimeout  = "OPENROUTER_TIMEOUT"
	keyOpenRouterOCRModel = "OPENROUTER_OCR_MODEL"
	keyOCREngine          = "OCR_ENGINE"
	keyOllamaURL          = "OLLAMA_URL"
	keyOllamaOCRModel     = "OLLAMA_OCR_MODEL"
	keyOllamaTimeout      = "OLLAMA_TIMEOUT"
	keyLogFormat          = "LOG_FORMAT"
)

// Config holds runtime settings.
type Config struct {
	Port               string
	SQLitePath         string
	UploadDir          string
	MaxUploadBytes     int64
	WorkerCount        int
	TesseractLang      string
	TypeSafeAPIKey     string
	TypeSafeBaseURL    string
	TypeSafeTimeout    time.Duration
	OpenRouterAPIKey   string
	OpenRouterBaseURL  string
	OpenRouterModel    string
	OpenRouterTimeout  time.Duration
	OpenRouterOCRModel string
	OCREngine          string
	OllamaURL          string
	OllamaOCRModel     string
	OllamaTimeout      time.Duration
	LogFormat          string
}

// Load reads .env via Viper, then overlays process environment variables.
func Load() (Config, error) {
	v := viper.New()
	v.SetConfigType("env")
	v.AutomaticEnv()

	for _, key := range []string{
		keyPort,
		keySQLitePath,
		keyUploadDir,
		keyMaxUploadBytes,
		keyWorkerCount,
		keyTesseractLang,
		keyTypeSafeAPIKey,
		keyTypeSafeBaseURL,
		keyTypeSafeTimeout,
		keyOpenRouterAPIKey,
		keyOpenRouterBaseURL,
		keyOpenRouterModel,
		keyOpenRouterTimeout,
		keyOpenRouterOCRModel,
		keyOCREngine,
		keyOllamaURL,
		keyOllamaOCRModel,
		keyOllamaTimeout,
		keyLogFormat,
	} {
		if err := v.BindEnv(key); err != nil {
			return Config{}, fmt.Errorf("bind env %s: %w", key, err)
		}
	}

	v.SetDefault(keyPort, "8080")
	v.SetDefault(keySQLitePath, "data/poc.db")
	v.SetDefault(keyUploadDir, "uploads")
	v.SetDefault(keyMaxUploadBytes, 8*1024*1024)
	v.SetDefault(keyWorkerCount, 2)
	v.SetDefault(keyTesseractLang, "por+eng")
	v.SetDefault(keyTypeSafeBaseURL, "https://api.typesafe.ai")
	v.SetDefault(keyTypeSafeTimeout, 30*time.Second)
	v.SetDefault(keyOpenRouterBaseURL, "https://openrouter.ai/api/v1")
	v.SetDefault(keyOpenRouterModel, "deepseek/deepseek-v4-flash")
	v.SetDefault(keyOpenRouterTimeout, 45*time.Second)
	v.SetDefault(keyOpenRouterOCRModel, "google/gemini-2.5-flash")
	v.SetDefault(keyOCREngine, "openrouter")
	v.SetDefault(keyOllamaURL, "http://127.0.0.1:11434")
	v.SetDefault(keyOllamaOCRModel, "glm-ocr:latest")
	v.SetDefault(keyOllamaTimeout, 120*time.Second)
	v.SetDefault(keyLogFormat, "text")

	dotEnvPath, err := mergeDotEnv(v)
	if err != nil {
		return Config{}, err
	}

	fileKey, fileHasKey := dotenvValue(dotEnvPath, keyTypeSafeAPIKey)
	orFile, orHas := dotenvValue(dotEnvPath, keyOpenRouterAPIKey)

	cfg := Config{
		Port:               strings.TrimSpace(v.GetString(keyPort)),
		SQLitePath:         strings.TrimSpace(v.GetString(keySQLitePath)),
		UploadDir:          strings.TrimSpace(v.GetString(keyUploadDir)),
		MaxUploadBytes:     v.GetInt64(keyMaxUploadBytes),
		WorkerCount:        v.GetInt(keyWorkerCount),
		TesseractLang:      strings.TrimSpace(v.GetString(keyTesseractLang)),
		TypeSafeAPIKey:     resolveAPIKey(fileKey, fileHasKey, v.GetString(keyTypeSafeAPIKey)),
		TypeSafeBaseURL:    strings.TrimSpace(v.GetString(keyTypeSafeBaseURL)),
		TypeSafeTimeout:    v.GetDuration(keyTypeSafeTimeout),
		OpenRouterAPIKey:   resolveAPIKey(orFile, orHas, v.GetString(keyOpenRouterAPIKey)),
		OpenRouterBaseURL:  strings.TrimSpace(v.GetString(keyOpenRouterBaseURL)),
		OpenRouterModel:    strings.TrimSpace(v.GetString(keyOpenRouterModel)),
		OpenRouterTimeout:  v.GetDuration(keyOpenRouterTimeout),
		OpenRouterOCRModel: strings.TrimSpace(v.GetString(keyOpenRouterOCRModel)),
		OCREngine:          strings.TrimSpace(v.GetString(keyOCREngine)),
		OllamaURL:          strings.TrimSpace(v.GetString(keyOllamaURL)),
		OllamaOCRModel:     strings.TrimSpace(v.GetString(keyOllamaOCRModel)),
		OllamaTimeout:      v.GetDuration(keyOllamaTimeout),
		LogFormat:          strings.ToLower(strings.TrimSpace(v.GetString(keyLogFormat))),
	}

	if cfg.MaxUploadBytes <= 0 {
		return Config{}, fmt.Errorf("invalid MAX_UPLOAD_BYTES: %d", cfg.MaxUploadBytes)
	}

	if cfg.WorkerCount <= 0 {
		return Config{}, fmt.Errorf("invalid WORKER_COUNT: %d", cfg.WorkerCount)
	}

	if cfg.TypeSafeTimeout <= 0 {
		return Config{}, fmt.Errorf("invalid TYPESAFE_TIMEOUT: %s", v.GetString(keyTypeSafeTimeout))
	}

	if cfg.OpenRouterTimeout <= 0 {
		return Config{}, fmt.Errorf("invalid OPENROUTER_TIMEOUT: %s", v.GetString(keyOpenRouterTimeout))
	}

	if cfg.OllamaTimeout <= 0 {
		return Config{}, fmt.Errorf("invalid OLLAMA_TIMEOUT: %s", v.GetString(keyOllamaTimeout))
	}

	switch cfg.LogFormat {
	case "text", "json":
	default:
		return Config{}, fmt.Errorf("invalid LOG_FORMAT: %s", cfg.LogFormat)
	}

	return cfg, nil
}

func mergeDotEnv(v *viper.Viper) (string, error) {
	for _, path := range []string{".env", "configs/.env"} {
		if _, err := os.Stat(path); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}

			return "", fmt.Errorf("stat %s: %w", path, err)
		}

		v.SetConfigFile(path)

		if err := v.MergeInConfig(); err != nil {
			return "", fmt.Errorf("read %s: %w", path, err)
		}

		return path, nil
	}

	return "", nil
}

func dotenvValue(path, want string) (string, bool) {
	if path == "" {
		return "", false
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}

	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		key, val, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(key) != want {
			continue
		}

		return stripQuotes(val), true
	}

	return "", false
}

func resolveAPIKey(fileKey string, fileHasKey bool, viperKey string) string {
	if fileHasKey && fileKey != "" {
		return fileKey
	}

	return stripQuotes(viperKey)
}

func stripQuotes(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		if (s[0] == '\'' && s[len(s)-1] == '\'') || (s[0] == '"' && s[len(s)-1] == '"') {
			return strings.TrimSpace(s[1 : len(s)-1])
		}
	}

	return s
}
