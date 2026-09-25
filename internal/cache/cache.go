// Package cache provides caching for OCR results with multiple backends.
package cache

import (
	"time"

	"github.com/williamkoller/invoice-ocr-poc/internal/extract"
	"github.com/williamkoller/invoice-ocr-poc/internal/jev"
)

// Cache defines the OCR result cache contract.
type Cache interface {
	Get(ocrText string) (*Entry, bool)
	Set(ocrText string, entry *Entry)
	Enabled() bool
	Stats() Stats
}

// Entry holds a cached OCR processing result.
type Entry struct {
	OCRText          string         `json:"ocr_text"`
	OCRConfidence    float64        `json:"ocr_confidence"`
	Parsed           extract.Result `json:"parsed"`
	Judgment         jev.Judgment   `json:"judgment"`
	JevUsage         jev.Usage      `json:"jev_usage"`
	AssistUsed       bool           `json:"assist_used"`
	AssistNotes      string         `json:"assist_notes"`
	AssistModel      string         `json:"assist_model"`
	OpenRouterTokens int            `json:"openrouter_tokens"`
	OllamaDurationMs int64          `json:"ollama_duration_ms"`
	CreatedAt        time.Time      `json:"created_at"`
}

// Stats holds cache statistics.
type Stats struct {
	Entries int   `json:"entries"`
	Hits    int64 `json:"hits"`
	Misses  int64 `json:"misses"`
}
