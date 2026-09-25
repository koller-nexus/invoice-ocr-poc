package worker

import (
	"context"
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
	p := NewPool(2, func(_ context.Context, _ string) error {
		n.Add(1)
		return nil
	}, nil)

	ctx, cancel := context.WithCancel(t.Context())
	p.Start(ctx)
	p.Enqueue("a")
	p.Enqueue("b")

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
