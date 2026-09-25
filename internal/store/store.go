// Package store persists invoices with GORM and SQLite.
package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// ErrClosed is returned when a write is attempted after Close.
var ErrClosed = errors.New("store closed")

type writeOp struct {
	fn   func() error
	done chan error
}

// Store wraps a GORM database. Writes are serialized on a single goroutine
// so concurrent workers do not hit "database is locked".
type Store struct {
	db     *gorm.DB
	writes chan writeOp
	done   chan struct{}

	mu         sync.Mutex
	stopped    bool
	isPostgres bool
}

// Open creates the SQLite file if needed, enables WAL, and starts the writer.
func Open(ctx context.Context, sqlitePath string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(sqlitePath), 0o750); err != nil {
		return nil, fmt.Errorf("create sqlite dir: %w", err)
	}

	dsn := sqlitePath + "?_journal_mode=WAL&_busy_timeout=5000&_synchronous=NORMAL"

	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("sql db: %w", err)
	}

	pragmas := []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA synchronous=NORMAL",
	}
	for _, pragma := range pragmas {
		if _, err := sqlDB.ExecContext(ctx, pragma); err != nil {
			_ = sqlDB.Close()

			return nil, fmt.Errorf("sqlite %s: %w", pragma, err)
		}
	}

	if err := db.WithContext(ctx).AutoMigrate(&Invoice{}); err != nil {
		_ = sqlDB.Close()

		return nil, fmt.Errorf("migrate: %w", err)
	}

	s := &Store{
		db:     db,
		writes: make(chan writeOp),
		done:   make(chan struct{}),
	}
	go s.writerLoop()

	return s, nil
}

func (s *Store) writerLoop() {
	defer close(s.done)

	for op := range s.writes {
		op.done <- op.fn()
	}
}

func (s *Store) doWrite(ctx context.Context, fn func() error) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	op := writeOp{fn: fn, done: make(chan error, 1)}

	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()

		return ErrClosed
	}

	s.writes <- op
	s.mu.Unlock()

	select {
	case err := <-op.done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Close stops the writer loop and closes the SQL handle.
func (s *Store) Close() error {
	if s == nil {
		return nil
	}

	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()

		return nil
	}

	s.stopped = true
	close(s.writes)
	s.mu.Unlock()

	<-s.done

	sqlDB, err := s.db.DB()
	if err != nil {
		return fmt.Errorf("sql db: %w", err)
	}

	if err := sqlDB.Close(); err != nil {
		return fmt.Errorf("close sqlite: %w", err)
	}

	return nil
}

// DB exposes the underlying handle for health checks.
func (s *Store) DB() *gorm.DB {
	return s.db
}

// Ping verifies the database connection.
func (s *Store) Ping(ctx context.Context) error {
	sqlDB, err := s.db.DB()
	if err != nil {
		return fmt.Errorf("sql db: %w", err)
	}

	if err := sqlDB.PingContext(ctx); err != nil {
		return fmt.Errorf("ping sqlite: %w", err)
	}

	return nil
}

// Create inserts a new invoice.
func (s *Store) Create(ctx context.Context, inv *Invoice) error {
	err := s.doWrite(ctx, func() error {
		return s.db.WithContext(ctx).Create(inv).Error
	})
	if err != nil {
		return fmt.Errorf("create invoice: %w", err)
	}

	return nil
}

// Update persists invoice changes.
func (s *Store) Update(ctx context.Context, inv *Invoice) error {
	err := s.doWrite(ctx, func() error {
		return s.db.WithContext(ctx).Save(inv).Error
	})
	if err != nil {
		return fmt.Errorf("update invoice: %w", err)
	}

	return nil
}

// Delete removes an invoice by id.
func (s *Store) Delete(ctx context.Context, id string) error {
	err := s.doWrite(ctx, func() error {
		return s.db.WithContext(ctx).Delete(&Invoice{}, "id = ?", id).Error
	})
	if err != nil {
		return fmt.Errorf("delete invoice: %w", err)
	}

	return nil
}

// Get loads one invoice by id.
func (s *Store) Get(ctx context.Context, id string) (*Invoice, error) {
	var inv Invoice
	if err := s.db.WithContext(ctx).First(&inv, "id = ?", id).Error; err != nil {
		return nil, err
	}

	return &inv, nil
}

// List returns recent invoices.
func (s *Store) List(ctx context.Context, limit, offset int) ([]Invoice, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	if offset < 0 {
		offset = 0
	}

	var rows []Invoice
	if err := s.db.WithContext(ctx).
		Order("created_at DESC").
		Limit(limit).
		Offset(offset).
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list invoices: %w", err)
	}

	return rows, nil
}
