//go:build gosseract && cgo

package ocr

import (
	"context"
	"fmt"
	"os/exec"

	"github.com/otiai10/gosseract/v2"
)

// Tesseract wraps gosseract. Build with -tags gosseract when leptonica/tesseract headers exist.
type Tesseract struct {
	lang string
}

// NewTesseract builds an engine for the given language(s).
func NewTesseract(lang string) *Tesseract {
	return &Tesseract{lang: lang}
}

// Available reports whether the tesseract binary is on PATH.
func (t *Tesseract) Available() bool {
	_, err := exec.LookPath("tesseract")
	return err == nil
}

// Recognize runs Tesseract on imagePath using a per-call client.
func (t *Tesseract) Recognize(ctx context.Context, imagePath string) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}

	client := gosseract.NewClient()
	defer client.Close()

	if err := client.SetLanguage(t.lang); err != nil {
		return Result{}, fmt.Errorf("set ocr language: %w", err)
	}

	if err := client.SetImage(imagePath); err != nil {
		return Result{}, fmt.Errorf("set ocr image: %w", err)
	}

	type done struct {
		text string
		err  error
	}

	ch := make(chan done, 1)

	go func() {
		text, err := client.Text()
		ch <- done{text: text, err: err}
	}()

	select {
	case <-ctx.Done():
		return Result{}, ctx.Err()
	case out := <-ch:
		if out.err != nil {
			return Result{}, fmt.Errorf("ocr text: %w", out.err)
		}

		return Result{Text: out.text, Confidence: 0}, nil
	}
}
