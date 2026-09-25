package jev

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/williamkoller/invoice-ocr-poc/internal/extract"
)

func TestEvaluate_ComposesReviewFlags(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/systemone" {
			t.Errorf("path %s", r.URL.Path)
		}

		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("missing bearer")
		}

		body, _ := io.ReadAll(r.Body)
		var req requestBody
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatalf("request json: %v", err)
		}

		if req.Model != defaultModel {
			t.Errorf("model %s", req.Model)
		}

		if _, ok := req.Questions["amounts_supported"]; !ok {
			t.Fatal("missing amounts_supported")
		}

		_ = json.NewEncoder(w).Encode(responseBody{
			Model: "jev-1.13.0",
			Answers: map[string]map[string]any{
				"document_type":       {"type": "choice", "choice": "nfe", "confidence": 0.9},
				"routing":             {"type": "choice", "choice": "fiscal", "confidence": 0.85},
				"amounts_supported":   {"type": "noul", "noul": 0.2},
				"items_qty_supported": {"type": "noul", "noul": 0.9},
				"total_consistent":    {"type": "noul", "noul": 0.9},
				"risk":                {"type": "score", "score": 2.0, "confidence": 0.8},
			},
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-key", 2*time.Second)
	got, err := c.Evaluate(t.Context(), State{
		OCRText: "Total 10,00",
		Extracted: extract.Result{
			EstimatedTotal:     10,
			ComputedItemsTotal: 10,
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	if !got.NeedsReview {
		t.Fatal("expected needs_review from low amounts noul")
	}

	if got.ItemsConfirmed {
		t.Fatal("items must not be confirmed")
	}

	if got.DocumentType != "nfe" {
		t.Fatalf("type %s", got.DocumentType)
	}

	if got.EffectiveRouting != "fiscal" {
		t.Fatalf("routing %s", got.EffectiveRouting)
	}

	if got.RiskLabel != "high" {
		t.Fatalf("risk %s", got.RiskLabel)
	}
}

func TestEvaluate_RetriesOn429(t *testing.T) {
	t.Parallel()

	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n++
		if n == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte("slow down"))

			return
		}

		_ = json.NewEncoder(w).Encode(responseBody{
			Answers: map[string]map[string]any{
				"document_type":       {"choice": "receipt", "confidence": 0.7},
				"routing":             {"choice": "accounts_payable", "confidence": 0.7},
				"amounts_supported":   {"noul": 0.8},
				"items_qty_supported": {"noul": 0.8},
				"total_consistent":    {"noul": 0.8},
				"risk":                {"score": 0.2, "confidence": 0.7},
			},
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "k", 2*time.Second)
	got, err := c.Evaluate(t.Context(), State{})
	if err != nil {
		t.Fatal(err)
	}

	if got.NeedsReview {
		t.Fatal("did not expect review")
	}

	if n != 2 {
		t.Fatalf("attempts %d", n)
	}
}
