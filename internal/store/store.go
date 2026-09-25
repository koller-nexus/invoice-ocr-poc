// Package store persists invoices with GORM and SQLite.
package store

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// Store wraps a GORM database.
type Store struct {
	db *gorm.DB
}

// Open creates the SQLite file if needed and migrates models.
func Open(ctx context.Context, sqlitePath string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(sqlitePath), 0o750); err != nil {
		return nil, fmt.Errorf("create sqlite dir: %w", err)
	}

	db, err := gorm.Open(sqlite.Open(sqlitePath), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	if err := db.WithContext(ctx).AutoMigrate(&Invoice{}); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}

	return &Store{db: db}, nil
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
	if err := s.db.WithContext(ctx).Create(inv).Error; err != nil {
		return fmt.Errorf("create invoice: %w", err)
	}

	return nil
}

// Update persists invoice changes.
func (s *Store) Update(ctx context.Context, inv *Invoice) error {
	if err := s.db.WithContext(ctx).Save(inv).Error; err != nil {
		return fmt.Errorf("update invoice: %w", err)
	}

	return nil
}

// Delete removes an invoice by id.
func (s *Store) Delete(ctx context.Context, id string) error {
	if err := s.db.WithContext(ctx).Delete(&Invoice{}, "id = ?", id).Error; err != nil {
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
