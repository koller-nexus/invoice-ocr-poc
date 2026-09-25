//go:build evaluation

package test

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/williamkoller/invoice-ocr-poc/internal/extract"
	"github.com/williamkoller/invoice-ocr-poc/internal/ocr"
	"github.com/williamkoller/invoice-ocr-poc/internal/preprocess"
)

type goldFile struct {
	Cases []goldCase `json:"cases"`
}

type goldCase struct {
	ID             string  `json:"id"`
	Image          string  `json:"image"`
	EstimatedTotal float64 `json:"estimated_total"`
	ItemCount      int     `json:"item_count"`
	Skip           bool    `json:"skip"`
}

func TestFieldAccuracy(t *testing.T) {
	root := filepath.Join("fixtures", "invoices")
	raw, err := os.ReadFile(filepath.Join(root, "gold.json"))
	if err != nil {
		t.Fatal(err)
	}

	var gold goldFile
	if err := json.Unmarshal(raw, &gold); err != nil {
		t.Fatal(err)
	}

	engine := ocr.NewEngine(ocr.EngineOptions{
		OpenRouter: ocr.OpenRouterOptions{
			APIKey: os.Getenv("OPENROUTER_API_KEY"),
		},
	})
	if !engine.Available() {
		t.Skip("OPENROUTER_API_KEY is not set")
	}

	prep := preprocess.New()
	totalOK, itemOK, n := 0, 0, 0

	for _, c := range gold.Cases {
		if c.Skip {
			t.Logf("skip %s", c.ID)
			continue
		}

		img := filepath.Join(root, c.Image)
		if _, err := os.Stat(img); err != nil {
			t.Logf("missing image %s", img)
			continue
		}

		n++
		cleaned, err := prep.Prepare(t.Context(), img, t.TempDir())
		if err != nil {
			t.Errorf("%s preprocess: %v", c.ID, err)
			continue
		}

		ocrRes, err := engine.Recognize(t.Context(), cleaned)
		if err != nil {
			t.Errorf("%s ocr: %v", c.ID, err)
			continue
		}

		parsed := extract.Parse(ocrRes.Text)
		if math.Abs(parsed.EstimatedTotal-c.EstimatedTotal) <= 0.05 {
			totalOK++
		}

		if c.ItemCount == 0 || len(parsed.Items) == c.ItemCount {
			itemOK++
		}

		t.Logf("%s total_got=%.2f total_gold=%.2f items=%d gold_items=%d",
			c.ID, parsed.EstimatedTotal, c.EstimatedTotal, len(parsed.Items), c.ItemCount)
	}

	if n == 0 {
		t.Skip("no gold cases with images")
	}

	t.Logf("total accuracy %d/%d item-count accuracy %d/%d", totalOK, n, itemOK, n)
}
