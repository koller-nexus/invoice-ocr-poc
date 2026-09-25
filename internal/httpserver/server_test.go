package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/williamkoller/invoice-ocr-poc/internal/invoice"
	"github.com/williamkoller/invoice-ocr-poc/internal/jev"
	"github.com/williamkoller/invoice-ocr-poc/internal/ocr"
	"github.com/williamkoller/invoice-ocr-poc/internal/store"
)

type stubOCR struct {
	available bool
}

func (s stubOCR) Recognize(context.Context, string) (ocr.Result, error) {
	return ocr.Result{}, nil
}

func (s stubOCR) Available() bool {
	return s.available
}

func testStore(t *testing.T) *store.Store {
	t.Helper()

	st, err := store.Open(t.Context(), t.TempDir()+"/t.db")
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = st.Close() })

	return st
}

type nopJobs struct{}

func (nopJobs) Enqueue(context.Context, *store.Invoice) error { return nil }

type fullJobs struct{}

func (fullJobs) Enqueue(context.Context, *store.Invoice) error { return invoice.ErrQueueFull }

func TestProcessImage_Accepted(t *testing.T) {
	t.Parallel()

	st := testStore(t)

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

	st := testStore(t)

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

	st := testStore(t)

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

	st := testStore(t)

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

	st := testStore(t)

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

	st := testStore(t)

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

func TestRuntime_PublicConfig(t *testing.T) {
	t.Parallel()

	st := testStore(t)

	r := NewRouter(Deps{
		Store:              st,
		OCR:                stubOCR{available: true},
		HasOpenRouterKey:   true,
		MaxBodyBytes:       8388608,
		OCRName:            "ollama",
		OllamaModel:        "glm-ocr:latest",
		OpenRouterModel:    "deepseek/deepseek-chat-v3-0324:free",
		OpenRouterOCRModel: "qwen/qwen3-vl-8b-instruct",
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/runtime", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}

	var got runtimeResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}

	if got.OCREngine != "ollama" {
		t.Fatalf("engine %q", got.OCREngine)
	}

	if got.MaxUploadBytes != 8388608 {
		t.Fatalf("max upload %d", got.MaxUploadBytes)
	}

	if got.Ollama.Model != "glm-ocr:latest" || !got.Ollama.Configured {
		t.Fatalf("ollama %+v", got.Ollama)
	}

	if got.OpenRouter.Model != "deepseek/deepseek-chat-v3-0324:free" {
		t.Fatalf("openrouter model %q", got.OpenRouter.Model)
	}

	if got.OpenRouter.OCRModel != "qwen/qwen3-vl-8b-instruct" || !got.OpenRouter.Configured {
		t.Fatalf("openrouter %+v", got.OpenRouter)
	}

	raw := rec.Body.String()
	if strings.Contains(raw, "sk-") || strings.Contains(raw, "api_key") || strings.Contains(raw, "API_KEY") {
		t.Fatalf("runtime leaked a secret: %s", raw)
	}
}

func TestRuntime_OpenRouterNotConfigured(t *testing.T) {
	t.Parallel()

	st := testStore(t)

	r := NewRouter(Deps{
		Store:            st,
		OCR:              stubOCR{available: false},
		HasOpenRouterKey: false,
		MaxBodyBytes:     1024,
		OCRName:          "openrouter",
		OllamaModel:      "glm-ocr",
		OpenRouterModel:  "deepseek/deepseek-chat",
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/runtime", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}

	var got runtimeResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}

	if got.OCREngine != "openrouter" {
		t.Fatalf("engine %q", got.OCREngine)
	}

	if got.Ollama.Configured {
		t.Fatalf("expected ollama unconfigured")
	}

	if got.OpenRouter.Configured {
		t.Fatalf("expected openrouter unconfigured")
	}
}

func TestGetInvoice_Usage(t *testing.T) {
	t.Parallel()

	st := testStore(t)

	svc := invoice.NewService(st, nopJobs{}, t.TempDir(), 1024, nil, nil)
	inv := &store.Invoice{
		ID:                         "u1",
		Status:                     store.StatusDone,
		OpenRouterPromptTokens:     12,
		OpenRouterCompletionTokens: 8,
		OpenRouterTotalTokens:      20,
		OpenRouterCostUSD:          0.0004,
		OpenRouterLatencyMs:        900,
		OllamaDurationMs:           1500,
		OllamaLoadDurationMs:       200,
		OllamaPromptEvalCount:      12,
		OllamaPromptEvalDurationMs: 300,
		OllamaEvalCount:            40,
		OllamaEvalDurationMs:       900,
		JevInputTokens:             296,
		JevOutputTokens:            20,
		JevTotalTokens:             316,
		JevLatencyMs:               410,
		JevModel:                   "jev-1.13.0",
		ProcessingMs:               4000,
	}
	if err := st.Create(t.Context(), inv); err != nil {
		t.Fatal(err)
	}

	r := NewRouter(Deps{Service: svc, Store: st, HasAPIKey: true, MaxBodyBytes: 1024})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/invoices/u1", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}

	var got invoiceResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}

	if got.Usage.OpenRouter.TotalTokens != 20 || got.Usage.OpenRouter.CostUSD != 0.0004 {
		t.Fatalf("openrouter %+v", got.Usage.OpenRouter)
	}

	if got.Usage.OpenRouter.LatencyMs != 900 || got.Usage.Ollama.DurationMs != 1500 {
		t.Fatalf("usage %+v", got.Usage)
	}

	if got.Usage.Ollama.LoadDurationMs != 200 || got.Usage.Ollama.EvalCount != 40 || got.Usage.Ollama.EvalDurationMs != 900 {
		t.Fatalf("ollama %+v", got.Usage.Ollama)
	}

	if got.Usage.Ollama.PromptEvalCount != 12 || got.Usage.Ollama.PromptEvalDurationMs != 300 {
		t.Fatalf("ollama prompt %+v", got.Usage.Ollama)
	}

	if got.Usage.Jev.TotalTokens != 316 || got.Usage.Jev.LatencyMs != 410 || got.Usage.Jev.Model != "jev-1.13.0" {
		t.Fatalf("jev %+v", got.Usage.Jev)
	}

	if got.Usage.Jev.CostUSD != jev.EstimateCostUSD(296) {
		t.Fatalf("jev cost %v", got.Usage.Jev.CostUSD)
	}

	raw := rec.Body.String()
	if strings.Contains(raw, "sk-") || strings.Contains(raw, "api_key") {
		t.Fatalf("usage leaked a secret: %s", raw)
	}
}
