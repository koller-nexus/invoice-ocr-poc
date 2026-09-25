package openrouter

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/williamkoller/invoice-ocr-poc/internal/extract"
	"github.com/williamkoller/invoice-ocr-poc/internal/ocr"
	"go.uber.org/zap"
)

func TestAssist_ParsesItems(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("path %s", r.URL.Path)
		}

		if r.Header.Get("Authorization") != "Bearer or-key" {
			t.Errorf("auth")
		}

		var req chatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}

		if req.Model != defaultModel {
			t.Errorf("model %s", req.Model)
		}

		if !req.Usage.Include {
			t.Error("expected usage.include")
		}

		_ = json.NewEncoder(w).Encode(chatResponse{
			Choices: []chatChoice{{
				Message: chatMessage{
					Content: `{"items":[{"produto":"Arroz","quantidade":1,"valor_unitario":10,"line_total":10}],"total_estimado":10,"soma_itens":10,"soma_confere":true,"notas":"OCR legível"}`,
				},
			}},
			Usage: ocr.TokenUsage{
				PromptTokens:     12,
				CompletionTokens: 8,
				TotalTokens:      20,
				Cost:             0.0004,
			},
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "or-key", "", nil)
	got, notes, usage, err := c.Assist(t.Context(), "Arroz 10,00 Total 10,00", extract.Result{})
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(notes, "OCR legível") || len(got.Items) != 1 || got.ComputedItemsTotal != 10 {
		t.Fatalf("%+v notes=%q", got, notes)
	}

	if usage.TotalTokens != 20 || usage.CostUSD != 0.0004 {
		t.Fatalf("usage %+v", usage)
	}
}

func TestParseAssist_IgnoresEmptyItems(t *testing.T) {
	t.Parallel()

	hint := extract.Result{EstimatedTotal: 5, ComputedItemsTotal: 5}
	got, notes, err := parseAssist(`{"items":[],"notas":"só cabeçalho"}`, hint)
	if err != nil {
		t.Fatal(err)
	}

	if got.EstimatedTotal != 5 || notes != "só cabeçalho" {
		t.Fatalf("%+v %q", got, notes)
	}
}

func TestEnabled(t *testing.T) {
	t.Parallel()

	if NewClient("", "", "", nil).Enabled() {
		t.Fatal("empty key should disable")
	}
}

func TestClient_WithLogger(t *testing.T) {
	t.Parallel()

	var missing *Client
	if missing.WithLogger(zap.NewNop().Sugar()) != nil {
		t.Fatal("nil receiver should stay nil")
	}

	c := NewClient("", "", "", nil)
	if c.WithLogger(nil) != c {
		t.Fatal("nil logger should keep receiver")
	}

	log := zap.NewNop().Sugar()
	if c.WithLogger(log) != c {
		t.Fatal("logger should return receiver")
	}

	if c.log != log {
		t.Fatal("logger not stored")
	}
}
