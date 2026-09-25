package httpclient

import (
	"net/http"
	"testing"
	"time"
)

func TestNew_KeepAliveTransport(t *testing.T) {
	t.Parallel()

	c := New()
	if c == nil {
		t.Fatal("nil client")
	}

	if c.Timeout != 0 {
		t.Fatalf("client timeout must be unset, got %s", c.Timeout)
	}

	tr, ok := c.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport type %T", c.Transport)
	}

	if tr.MaxIdleConns != 32 {
		t.Fatalf("MaxIdleConns %d", tr.MaxIdleConns)
	}

	if tr.MaxIdleConnsPerHost != 8 {
		t.Fatalf("MaxIdleConnsPerHost %d", tr.MaxIdleConnsPerHost)
	}

	if tr.IdleConnTimeout != 90*time.Second {
		t.Fatalf("IdleConnTimeout %s", tr.IdleConnTimeout)
	}
}
