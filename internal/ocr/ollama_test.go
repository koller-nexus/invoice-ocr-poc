package ocr

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOllama_Recognize(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/generate" {
			t.Errorf("path %s", r.URL.Path)
		}

		var body ollamaGenerateRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}

		if body.Model != "glm-ocr:latest" {
			t.Errorf("model %s", body.Model)
		}

		if len(body.Images) != 1 || body.Images[0] == "" {
			t.Fatal("missing image")
		}

		if body.Stream {
			t.Error("stream should be false")
		}

		_ = json.NewEncoder(w).Encode(ollamaGenerateResponse{
			Response:           "Cafe 10,00\nTOTAL 10,00",
			TotalDuration:      1_500_000_000,
			LoadDuration:       200_000_000,
			EvalCount:          40,
			EvalDuration:       900_000_000,
			PromptEvalCount:    12,
			PromptEvalDuration: 300_000_000,
		})
	}))
	defer srv.Close()

	img := filepath.Join(t.TempDir(), "a.png")
	if err := os.WriteFile(img, []byte("fakeimg"), 0o600); err != nil {
		t.Fatal(err)
	}

	e := NewOllama(OllamaOptions{BaseURL: srv.URL, Model: "glm-ocr:latest", Timeout: 2 * time.Second})
	got, err := e.Recognize(t.Context(), img)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(got.Text, "TOTAL 10,00") {
		t.Fatalf("%q", got.Text)
	}

	if got.Usage.DurationMs != 1500 || got.Usage.LoadDurationMs != 200 {
		t.Fatalf("duration %+v", got.Usage)
	}

	if got.Usage.EvalCount != 40 || got.Usage.EvalDurationMs != 900 {
		t.Fatalf("eval %+v", got.Usage)
	}

	if got.Usage.PromptEvalCount != 12 || got.Usage.PromptEvalDurationMs != 300 {
		t.Fatalf("prompt eval %+v", got.Usage)
	}
}

func TestOllama_Inspect(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/show":
			_ = json.NewEncoder(w).Encode(ollamaShowResponse{
				Details: ollamaModelDetails{
					ParameterSize:     "0.9B",
					QuantizationLevel: "Q8_0",
				},
				ModelInfo: map[string]any{"glmocr.context_length": float64(8192)},
			})
		case "/api/ps":
			_ = json.NewEncoder(w).Encode(ollamaPSResponse{
				Models: []ollamaPSModel{{
					Name:          "glm-ocr:latest",
					Size:          900_000_000,
					SizeVRAM:      800_000_000,
					ContextLength: 8192,
				}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	e := NewOllama(OllamaOptions{BaseURL: srv.URL, Model: "glm-ocr:latest"})
	got, err := e.Inspect(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	if !got.Reachable || !got.Loaded {
		t.Fatalf("%+v", got)
	}

	if got.ParameterSize != "0.9B" || got.Quantization != "Q8_0" || got.ContextLength != 8192 {
		t.Fatalf("%+v", got)
	}

	if got.SizeBytes != 900_000_000 || got.VRAMBytes != 800_000_000 {
		t.Fatalf("%+v", got)
	}
}

func TestNsToMs(t *testing.T) {
	t.Parallel()

	if got := nsToMs(0); got != 0 {
		t.Fatalf("%d", got)
	}

	if got := nsToMs(2_400_000_000); got != 2400 {
		t.Fatalf("%d", got)
	}
}

func TestTrimOCRFences(t *testing.T) {
	t.Parallel()

	got := trimOCRFences("CAFE\nTOTAL 10.00\n```markdown\nCAFE\n```\n```\n```")
	if got != "CAFE\nTOTAL 10.00" {
		t.Fatalf("%q", got)
	}
}
