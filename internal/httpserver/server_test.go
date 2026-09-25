package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/williamkoller/invoice-ocr-poc/internal/invoice"
	"github.com/williamkoller/invoice-ocr-poc/internal/store"
)

type nopJobs struct{}

func (nopJobs) Enqueue(context.Context, string) error { return nil }

type fullJobs struct{}

func (fullJobs) Enqueue(context.Context, string) error { return invoice.ErrQueueFull }

func TestProcessImage_Accepted(t *testing.T) {
	t.Parallel()

	st, err := store.Open(t.Context(), t.TempDir()+"/t.db")
	if err != nil {
		t.Fatal(err)
	}

	svc := invoice.NewService(st, nopJobs{}, t.TempDir(), 1024*1024, nil, nil)
	r := NewRouter(Deps{
		Service:      svc,
		Store:        st,
		HasAPIKey:    true,
		MaxBodyBytes: 1024 * 1024,
	})

	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, err := w.CreateFormFile("image", "note.png")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := part.Write([]byte("\x89PNG\r\n\x1a\n")); err != nil {
		t.Fatal(err)
	}

	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/image/processor", &body)
	req.Header.Set("Content-Type", w.FormDataContentType())

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}

	var got enqueueResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}

	if got.ID == "" || got.Status != store.StatusQueued {
		t.Fatalf("%+v", got)
	}

	get := httptest.NewRequest(http.MethodGet, "/api/v1/invoices/"+got.ID, nil)
	grec := httptest.NewRecorder()
	r.ServeHTTP(grec, get)

	if grec.Code != http.StatusOK {
		t.Fatalf("get %d", grec.Code)
	}
}

func TestProcessImage_QueueFull(t *testing.T) {
	t.Parallel()

	st, err := store.Open(t.Context(), t.TempDir()+"/t.db")
	if err != nil {
		t.Fatal(err)
	}

	svc := invoice.NewService(st, fullJobs{}, t.TempDir(), 1024*1024, nil, nil)
	r := NewRouter(Deps{
		Service:      svc,
		Store:        st,
		HasAPIKey:    true,
		MaxBodyBytes: 1024 * 1024,
	})

	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, err := w.CreateFormFile("image", "note.png")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := part.Write([]byte("\x89PNG\r\n\x1a\n")); err != nil {
		t.Fatal(err)
	}

	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/image/processor", &body)
	req.Header.Set("Content-Type", w.FormDataContentType())

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}

	rows, err := st.List(t.Context(), 20, 0)
	if err != nil {
		t.Fatal(err)
	}

	if len(rows) != 0 {
		t.Fatalf("expected rollback, got %d rows", len(rows))
	}
}

func TestCORS_PreflightAndGET(t *testing.T) {
	t.Parallel()

	st, err := store.Open(t.Context(), t.TempDir()+"/cors.db")
	if err != nil {
		t.Fatal(err)
	}

	r := NewRouter(Deps{Store: st, HasAPIKey: true})
	origin := "http://localhost:5173"

	opt := httptest.NewRequest(http.MethodOptions, "/api/v1/invoices", nil)
	opt.Header.Set("Origin", origin)
	opt.Header.Set("Access-Control-Request-Method", "GET")
	orec := httptest.NewRecorder()
	r.ServeHTTP(orec, opt)

	if orec.Code != http.StatusNoContent {
		t.Fatalf("preflight status %d", orec.Code)
	}

	if got := orec.Header().Get("Access-Control-Allow-Origin"); got != origin {
		t.Fatalf("preflight origin %q", got)
	}

	get := httptest.NewRequest(http.MethodGet, "/health", nil)
	get.Header.Set("Origin", origin)
	grec := httptest.NewRecorder()
	r.ServeHTTP(grec, get)

	if grec.Code != http.StatusOK {
		t.Fatalf("get status %d", grec.Code)
	}

	if got := grec.Header().Get("Access-Control-Allow-Origin"); got != origin {
		t.Fatalf("get origin %q", got)
	}
}

func TestHealth(t *testing.T) {
	t.Parallel()

	st, err := store.Open(t.Context(), t.TempDir()+"/h.db")
	if err != nil {
		t.Fatal(err)
	}

	r := NewRouter(Deps{Store: st, HasAPIKey: false})
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}

	var got healthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}

	if got.Status != "degraded" {
		t.Fatalf("status %s", got.Status)
	}
}

func TestGetAnalysis_NotReady(t *testing.T) {
	t.Parallel()

	st, err := store.Open(t.Context(), t.TempDir()+"/q.db")
	if err != nil {
		t.Fatal(err)
	}

	svc := invoice.NewService(st, nopJobs{}, t.TempDir(), 1024*1024, nil, nil)
	inv := &store.Invoice{ID: "queued1", Status: store.StatusQueued}
	if err := st.Create(t.Context(), inv); err != nil {
		t.Fatal(err)
	}

	r := NewRouter(Deps{Service: svc, Store: st, HasAPIKey: true, MaxBodyBytes: 1024})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/invoices/queued1/analysis", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
}

func TestGetAnalysis_Done(t *testing.T) {
	t.Parallel()

	st, err := store.Open(t.Context(), t.TempDir()+"/d.db")
	if err != nil {
		t.Fatal(err)
	}

	svc := invoice.NewService(st, nopJobs{}, t.TempDir(), 1024*1024, nil, nil)
	inv := &store.Invoice{
		ID:                 "done1",
		Status:             store.StatusDone,
		DocumentType:       "receipt",
		DocumentTypeConf:   0.9,
		SuggestedRouting:   "fiscal",
		RoutingConfidence:  0.85,
		EffectiveRouting:   "fiscal",
		AmountsSupported:   0.2,
		ItemsQtySupported:  0.8,
		TotalConsistent:    0.8,
		RiskConfidence:     0.8,
		EstimatedTotal:     10,
		ComputedItemsTotal: 10,
		NeedsReview:        true,
	}
	if err := st.Create(t.Context(), inv); err != nil {
		t.Fatal(err)
	}

	r := NewRouter(Deps{Service: svc, Store: st, HasAPIKey: true, MaxBodyBytes: 1024})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/invoices/done1/analysis", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}

	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}

	if got["invoice_id"] != "done1" {
		t.Fatalf("%+v", got)
	}
}
