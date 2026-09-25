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
	"sync"
	"time"

	"github.com/williamkoller/invoice-ocr-poc/internal/cache"
	"github.com/williamkoller/invoice-ocr-poc/internal/extract"
	"github.com/williamkoller/invoice-ocr-poc/internal/jev"
	"github.com/williamkoller/invoice-ocr-poc/internal/ocr"
	"github.com/williamkoller/invoice-ocr-poc/internal/preprocess"
	"github.com/williamkoller/invoice-ocr-poc/internal/store"
	"github.com/williamkoller/invoice-ocr-poc/internal/worker"
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

// ErrQueueFull is returned when the worker queue cannot accept another job.
var ErrQueueFull = errors.New("job queue is full")

// Jobs accepts background work.
type Jobs interface {
	Enqueue(ctx context.Context, inv *store.Invoice) error
}

// Assistant optionally improves OCR line-item extraction.
type Assistant interface {
	Enabled() bool
	Model() string
	Assist(ctx context.Context, ocrText string, hint extract.Result) (extract.Result, string, ocr.Usage, error)
}

// Judge is the Jev (or test) evaluator. Interface at the consumer.
type Judge interface {
	Evaluate(ctx context.Context, state jev.State) (jev.Judgment, jev.Usage, error)
}

// OCRInput is the local engines used by StageOCR (Tesseract then Ollama).
type OCRInput struct {
	Tess   ocr.Engine
	Ollama ocr.Engine
}

// JevInput is OpenRouter OCR fallback, Assist, and Judge for the API stage.
type JevInput struct {
	Judge      Judge
	Assist     Assistant
	OpenRouter ocr.Engine
}

const minOCRChars = 40

// Service coordinates storage, files, and jobs.
type Service struct {
	store         *store.Store
	jobs          Jobs
	uploadDir     string
	maxBytes      int64
	prep          preprocess.Preparer
	log           *zap.SugaredLogger
	cache         cache.Cache
	minConfidence float64
	ocrTimeout    time.Duration
	assistTimeout time.Duration
	jevTimeout    time.Duration
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
		store:         st,
		jobs:          jobs,
		uploadDir:     uploadDir,
		maxBytes:      maxBytes,
		prep:          prep,
		log:           log,
		minConfidence: 0.75,
		ocrTimeout:    30 * time.Second,
		assistTimeout: 45 * time.Second,
		jevTimeout:    30 * time.Second,
	}
}

// SetCache injects the OCR result cache.
func (s *Service) SetCache(c cache.Cache) {
	s.cache = c
}

// Configure sets cascade confidence and per-hop timeouts.
func (s *Service) Configure(minConfidence float64, ocrTimeout, assistTimeout, jevTimeout time.Duration) {
	if minConfidence >= 0 && minConfidence <= 1 {
		s.minConfidence = minConfidence
	}

	if ocrTimeout > 0 {
		s.ocrTimeout = ocrTimeout
	}

	if assistTimeout > 0 {
		s.assistTimeout = assistTimeout
	}

	if jevTimeout > 0 {
		s.jevTimeout = jevTimeout
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

	if err := s.jobs.Enqueue(ctx, inv); err != nil {
		s.rollbackEnqueue(ctx, id, stored)
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}

		return nil, ErrQueueFull
	}

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

// StagePreprocess marks the invoice as processing and reuses the upload path
// (frontend already compressed the image).
func (s *Service) StagePreprocess(ctx context.Context, j *worker.Job) error {
	started := time.Now()
	inv := j.Inv
	s.log.Infow("job.start", "step", "job.start", "invoice_id", inv.ID)

	inv.Status = store.StatusProcessing
	if err := s.store.Update(ctx, inv); err != nil {
		return err
	}

	imgPath, err := s.prep.Prepare(ctx, inv.StoredPath, "")
	if err != nil {
		return s.fail(ctx, inv, fmt.Errorf("preprocess: %w", err), j.Started)
	}

	j.ImgPath = imgPath
	j.PrepMs = time.Since(started).Milliseconds()
	s.log.Infow("preprocess.done", "step", "preprocess.done", "invoice_id", inv.ID, "prep_ms", j.PrepMs)

	return nil
}

// StageOCR is the local pool: sequential Tesseract then Ollama cascade.
func (s *Service) StageOCR(ctx context.Context, j *worker.Job, in OCRInput) error {
	started := time.Now()
	inv := j.Inv
	id := inv.ID

	ocrRes, parsed, engine, escalated, err := s.recognizeCascade(ctx, j.ImgPath, in.Tess, in.Ollama)
	if err != nil {
		return s.fail(ctx, inv, fmt.Errorf("ocr: %w", err), j.Started)
	}

	inv.OCRText = ocrRes.Text
	inv.OCRConfidence = ocrRes.Confidence
	applyOCRUsage(inv, ocrRes.Usage)
	s.log.Infow("ocr.done",
		"step", "ocr.done",
		"invoice_id", id,
		"engine", engine,
		"escalated", escalated,
		"confidence", ocrRes.Confidence,
		"text_chars", len(ocrRes.Text),
		"preview", previewText(ocrRes.Text, 80),
	)

	// Check cache before expensive API calls
	if s.cache != nil {
		if entry, ok := s.cache.Get(ocrRes.Text); ok {
			s.log.Infow("cache.hit",
				"step", "cache.hit",
				"invoice_id", id,
				"cached_at", entry.CreatedAt,
			)
			j.CacheHit = true
			j.CacheEntry = entry
		}
	}

	s.log.Infow("extract.done",
		"step", "extract.done",
		"invoice_id", id,
		"items", len(parsed.Items),
	)

	itemsJSON, err := json.Marshal(parsed.Items)
	if err != nil {
		return s.fail(ctx, inv, fmt.Errorf("marshal items: %w", err), j.Started)
	}

	inv.ItemsJSON = string(itemsJSON)
	inv.EstimatedTotal = parsed.EstimatedTotal
	inv.ComputedItemsTotal = parsed.ComputedItemsTotal

	j.OCRRes = ocrRes
	j.Parsed = parsed
	j.OCRMs = time.Since(started).Milliseconds()

	return nil
}

func (s *Service) recognizeCascade(
	ctx context.Context,
	imgPath string,
	tess ocr.Engine,
	ollama ocr.Engine,
) (ocr.Result, extract.Result, string, bool, error) {
	tessRes, tessParsed, tessErr := s.recognizeOne(ctx, imgPath, tess)
	if tessErr == nil && !s.belowMinConfidence(tessRes.Text, tessParsed) {
		s.log.Infow("ocr.start", "step", "ocr.start", "engine", engineName(tess), "escalated", false)

		return tessRes, tessParsed, engineName(tess), false, nil
	}

	// Skip Ollama if Tesseract already found items — let API stage (OpenRouter)
	// refine if still incomplete. This avoids the slow local VLM hop.
	if tessErr == nil && len(tessParsed.Items) > 0 {
		s.log.Infow("ocr.skip_ollama",
			"step", "ocr.skip_ollama",
			"engine", engineName(tess),
			"reason", "has_items",
			"items", len(tessParsed.Items),
		)

		return tessRes, tessParsed, engineName(tess), false, nil
	}

	if tessErr != nil && engineAvailable(tess) {
		s.log.Errorw("ocr.tess.fail", "step", "ocr.tess.fail", "engine", engineName(tess), "err", tessErr)
	} else if tessErr == nil {
		s.log.Infow("ocr.escalate",
			"step", "ocr.escalate",
			"from", engineName(tess),
			"to", engineName(ollama),
			"confidence", tessRes.Confidence,
			"text_chars", len(tessRes.Text),
			"items", len(tessParsed.Items),
		)
	}

	ollamaRes, ollamaParsed, ollamaErr := s.recognizeOne(ctx, imgPath, ollama)
	if ollamaErr == nil {
		return ollamaRes, ollamaParsed, engineName(ollama), true, nil
	}

	if engineAvailable(ollama) {
		s.log.Errorw("ocr.ollama.fail", "step", "ocr.ollama.fail", "engine", engineName(ollama), "err", ollamaErr)
	}

	if tessErr == nil {
		return tessRes, tessParsed, engineName(tess), true, nil
	}

	if tessErr != nil && ollamaErr != nil {
		return ocr.Result{}, extract.Result{}, "", true, fmt.Errorf("ocr: %w", errors.Join(tessErr, ollamaErr))
	}

	if tessErr != nil {
		return ocr.Result{}, extract.Result{}, "", false, tessErr
	}

	return ocr.Result{}, extract.Result{}, "", false, errors.New("ocr engine is not configured")
}

func (s *Service) recognizeOne(ctx context.Context, imgPath string, engine ocr.Engine) (ocr.Result, extract.Result, error) {
	if !engineAvailable(engine) {
		return ocr.Result{}, extract.Result{}, errors.New("ocr engine is not configured")
	}

	s.log.Infow("ocr.start", "step", "ocr.start", "engine", engineName(engine))

	hop, cancel := withHop(ctx, s.ocrTimeout)
	defer cancel()

	res, err := engine.Recognize(hop, imgPath)
	if err != nil {
		return ocr.Result{}, extract.Result{}, err
	}

	res.Text = ocr.CleanText(res.Text)
	parsed := extract.Parse(res.Text)
	res.Confidence = extractConfidence(res.Text, parsed)

	return res, parsed, nil
}

func withHop(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	if d <= 0 {
		return context.WithCancel(ctx)
	}

	return context.WithTimeout(ctx, d)
}

func (s *Service) applyAssist(
	ctx context.Context,
	inv *store.Invoice,
	assist Assistant,
	ocrText string,
	parsed extract.Result,
) extract.Result {
	if assist == nil || !assist.Enabled() {
		return parsed
	}

	if !needsAssist(parsed) {
		s.log.Infow("assist.skipped",
			"step", "assist.skipped",
			"invoice_id", inv.ID,
			"reason", "extract_defensible",
			"items", len(parsed.Items),
		)

		return parsed
	}

	hop, cancel := withHop(ctx, s.assistTimeout)
	defer cancel()

	improved, notes, usage, aerr := assist.Assist(hop, ocrText, parsed)
	inv.AssistModel = assist.Model()
	applyOpenRouterUsage(inv, usage)

	if aerr != nil {
		inv.AssistNotes = "falha no apoio LLM: " + aerr.Error()
		inv.AssistUsed = false
		s.log.Errorw("assist.done",
			"step", "assist.done",
			"invoice_id", inv.ID,
			"model", inv.AssistModel,
			"used", false,
			"err", aerr,
		)

		return parsed
	}

	inv.AssistUsed = true
	inv.AssistNotes = notes

	if len(improved.Items) > 0 {
		parsed = improved
	}

	s.log.Infow("assist.done",
		"step", "assist.done",
		"invoice_id", inv.ID,
		"model", inv.AssistModel,
		"used", true,
		"items", len(parsed.Items),
	)

	return parsed
}

func engineAvailable(e ocr.Engine) bool {
	return e != nil && e.Available()
}

func (s *Service) belowMinConfidence(text string, parsed extract.Result) bool {
	return extractConfidence(text, parsed) < s.minConfidence
}

func extractConfidence(text string, parsed extract.Result) float64 {
	if needsEscalate(text, parsed) {
		return 0
	}

	return 1
}

func engineName(e ocr.Engine) string {
	if e == nil {
		return ""
	}

	return fmt.Sprintf("%T", e)
}

func needsEscalate(text string, parsed extract.Result) bool {
	if len(strings.TrimSpace(text)) < minOCRChars {
		return true
	}

	return parsed.Incomplete()
}

func needsAssist(parsed extract.Result) bool {
	return parsed.Incomplete()
}

func (s *Service) judgeWithAssist(
	ctx context.Context,
	inv *store.Invoice,
	ocrText string,
	parsed extract.Result,
	in JevInput,
	j *worker.Job,
) (extract.Result, jev.Judgment, jev.Usage, error) {
	if in.Judge == nil {
		return parsed, jev.Judgment{}, jev.Usage{}, errors.New("jev client is not configured")
	}

	if in.Assist == nil || !in.Assist.Enabled() || !needsAssist(parsed) {
		if in.Assist != nil && in.Assist.Enabled() && !needsAssist(parsed) {
			s.log.Infow("assist.skipped",
				"step", "assist.skipped",
				"invoice_id", inv.ID,
				"reason", "extract_defensible",
				"items", len(parsed.Items),
			)
		}

		started := time.Now()
		judgment, usage, err := s.evaluateJudge(ctx, in.Judge, ocrText, parsed)
		if j != nil {
			j.JevMs = time.Since(started).Milliseconds()
		}

		return parsed, judgment, usage, err
	}

	hint := parsed

	var (
		judgment jev.Judgment
		usage    jev.Usage
		jerr     error
		wg       sync.WaitGroup
	)

	wg.Go(func() {
		started := time.Now()
		parsed = s.applyAssist(ctx, inv, in.Assist, ocrText, hint)
		if j != nil {
			j.AssistMs = time.Since(started).Milliseconds()
		}
	})
	wg.Go(func() {
		started := time.Now()
		judgment, usage, jerr = s.evaluateJudge(ctx, in.Judge, ocrText, hint)
		if j != nil {
			j.JevMs = time.Since(started).Milliseconds()
		}
	})
	wg.Wait()

	if extractChanged(hint, parsed) {
		s.log.Infow("jev.reevaluate",
			"step", "jev.reevaluate",
			"invoice_id", inv.ID,
			"reason", "assist_changed_extract",
			"items", len(parsed.Items),
		)

		started := time.Now()
		judgment, usage, jerr = s.evaluateJudge(ctx, in.Judge, ocrText, parsed)
		if j != nil {
			j.JevMs += time.Since(started).Milliseconds()
		}
	}

	return parsed, judgment, usage, jerr
}

func (s *Service) evaluateJudge(ctx context.Context, judge Judge, ocrText string, parsed extract.Result) (jev.Judgment, jev.Usage, error) {
	hop, cancel := withHop(ctx, s.jevTimeout)
	defer cancel()

	return judge.Evaluate(hop, jev.State{
		OCRText:   ocrText,
		Extracted: parsed,
	})
}

func extractChanged(before, after extract.Result) bool {
	if len(after.Items) == 0 {
		return false
	}

	if len(before.Items) != len(after.Items) {
		return true
	}

	if before.EstimatedTotal != after.EstimatedTotal || before.ComputedItemsTotal != after.ComputedItemsTotal {
		return true
	}

	for i := range before.Items {
		if before.Items[i] != after.Items[i] {
			return true
		}
	}

	return false
}

// StageJev is the API pool: OpenRouter OCR fallback, Assist, then Jev.
func (s *Service) StageJev(ctx context.Context, j *worker.Job, in JevInput) error {
	inv := j.Inv
	id := inv.ID

	// Fast path: apply cached result, skip OpenRouter/Assist/Jev
	if j.CacheHit {
		return s.applyCacheHit(ctx, j)
	}

	if err := s.applyOpenRouterOCR(ctx, j, in.OpenRouter); err != nil {
		return s.fail(ctx, inv, fmt.Errorf("ocr: %w", err), j.Started)
	}

	parsed, judgment, usage, err := s.judgeWithAssist(ctx, inv, j.OCRRes.Text, j.Parsed, in, j)
	if err != nil {
		return s.fail(ctx, inv, fmt.Errorf("jev: %w", err), j.Started)
	}

	itemsJSON, err := json.Marshal(parsed.Items)
	if err != nil {
		return s.fail(ctx, inv, fmt.Errorf("marshal items: %w", err), j.Started)
	}

	inv.ItemsJSON = string(itemsJSON)
	inv.EstimatedTotal = parsed.EstimatedTotal
	inv.ComputedItemsTotal = parsed.ComputedItemsTotal
	j.Parsed = parsed

	applyJudgment(inv, judgment)
	applyJevUsage(inv, usage)
	inv.ProcessingMs = time.Since(j.Started).Milliseconds()
	inv.Status = store.StatusDone

	s.log.Infow("jev.done",
		"step", "jev.done",
		"invoice_id", id,
		"document_type", judgment.DocumentType,
		"routing", judgment.EffectiveRouting,
		"needs_review", judgment.NeedsReview,
		"input_tokens", usage.InputTokens,
		"output_tokens", usage.OutputTokens,
		"cost_usd", usage.CostUSD,
		"latency_ms", usage.LatencyMs,
	)

	dbStarted := time.Now()
	if err := s.store.Update(ctx, inv); err != nil {
		return err
	}

	// Store in cache for future hits
	if s.cache != nil {
		s.cache.Set(j.OCRRes.Text, &cache.Entry{
			OCRText:       j.OCRRes.Text,
			OCRConfidence: j.OCRRes.Confidence,
			Parsed:        parsed,
			Judgment:      judgment,
			JevUsage:      usage,
			AssistUsed:    inv.AssistUsed,
			AssistNotes:   inv.AssistNotes,
			AssistModel:   inv.AssistModel,
		})
	}

	j.DBMs = time.Since(dbStarted).Milliseconds()
	s.log.Infow("job.done",
		"step", "job.done",
		"invoice_id", id,
		"status", inv.Status,
		"duration_ms", inv.ProcessingMs,
		"prep_ms", j.PrepMs,
		"ocr_ms", j.OCRMs,
		"assist_ms", j.AssistMs,
		"jev_ms", j.JevMs,
		"db_ms", j.DBMs,
		"cache_hit", false,
	)

	return nil
}

func (s *Service) applyCacheHit(ctx context.Context, j *worker.Job) error {
	entry, ok := j.CacheEntry.(*cache.Entry)
	if !ok || entry == nil {
		return errors.New("invalid cache entry")
	}

	inv := j.Inv

	// Apply cached extraction
	j.Parsed = entry.Parsed
	itemsJSON, err := json.Marshal(entry.Parsed.Items)
	if err != nil {
		return s.fail(ctx, inv, fmt.Errorf("marshal items: %w", err), j.Started)
	}

	inv.ItemsJSON = string(itemsJSON)
	inv.EstimatedTotal = entry.Parsed.EstimatedTotal
	inv.ComputedItemsTotal = entry.Parsed.ComputedItemsTotal

	// Apply cached judgment
	applyJudgment(inv, entry.Judgment)
	applyJevUsage(inv, entry.JevUsage)

	// Apply cached assist info
	inv.AssistUsed = entry.AssistUsed
	inv.AssistNotes = entry.AssistNotes
	inv.AssistModel = entry.AssistModel

	inv.ProcessingMs = time.Since(j.Started).Milliseconds()
	inv.Status = store.StatusDone

	dbStarted := time.Now()
	if err := s.store.Update(ctx, inv); err != nil {
		return err
	}

	j.DBMs = time.Since(dbStarted).Milliseconds()
	s.log.Infow("job.done",
		"step", "job.done",
		"invoice_id", inv.ID,
		"status", inv.Status,
		"duration_ms", inv.ProcessingMs,
		"prep_ms", j.PrepMs,
		"ocr_ms", j.OCRMs,
		"db_ms", j.DBMs,
		"cache_hit", true,
	)

	return nil
}

func (s *Service) applyOpenRouterOCR(ctx context.Context, j *worker.Job, engine ocr.Engine) error {
	if !engineAvailable(engine) {
		return nil
	}

	if !s.belowMinConfidence(j.OCRRes.Text, j.Parsed) {
		return nil
	}

	started := time.Now()
	res, parsed, err := s.recognizeOne(ctx, j.ImgPath, engine)
	j.OCRMs += time.Since(started).Milliseconds()
	if err != nil {
		return err
	}

	j.OCRRes = res
	j.Parsed = parsed
	j.Inv.OCRText = res.Text
	j.Inv.OCRConfidence = res.Confidence
	applyOCRUsage(j.Inv, res.Usage)

	s.log.Infow("ocr.done",
		"step", "ocr.done",
		"invoice_id", j.Inv.ID,
		"engine", engineName(engine),
		"escalated", true,
		"confidence", res.Confidence,
		"text_chars", len(res.Text),
	)

	return nil
}

func (s *Service) fail(ctx context.Context, inv *store.Invoice, cause error, started time.Time) error {
	if errors.Is(cause, context.DeadlineExceeded) {
		cause = fmt.Errorf("timeout: %w", cause)
	}

	inv.ProcessingMs = time.Since(started).Milliseconds()
	s.log.Errorw("job.fail",
		"step", "job.fail",
		"invoice_id", inv.ID,
		"err", cause,
		"duration_ms", inv.ProcessingMs,
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

func applyOCRUsage(inv *store.Invoice, usage ocr.Usage) {
	if usage.HasOpenRouter() {
		applyOpenRouterUsage(inv, usage)
		return
	}

	inv.OllamaDurationMs += usage.DurationMs
	inv.OllamaLoadDurationMs += usage.LoadDurationMs
	inv.OllamaPromptEvalCount += usage.PromptEvalCount
	inv.OllamaPromptEvalDurationMs += usage.PromptEvalDurationMs
	inv.OllamaEvalCount += usage.EvalCount
	inv.OllamaEvalDurationMs += usage.EvalDurationMs
}

func applyOpenRouterUsage(inv *store.Invoice, usage ocr.Usage) {
	inv.OpenRouterPromptTokens += usage.PromptTokens
	inv.OpenRouterCompletionTokens += usage.CompletionTokens
	inv.OpenRouterTotalTokens += usage.TotalTokens
	inv.OpenRouterCostUSD += usage.CostUSD
	inv.OpenRouterLatencyMs += usage.DurationMs
}

func applyJevUsage(inv *store.Invoice, usage jev.Usage) {
	inv.JevInputTokens += usage.InputTokens
	inv.JevOutputTokens += usage.OutputTokens
	inv.JevTotalTokens += usage.TotalTokens
	inv.JevCostUSD += usage.CostUSD
	inv.JevLatencyMs += usage.LatencyMs
	if usage.Model != "" {
		inv.JevModel = usage.Model
	}
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

func (s *Service) rollbackEnqueue(ctx context.Context, id, stored string) {
	if err := s.store.Delete(ctx, id); err != nil {
		s.log.Errorw("enqueue rollback delete", "invoice_id", id, "err", err)
	}

	if err := os.Remove(stored); err != nil && !errors.Is(err, os.ErrNotExist) {
		s.log.Errorw("enqueue rollback file", "path", stored, "err", err)
	}
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
