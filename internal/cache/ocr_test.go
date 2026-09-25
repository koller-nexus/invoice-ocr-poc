package cache

import (
	"sync"
	"testing"
	"time"

	"github.com/williamkoller/invoice-ocr-poc/internal/extract"
	"github.com/williamkoller/invoice-ocr-poc/internal/jev"
)

func TestOCRCache_GetSet(t *testing.T) {
	t.Parallel()

	c := New(time.Hour, true)
	text := "Cafe 2x 5,00 10,00\nTotal R$ 10,00"

	_, ok := c.Get(text)
	if ok {
		t.Fatal("expected miss")
	}

	entry := &Entry{
		OCRText:       text,
		OCRConfidence: 1.0,
		Parsed: extract.Result{
			Items:              []extract.Item{{Description: "Cafe", Quantity: 2, UnitAmount: 5, LineTotal: 10}},
			EstimatedTotal:     10,
			ComputedItemsTotal: 10,
		},
		Judgment: jev.Judgment{DocumentType: "receipt"},
	}
	c.Set(text, entry)

	got, ok := c.Get(text)
	if !ok {
		t.Fatal("expected hit")
	}

	if got.OCRText != text {
		t.Fatalf("text %q", got.OCRText)
	}

	if got.Judgment.DocumentType != "receipt" {
		t.Fatalf("doc type %s", got.Judgment.DocumentType)
	}
}

func TestOCRCache_TTLExpiry(t *testing.T) {
	t.Parallel()

	c := New(10*time.Millisecond, true)
	text := "short lived"

	c.Set(text, &Entry{OCRText: text})

	if _, ok := c.Get(text); !ok {
		t.Fatal("expected hit before expiry")
	}

	time.Sleep(20 * time.Millisecond)

	if _, ok := c.Get(text); ok {
		t.Fatal("expected miss after expiry")
	}
}

func TestOCRCache_Disabled(t *testing.T) {
	t.Parallel()

	c := New(time.Hour, false)
	text := "ignored"

	c.Set(text, &Entry{OCRText: text})

	if _, ok := c.Get(text); ok {
		t.Fatal("expected miss when disabled")
	}

	if c.Enabled() {
		t.Fatal("expected disabled")
	}
}

func TestOCRCache_NilSafe(t *testing.T) {
	t.Parallel()

	var c *MemoryCache

	if c.Enabled() {
		t.Fatal("nil cache should not be enabled")
	}

	if _, ok := c.Get("x"); ok {
		t.Fatal("nil cache should miss")
	}

	c.Set("x", &Entry{}) // should not panic
}

func TestOCRCache_ConcurrentAccess(t *testing.T) {
	t.Parallel()

	c := New(time.Hour, true)
	const n = 100

	var wg sync.WaitGroup

	for i := range n {
		wg.Go(func() {
			text := string(rune('a' + (i % 26)))
			c.Set(text, &Entry{OCRText: text})
			c.Get(text)
		})
	}

	wg.Wait()

	stats := c.Stats()
	if stats.Entries == 0 {
		t.Fatal("expected some entries")
	}
}

func TestOCRCache_SameTextSameKey(t *testing.T) {
	t.Parallel()

	c := New(time.Hour, true)
	text := "same text"

	c.Set(text, &Entry{OCRText: text, OCRConfidence: 0.5})
	c.Set(text, &Entry{OCRText: text, OCRConfidence: 1.0})

	got, ok := c.Get(text)
	if !ok {
		t.Fatal("expected hit")
	}

	if got.OCRConfidence != 1.0 {
		t.Fatalf("expected overwrite, got %v", got.OCRConfidence)
	}
}
