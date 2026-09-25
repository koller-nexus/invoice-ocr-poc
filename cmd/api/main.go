package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/pprof"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/williamkoller/invoice-ocr-poc/internal/cache"
	"github.com/williamkoller/invoice-ocr-poc/internal/config"
	"github.com/williamkoller/invoice-ocr-poc/internal/httpclient"
	"github.com/williamkoller/invoice-ocr-poc/internal/httpserver"
	"github.com/williamkoller/invoice-ocr-poc/internal/invoice"
	"github.com/williamkoller/invoice-ocr-poc/internal/jev"
	"github.com/williamkoller/invoice-ocr-poc/internal/ocr"
	"github.com/williamkoller/invoice-ocr-poc/internal/openrouter"
	"github.com/williamkoller/invoice-ocr-poc/internal/preprocess"
	"github.com/williamkoller/invoice-ocr-poc/internal/store"
	"github.com/williamkoller/invoice-ocr-poc/internal/worker"
	"go.uber.org/zap"
	"gorm.io/gorm/logger"
)

func main() {
	if err := run(); err != nil {
		log := zap.Must(zap.NewProduction()).Sugar()
		log.Errorw("server exited", "err", err)
		_ = log.Sync()
		os.Exit(1)
	}
}

func newLogger(format string) (*zap.SugaredLogger, error) {
	var cfg zap.Config
	if format == "json" {
		cfg = zap.NewProductionConfig()
	} else {
		cfg = zap.NewDevelopmentConfig()
	}

	cfg.Level = zap.NewAtomicLevelAt(zap.InfoLevel)

	logger, err := cfg.Build()
	if err != nil {
		return nil, fmt.Errorf("build logger: %w", err)
	}

	return logger.Sugar(), nil
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	log, err := newLogger(cfg.LogFormat)
	if err != nil {
		return err
	}
	defer func() { _ = log.Sync() }()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Initialize database backend
	st, err := openStore(ctx, cfg, log)
	if err != nil {
		return err
	}
	defer func() {
		if err := st.Close(); err != nil {
			log.Errorw("store close", "err", err)
		}
	}()

	// Initialize cache backend
	ocrCache, cacheCloser, err := openCache(ctx, cfg, log)
	if err != nil {
		return err
	}
	if cacheCloser != nil {
		defer func() {
			if err := cacheCloser.Close(); err != nil {
				log.Errorw("cache close", "err", err)
			}
		}()
	}

	sharedHTTP := httpclient.New()
	tess := ocr.NewTesseract(cfg.TesseractLang)
	ollama := ocr.NewOllama(ocr.OllamaOptions{
		BaseURL:    cfg.OllamaURL,
		Model:      cfg.OllamaOCRModel,
		HTTPClient: sharedHTTP,
		Log:        log,
	})
	orOCR := ocr.NewOpenRouter(ocr.OpenRouterOptions{
		BaseURL:    cfg.OpenRouterBaseURL,
		APIKey:     cfg.OpenRouterAPIKey,
		Model:      cfg.OpenRouterOCRModel,
		HTTPClient: sharedHTTP,
		Log:        log,
	})

	prep := preprocess.New()
	judge := jev.NewClient(cfg.TypeSafeBaseURL, cfg.TypeSafeAPIKey, sharedHTTP)
	assist := openrouter.NewClient(
		cfg.OpenRouterBaseURL,
		cfg.OpenRouterAPIKey,
		cfg.OpenRouterModel,
		sharedHTTP,
	).WithLogger(log)

	var svc *invoice.Service

	pipeline := worker.NewPipeline(worker.PipelineOptions{
		PrepN:        cfg.PrepWorkers,
		OCRN:         cfg.WorkerCountLocal,
		JevN:         cfg.WorkerCountAPI,
		IngestBuffer: cfg.IngestBuffer,
		OCRBuffer:    cfg.OCRBuffer,
		JevBuffer:    cfg.JevBuffer,
		JobTimeout:   cfg.JobTimeout,
		Prep: func(ctx context.Context, j *worker.Job) error {
			return svc.StagePreprocess(ctx, j)
		},
		OCR: func(ctx context.Context, j *worker.Job) error {
			return svc.StageOCR(ctx, j, invoice.OCRInput{
				Tess:   tess,
				Ollama: ollama,
			})
		},
		Jev: func(ctx context.Context, j *worker.Job) error {
			return svc.StageJev(ctx, j, invoice.JevInput{
				Judge:      judge,
				Assist:     assist,
				OpenRouter: orOCR,
			})
		},
		Log: log,
	})

	svc = invoice.NewService(st, pipeline, cfg.UploadDir, cfg.MaxUploadBytes, prep, log)
	svc.Configure(cfg.OCRMinConfidence, cfg.OCRTimeout, cfg.AssistTimeout, cfg.JevTimeout)
	svc.SetCache(ocrCache)
	pipeline.Start(ctx)

	router := httpserver.NewRouter(httpserver.Deps{
		Service:            svc,
		Store:              st,
		OCR:                tess,
		Ollama:             ollama,
		HasAPIKey:          cfg.TypeSafeAPIKey != "",
		HasOpenRouterKey:   cfg.OpenRouterAPIKey != "",
		MaxBodyBytes:       cfg.MaxUploadBytes,
		OCRName:            cfg.OCREngine,
		OllamaModel:        cfg.OllamaOCRModel,
		OpenRouterModel:    cfg.OpenRouterModel,
		OpenRouterOCRModel: cfg.OpenRouterOCRModel,
	})

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)

	go func() {
		log.Infow("listening", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	var pprofSrv *http.Server
	if cfg.DebugPprof {
		pprofSrv, err = startPprof(cfg.DebugPprofAddr, log)
		if err != nil {
			stop()
			pipeline.Shutdown(cfg.ShutdownTimeout)

			return err
		}
	}

	var serverErr error

	select {
	case <-ctx.Done():
	case err := <-errCh:
		stop()

		serverErr = err
		log.Errorw("server error", "err", err)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Errorw("http shutdown error", "err", err)
	}

	if pprofSrv != nil {
		if err := pprofSrv.Shutdown(shutdownCtx); err != nil {
			log.Errorw("pprof shutdown error", "err", err)
		}
	}

	pipeline.Shutdown(cfg.ShutdownTimeout)

	return serverErr
}

func startPprof(addr string, log *zap.SugaredLogger) (*http.Server, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("debug pprof addr: %w", err)
	}

	if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
		if host != "localhost" {
			return nil, fmt.Errorf("DEBUG_PPROF_ADDR must bind loopback, got %s", addr)
		}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)

	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		log.Infow("pprof listening", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Errorw("pprof error", "err", err)
		}
	}()

	return srv, nil
}

// openStore initializes the database backend based on configuration.
func openStore(ctx context.Context, cfg config.Config, log *zap.SugaredLogger) (*store.Store, error) {
	switch cfg.DBBackend {
	case "postgres":
		log.Infow("store.init",
			"backend", "postgres",
			"host", cfg.PGHost,
			"port", cfg.PGPort,
			"db", cfg.PGDBName,
			"sync_commit", cfg.PGSyncCommit,
			"max_open_conns", cfg.PGMaxOpenConns,
		)

		opts := store.PostgresOptions{
			Host:               cfg.PGHost,
			Port:               cfg.PGPort,
			User:               cfg.PGUser,
			Password:           cfg.PGPassword,
			DBName:             cfg.PGDBName,
			SSLMode:            cfg.PGSSLMode,
			MaxOpenConns:       cfg.PGMaxOpenConns,
			MaxIdleConns:       cfg.PGMaxIdleConns,
			ConnMaxLifetime:    cfg.PGConnMaxLifetime,
			ConnMaxIdleTime:    10 * time.Minute,
			SyncCommit:         cfg.PGSyncCommit,
			WorkMem:            "64MB",
			MaintenanceWorkMem: "256MB",
			LogLevel:           logger.Warn,
		}

		return store.OpenPostgres(ctx, opts)

	default: // sqlite
		log.Infow("store.init", "backend", "sqlite", "path", cfg.SQLitePath)
		return store.Open(ctx, cfg.SQLitePath)
	}
}

// openCache initializes the cache backend based on configuration.
func openCache(ctx context.Context, cfg config.Config, log *zap.SugaredLogger) (cache.Cache, io.Closer, error) {
	if !cfg.OCRCacheEnabled {
		log.Infow("cache.init", "backend", "disabled")
		return cache.New(0, false), nil, nil
	}

	switch cfg.CacheBackend {
	case "redis":
		log.Infow("cache.init",
			"backend", "redis",
			"addr", cfg.RedisAddr,
			"db", cfg.RedisDB,
			"pool_size", cfg.RedisPoolSize,
			"ttl", cfg.OCRCacheTTL,
		)

		rc, err := cache.NewRedis(ctx, cache.RedisOptions{
			Addr:     cfg.RedisAddr,
			Password: cfg.RedisPassword,
			DB:       cfg.RedisDB,
			TTL:      cfg.OCRCacheTTL,
			Prefix:   "invoice-ocr:",
			PoolSize: cfg.RedisPoolSize,
		})
		if err != nil {
			return nil, nil, fmt.Errorf("redis cache: %w", err)
		}

		return rc, rc, nil

	default: // memory
		log.Infow("cache.init", "backend", "memory", "ttl", cfg.OCRCacheTTL)
		return cache.New(cfg.OCRCacheTTL, true), nil, nil
	}
}
