package preprocess

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestPassthroughPrepare(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		cancel bool
	}{
		{name: "returns source path"},
		{name: "canceled context", cancel: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			src := filepath.Join(t.TempDir(), "upload.jpg")
			if err := os.WriteFile(src, []byte("already-compressed"), 0o600); err != nil {
				t.Fatal(err)
			}

			ctx := t.Context()
			if tc.cancel {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}

			out, err := New().Prepare(ctx, src, t.TempDir())
			if tc.cancel {
				if err == nil {
					t.Fatal("expected cancel error")
				}

				return
			}

			if err != nil {
				t.Fatal(err)
			}

			if out != src {
				t.Fatalf("path %s want %s", out, src)
			}
		})
	}
}
