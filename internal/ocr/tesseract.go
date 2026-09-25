//go:build !gosseract

package ocr

import (
	"context"
	"fmt"
	"os/exec"
)

// Tesseract runs the tesseract binary. One logical engine per worker is enough
// because each Recognize call starts an isolated process.
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

// Recognize runs Tesseract on imagePath.
func (t *Tesseract) Recognize(ctx context.Context, imagePath string) (Result, error) {
	cmd := exec.CommandContext(ctx, "tesseract", imagePath, "stdout", "-l", t.lang)

	out, err := cmd.Output()
	if err != nil {
		return Result{}, fmt.Errorf("tesseract: %w", err)
	}

	return Result{Text: string(out), Confidence: 0}, nil
}
