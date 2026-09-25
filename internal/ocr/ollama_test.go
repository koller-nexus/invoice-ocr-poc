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
			Response: "Cafe 10,00\nTOTAL 10,00",
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
}

func TestTrimOCRFences(t *testing.T) {
	t.Parallel()

	got := trimOCRFences("CAFE\nTOTAL 10.00\n```markdown\nCAFE\n```\n```\n```")
	if got != "CAFE\nTOTAL 10.00" {
		t.Fatalf("%q", got)
	}
}
