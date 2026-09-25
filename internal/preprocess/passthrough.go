package preprocess

import "context"

// Passthrough uses the uploaded file as-is. The client already compresses.
type Passthrough struct{}

// New returns a preparer that does not resize or re-encode.
func New() Preparer {
	return Passthrough{}
}

// Prepare returns srcPath. destDir is unused.
func (Passthrough) Prepare(ctx context.Context, srcPath, _ string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	return srcPath, nil
}
