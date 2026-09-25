// Package ocr extracts text from images.
package ocr

import "context"

// Result is OCR output.
type Result struct {
	Text       string
	Confidence float64
}

// Engine reads text from an image file.
type Engine interface {
	Recognize(ctx context.Context, imagePath string) (Result, error)
	Available() bool
}
