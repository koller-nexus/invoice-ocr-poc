// Package invoice owns upload enqueueing and invoice reads.
package invoice

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"strings"

	"github.com/williamkoller/tesseract-poc-go/internal/extract"
	"github.com/williamkoller/tesseract-poc-go/internal/jev"
	"github.com/williamkoller/tesseract-poc-go/internal/ocr"
	"github.com/williamkoller/tesseract-poc-go/internal/preprocess"
	"github.com/williamkoller/tesseract-poc-go/internal/store"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

var allowedMIME = map[string]string{
	"image/jpeg": ".jpg",
	"image/png":  ".png",
	"image/webp": ".webp",
	"image/tiff": ".tiff",
}

// ErrNotFound is returned when an invoice id is unknown.
var ErrNotFound = errors.New("invoice not found")

// ErrInvalidImage is returned for rejected uploads.
var ErrInvalidImage = errors.New("invalid image upload")

// Jobs accepts background work.
type Jobs interface {
	Enqueue(id string)
}

// Assistant optionally improves OCR line-item extraction.
type Assistant interface {
	Enabled() bool
	Model() string
	Assist(ctx context.Context, ocrText string, hint extract.Result) (extract.Result, string, error)
}

// Service coordinates storage, files, and jobs.
type Service struct {
	store     *store.Store
	jobs      Jobs
	uploadDir string
	maxBytes  int64
	prep      preprocess.Preparer
	log       *zap.SugaredLogger
}

// NewService constructs a Service.
func NewService(st *store.Store, jobs Jobs, uploadDir string, maxBytes int64, prep preprocess.Preparer, log *zap.SugaredLogger) *Service {
	if prep == nil {
		prep = preprocess.New()
	}

	if log == nil {
		log = zap.NewNop().Sugar()
	}

	return &Service{
		store:     st,
		jobs:      jobs,
		uploadDir: uploadDir,
		maxBytes:  maxBytes,
		prep:      prep,
		log:       log,
	}
}

// EnqueueInput is a validated upload.
type EnqueueInput struct {
	OriginalName string
	MIMEType     string
	Body         io.Reader
}

// Enqueue stores the file and queues OCR + Jev.
func (s *Service) Enqueue(ctx context.Context, in EnqueueInput) (*store.Invoice, error) {
	ext, mimeType, ok := resolveImageType(in.MIMEType, in.OriginalName)
	if !ok {
		return nil, fmt.Errorf("%w: unsupported content type", ErrInvalidImage)
	}

	in.MIMEType = mimeType

	if err := os.MkdirAll(s.uploadDir, 0o750); err != nil {
		return nil, fmt.Errorf("create upload dir: %w", err)
	}

	id, err := newID()
	if err != nil {
		return nil, err
	}

	stored := filepath.Join(s.uploadDir, id+ext)

	if err := writeLimited(stored, in.Body, s.maxBytes); err != nil {
		return nil, err
	}

	inv := &store.Invoice{
		ID:           id,
		Status:       store.StatusQueued,
		OriginalName: sanitizeName(in.OriginalName),
		StoredPath:   stored,
		MimeType:     in.MIMEType,
	}

	if err := s.store.Create(ctx, inv); err != nil {
		return nil, err
	}

	s.jobs.Enqueue(id)
	s.log.Infow("invoice queued",
		"step", "enqueue",
		"invoice_id", id,
		"mime", in.MIMEType,
		"name", inv.OriginalName,
	)

	return inv, nil
}

// Get returns one invoice.
func (s *Service) Get(ctx context.Context, id string) (*store.Invoice, error) {
	inv, err := s.store.Get(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}

		return nil, err
	}

	return inv, nil
}

// List returns recent invoices.
func (s *Service) List(ctx context.Context, limit, offset int) ([]store.Invoice, error) {
	return s.store.List(ctx, limit, offset)
}

// ProcessJob runs OCR, optional DeepSeek assist, extract, and Jev for one invoice.
func (s *Service) ProcessJob(ctx context.Context, id string, engine ocr.Engine, judge *jev.Client, assist Assistant) error {
	inv, err := s.store.Get(ctx, id)
	if err != nil {
		return err
	}

	s.log.Infow("job.start", "step", "job.start", "invoice_id", id)

	inv.Status = store.StatusProcessing
	if err := s.store.Update(ctx, inv); err != nil {
		return err
	}

	prepDir := filepath.Join(s.uploadDir, inv.ID+"-prep")
	imgPath, err := s.prep.Prepare(ctx, inv.StoredPath, prepDir)
	if err != nil {
		return s.fail(ctx, inv, fmt.Errorf("preprocess: %w", err))
	}

	s.log.Infow("preprocess.done", "step", "preprocess.done", "invoice_id", id)

	s.log.Infow("ocr.start", "step", "ocr.start", "invoice_id", id, "engine", fmt.Sprintf("%T", engine))

	ocrRes, err := engine.Recognize(ctx, imgPath)
	if err != nil {
		return s.fail(ctx, inv, fmt.Errorf("ocr: %w", err))
	}

	inv.OCRText = ocrRes.Text
	inv.OCRConfidence = ocrRes.Confidence
	s.log.Infow("ocr.done",
		"step", "ocr.done",
		"invoice_id", id,
		"text_chars", len(ocrRes.Text),
		"preview", previewText(ocrRes.Text, 80),
	)

	parsed := extract.Parse(ocrRes.Text)
	s.log.Infow("extract.done",
		"step", "extract.done",
		"invoice_id", id,
		"items", len(parsed.Items),
	)

	if assist != nil && assist.Enabled() {
		improved, notes, aerr := assist.Assist(ctx, ocrRes.Text, parsed)
		inv.AssistModel = assist.Model()
		if aerr != nil {
			inv.AssistNotes = "falha no apoio LLM: " + aerr.Error()
			inv.AssistUsed = false
			s.log.Errorw("assist.done",
				"step", "assist.done",
				"invoice_id", id,
				"model", inv.AssistModel,
				"used", false,
				"err", aerr,
			)
		} else {
			inv.AssistUsed = true
			inv.AssistNotes = notes
			if len(improved.Items) > 0 {
				parsed = improved
			}

			s.log.Infow("assist.done",
				"step", "assist.done",
				"invoice_id", id,
				"model", inv.AssistModel,
				"used", true,
				"items", len(parsed.Items),
			)
		}
	}

	itemsJSON, err := json.Marshal(parsed.Items)
	if err != nil {
		return s.fail(ctx, inv, fmt.Errorf("marshal items: %w", err))
	}

	inv.ItemsJSON = string(itemsJSON)
	inv.EstimatedTotal = parsed.EstimatedTotal
	inv.ComputedItemsTotal = parsed.ComputedItemsTotal

	judgment, err := judge.Evaluate(ctx, jev.State{
		OCRText:   ocrRes.Text,
		Extracted: parsed,
	})
	if err != nil {
		return s.fail(ctx, inv, fmt.Errorf("jev: %w", err))
	}

	applyJudgment(inv, judgment)
	inv.Status = store.StatusDone

	s.log.Infow("jev.done",
		"step", "jev.done",
		"invoice_id", id,
		"document_type", judgment.DocumentType,
		"routing", judgment.EffectiveRouting,
		"needs_review", judgment.NeedsReview,
	)

	if err := s.store.Update(ctx, inv); err != nil {
		return err
	}

	s.log.Infow("job.done", "step", "job.done", "invoice_id", id, "status", inv.Status)

	return nil
}

func (s *Service) fail(ctx context.Context, inv *store.Invoice, cause error) error {
	s.log.Errorw("job.fail",
		"step", "job.fail",
		"invoice_id", inv.ID,
		"err", cause,
	)

	inv.Status = store.StatusFailed
	inv.ErrorMessage = cause.Error()
	inv.NeedsReview = true

	if err := s.store.Update(ctx, inv); err != nil {
		return fmt.Errorf("save failed invoice: %w", err)
	}

	return cause
}

func previewText(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.TrimSpace(s)
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}

	return string(runes[:n])
}

func applyJudgment(inv *store.Invoice, j jev.Judgment) {
	inv.DocumentType = j.DocumentType
	inv.DocumentTypeConf = j.DocumentTypeConf
	inv.SuggestedRouting = j.SuggestedRouting
	inv.RoutingConfidence = j.RoutingConfidence
	inv.EffectiveRouting = j.EffectiveRouting
	inv.AmountsSupported = j.AmountsSupported
	inv.ItemsQtySupported = j.ItemsQtySupported
	inv.TotalConsistent = j.TotalConsistent
	inv.RiskScore = j.RiskScore
	inv.RiskLabel = j.RiskLabel
	inv.RiskConfidence = j.RiskConfidence
	inv.NeedsReview = j.NeedsReview
	inv.ItemsConfirmed = j.ItemsConfirmed
}

func writeLimited(path string, r io.Reader, maxBytes int64) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create upload file: %w", err)
	}

	defer f.Close()

	n, err := io.Copy(f, io.LimitReader(r, maxBytes+1))
	if err != nil {
		return fmt.Errorf("write upload: %w", err)
	}

	if n > maxBytes {
		_ = os.Remove(path)
		return fmt.Errorf("%w: file exceeds size limit", ErrInvalidImage)
	}

	return nil
}

func newID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate id: %w", err)
	}

	return hex.EncodeToString(b[:]), nil
}

func resolveImageType(mimeType, filename string) (ext string, resolved string, ok bool) {
	if guessed, _, err := mime.ParseMediaType(mimeType); err == nil {
		mimeType = guessed
	}

	if ext, ok = allowedMIME[mimeType]; ok {
		return ext, mimeType, true
	}

	switch strings.ToLower(filepath.Ext(filename)) {
	case ".jpg", ".jpeg":
		return ".jpg", "image/jpeg", true
	case ".png":
		return ".png", "image/png", true
	case ".webp":
		return ".webp", "image/webp", true
	case ".tif", ".tiff":
		return ".tiff", "image/tiff", true
	default:
		return "", mimeType, false
	}
}

func sanitizeName(name string) string {
	base := filepath.Base(name)
	base = strings.ReplaceAll(base, "..", "")

	if base == "." || base == string(filepath.Separator) {
		return "upload"
	}

	return base
}
