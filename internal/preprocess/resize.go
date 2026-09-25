package preprocess

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/image/draw"
)

const (
	maxSide      = 1600
	jpegQuality  = 80
	preprocessed = "preprocessed.jpg"
)

// Resize downscales large images and re-encodes them as JPEG for OCR.
type Resize struct{}

// New returns a preparer that resizes and compresses JPEG/PNG uploads.
func New() Preparer {
	return Resize{}
}

// Prepare writes destDir/preprocessed.jpg, or a byte copy if encode is not useful.
func (Resize) Prepare(ctx context.Context, srcPath, destDir string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	if err := os.MkdirAll(destDir, 0o750); err != nil {
		return "", fmt.Errorf("create preprocess dir: %w", err)
	}

	srcInfo, err := os.Stat(srcPath)
	if err != nil {
		return "", fmt.Errorf("stat source image: %w", err)
	}

	in, err := os.Open(srcPath)
	if err != nil {
		return "", fmt.Errorf("open source image: %w", err)
	}
	defer in.Close()

	img, _, err := image.Decode(in)
	if err != nil {
		return copySource(srcPath, destDir)
	}

	if err := ctx.Err(); err != nil {
		return "", err
	}

	outImg := scaleToMaxSide(img, maxSide)

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, outImg, &jpeg.Options{Quality: jpegQuality}); err != nil {
		return copySource(srcPath, destDir)
	}

	if int64(buf.Len()) >= srcInfo.Size() {
		return copySource(srcPath, destDir)
	}

	dst := filepath.Join(destDir, preprocessed)
	if err := os.WriteFile(dst, buf.Bytes(), 0o600); err != nil {
		return "", fmt.Errorf("write preprocessed image: %w", err)
	}

	return dst, nil
}

func scaleToMaxSide(src image.Image, side int) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return src
	}

	longest := w
	if h > longest {
		longest = h
	}
	if longest <= side {
		return src
	}

	nw := w * side / longest
	nh := h * side / longest
	if nw < 1 {
		nw = 1
	}
	if nh < 1 {
		nh = 1
	}

	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	draw.ApproxBiLinear.Scale(dst, dst.Bounds(), src, b, draw.Src, nil)

	return dst
}

func copySource(srcPath, destDir string) (string, error) {
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
