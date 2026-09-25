// Package preprocess improves scan quality before OCR.
package preprocess

import "context"

// Preparer writes a cleaned image and returns its path.
type Preparer interface {
	Prepare(ctx context.Context, srcPath, destDir string) (string, error)
	Available() bool
}
