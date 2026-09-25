//go:build gocv

package preprocess

import (
	"context"
	"fmt"
	"image"
	"math"
	"os"
	"path/filepath"

	"gocv.io/x/gocv"
)

// OpenCV applies grayscale, denoise, adaptive threshold, and deskew.
type OpenCV struct{}

// New returns a GoCV preparer.
func New() Preparer {
	return OpenCV{}
}

// Available is true when built with -tags gocv.
func (OpenCV) Available() bool {
	return true
}

// Prepare writes a cleaned PNG under destDir.
func (OpenCV) Prepare(ctx context.Context, srcPath, destDir string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	if err := os.MkdirAll(destDir, 0o750); err != nil {
		return "", fmt.Errorf("create preprocess dir: %w", err)
	}

	src := gocv.IMRead(srcPath, gocv.IMReadColor)
	if src.Empty() {
		return "", fmt.Errorf("imread failed: %s", srcPath)
	}
	defer src.Close()

	gray := gocv.NewMat()
	defer gray.Close()
	gocv.CvtColor(src, &gray, gocv.ColorBGRToGray)

	blur := gocv.NewMat()
	defer blur.Close()
	gocv.GaussianBlur(gray, &blur, image.Pt(5, 5), 0, 0, gocv.BorderDefault)

	bin := gocv.NewMat()
	defer bin.Close()
	gocv.AdaptiveThreshold(blur, &bin, 255, gocv.AdaptiveThresholdGaussian, gocv.ThresholdBinary, 11, 2)

	deskewed := deskew(bin)
	defer deskewed.Close()

	dst := filepath.Join(destDir, "preprocessed.png")
	if ok := gocv.IMWrite(dst, deskewed); !ok {
		return "", fmt.Errorf("imwrite failed: %s", dst)
	}

	return dst, nil
}

func deskew(src gocv.Mat) gocv.Mat {
	coords := gocv.FindNonZero(src)
	defer coords.Close()

	out := gocv.NewMat()
	if coords.Empty() || coords.Rows() < 10 {
		src.CopyTo(&out)
		return out
	}

	pts := gocv.NewPointVectorFromMat(coords)
	defer pts.Close()

	rect := gocv.MinAreaRect(pts)
	angle := float64(rect.Angle)
	if angle < -45 {
		angle += 90
	}

	if math.Abs(angle) < 0.3 {
		src.CopyTo(&out)
		return out
	}

	center := image.Pt(src.Cols()/2, src.Rows()/2)
	rot := gocv.GetRotationMatrix2D(center, angle, 1.0)
	defer rot.Close()
	gocv.WarpAffine(src, &out, rot, image.Pt(src.Cols(), src.Rows()))

	return out
}
