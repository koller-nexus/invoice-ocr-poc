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
	keyJobQueueSize       = "JOB_QUEUE_SIZE"
	keyPrepWorkers        = "PREP_WORKERS"
	keyOCRWorkers         = "OCR_WORKERS"
	keyJevWorkers         = "JEV_WORKERS"
	keyIngestBuffer       = "INGEST_BUFFER"
	keyOCRBuffer          = "OCR_BUFFER"
	keyJevBuffer          = "JEV_BUFFER"
	keyJobTimeout         = "JOB_TIMEOUT"
	keyShutdownTimeout    = "SHUTDOWN_TIMEOUT"
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
	keyOCRMinConfidence   = "OCR_MIN_CONFIDENCE"
	keyWorkerCountLocal   = "WORKER_COUNT_LOCAL"
	keyWorkerCountAPI     = "WORKER_COUNT_API"
	keyOCRTimeout         = "OCR_TIMEOUT"
	keyAssistTimeout      = "ASSIST_TIMEOUT"
	keyJevTimeout         = "JEV_TIMEOUT"
	keyDebugPprof         = "DEBUG_PPROF"
	keyDebugPprofAddr     = "DEBUG_PPROF_ADDR"
	keyOCRCacheEnabled    = "OCR_CACHE_ENABLED"
	keyOCRCacheTTL        = "OCR_CACHE_TTL"

	// Database backend selection
	keyDBBackend = "DB_BACKEND" // "sqlite" or "postgres"

	// PostgreSQL settings
	keyPGHost            = "PG_HOST"
	keyPGPort            = "PG_PORT"
	keyPGUser            = "PG_USER"
	keyPGPassword        = "PG_PASSWORD"
	keyPGDBName          = "PG_DBNAME"
	keyPGSSLMode         = "PG_SSLMODE"
	keyPGMaxOpenConns    = "PG_MAX_OPEN_CONNS"
	keyPGMaxIdleConns    = "PG_MAX_IDLE_CONNS"
	keyPGConnMaxLifetime = "PG_CONN_MAX_LIFETIME"
	keyPGSyncCommit      = "PG_SYNC_COMMIT" // "off" for fast writes, "on" for durability

	// Cache backend selection
	keyCacheBackend = "CACHE_BACKEND" // "memory" or "redis"

	// Redis settings
	keyRedisAddr     = "REDIS_ADDR"
	keyRedisPassword = "REDIS_PASSWORD"
	keyRedisDB       = "REDIS_DB"
	keyRedisPoolSize = "REDIS_POOL_SIZE"
)

// Config holds runtime settings.
type Config struct {
	Port               string
	SQLitePath         string
	UploadDir          string
	MaxUploadBytes     int64
	WorkerCount        int
	JobQueueSize       int
	PrepWorkers        int
	OCRWorkers         int
	JevWorkers         int
	IngestBuffer       int
	OCRBuffer          int
	JevBuffer          int
	JobTimeout         time.Duration
	ShutdownTimeout    time.Duration
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
	OCRMinConfidence   float64
	WorkerCountLocal   int
	WorkerCountAPI     int
	OCRTimeout         time.Duration
	AssistTimeout      time.Duration
	JevTimeout         time.Duration
	DebugPprof         bool
	DebugPprofAddr     string
	OCRCacheEnabled    bool
	OCRCacheTTL        time.Duration

	// Database backend selection
	DBBackend string // "sqlite" or "postgres"

	// PostgreSQL settings
	PGHost            string
	PGPort            int
	PGUser            string
	PGPassword        string
	PGDBName          string
	PGSSLMode         string
	PGMaxOpenConns    int
	PGMaxIdleConns    int
	PGConnMaxLifetime time.Duration
	PGSyncCommit      string // "off" for fast writes, "on" for durability

	// Cache backend selection
	CacheBackend string // "memory" or "redis"

	// Redis settings
	RedisAddr     string
	RedisPassword string
	RedisDB       int
	RedisPoolSize int
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
		keyJobQueueSize,
		keyPrepWorkers,
		keyOCRWorkers,
		keyJevWorkers,
		keyIngestBuffer,
		keyOCRBuffer,
		keyJevBuffer,
		keyJobTimeout,
		keyShutdownTimeout,
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
		keyOCRMinConfidence,
		keyWorkerCountLocal,
		keyWorkerCountAPI,
		keyOCRTimeout,
		keyAssistTimeout,
		keyJevTimeout,
		keyDebugPprof,
		keyDebugPprofAddr,
		keyOCRCacheEnabled,
		keyOCRCacheTTL,
		// Database backend
		keyDBBackend,
		// PostgreSQL
		keyPGHost,
		keyPGPort,
		keyPGUser,
		keyPGPassword,
		keyPGDBName,
		keyPGSSLMode,
		keyPGMaxOpenConns,
		keyPGMaxIdleConns,
		keyPGConnMaxLifetime,
		keyPGSyncCommit,
		// Cache backend
		keyCacheBackend,
		// Redis
		keyRedisAddr,
		keyRedisPassword,
		keyRedisDB,
		keyRedisPoolSize,
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
	v.SetDefault(keyJobQueueSize, 32)
	v.SetDefault(keyPrepWorkers, 1)
	v.SetDefault(keyOCRWorkers, 2)
	v.SetDefault(keyJevWorkers, 2)
	v.SetDefault(keyOCRMinConfidence, 0.75)
	v.SetDefault(keyDebugPprof, false)
	v.SetDefault(keyDebugPprofAddr, "127.0.0.1:6060")
	v.SetDefault(keyOCRCacheEnabled, true)
	v.SetDefault(keyOCRCacheTTL, time.Hour)
	v.SetDefault(keyIngestBuffer, 16)
	v.SetDefault(keyOCRBuffer, 8)
	v.SetDefault(keyJevBuffer, 8)
	v.SetDefault(keyJobTimeout, 90*time.Second)
	v.SetDefault(keyShutdownTimeout, 15*time.Second)
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

	// Database backend defaults
	v.SetDefault(keyDBBackend, "sqlite") // "sqlite" or "postgres"

	// PostgreSQL defaults (write-optimized)
	v.SetDefault(keyPGHost, "localhost")
	v.SetDefault(keyPGPort, 5432)
	v.SetDefault(keyPGUser, "postgres")
	v.SetDefault(keyPGPassword, "")
	v.SetDefault(keyPGDBName, "invoice_ocr")
	v.SetDefault(keyPGSSLMode, "disable")
	v.SetDefault(keyPGMaxOpenConns, 25)
	v.SetDefault(keyPGMaxIdleConns, 10)
	v.SetDefault(keyPGConnMaxLifetime, time.Hour)
	v.SetDefault(keyPGSyncCommit, "off") // Fast writes, slight durability risk

	// Cache backend defaults
	v.SetDefault(keyCacheBackend, "memory") // "memory" or "redis"

	// Redis defaults
	v.SetDefault(keyRedisAddr, "localhost:6379")
	v.SetDefault(keyRedisPassword, "")
	v.SetDefault(keyRedisDB, 0)
	v.SetDefault(keyRedisPoolSize, 10)

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
		JobQueueSize:       v.GetInt(keyJobQueueSize),
		PrepWorkers:        v.GetInt(keyPrepWorkers),
		OCRWorkers:         v.GetInt(keyOCRWorkers),
		JevWorkers:         v.GetInt(keyJevWorkers),
		IngestBuffer:       v.GetInt(keyIngestBuffer),
		OCRBuffer:          v.GetInt(keyOCRBuffer),
		JevBuffer:          v.GetInt(keyJevBuffer),
		JobTimeout:         v.GetDuration(keyJobTimeout),
		ShutdownTimeout:    v.GetDuration(keyShutdownTimeout),
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
		OCRMinConfidence:   v.GetFloat64(keyOCRMinConfidence),
		WorkerCountLocal:   resolveWorkerCount(v, keyWorkerCountLocal, keyOCRWorkers),
		WorkerCountAPI:     resolveWorkerCount(v, keyWorkerCountAPI, keyJevWorkers),
		OCRTimeout:         resolveDuration(v, keyOCRTimeout, keyOllamaTimeout),
		AssistTimeout:      resolveDuration(v, keyAssistTimeout, keyOpenRouterTimeout),
		JevTimeout:         resolveDuration(v, keyJevTimeout, keyTypeSafeTimeout),
		DebugPprof:         v.GetBool(keyDebugPprof),
		DebugPprofAddr:     strings.TrimSpace(v.GetString(keyDebugPprofAddr)),
		OCRCacheEnabled:    v.GetBool(keyOCRCacheEnabled),
		OCRCacheTTL:        v.GetDuration(keyOCRCacheTTL),

		// Database backend
		DBBackend: strings.ToLower(strings.TrimSpace(v.GetString(keyDBBackend))),

		// PostgreSQL
		PGHost:            strings.TrimSpace(v.GetString(keyPGHost)),
		PGPort:            v.GetInt(keyPGPort),
		PGUser:            strings.TrimSpace(v.GetString(keyPGUser)),
		PGPassword:        v.GetString(keyPGPassword),
		PGDBName:          strings.TrimSpace(v.GetString(keyPGDBName)),
		PGSSLMode:         strings.TrimSpace(v.GetString(keyPGSSLMode)),
		PGMaxOpenConns:    v.GetInt(keyPGMaxOpenConns),
		PGMaxIdleConns:    v.GetInt(keyPGMaxIdleConns),
		PGConnMaxLifetime: v.GetDuration(keyPGConnMaxLifetime),
		PGSyncCommit:      strings.ToLower(strings.TrimSpace(v.GetString(keyPGSyncCommit))),

		// Cache backend
		CacheBackend: strings.ToLower(strings.TrimSpace(v.GetString(keyCacheBackend))),

		// Redis
		RedisAddr:     strings.TrimSpace(v.GetString(keyRedisAddr)),
		RedisPassword: v.GetString(keyRedisPassword),
		RedisDB:       v.GetInt(keyRedisDB),
		RedisPoolSize: v.GetInt(keyRedisPoolSize),
	}

	if cfg.MaxUploadBytes <= 0 {
		return Config{}, fmt.Errorf("invalid MAX_UPLOAD_BYTES: %d", cfg.MaxUploadBytes)
	}

	if cfg.WorkerCount <= 0 {
		return Config{}, fmt.Errorf("invalid WORKER_COUNT: %d", cfg.WorkerCount)
	}

	if cfg.JobQueueSize <= 0 {
		return Config{}, fmt.Errorf("invalid JOB_QUEUE_SIZE: %d", cfg.JobQueueSize)
	}

	if cfg.PrepWorkers <= 0 {
		return Config{}, fmt.Errorf("invalid PREP_WORKERS: %d", cfg.PrepWorkers)
	}

	if cfg.OCRWorkers <= 0 {
		return Config{}, fmt.Errorf("invalid OCR_WORKERS: %d", cfg.OCRWorkers)
	}

	if cfg.JevWorkers <= 0 {
		return Config{}, fmt.Errorf("invalid JEV_WORKERS: %d", cfg.JevWorkers)
	}

	if cfg.IngestBuffer <= 0 {
		return Config{}, fmt.Errorf("invalid INGEST_BUFFER: %d", cfg.IngestBuffer)
	}

	if cfg.OCRBuffer <= 0 {
		return Config{}, fmt.Errorf("invalid OCR_BUFFER: %d", cfg.OCRBuffer)
	}

	if cfg.JevBuffer <= 0 {
		return Config{}, fmt.Errorf("invalid JEV_BUFFER: %d", cfg.JevBuffer)
	}

	if cfg.JobTimeout <= 0 {
		return Config{}, fmt.Errorf("invalid JOB_TIMEOUT: %s", v.GetString(keyJobTimeout))
	}

	if cfg.ShutdownTimeout <= 0 {
		return Config{}, fmt.Errorf("invalid SHUTDOWN_TIMEOUT: %s", v.GetString(keyShutdownTimeout))
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

	if cfg.OCRMinConfidence < 0 || cfg.OCRMinConfidence > 1 {
		return Config{}, fmt.Errorf("invalid OCR_MIN_CONFIDENCE: %v", cfg.OCRMinConfidence)
	}

	if cfg.WorkerCountLocal <= 0 {
		return Config{}, fmt.Errorf("invalid WORKER_COUNT_LOCAL: %d", cfg.WorkerCountLocal)
	}

	if cfg.WorkerCountAPI <= 0 {
		return Config{}, fmt.Errorf("invalid WORKER_COUNT_API: %d", cfg.WorkerCountAPI)
	}

	if cfg.OCRTimeout <= 0 {
		return Config{}, fmt.Errorf("invalid OCR_TIMEOUT: %s", v.GetString(keyOCRTimeout))
	}

	if cfg.AssistTimeout <= 0 {
		return Config{}, fmt.Errorf("invalid ASSIST_TIMEOUT: %s", v.GetString(keyAssistTimeout))
	}

	if cfg.JevTimeout <= 0 {
		return Config{}, fmt.Errorf("invalid JEV_TIMEOUT: %s", v.GetString(keyJevTimeout))
	}

	if cfg.DebugPprofAddr == "" {
		return Config{}, fmt.Errorf("invalid DEBUG_PPROF_ADDR: empty")
	}

	switch cfg.LogFormat {
	case "text", "json":
	default:
		return Config{}, fmt.Errorf("invalid LOG_FORMAT: %s", cfg.LogFormat)
	}

	// Validate database backend
	switch cfg.DBBackend {
	case "sqlite", "postgres":
	default:
		return Config{}, fmt.Errorf("invalid DB_BACKEND: %s (must be 'sqlite' or 'postgres')", cfg.DBBackend)
	}

	// Validate PostgreSQL settings when using postgres backend
	if cfg.DBBackend == "postgres" {
		if cfg.PGHost == "" {
			return Config{}, fmt.Errorf("PG_HOST is required when DB_BACKEND=postgres")
		}
		if cfg.PGPort <= 0 || cfg.PGPort > 65535 {
			return Config{}, fmt.Errorf("invalid PG_PORT: %d", cfg.PGPort)
		}
		if cfg.PGUser == "" {
			return Config{}, fmt.Errorf("PG_USER is required when DB_BACKEND=postgres")
		}
		if cfg.PGDBName == "" {
			return Config{}, fmt.Errorf("PG_DBNAME is required when DB_BACKEND=postgres")
		}
		switch cfg.PGSyncCommit {
		case "off", "local", "remote_write", "on":
		default:
			return Config{}, fmt.Errorf("invalid PG_SYNC_COMMIT: %s", cfg.PGSyncCommit)
		}
	}

	// Validate cache backend
	switch cfg.CacheBackend {
	case "memory", "redis":
	default:
		return Config{}, fmt.Errorf("invalid CACHE_BACKEND: %s (must be 'memory' or 'redis')", cfg.CacheBackend)
	}

	// Validate Redis settings when using redis backend
	if cfg.CacheBackend == "redis" {
		if cfg.RedisAddr == "" {
			return Config{}, fmt.Errorf("REDIS_ADDR is required when CACHE_BACKEND=redis")
		}
		if cfg.RedisDB < 0 || cfg.RedisDB > 15 {
			return Config{}, fmt.Errorf("invalid REDIS_DB: %d (must be 0-15)", cfg.RedisDB)
		}
		if cfg.RedisPoolSize <= 0 {
			return Config{}, fmt.Errorf("invalid REDIS_POOL_SIZE: %d", cfg.RedisPoolSize)
		}
	}

	return cfg, nil
}

func resolveWorkerCount(v *viper.Viper, primary, fallback string) int {
	if v.IsSet(primary) {
		n := v.GetInt(primary)
		if n > 0 {
			return n
		}
	}

	return v.GetInt(fallback)
}

func resolveDuration(v *viper.Viper, primary, fallback string) time.Duration {
	if v.IsSet(primary) {
		d := v.GetDuration(primary)
		if d > 0 {
			return d
		}
	}

	return v.GetDuration(fallback)
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
