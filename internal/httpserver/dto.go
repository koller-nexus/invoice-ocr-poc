package httpserver

import (
	"encoding/json"

	"github.com/williamkoller/tesseract-poc-go/internal/extract"
	"github.com/williamkoller/tesseract-poc-go/internal/jev"
	"github.com/williamkoller/tesseract-poc-go/internal/store"
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
	}
}

type healthResponse struct {
	Status string            `json:"status"`
	Checks map[string]string `json:"checks"`
}

type errorBody struct {
	Error string `json:"error"`
}
