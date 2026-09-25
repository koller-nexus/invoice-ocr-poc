// Package httpclient builds a shared *http.Client with connection reuse.
package httpclient

import (
	"net/http"
	"time"
)

// New returns a client with keep-alive pooling. Callers must set deadlines
// with context.WithTimeout; Client.Timeout is left unset so hop timeouts win.
func New() *http.Client {
	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return &http.Client{}
	}

	transport := base.Clone()
	transport.MaxIdleConns = 32
	transport.MaxIdleConnsPerHost = 8
	transport.IdleConnTimeout = 90 * time.Second

	return &http.Client{Transport: transport}
}
