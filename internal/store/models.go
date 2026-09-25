package store

import (
	"time"

	"gorm.io/gorm"
)

// Status values for an invoice processing job.
const (
	StatusQueued     = "queued"
	StatusProcessing = "processing"
	StatusDone       = "done"
	StatusFailed     = "failed"
)

// Invoice is a persisted OCR + Jev job.
type Invoice struct {
	ID                         string `gorm:"primaryKey"`
	Status                     string `gorm:"index"`
	OriginalName               string
	StoredPath                 string
	MimeType                   string
	OCRText                    string
	OCRConfidence              float64
	EstimatedTotal             float64
	ComputedItemsTotal         float64
	ItemsJSON                  string
	ItemsConfirmed             bool
	DocumentType               string
	DocumentTypeConf           float64
	SuggestedRouting           string
	RoutingConfidence          float64
	EffectiveRouting           string
	AmountsSupported           float64
	ItemsQtySupported          float64
	TotalConsistent            float64
	RiskScore                  float64
	RiskLabel                  string
	RiskConfidence             float64
	NeedsReview                bool
	AssistModel                string
	AssistNotes                string
	AssistUsed                 bool
	ErrorMessage               string
	ProcessingMs               int64
	OpenRouterPromptTokens     int
	OpenRouterCompletionTokens int
	OpenRouterTotalTokens      int
	OpenRouterCostUSD          float64
	OpenRouterLatencyMs        int64
	OllamaDurationMs           int64
	OllamaLoadDurationMs       int64
	OllamaPromptEvalCount      int
	OllamaPromptEvalDurationMs int64
	OllamaEvalCount            int
	OllamaEvalDurationMs       int64
	JevInputTokens             int
	JevOutputTokens            int
	JevTotalTokens             int
	JevCostUSD                 float64
	JevLatencyMs               int64
	JevModel                   string
	CreatedAt                  time.Time
	UpdatedAt                  time.Time
	DeletedAt                  gorm.DeletedAt `gorm:"index"`
}
