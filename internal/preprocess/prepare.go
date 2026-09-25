// Package preprocess exposes the working image path for OCR.
package preprocess

import "context"

// Preparer writes a working copy of the image and returns its path.
type Preparer interface {
	Prepare(ctx context.Context, srcPath, destDir string) (string, error)
}
