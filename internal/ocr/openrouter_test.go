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

func TestOpenRouter_Recognize(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("path %s", r.URL.Path)
		}

		if r.Header.Get("Authorization") != "Bearer or-key" {
			t.Errorf("auth")
		}

		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}

		if body["model"] != "google/gemini-2.5-flash" {
			t.Errorf("model %v", body["model"])
		}

		usage, _ := body["usage"].(map[string]any)
		if usage["include"] != true {
			t.Errorf("usage %#v", body["usage"])
		}

		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]string{"content": "Cafe 10,00\nTOTAL 10,00"}},
			},
			"usage": map[string]any{
				"prompt_tokens":     30,
				"completion_tokens": 10,
				"total_tokens":      40,
				"cost":              0.0012,
			},
		})
	}))
	defer srv.Close()

	img := filepath.Join(t.TempDir(), "a.png")
	if err := os.WriteFile(img, []byte("fakeimg"), 0o600); err != nil {
		t.Fatal(err)
	}

	e := NewOpenRouter(OpenRouterOptions{
		BaseURL: srv.URL,
		APIKey:  "or-key",
		Timeout: 2 * time.Second,
	})
	got, err := e.Recognize(t.Context(), img)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(got.Text, "TOTAL 10,00") {
		t.Fatalf("%q", got.Text)
	}

	if got.Usage.TotalTokens != 40 || got.Usage.CostUSD != 0.0012 {
		t.Fatalf("usage %+v", got.Usage)
	}
}

func TestOpenRouter_Available(t *testing.T) {
	t.Parallel()

	if NewOpenRouter(OpenRouterOptions{}).Available() {
		t.Fatal("empty key should be unavailable")
	}
}

func TestNewEngine(t *testing.T) {
	t.Parallel()

	e := NewEngine(EngineOptions{
		OpenRouter: OpenRouterOptions{APIKey: "k"},
	})
	if _, ok := e.(*OpenRouter); !ok {
		t.Fatalf("%T", e)
	}

	e = NewEngine(EngineOptions{Name: "tesseract", TessLang: "por+eng"})
	if _, ok := e.(*Tesseract); !ok {
		t.Fatalf("%T", e)
	}

	e = NewEngine(EngineOptions{Name: "ollama"})
	if _, ok := e.(*Ollama); !ok {
		t.Fatalf("%T", e)
	}
}
