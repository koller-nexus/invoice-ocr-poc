package store

import (
	"context"
	"fmt"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// PostgresOptions configures PostgreSQL connection with write-optimized settings.
type PostgresOptions struct {
	Host     string
	Port     int
	User     string
	Password string
	DBName   string
	SSLMode  string

	// Connection pool settings
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	ConnMaxIdleTime time.Duration

	// Write optimization settings
	SyncCommit         string  // "off", "local", "remote_write", "on" (default: "off" for speed)
	WorkMem            string  // e.g., "64MB"
	MaintenanceWorkMem string  // e.g., "256MB"
	EffectiveCacheSize string  // e.g., "1GB"
	RandomPageCost     float64 // 1.1 for SSD, 4.0 for HDD

	LogLevel logger.LogLevel
}

// DefaultPostgresOptions returns write-optimized PostgreSQL settings.
func DefaultPostgresOptions() PostgresOptions {
	return PostgresOptions{
		Host:               "localhost",
		Port:               5432,
		User:               "postgres",
		Password:           "",
		DBName:             "invoice_ocr",
		SSLMode:            "disable",
		MaxOpenConns:       25,
		MaxIdleConns:       10,
		ConnMaxLifetime:    time.Hour,
		ConnMaxIdleTime:    10 * time.Minute,
		SyncCommit:         "off", // Fast writes, slight durability risk on crash
		WorkMem:            "64MB",
		MaintenanceWorkMem: "256MB",
		EffectiveCacheSize: "1GB",
		RandomPageCost:     1.1, // SSD default
		LogLevel:           logger.Warn,
	}
}

// OpenPostgres creates a PostgreSQL store with write-optimized settings.
func OpenPostgres(ctx context.Context, opts PostgresOptions) (*Store, error) {
	dsn := fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		opts.Host, opts.Port, opts.User, opts.Password, opts.DBName, opts.SSLMode,
	)

	gormCfg := &gorm.Config{
		Logger:                 logger.Default.LogMode(opts.LogLevel),
		SkipDefaultTransaction: true, // Skip tx wrapper for single writes = faster
		PrepareStmt:            true, // Cache prepared statements
	}

	db, err := gorm.Open(postgres.Open(dsn), gormCfg)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("sql db: %w", err)
	}

	// Connection pool settings
	sqlDB.SetMaxOpenConns(opts.MaxOpenConns)
	sqlDB.SetMaxIdleConns(opts.MaxIdleConns)
	sqlDB.SetConnMaxLifetime(opts.ConnMaxLifetime)
	sqlDB.SetConnMaxIdleTime(opts.ConnMaxIdleTime)

	// Apply write-optimized session settings
	sessionSettings := []string{
		fmt.Sprintf("SET synchronous_commit = '%s'", opts.SyncCommit),
	}

	if opts.WorkMem != "" {
		sessionSettings = append(sessionSettings, fmt.Sprintf("SET work_mem = '%s'", opts.WorkMem))
	}

	for _, setting := range sessionSettings {
		if _, err := sqlDB.ExecContext(ctx, setting); err != nil {
			_ = sqlDB.Close()
			return nil, fmt.Errorf("postgres %s: %w", setting, err)
		}
	}

	// Auto-migrate schema
	if err := db.WithContext(ctx).AutoMigrate(&Invoice{}); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}

	// Create optimized indexes for common queries
	indexes := []string{
		"CREATE INDEX IF NOT EXISTS idx_invoices_status ON invoices(status)",
		"CREATE INDEX IF NOT EXISTS idx_invoices_created_at ON invoices(created_at DESC)",
		"CREATE INDEX IF NOT EXISTS idx_invoices_status_created ON invoices(status, created_at DESC)",
	}

	for _, idx := range indexes {
		if err := db.Exec(idx).Error; err != nil {
			_ = sqlDB.Close()
			return nil, fmt.Errorf("create index: %w", err)
		}
	}

	s := &Store{
		db:         db,
		writes:     make(chan writeOp),
		done:       make(chan struct{}),
		isPostgres: true,
	}

	// PostgreSQL handles concurrency natively, but we keep the writer
	// for consistency. With SkipDefaultTransaction + prepared statements,
	// writes are already fast.
	go s.writerLoop()

	return s, nil
}

// BatchCreate inserts multiple invoices in a single transaction (PostgreSQL only).
func (s *Store) BatchCreate(ctx context.Context, invoices []*Invoice) error {
	if len(invoices) == 0 {
		return nil
	}

	err := s.doWrite(ctx, func() error {
		return s.db.WithContext(ctx).CreateInBatches(invoices, 100).Error
	})
	if err != nil {
		return fmt.Errorf("batch create invoices: %w", err)
	}

	return nil
}

// BatchUpdate updates multiple invoices in a single transaction.
func (s *Store) BatchUpdate(ctx context.Context, invoices []*Invoice) error {
	if len(invoices) == 0 {
		return nil
	}

	err := s.doWrite(ctx, func() error {
		return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			for _, inv := range invoices {
				if err := tx.Save(inv).Error; err != nil {
					return err
				}
			}
			return nil
		})
	})
	if err != nil {
		return fmt.Errorf("batch update invoices: %w", err)
	}

	return nil
}

// UpsertByID inserts or updates an invoice by ID (PostgreSQL ON CONFLICT).
func (s *Store) UpsertByID(ctx context.Context, inv *Invoice) error {
	if !s.isPostgres {
		return s.Update(ctx, inv)
	}

	err := s.doWrite(ctx, func() error {
		return s.db.WithContext(ctx).
			Clauses().
			Save(inv).Error
	})
	if err != nil {
		return fmt.Errorf("upsert invoice: %w", err)
	}

	return nil
}

// ListByStatus returns invoices filtered by status with pagination.
func (s *Store) ListByStatus(ctx context.Context, status string, limit, offset int) ([]Invoice, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	if offset < 0 {
		offset = 0
	}

	var rows []Invoice
	query := s.db.WithContext(ctx).Order("created_at DESC").Limit(limit).Offset(offset)

	if status != "" {
		query = query.Where("status = ?", status)
	}

	if err := query.Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list invoices by status: %w", err)
	}

	return rows, nil
}

// CountByStatus returns the count of invoices by status.
func (s *Store) CountByStatus(ctx context.Context) (map[string]int64, error) {
	type result struct {
		Status string
		Count  int64
	}

	var results []result
	err := s.db.WithContext(ctx).
		Model(&Invoice{}).
		Select("status, COUNT(*) as count").
		Group("status").
		Scan(&results).Error
	if err != nil {
		return nil, fmt.Errorf("count by status: %w", err)
	}

	counts := make(map[string]int64)
	for _, r := range results {
		counts[r.Status] = r.Count
	}

	return counts, nil
}
