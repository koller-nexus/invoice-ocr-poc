// Package worker runs invoice jobs without blocking HTTP handlers.
package worker

import (
	"context"
	"log/slog"
	"sync"
)

// Handler processes one job id.
type Handler func(ctx context.Context, id string) error

// Pool is a fixed-size worker pool.
type Pool struct {
	jobs    chan string
	handler Handler
	log     *slog.Logger
	count   int
	wg      sync.WaitGroup
}

// NewPool builds a pool with workerCount goroutines.
func NewPool(workerCount int, handler Handler, log *slog.Logger) *Pool {
	if workerCount < 1 {
		workerCount = 1
	}

	if log == nil {
		log = slog.Default()
	}

	return &Pool{
		jobs:    make(chan string, workerCount),
		handler: handler,
		log:     log,
		count:   workerCount,
	}
}

// Start launches workers that exit when ctx is cancelled.
func (p *Pool) Start(ctx context.Context) {
	for range p.count {
		p.wg.Go(func() {
			p.loop(ctx)
		})
	}
}

func (p *Pool) loop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case id, ok := <-p.jobs:
			if !ok {
				return
			}

			if err := p.handler(ctx, id); err != nil && ctx.Err() == nil {
				p.log.Error("job failed", "step", "job.failed", "invoice_id", id, "err", err)
			} else if err == nil {
				p.log.Info("job.ok", "step", "job.ok", "invoice_id", id)
			}
		}
	}
}

// Enqueue submits a job id. The unbuffered channel applies backpressure
// instead of spawning a goroutine per upload.
func (p *Pool) Enqueue(id string) {
	p.log.Info("job.accepted", "step", "job.accepted", "invoice_id", id)
	p.jobs <- id
}

// Shutdown closes the job channel and waits for workers.
func (p *Pool) Shutdown() {
	close(p.jobs)
	p.wg.Wait()
}
