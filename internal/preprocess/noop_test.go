package preprocess

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNoopPrepare_Copies(t *testing.T) {
	t.Parallel()

	src := filepath.Join(t.TempDir(), "a.png")
	if err := os.WriteFile(src, []byte("png"), 0o600); err != nil {
		t.Fatal(err)
	}

	out, err := New().Prepare(t.Context(), src, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}

	if string(raw) != "png" {
		t.Fatalf("%q", raw)
	}
}
