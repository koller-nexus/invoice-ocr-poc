package httpserver

import (
	"encoding/json"

	"github.com/williamkoller/invoice-ocr-poc/internal/extract"
	"github.com/williamkoller/invoice-ocr-poc/internal/jev"
	"github.com/williamkoller/invoice-ocr-poc/internal/store"
)

type enqueueResponse struct {
	ID      string `json:"id"`
	Status  string `json:"status"`
	PollURL string `json:"poll_url"`
}

type invoiceResponse struct {
	ID                 string         `json:"id"`
	Status             string         `json:"status"`
	OriginalName       string         `json:"original_name"`
	MimeType           string         `json:"mime_type"`
	OCRText            string         `json:"ocr_text,omitempty"`
	OCRConfidence      float64        `json:"ocr_confidence,omitempty"`
	Items              []extract.Item `json:"items"`
	EstimatedTotal     float64        `json:"estimated_total"`
	ComputedItemsTotal float64        `json:"computed_items_total"`
	ItemsConfirmed     bool           `json:"items_confirmed"`
	DocumentType       string         `json:"document_type,omitempty"`
	DocumentTypeConf   float64        `json:"document_type_confidence,omitempty"`
	SuggestedRouting   string         `json:"suggested_routing,omitempty"`
	RoutingConfidence  float64        `json:"routing_confidence,omitempty"`
	EffectiveRouting   string         `json:"effective_routing,omitempty"`
	AmountsSupported   float64        `json:"amounts_supported"`
	ItemsQtySupported  float64        `json:"items_qty_supported"`
	TotalConsistent    float64        `json:"total_consistent"`
	RiskScore          float64        `json:"risk_score"`
	RiskLabel          string         `json:"risk_label,omitempty"`
	RiskConfidence     float64        `json:"risk_confidence,omitempty"`
	NeedsReview        bool           `json:"needs_review"`
	AssistUsed         bool           `json:"assist_used"`
	AssistModel        string         `json:"assist_model,omitempty"`
	AssistNotes        string         `json:"assist_notes,omitempty"`
	ErrorMessage       string         `json:"error_message,omitempty"`
	ProcessingMs       int64          `json:"processing_ms,omitempty"`
	Usage              invoiceUsage   `json:"usage"`
}

type usageOpenRouter struct {
	PromptTokens     int     `json:"prompt_tokens,omitempty"`
	CompletionTokens int     `json:"completion_tokens,omitempty"`
	TotalTokens      int     `json:"total_tokens,omitempty"`
	CostUSD          float64 `json:"cost_usd,omitempty"`
	LatencyMs        int64   `json:"latency_ms,omitempty"`
}

type usageOllama struct {
	DurationMs           int64 `json:"duration_ms,omitempty"`
	LoadDurationMs       int64 `json:"load_duration_ms,omitempty"`
	PromptEvalCount      int   `json:"prompt_eval_count,omitempty"`
	PromptEvalDurationMs int64 `json:"prompt_eval_duration_ms,omitempty"`
	EvalCount            int   `json:"eval_count,omitempty"`
	EvalDurationMs       int64 `json:"eval_duration_ms,omitempty"`
}

type usageJev struct {
	InputTokens  int     `json:"input_tokens,omitempty"`
	OutputTokens int     `json:"output_tokens,omitempty"`
	TotalTokens  int     `json:"total_tokens,omitempty"`
	CostUSD      float64 `json:"cost_usd,omitempty"`
	LatencyMs    int64   `json:"latency_ms,omitempty"`
	Model        string  `json:"model,omitempty"`
}

type invoiceUsage struct {
	OpenRouter usageOpenRouter `json:"openrouter"`
	Ollama     usageOllama     `json:"ollama"`
	Jev        usageJev        `json:"jev"`
}

func toInvoiceResponse(inv *store.Invoice) invoiceResponse {
	items := []extract.Item{}
	if inv.ItemsJSON != "" {
		_ = json.Unmarshal([]byte(inv.ItemsJSON), &items)
	}

	return invoiceResponse{
		ID:                 inv.ID,
		Status:             inv.Status,
		OriginalName:       inv.OriginalName,
		MimeType:           inv.MimeType,
		OCRText:            inv.OCRText,
		OCRConfidence:      inv.OCRConfidence,
		Items:              items,
		EstimatedTotal:     inv.EstimatedTotal,
		ComputedItemsTotal: inv.ComputedItemsTotal,
		ItemsConfirmed:     inv.ItemsConfirmed,
		DocumentType:       jev.LocalizeDocType(inv.DocumentType),
		DocumentTypeConf:   inv.DocumentTypeConf,
		SuggestedRouting:   jev.LocalizeRouting(inv.SuggestedRouting),
		RoutingConfidence:  inv.RoutingConfidence,
		EffectiveRouting:   jev.LocalizeRouting(inv.EffectiveRouting),
		AmountsSupported:   inv.AmountsSupported,
		ItemsQtySupported:  inv.ItemsQtySupported,
		TotalConsistent:    inv.TotalConsistent,
		RiskScore:          inv.RiskScore,
		RiskLabel:          jev.LocalizeRisk(inv.RiskLabel),
		RiskConfidence:     inv.RiskConfidence,
		NeedsReview:        inv.NeedsReview,
		AssistUsed:         inv.AssistUsed,
		AssistModel:        inv.AssistModel,
		AssistNotes:        inv.AssistNotes,
		ErrorMessage:       inv.ErrorMessage,
		ProcessingMs:       inv.ProcessingMs,
		Usage: invoiceUsage{
			OpenRouter: usageOpenRouter{
				PromptTokens:     inv.OpenRouterPromptTokens,
				CompletionTokens: inv.OpenRouterCompletionTokens,
				TotalTokens:      inv.OpenRouterTotalTokens,
				CostUSD:          inv.OpenRouterCostUSD,
				LatencyMs:        inv.OpenRouterLatencyMs,
			},
			Ollama: usageOllama{
				DurationMs:           inv.OllamaDurationMs,
				LoadDurationMs:       inv.OllamaLoadDurationMs,
				PromptEvalCount:      inv.OllamaPromptEvalCount,
				PromptEvalDurationMs: inv.OllamaPromptEvalDurationMs,
				EvalCount:            inv.OllamaEvalCount,
				EvalDurationMs:       inv.OllamaEvalDurationMs,
			},
			Jev: usageJev{
				InputTokens:  inv.JevInputTokens,
				OutputTokens: inv.JevOutputTokens,
				TotalTokens:  inv.JevTotalTokens,
				CostUSD:      jevCostUSD(inv),
				LatencyMs:    inv.JevLatencyMs,
				Model:        inv.JevModel,
			},
		},
	}
}

func jevCostUSD(inv *store.Invoice) float64 {
	if inv.JevCostUSD > 0 {
		return inv.JevCostUSD
	}

	if inv.JevInputTokens <= 0 {
		return 0
	}

	return jev.EstimateCostUSD(inv.JevInputTokens)
}

type healthResponse struct {
	Status string            `json:"status"`
	Checks map[string]string `json:"checks"`
}

type runtimeOllama struct {
	Model         string `json:"model"`
	Configured    bool   `json:"configured"`
	Reachable     bool   `json:"reachable"`
	Loaded        bool   `json:"loaded"`
	ParameterSize string `json:"parameter_size,omitempty"`
	Quantization  string `json:"quantization,omitempty"`
	ContextLength int    `json:"context_length,omitempty"`
	SizeBytes     int64  `json:"size_bytes,omitempty"`
	VRAMBytes     int64  `json:"vram_bytes,omitempty"`
}

type runtimeOpenRouter struct {
	Model      string `json:"model"`
	OCRModel   string `json:"ocr_model"`
	Configured bool   `json:"configured"`
}

type runtimeResponse struct {
	OCREngine      string            `json:"ocr_engine"`
	MaxUploadBytes int64             `json:"max_upload_bytes"`
	Ollama         runtimeOllama     `json:"ollama"`
	OpenRouter     runtimeOpenRouter `json:"openrouter"`
}

type errorBody struct {
	Error string `json:"error"`
}
