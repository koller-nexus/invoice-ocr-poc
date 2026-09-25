package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/williamkoller/tesseract-poc-go/internal/config"
	"github.com/williamkoller/tesseract-poc-go/internal/httpserver"
	"github.com/williamkoller/tesseract-poc-go/internal/invoice"
	"github.com/williamkoller/tesseract-poc-go/internal/jev"
	"github.com/williamkoller/tesseract-poc-go/internal/ocr"
	"github.com/williamkoller/tesseract-poc-go/internal/openrouter"
	"github.com/williamkoller/tesseract-poc-go/internal/preprocess"
	"github.com/williamkoller/tesseract-poc-go/internal/store"
	"github.com/williamkoller/tesseract-poc-go/internal/worker"
)

func main() {
	if err := run(); err != nil {
		slog.Error("server exited", "err", err)
		os.Exit(1)
	}
}

func newLogger(format string) *slog.Logger {
	opts := &slog.HandlerOptions{Level: slog.LevelInfo}
	var h slog.Handler
	if format == "json" {
		h = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		h = slog.NewTextHandler(os.Stdout, opts)
	}

	return slog.New(h)
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	log := newLogger(cfg.LogFormat)
	slog.SetDefault(log)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	st, err := store.Open(ctx, cfg.SQLitePath)
	if err != nil {
		return err
	}

	engine := ocr.NewEngine(ocr.EngineOptions{
		Name:     cfg.OCREngine,
		TessLang: cfg.TesseractLang,
		OpenRouter: ocr.OpenRouterOptions{
			BaseURL: cfg.OpenRouterBaseURL,
			APIKey:  cfg.OpenRouterAPIKey,
			Model:   cfg.OpenRouterOCRModel,
			Timeout: cfg.OpenRouterTimeout,
		},
		Ollama: ocr.OllamaOptions{
			BaseURL: cfg.OllamaURL,
			Model:   cfg.OllamaOCRModel,
			Timeout: cfg.OllamaTimeout,
		},
	})
	prep := preprocess.New()
	judge := jev.NewClient(cfg.TypeSafeBaseURL, cfg.TypeSafeAPIKey, cfg.TypeSafeTimeout)
	assist := openrouter.NewClient(cfg.OpenRouterBaseURL, cfg.OpenRouterAPIKey, cfg.OpenRouterModel, cfg.OpenRouterTimeout)

	var svc *invoice.Service

	pool := worker.NewPool(cfg.WorkerCount, func(jobCtx context.Context, id string) error {
		return svc.ProcessJob(jobCtx, id, engine, judge, assist)
	}, log)

	svc = invoice.NewService(st, pool, cfg.UploadDir, cfg.MaxUploadBytes, prep, log)
	pool.Start(ctx)

	router := httpserver.NewRouter(httpserver.Deps{
		Service:          svc,
		Store:            st,
		OCR:              engine,
		HasAPIKey:        cfg.TypeSafeAPIKey != "",
		HasOpenRouterKey: cfg.OpenRouterAPIKey != "",
		MaxBodyBytes:     cfg.MaxUploadBytes,
		OCRName:          cfg.OCREngine,
		GoCV:             prep.Available(),
	})

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)

	go func() {
		log.Info("listening", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
	case err := <-errCh:
		stop()
		pool.Shutdown()

		return err
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		pool.Shutdown()
		return err
	}

	pool.Shutdown()

	return nil
}
