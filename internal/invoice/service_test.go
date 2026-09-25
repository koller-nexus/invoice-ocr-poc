package invoice

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/williamkoller/invoice-ocr-poc/internal/store"
)

type okJobs struct{}

func (okJobs) Enqueue(context.Context, string) error { return nil }

type fullJobs struct{}

func (fullJobs) Enqueue(context.Context, string) error { return ErrQueueFull }

func TestEnqueue_QueueFullRollsBack(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	st, err := store.Open(t.Context(), filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}

	svc := NewService(st, fullJobs{}, dir, 1024, nil, nil)
	_, err = svc.Enqueue(t.Context(), EnqueueInput{
		OriginalName: "note.png",
		MIMEType:     "image/png",
		Body:         bytes.NewReader([]byte("\x89PNG\r\n\x1a\n")),
	})
	if !errors.Is(err, ErrQueueFull) {
		t.Fatalf("got %v", err)
	}

	rows, err := st.List(t.Context(), 20, 0)
	if err != nil {
		t.Fatal(err)
	}

	if len(rows) != 0 {
		t.Fatalf("expected no invoices, got %d", len(rows))
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	for _, e := range entries {
		if e.Name() == "t.db" || e.Name() == "t.db-wal" || e.Name() == "t.db-shm" {
			continue
		}

		t.Fatalf("leftover file %s", e.Name())
	}
}

func TestEnqueue_OK(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	st, err := store.Open(t.Context(), filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}

	svc := NewService(st, okJobs{}, dir, 1024, nil, nil)
	inv, err := svc.Enqueue(t.Context(), EnqueueInput{
		OriginalName: "note.png",
		MIMEType:     "image/png",
		Body:         bytes.NewReader([]byte("\x89PNG\r\n\x1a\n")),
	})
	if err != nil {
		t.Fatal(err)
	}

	if inv.Status != store.StatusQueued {
		t.Fatalf("status %s", inv.Status)
	}
}
