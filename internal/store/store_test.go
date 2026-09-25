package store

import (
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

func openTestStore(t *testing.T) *Store {
	t.Helper()

	st, err := Open(t.Context(), filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if err := st.Close(); err != nil {
			t.Errorf("close store: %v", err)
		}
	})

	return st
}

func TestOpen_WALMode(t *testing.T) {
	t.Parallel()

	st := openTestStore(t)
	sqlDB, err := st.DB().DB()
	if err != nil {
		t.Fatal(err)
	}

	var mode string
	if err := sqlDB.QueryRowContext(t.Context(), "PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatal(err)
	}

	if mode != "wal" {
		t.Fatalf("journal_mode %q want wal", mode)
	}
}

func TestUpdate_ConcurrentWrites(t *testing.T) {
	t.Parallel()

	st := openTestStore(t)
	const n = 10

	for i := range n {
		inv := &Invoice{
			ID:     string(rune('a' + i)),
			Status: StatusQueued,
		}
		if err := st.Create(t.Context(), inv); err != nil {
			t.Fatal(err)
		}
	}

	var wg sync.WaitGroup

	for i := range n {
		wg.Go(func() {
			inv, err := st.Get(t.Context(), string(rune('a'+i)))
			if err != nil {
				t.Errorf("get: %v", err)

				return
			}

			inv.Status = StatusDone
			inv.OCRText = "updated"
			if err := st.Update(t.Context(), inv); err != nil {
				t.Errorf("update: %v", err)
			}
		})
	}

	wg.Wait()

	for i := range n {
		inv, err := st.Get(t.Context(), string(rune('a'+i)))
		if err != nil {
			t.Fatal(err)
		}

		if inv.Status != StatusDone {
			t.Fatalf("id %s status %s", inv.ID, inv.Status)
		}
	}
}

func TestClose_RejectsWrites(t *testing.T) {
	t.Parallel()

	st, err := Open(t.Context(), filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}

	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	err = st.Create(t.Context(), &Invoice{ID: "x", Status: StatusQueued})
	if !errors.Is(err, ErrClosed) {
		t.Fatalf("got %v want %v", err, ErrClosed)
	}
}
