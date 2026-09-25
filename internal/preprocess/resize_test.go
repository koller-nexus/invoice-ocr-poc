package preprocess

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestResizePrepare(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		build    func(t *testing.T, dir string) string
		cancel   bool
		wantJPEG bool
		wantMax  int
		wantCopy string
	}{
		{
			name: "large png becomes jpeg at most 1600",
			build: func(t *testing.T, dir string) string {
				t.Helper()
				return writePNG(t, filepath.Join(dir, "big.png"), 2000, 1200)
			},
			wantJPEG: true,
			wantMax:  maxSide,
		},
		{
			name: "small png yields valid jpeg or copy",
			build: func(t *testing.T, dir string) string {
				t.Helper()
				return writePNG(t, filepath.Join(dir, "small.png"), 80, 60)
			},
			wantMax: 80,
		},
		{
			name: "invalid bytes copies source",
			build: func(t *testing.T, dir string) string {
				t.Helper()
				p := filepath.Join(dir, "a.bin")
				if err := os.WriteFile(p, []byte("not-an-image"), 0o600); err != nil {
					t.Fatal(err)
				}
				return p
			},
			wantCopy: "not-an-image",
		},
		{
			name: "canceled context",
			build: func(t *testing.T, dir string) string {
				t.Helper()
				return writePNG(t, filepath.Join(dir, "c.png"), 16, 16)
			},
			cancel: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			src := tc.build(t, t.TempDir())
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

			raw, err := os.ReadFile(out)
			if err != nil {
				t.Fatal(err)
			}

			if tc.wantCopy != "" {
				if string(raw) != tc.wantCopy {
					t.Fatalf("copy %q", raw)
				}
				return
			}

			img, format, err := image.Decode(bytes.NewReader(raw))
			if err != nil {
				t.Fatal(err)
			}
			if tc.wantJPEG && format != "jpeg" {
				t.Fatalf("format %s", format)
			}

			b := img.Bounds()
			if b.Dx() > tc.wantMax || b.Dy() > tc.wantMax {
				t.Fatalf("size %dx%d > %d", b.Dx(), b.Dy(), tc.wantMax)
			}
		})
	}
}

func writePNG(t *testing.T, path string, w, h int) string {
	t.Helper()

	img := image.NewRGBA(image.Rect(0, 0, w, h))
	seed := uint32(0x9e3779b9)
	for y := range h {
		for x := range w {
			seed = seed*1664525 + 1013904223
			img.Set(x, y, color.RGBA{
				R: uint8(seed),
				G: uint8(seed >> 8),
				B: uint8(seed >> 16),
				A: 255,
			})
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}

	return path
}

func TestScaleToMaxSide(t *testing.T) {
	t.Parallel()

	src := image.NewRGBA(image.Rect(0, 0, 2000, 1000))
	out := scaleToMaxSide(src, maxSide)
	b := out.Bounds()
	if b.Dx() != 1600 || b.Dy() != 800 {
		t.Fatalf("%dx%d", b.Dx(), b.Dy())
	}
}
