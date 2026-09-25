package worker

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

func TestPool_ProcessesAndExits(t *testing.T) {
	var n atomic.Int32
	p := NewPool(2, 8, func(_ context.Context, _ string) error {
		n.Add(1)
		return nil
	}, nil)

	ctx, cancel := context.WithCancel(t.Context())
	p.Start(ctx)
	if err := p.Enqueue(t.Context(), "a"); err != nil {
		t.Fatal(err)
	}
	if err := p.Enqueue(t.Context(), "b"); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for n.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	if n.Load() != 2 {
		t.Fatalf("got %d jobs", n.Load())
	}

	cancel()
	p.Shutdown()
}

func TestPool_EnqueueRejectsWhenFull(t *testing.T) {
	t.Parallel()

	started := make(chan struct{})
	var startOnce sync.Once
	p := NewPool(1, 1, func(ctx context.Context, _ string) error {
		startOnce.Do(func() { close(started) })
		<-ctx.Done()
		return ctx.Err()
	}, nil)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	p.Start(ctx)

	if err := p.Enqueue(t.Context(), "busy"); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		select {
		case <-started:
		default:
			if time.Now().After(deadline) {
				t.Fatal("worker did not start")
			}
			time.Sleep(5 * time.Millisecond)
			continue
		}
		break
	}

	if err := p.Enqueue(t.Context(), "queued"); err != nil {
		t.Fatal(err)
	}

	err := p.Enqueue(t.Context(), "overflow")
	if !errors.Is(err, ErrQueueFull) {
		t.Fatalf("got %v", err)
	}

	cancel()
	p.Shutdown()
}

func TestPool_EnqueueCanceled(t *testing.T) {
	t.Parallel()

	p := NewPool(1, 1, func(context.Context, string) error { return nil }, nil)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if err := p.Enqueue(ctx, "x"); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
}
