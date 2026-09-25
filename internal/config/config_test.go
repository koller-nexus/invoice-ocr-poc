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
		t.Setenv(key, "")
	}
}
