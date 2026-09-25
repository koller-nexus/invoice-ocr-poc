package ocr

import "testing"

func TestTokenUsage_ToUsage(t *testing.T) {
	t.Parallel()

	got := TokenUsage{
		PromptTokens:     10,
		CompletionTokens: 5,
		Cost:             0.02,
	}.ToUsage(40)

	if got.TotalTokens != 15 || got.CostUSD != 0.02 || got.DurationMs != 40 {
		t.Fatalf("%+v", got)
	}

	if !got.HasOpenRouter() {
		t.Fatal("expected openrouter usage")
	}
}

func TestUsage_Add(t *testing.T) {
	t.Parallel()

	got := Usage{PromptTokens: 1, DurationMs: 10}.Add(Usage{PromptTokens: 2, DurationMs: 5, CostUSD: 0.1})
	if got.PromptTokens != 3 || got.DurationMs != 15 || got.CostUSD != 0.1 {
		t.Fatalf("%+v", got)
	}
}
