package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoad_Defaults(t *testing.T) {
	t.Chdir(t.TempDir())
	clearConfigEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Port != "8080" {
		t.Fatalf("port %s", cfg.Port)
	}

	if cfg.TypeSafeTimeout != 30*time.Second {
		t.Fatalf("timeout %s", cfg.TypeSafeTimeout)
	}

	if cfg.TypeSafeAPIKey != "" {
		t.Fatalf("expected empty api key, got %q", cfg.TypeSafeAPIKey)
	}

	if cfg.LogFormat != "text" {
		t.Fatalf("log format %s", cfg.LogFormat)
	}

	if cfg.JobQueueSize != 32 {
		t.Fatalf("queue size %d", cfg.JobQueueSize)
	}

	if cfg.OCRMinConfidence != 0.75 {
		t.Fatalf("min confidence %v", cfg.OCRMinConfidence)
	}

	if cfg.WorkerCountLocal != 2 {
		t.Fatalf("local workers %d", cfg.WorkerCountLocal)
	}

	if cfg.WorkerCountAPI != 2 {
		t.Fatalf("api workers %d", cfg.WorkerCountAPI)
	}

	if cfg.OCRTimeout != 120*time.Second {
		t.Fatalf("ocr timeout %s", cfg.OCRTimeout)
	}

	if cfg.AssistTimeout != 45*time.Second {
		t.Fatalf("assist timeout %s", cfg.AssistTimeout)
	}

	if cfg.JevTimeout != 30*time.Second {
		t.Fatalf("jev timeout %s", cfg.JevTimeout)
	}

	if cfg.DebugPprof {
		t.Fatal("pprof should be off by default")
	}

	if cfg.DebugPprofAddr != "127.0.0.1:6060" {
		t.Fatalf("pprof addr %s", cfg.DebugPprofAddr)
	}

	if cfg.PrepWorkers != 1 {
		t.Fatalf("prep workers %d", cfg.PrepWorkers)
	}

	if !cfg.OCRCacheEnabled {
		t.Fatal("cache should be enabled by default")
	}

	if cfg.OCRCacheTTL != time.Hour {
		t.Fatalf("cache ttl %s", cfg.OCRCacheTTL)
	}
}

func TestLoad_InvalidJobQueueSize(t *testing.T) {
	t.Chdir(t.TempDir())
	clearConfigEnv(t)
	t.Setenv(keyJobQueueSize, "0")

	if _, err := Load(); err == nil {
		t.Fatal("expected error")
	}
}

func TestLoad_WorkerAliases(t *testing.T) {
	t.Chdir(t.TempDir())
	clearConfigEnv(t)
	t.Setenv(keyOCRWorkers, "3")
	t.Setenv(keyJevWorkers, "5")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}

	if cfg.WorkerCountLocal != 3 || cfg.WorkerCountAPI != 5 {
		t.Fatalf("alias local=%d api=%d", cfg.WorkerCountLocal, cfg.WorkerCountAPI)
	}

	t.Setenv(keyWorkerCountLocal, "4")
	t.Setenv(keyWorkerCountAPI, "8")
	t.Setenv(keyOCRTimeout, "30s")
	t.Setenv(keyAssistTimeout, "45s")
	t.Setenv(keyJevTimeout, "30s")

	cfg, err = Load()
	if err != nil {
		t.Fatal(err)
	}

	if cfg.WorkerCountLocal != 4 || cfg.WorkerCountAPI != 8 {
		t.Fatalf("override local=%d api=%d", cfg.WorkerCountLocal, cfg.WorkerCountAPI)
	}

	if cfg.OCRTimeout != 30*time.Second || cfg.AssistTimeout != 45*time.Second || cfg.JevTimeout != 30*time.Second {
		t.Fatalf("hop timeouts %s %s %s", cfg.OCRTimeout, cfg.AssistTimeout, cfg.JevTimeout)
	}
}

func TestLoad_InvalidWorkerCount(t *testing.T) {
	t.Chdir(t.TempDir())
	clearConfigEnv(t)
	t.Setenv(keyWorkerCount, "0")

	if _, err := Load(); err == nil {
		t.Fatal("expected error")
	}
}

func TestLoad_DotEnvAPIKey(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	clearConfigEnv(t)

	content := "TYPESAFE_API_KEY=from-file\nPORT=9090\n"
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}

	if cfg.TypeSafeAPIKey != "from-file" {
		t.Fatalf("api key %q", cfg.TypeSafeAPIKey)
	}

	if cfg.Port != "9090" {
		t.Fatalf("port %s", cfg.Port)
	}
}

func TestLoad_DotEnvWinsOverStaleEnv(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	clearConfigEnv(t)

	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("TYPESAFE_API_KEY='from-file'\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv(keyTypeSafeAPIKey, "stale-env-key-that-is-forty-four-chars!!")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}

	if cfg.TypeSafeAPIKey != "from-file" {
		t.Fatalf("api key %q", cfg.TypeSafeAPIKey)
	}
}

func clearConfigEnv(t *testing.T) {
	t.Helper()

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
	} {
		t.Setenv(key, "")
	}
}
