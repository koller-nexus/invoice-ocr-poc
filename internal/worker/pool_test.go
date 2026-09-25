package worker

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/williamkoller/invoice-ocr-poc/internal/store"
	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

func newTestPipeline(t *testing.T, opts PipelineOptions) *Pipeline {
	t.Helper()

	if opts.JobTimeout <= 0 {
		opts.JobTimeout = 5 * time.Second
	}

	return NewPipeline(opts)
}

func inv(id string) *store.Invoice {
	return &store.Invoice{ID: id, Status: store.StatusQueued}
}

func TestPipeline_ProcessesThroughAllStages(t *testing.T) {
	t.Parallel()

	var prep, ocr, jev atomic.Int32

	p := newTestPipeline(t, PipelineOptions{
		PrepN:        2,
		OCRN:         2,
		JevN:         2,
		IngestBuffer: 8,
		OCRBuffer:    8,
		JevBuffer:    8,
		Prep: func(_ context.Context, j *Job) error {
			prep.Add(1)

			j.ImgPath = "/tmp/" + j.ID + ".png"

			return nil
		},
		OCR: func(_ context.Context, j *Job) error {
			if j.ImgPath == "" {
				t.Errorf("imgPath not set for %s", j.ID)
			}

			ocr.Add(1)

			return nil
		},
		Jev: func(_ context.Context, _ *Job) error {
			jev.Add(1)

			return nil
		},
	})

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	p.Start(ctx)

	for _, id := range []string{"a", "b", "c"} {
		if err := p.Enqueue(t.Context(), inv(id)); err != nil {
			t.Fatal(err)
		}
	}

	deadline := time.Now().Add(2 * time.Second)

	for jev.Load() < 3 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	if got := prep.Load(); got != 3 {
		t.Fatalf("prep stage ran %d times", got)
	}

	if got := ocr.Load(); got != 3 {
		t.Fatalf("ocr stage ran %d times", got)
	}

	if got := jev.Load(); got != 3 {
		t.Fatalf("jev stage ran %d times", got)
	}

	p.Shutdown(2 * time.Second)
}

func TestPipeline_EnqueueRejectsWhenFull(t *testing.T) {
	t.Parallel()

	started := make(chan struct{})

	var startOnce sync.Once

	p := newTestPipeline(t, PipelineOptions{
		PrepN:        1,
		OCRN:         1,
		JevN:         1,
		IngestBuffer: 1,
		OCRBuffer:    1,
		JevBuffer:    1,
		Prep: func(ctx context.Context, _ *Job) error {
			startOnce.Do(func() { close(started) })
			<-ctx.Done()

			return ctx.Err()
		},
		OCR: func(_ context.Context, _ *Job) error { return nil },
		Jev: func(_ context.Context, _ *Job) error { return nil },
	})

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	p.Start(ctx)

	if err := p.Enqueue(t.Context(), inv("busy")); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(2 * time.Second)

	for {
		select {
		case <-started:
		default:
			if time.Now().After(deadline) {
				t.Fatal("prep worker did not start")
			}

			time.Sleep(5 * time.Millisecond)

			continue
		}

		break
	}

	if err := p.Enqueue(t.Context(), inv("queued")); err != nil {
		t.Fatal(err)
	}

	err := p.Enqueue(t.Context(), inv("overflow"))
	if !errors.Is(err, ErrQueueFull) {
		t.Fatalf("got %v, want ErrQueueFull", err)
	}

	cancel()
	p.Shutdown(2 * time.Second)
}

func TestPipeline_EnqueueCanceled(t *testing.T) {
	t.Parallel()

	p := newTestPipeline(t, PipelineOptions{
		PrepN:        1,
		OCRN:         1,
		JevN:         1,
		IngestBuffer: 1,
		OCRBuffer:    1,
		JevBuffer:    1,
		Prep: func(ctx context.Context, _ *Job) error {
			<-ctx.Done()

			return ctx.Err()
		},
		OCR: func(_ context.Context, _ *Job) error { return nil },
		Jev: func(_ context.Context, _ *Job) error { return nil },
	})

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	p.Start(ctx)

	callerCtx, callerCancel := context.WithCancel(t.Context())
	callerCancel()

	if err := p.Enqueue(callerCtx, inv("x")); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want context.Canceled", err)
	}

	p.Shutdown(time.Second)
}

func TestPipeline_GracefulShutdownDrains(t *testing.T) {
	t.Parallel()

	var done atomic.Int32

	block := make(chan struct{})

	p := newTestPipeline(t, PipelineOptions{
		PrepN:        1,
		OCRN:         1,
		JevN:         1,
		IngestBuffer: 4,
		OCRBuffer:    4,
		JevBuffer:    4,
		Prep: func(_ context.Context, _ *Job) error {
			<-block

			return nil
		},
		OCR: func(_ context.Context, _ *Job) error { return nil },
		Jev: func(_ context.Context, _ *Job) error {
			done.Add(1)

			return nil
		},
	})

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	p.Start(ctx)

	for i := range 3 {
		if err := p.Enqueue(t.Context(), inv(string(rune('a'+i)))); err != nil {
			t.Fatal(err)
		}
	}

	go func() {
		time.Sleep(100 * time.Millisecond)
		close(block)
	}()

	p.Shutdown(5 * time.Second)

	if got := done.Load(); got != 3 {
		t.Fatalf("drained %d jobs, want 3", got)
	}
}

func TestPipeline_HardCancelOnTimeout(t *testing.T) {
	t.Parallel()

	var cancelled atomic.Int32

	block := make(chan struct{})

	p := newTestPipeline(t, PipelineOptions{
		PrepN:        1,
		OCRN:         1,
		JevN:         1,
		IngestBuffer: 2,
		OCRBuffer:    2,
		JevBuffer:    2,
		JobTimeout:   10 * time.Second,
		Prep: func(ctx context.Context, _ *Job) error {
			<-ctx.Done()
			cancelled.Add(1)

			return ctx.Err()
		},
		OCR: func(_ context.Context, _ *Job) error { return nil },
		Jev: func(_ context.Context, _ *Job) error { return nil },
	})

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	p.Start(ctx)

	if err := p.Enqueue(t.Context(), inv("a")); err != nil {
		t.Fatal(err)
	}

	p.Shutdown(50 * time.Millisecond)

	if got := cancelled.Load(); got != 1 {
		t.Fatalf("expected 1 cancelled job, got %d", got)
	}

	close(block)
}

func TestPipeline_ShutdownIsIdempotent(t *testing.T) {
	t.Parallel()

	p := newTestPipeline(t, PipelineOptions{
		PrepN: 1,
		OCRN:  1,
		JevN:  1,
		Prep:  func(_ context.Context, _ *Job) error { return nil },
		OCR:   func(_ context.Context, _ *Job) error { return nil },
		Jev:   func(_ context.Context, _ *Job) error { return nil },
	})

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	p.Start(ctx)

	p.Shutdown(time.Second)
	p.Shutdown(time.Second) // must not panic
}

func TestPipeline_EnqueueAfterShutdown(t *testing.T) {
	t.Parallel()

	p := newTestPipeline(t, PipelineOptions{
		PrepN: 1,
		OCRN:  1,
		JevN:  1,
		Prep:  func(_ context.Context, _ *Job) error { return nil },
		OCR:   func(_ context.Context, _ *Job) error { return nil },
		Jev:   func(_ context.Context, _ *Job) error { return nil },
	})

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	p.Start(ctx)
	p.Shutdown(time.Second)

	if err := p.Enqueue(t.Context(), inv("late")); !errors.Is(err, ErrPipelineStopped) {
		t.Fatalf("got %v, want ErrPipelineStopped", err)
	}
}
