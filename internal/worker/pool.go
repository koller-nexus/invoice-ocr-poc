// Package worker runs invoice jobs without blocking HTTP handlers.
package worker

import (
	"context"
	"errors"
	"sync"

	"go.uber.org/zap"
)

// ErrQueueFull is returned when the job channel cannot accept another id.
var ErrQueueFull = errors.New("job queue is full")

// Handler processes one job id.
type Handler func(ctx context.Context, id string) error

// Pool is a fixed-size worker pool.
type Pool struct {
	jobs    chan string
	handler Handler
	log     *zap.SugaredLogger
	count   int
	wg      sync.WaitGroup
}

// NewPool builds a pool with workerCount goroutines and a buffered job queue.
func NewPool(workerCount, queueSize int, handler Handler, log *zap.SugaredLogger) *Pool {
	if workerCount < 1 {
		workerCount = 1
	}

	if queueSize < 1 {
		queueSize = 1
	}

	if log == nil {
		log = zap.NewNop().Sugar()
	}

	return &Pool{
		jobs:    make(chan string, queueSize),
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
				p.log.Errorw("job failed", "step", "job.failed", "invoice_id", id, "err", err)
			} else if err == nil {
				p.log.Infow("job.ok", "step", "job.ok", "invoice_id", id)
			}
		}
	}
}

// Enqueue submits a job id without waiting for a worker.
func (p *Pool) Enqueue(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case p.jobs <- id:
		p.log.Infow("job.accepted", "step", "job.accepted", "invoice_id", id)
		return nil
	default:
		return ErrQueueFull
	}
}

// Shutdown closes the job channel and waits for workers.
func (p *Pool) Shutdown() {
	close(p.jobs)
	p.wg.Wait()
}
