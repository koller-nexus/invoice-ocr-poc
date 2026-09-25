//go:build integration

package store_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/williamkoller/invoice-ocr-poc/internal/store"
	"gorm.io/gorm/logger"
)

func pgOptions() store.PostgresOptions {
	opts := store.DefaultPostgresOptions()

	if host := os.Getenv("PG_HOST"); host != "" {
		opts.Host = host
	}
	if user := os.Getenv("PG_USER"); user != "" {
		opts.User = user
	}
	if pass := os.Getenv("PG_PASSWORD"); pass != "" {
		opts.Password = pass
	}
	if db := os.Getenv("PG_DBNAME"); db != "" {
		opts.DBName = db
	}

	opts.LogLevel = logger.Silent
	return opts
}

func TestPostgres_OpenAndPing(t *testing.T) {
	ctx := t.Context()

	st, err := store.OpenPostgres(ctx, pgOptions())
	require.NoError(t, err)
	defer st.Close()

	err = st.Ping(ctx)
	require.NoError(t, err)
}

func TestPostgres_CRUD(t *testing.T) {
	ctx := t.Context()

	st, err := store.OpenPostgres(ctx, pgOptions())
	require.NoError(t, err)
	defer st.Close()

	// Create
	inv := &store.Invoice{
		ID:           "test-pg-" + time.Now().Format("20060102150405"),
		Status:       store.StatusQueued,
		OriginalName: "test.jpg",
		StoredPath:   "/tmp/test.jpg",
		MimeType:     "image/jpeg",
	}

	err = st.Create(ctx, inv)
	require.NoError(t, err)

	// Read
	got, err := st.Get(ctx, inv.ID)
	require.NoError(t, err)
	require.Equal(t, inv.ID, got.ID)
	require.Equal(t, store.StatusQueued, got.Status)

	// Update
	got.Status = store.StatusProcessing
	got.OCRText = "Sample OCR text"
	err = st.Update(ctx, got)
	require.NoError(t, err)

	updated, err := st.Get(ctx, inv.ID)
	require.NoError(t, err)
	require.Equal(t, store.StatusProcessing, updated.Status)
	require.Equal(t, "Sample OCR text", updated.OCRText)

	// Delete
	err = st.Delete(ctx, inv.ID)
	require.NoError(t, err)

	_, err = st.Get(ctx, inv.ID)
	require.Error(t, err)
}

func TestPostgres_List(t *testing.T) {
	ctx := t.Context()

	st, err := store.OpenPostgres(ctx, pgOptions())
	require.NoError(t, err)
	defer st.Close()

	// Create multiple invoices
	prefix := "test-list-" + time.Now().Format("20060102150405") + "-"
	for i := range 5 {
		inv := &store.Invoice{
			ID:     prefix + string(rune('a'+i)),
			Status: store.StatusQueued,
		}
		err := st.Create(ctx, inv)
		require.NoError(t, err)
	}

	// List
	list, err := st.List(ctx, 10, 0)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(list), 5)

	// Cleanup
	for i := range 5 {
		_ = st.Delete(ctx, prefix+string(rune('a'+i)))
	}
}

func TestPostgres_BatchCreate(t *testing.T) {
	ctx := t.Context()

	st, err := store.OpenPostgres(ctx, pgOptions())
	require.NoError(t, err)
	defer st.Close()

	prefix := "test-batch-" + time.Now().Format("20060102150405") + "-"
	invoices := make([]*store.Invoice, 10)
	for i := range invoices {
		invoices[i] = &store.Invoice{
			ID:     prefix + string(rune('a'+i)),
			Status: store.StatusQueued,
		}
	}

	err = st.BatchCreate(ctx, invoices)
	require.NoError(t, err)

	// Verify all were created
	for _, inv := range invoices {
		got, err := st.Get(ctx, inv.ID)
		require.NoError(t, err)
		require.Equal(t, inv.ID, got.ID)
	}

	// Cleanup
	for _, inv := range invoices {
		_ = st.Delete(ctx, inv.ID)
	}
}

func TestPostgres_ListByStatus(t *testing.T) {
	ctx := t.Context()

	st, err := store.OpenPostgres(ctx, pgOptions())
	require.NoError(t, err)
	defer st.Close()

	prefix := "test-status-" + time.Now().Format("20060102150405") + "-"

	// Create invoices with different statuses
	statuses := []string{store.StatusQueued, store.StatusProcessing, store.StatusDone, store.StatusFailed}
	for i, status := range statuses {
		inv := &store.Invoice{
			ID:     prefix + string(rune('a'+i)),
			Status: status,
		}
		err := st.Create(ctx, inv)
		require.NoError(t, err)
	}

	// List by status
	queued, err := st.ListByStatus(ctx, store.StatusQueued, 10, 0)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(queued), 1)

	done, err := st.ListByStatus(ctx, store.StatusDone, 10, 0)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(done), 1)

	// Cleanup
	for i := range statuses {
		_ = st.Delete(ctx, prefix+string(rune('a'+i)))
	}
}

func TestPostgres_CountByStatus(t *testing.T) {
	ctx := t.Context()

	st, err := store.OpenPostgres(ctx, pgOptions())
	require.NoError(t, err)
	defer st.Close()

	counts, err := st.CountByStatus(ctx)
	require.NoError(t, err)
	require.NotNil(t, counts)
	// At least we got a valid map back
}

func TestPostgres_ConnectionFailure(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	opts := pgOptions()
	opts.Host = "localhost"
	opts.Port = 59999 // Invalid port

	_, err := store.OpenPostgres(ctx, opts)
	require.Error(t, err)
}
