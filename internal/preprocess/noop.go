//go:build !gocv

package preprocess

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Noop copies the source image when OpenCV is not built in.
type Noop struct{}

// New returns a preparer that does not alter pixels.
func New() Preparer {
	return Noop{}
}

// Available is false without the gocv build tag.
func (Noop) Available() bool {
	return false
}

// Prepare copies src into destDir so the worker always has a stable path.
func (Noop) Prepare(ctx context.Context, srcPath, destDir string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	if err := os.MkdirAll(destDir, 0o750); err != nil {
		return "", fmt.Errorf("create preprocess dir: %w", err)
	}

	dst := filepath.Join(destDir, "preprocessed"+filepath.Ext(srcPath))
	if dst == srcPath {
		return srcPath, nil
	}

	in, err := os.Open(srcPath)
	if err != nil {
		return "", fmt.Errorf("open source image: %w", err)
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return "", fmt.Errorf("create preprocessed image: %w", err)
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return "", fmt.Errorf("copy image: %w", err)
	}

	return dst, nil
}
