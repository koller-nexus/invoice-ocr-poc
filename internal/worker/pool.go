// Package worker runs invoice jobs through a multi-stage pipeline without
// blocking HTTP handlers.
package worker

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/williamkoller/invoice-ocr-poc/internal/extract"
	"github.com/williamkoller/invoice-ocr-poc/internal/ocr"
	"github.com/williamkoller/invoice-ocr-poc/internal/store"
	"go.uber.org/zap"
)

// ErrQueueFull is returned when the ingest channel cannot accept another job.
var ErrQueueFull = errors.New("job queue is full")

// ErrPipelineStopped is returned when Enqueue is called after Shutdown.
var ErrPipelineStopped = errors.New("pipeline stopped")

// StageFunc processes one job at a pipeline stage. Mutating j carries state
// forward to the next stage. The ctx is the per-job context with timeout.
type StageFunc func(ctx context.Context, j *Job) error

// Job carries an invoice through the pipeline stages. Fields are mutated by
// the stage that produces them and read by the next stage; never concurrently.
type Job struct {
	ID      string
	Inv     *store.Invoice
	Started time.Time

	// Ctx is the per-job context with timeout; carried through channels so each
	// stage observes the same deadline. Required for structured cancellation
	// of long-running OCR/Jev calls independent of the pipeline lifecycle.
	//
	//nolint:containedctx // per-job deadline travels with the job through staged channels
	Ctx    context.Context
	cancel context.CancelFunc

	// Stage 1 -> 2 output.
	ImgPath string

	// Stage 2 -> 3 output.
	OCRRes ocr.Result
	Parsed extract.Result

	PrepMs   int64
	OCRMs    int64
	AssistMs int64
	JevMs    int64
	DBMs     int64

	// CacheHit indicates the OCR text was found in cache; StageJev can skip
	// OpenRouter/Assist/Jev and apply CacheEntry directly.
	CacheHit   bool
	CacheEntry any // *cache.Entry; any to avoid import cycle
}

// PipelineOptions configures a Pipeline.
type PipelineOptions struct {
	PrepN        int
	OCRN         int
	JevN         int
	IngestBuffer int
	OCRBuffer    int
	JevBuffer    int
	JobTimeout   time.Duration
	Prep         StageFunc
	OCR          StageFunc
	Jev          StageFunc
	Log          *zap.SugaredLogger
}

// Pipeline runs three sequential stages with buffered channels between them.
// Stage 1 (prep) reads from ingest, writes to s2. Stage 2 (ocr) reads s2,
// writes s3. Stage 3 (jev) reads s3 and persists; terminal.
type Pipeline struct {
	ingest chan *Job
	s2     chan *Job
	s3     chan *Job

	prep StageFunc
	ocr  StageFunc
	jev  StageFunc

	prepN int
	ocrN  int
	jevN  int

	jobTimeout time.Duration
	log        *zap.SugaredLogger

	// jobBase is the parent of every per-job context. It is derived from
	// context.WithoutCancel(ctx) so in-flight jobs survive the root context
	// cancellation during graceful drain; jobCancel is invoked only on hard
	// shutdown (drain timeout exceeded).
	//
	//nolint:containedctx // base context for deriving per-job deadlines; owned by the pipeline
	jobBase   context.Context
	jobCancel context.CancelFunc

	mu           sync.Mutex
	stopped      bool
	wg           sync.WaitGroup
	shutdownOnce sync.Once
	started      bool
}

// NewPipeline builds a Pipeline with the given options. Defaults are applied
// for non-positive counts and buffers.
func NewPipeline(opts PipelineOptions) *Pipeline {
	applyDefaults(&opts)

	return &Pipeline{
		ingest:     make(chan *Job, opts.IngestBuffer),
		s2:         make(chan *Job, opts.OCRBuffer),
		s3:         make(chan *Job, opts.JevBuffer),
		prep:       opts.Prep,
		ocr:        opts.OCR,
		jev:        opts.Jev,
		prepN:      opts.PrepN,
		ocrN:       opts.OCRN,
		jevN:       opts.JevN,
		jobTimeout: opts.JobTimeout,
		log:        opts.Log,
	}
}

func applyDefaults(opts *PipelineOptions) {
	if opts.PrepN < 1 {
		opts.PrepN = 1
	}

	if opts.OCRN < 1 {
		opts.OCRN = 1
	}

	if opts.JevN < 1 {
		opts.JevN = 1
	}

	if opts.IngestBuffer < 1 {
		opts.IngestBuffer = 1
	}

	if opts.OCRBuffer < 1 {
		opts.OCRBuffer = 1
	}

	if opts.JevBuffer < 1 {
		opts.JevBuffer = 1
	}

	if opts.JobTimeout <= 0 {
		opts.JobTimeout = 90 * time.Second
	}

	if opts.Log == nil {
		opts.Log = zap.NewNop().Sugar()
	}
}

// Start launches the stage workers. jobBase is derived from
// context.WithoutCancel(ctx) so that in-flight jobs survive the root context
// cancellation during graceful drain; jobCancel is invoked only on hard
// shutdown (drain timeout exceeded).
func (p *Pipeline) Start(ctx context.Context) {
	p.jobBase, p.jobCancel = context.WithCancel(context.WithoutCancel(ctx))
	p.started = true

	p.startStage(p.prepN, p.ingest, p.s2, p.prep, "preprocess")
	p.startStage(p.ocrN, p.s2, p.s3, p.ocr, "local")
	p.startStage(p.jevN, p.s3, nil, p.jev, "api")
}

// startStage launches workers and a closer goroutine that closes out once
// all workers exit. The closer is registered on p.wg so Shutdown can wait.
func (p *Pipeline) startStage(workers int, in <-chan *Job, out chan<- *Job, fn StageFunc, name string) {
	var stageWg sync.WaitGroup

	for range workers {
		stageWg.Go(func() { p.runStage(in, out, fn, name) })
	}

	p.wg.Go(func() {
		stageWg.Wait()

		if out != nil {
			close(out)
		}
	})
}

// runStage pulls jobs from in, runs fn, and forwards to out. It exits when in
// is closed (cascade drain) or when jobBase is cancelled (hard shutdown).
func (p *Pipeline) runStage(in <-chan *Job, out chan<- *Job, fn StageFunc, name string) {
	for {
		select {
		case <-p.jobBase.Done():
			return
		case j, ok := <-in:
			if !ok {
				return
			}

			if err := fn(j.Ctx, j); err != nil {
				if p.jobBase.Err() == nil {
					p.log.Errorw("stage.fail",
						"step", "stage.fail",
						"stage", name,
						"invoice_id", j.ID,
						"err", err,
					)
				}

				j.cancel()

				continue
			}

			if out == nil {
				j.cancel()

				continue
			}

			select {
			case out <- j:
			case <-p.jobBase.Done():
				j.cancel()

				return
			}
		}
	}
}

// Enqueue submits a job to the ingest channel. Returns ErrQueueFull when the
// ingest buffer is full, ErrPipelineStopped after Shutdown, or ctx.Err()
// when the caller's context is cancelled.
func (p *Pipeline) Enqueue(ctx context.Context, inv *store.Invoice) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if !p.started {
		return ErrPipelineStopped
	}

	jobCtx, cancel := context.WithTimeout(p.jobBase, p.jobTimeout)
	j := &Job{
		ID:      inv.ID,
		Inv:     inv,
		Started: time.Now(),
		Ctx:     jobCtx,
		cancel:  cancel,
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.stopped {
		cancel()

		return ErrPipelineStopped
	}

	select {
	case <-ctx.Done():
		cancel()

		return ctx.Err()
	case p.ingest <- j:
		p.log.Infow("job.accepted", "step", "job.accepted", "invoice_id", inv.ID)

		return nil
	default:
		cancel()

		return ErrQueueFull
	}
}

// Shutdown stops accepting new jobs and drains in-flight work. After drain
// timeout, it cancels jobBase to forcibly interrupt remaining jobs. Idempotent.
func (p *Pipeline) Shutdown(timeout time.Duration) {
	p.shutdownOnce.Do(func() {
		p.mu.Lock()
		p.stopped = true
		close(p.ingest)
		p.mu.Unlock()
	})

	if p.jobCancel == nil {
		return
	}

	drained := make(chan struct{})

	go func() {
		p.wg.Wait()
		close(drained)
	}()

	select {
	case <-drained:
	case <-time.After(timeout):
		p.jobCancel()
		p.wg.Wait()
	}
}
