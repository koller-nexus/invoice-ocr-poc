// Package ocr extracts text from images.
package ocr

import "context"

// Usage is provider telemetry from one OCR or assist call.
type Usage struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	CostUSD          float64
	DurationMs       int64
}

// Add sums another call's usage into u.
func (u Usage) Add(other Usage) Usage {
	return Usage{
		PromptTokens:     u.PromptTokens + other.PromptTokens,
		CompletionTokens: u.CompletionTokens + other.CompletionTokens,
		TotalTokens:      u.TotalTokens + other.TotalTokens,
		CostUSD:          u.CostUSD + other.CostUSD,
		DurationMs:       u.DurationMs + other.DurationMs,
	}
}

// HasOpenRouter reports whether the call billed OpenRouter tokens or cost.
func (u Usage) HasOpenRouter() bool {
	return u.PromptTokens > 0 || u.CompletionTokens > 0 || u.TotalTokens > 0 || u.CostUSD > 0
}

// TokenUsage is the OpenRouter/OpenAI usage object.
type TokenUsage struct {
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	TotalTokens      int     `json:"total_tokens"`
	Cost             float64 `json:"cost"`
}

// ToUsage maps billed tokens onto Usage.
func (u TokenUsage) ToUsage(durationMs int64) Usage {
	total := u.TotalTokens
	if total == 0 {
		total = u.PromptTokens + u.CompletionTokens
	}

	return Usage{
		PromptTokens:     u.PromptTokens,
		CompletionTokens: u.CompletionTokens,
		TotalTokens:      total,
		CostUSD:          u.Cost,
		DurationMs:       durationMs,
	}
}

// UsageInclude asks OpenRouter to return cost in the usage object.
type UsageInclude struct {
	Include bool `json:"include"`
}

// Result is OCR output.
type Result struct {
	Text       string
	Confidence float64
	Usage      Usage
}

// Engine reads text from an image file.
type Engine interface {
	Recognize(ctx context.Context, imagePath string) (Result, error)
	Available() bool
}
