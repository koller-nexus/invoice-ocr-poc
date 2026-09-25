package jev

import (
	"fmt"

	"github.com/williamkoller/invoice-ocr-poc/internal/store"
)

// Analysis is a read-only explanation of a stored Jev verdict.
type Analysis struct {
	InvoiceID    string      `json:"invoice_id"`
	Verdict      Verdict     `json:"verdict"`
	Questions    []Question  `json:"questions"`
	CodeChecks   []CodeCheck `json:"code_checks"`
	Determinants []Factor    `json:"determinants"`
	LLMSupport   LLMSupport  `json:"llm_support"`
}

// LLMSupport is optional DeepSeek commentary; it does not override the Jev verdict.
type LLMSupport struct {
	Used  bool   `json:"used"`
	Model string `json:"model,omitempty"`
	Notes string `json:"notes,omitempty"`
	Role  string `json:"role"`
}

// Verdict is the composed application decision.
type Verdict struct {
	NeedsReview      bool   `json:"needs_review"`
	ItemsConfirmed   bool   `json:"items_confirmed"`
	EffectiveRouting string `json:"effective_routing"`
	SuggestedRouting string `json:"suggested_routing"`
}

// Question is one fan-out judgment persisted on the invoice.
type Question struct {
	ID           string  `json:"id"`
	Type         string  `json:"type"`
	Instructions string  `json:"instructions"`
	Answer       string  `json:"answer,omitempty"`
	Noul         float64 `json:"noul,omitempty"`
	Score        float64 `json:"score,omitempty"`
	Confidence   float64 `json:"confidence,omitempty"`
	Threshold    float64 `json:"threshold,omitempty"`
	Triggered    bool    `json:"triggered"`
}

// CodeCheck is a deterministic rule applied in compose.
type CodeCheck struct {
	ID                 string  `json:"id"`
	Description        string  `json:"description"`
	EstimatedTotal     float64 `json:"estimated_total"`
	ComputedItemsTotal float64 `json:"computed_items_total"`
	Triggered          bool    `json:"triggered"`
}

// Factor is a triggered rule that determined needs_review or routing.
type Factor struct {
	ID     string `json:"id"`
	Reason string `json:"reason"`
}

// Explain rebuilds the Jev analysis from a stored invoice. It does not call TypeSafe.
func Explain(inv store.Invoice) Analysis {
	qs := []Question{
		choiceQuestion("document_type", "Que tipo de documento é o texto do OCR?", LocalizeDocType(inv.DocumentType), inv.DocumentTypeConf),
		choiceQuestion("routing", "Se este documento precisa ser encaminhado, para qual setor?", LocalizeRouting(inv.SuggestedRouting), inv.RoutingConfidence),
		noulQuestion("amounts_supported", "Os valores extraídos dos itens e o total estimado aparecem no texto do OCR?", inv.AmountsSupported),
		noulQuestion("items_qty_supported", "Os itens e as quantidades extraídos têm suporte no texto do OCR?", inv.ItemsQtySupported),
		noulQuestion("total_consistent", "O total estimado é consistente com a soma dos itens e com os totais do OCR?", inv.TotalConsistent),
		scoreQuestion("risk", "Qual o risco de confiar nos dados extraídos em relação ao OCR?", LocalizeRisk(inv.RiskLabel), inv.RiskScore, inv.RiskConfidence),
	}

	mismatch := abs(inv.EstimatedTotal-inv.ComputedItemsTotal) > TotalDelta
	checks := []CodeCheck{{
		ID: "item_total_mismatch",
		Description: "estimated_total é o valor de total lido no cupom/OCR. " +
			"computed_items_total é a soma das linhas extraídas (quantidade × valor ou subtotal). " +
			"Quando os dois são iguais, a conta fecha; se diferirem além do limiar, o código marca revisão.",
		EstimatedTotal:     inv.EstimatedTotal,
		ComputedItemsTotal: inv.ComputedItemsTotal,
		Triggered:          mismatch,
	}}

	determinants := make([]Factor, 0)
	for _, q := range qs {
		if !q.Triggered {
			continue
		}

		determinants = append(determinants, Factor{ID: q.ID, Reason: questionReason(q)})
	}

	if mismatch {
		determinants = append(determinants, Factor{
			ID: "item_total_mismatch",
			Reason: fmt.Sprintf(
				"o total estimado (%.2f) e a soma dos itens (%.2f) diferem mais que %.2f",
				inv.EstimatedTotal, inv.ComputedItemsTotal, TotalDelta,
			),
		})
	}

	if inv.SuggestedRouting == "other" || inv.RoutingConfidence < ConfidenceFloor {
		if !hasFactor(determinants, "routing") {
			determinants = append(determinants, Factor{
				ID:     "effective_routing",
				Reason: "o roteamento sugerido é indefinido ou a confiança está abaixo do limiar; o roteamento efetivo é revisão",
			})
		}
	}

	return Analysis{
		InvoiceID: inv.ID,
		Verdict: Verdict{
			NeedsReview:      inv.NeedsReview,
			ItemsConfirmed:   inv.ItemsConfirmed,
			EffectiveRouting: LocalizeRouting(inv.EffectiveRouting),
			SuggestedRouting: LocalizeRouting(inv.SuggestedRouting),
		},
		Questions:    qs,
		CodeChecks:   checks,
		Determinants: determinants,
		LLMSupport: LLMSupport{
			Used:  inv.AssistUsed,
			Model: inv.AssistModel,
			Notes: inv.AssistNotes,
			Role:  "Apoio à leitura do OCR (itens e notas). O Jev e o código decidem revisão e roteamento.",
		},
	}
}

func choiceQuestion(id, instructions, answer string, conf float64) Question {
	return Question{
		ID:           id,
		Type:         "choice",
		Instructions: instructions,
		Answer:       answer,
		Confidence:   conf,
		Threshold:    ConfidenceFloor,
		Triggered:    conf < ConfidenceFloor,
	}
}

func noulQuestion(id, instructions string, noul float64) Question {
	return Question{
		ID:           id,
		Type:         "noul",
		Instructions: instructions,
		Noul:         noul,
		Threshold:    NoulThreshold,
		Triggered:    noul < NoulThreshold,
	}
}

func scoreQuestion(id, instructions, label string, score, conf float64) Question {
	return Question{
		ID:           id,
		Type:         "score",
		Instructions: instructions,
		Answer:       label,
		Score:        score,
		Confidence:   conf,
		Threshold:    ConfidenceFloor,
		Triggered:    conf < ConfidenceFloor,
	}
}

func questionReason(q Question) string {
	switch q.Type {
	case "noul":
		return fmt.Sprintf("probabilidade sim/não %.2f está abaixo do limiar %.2f", q.Noul, q.Threshold)
	case "choice", "score":
		return fmt.Sprintf("confiança %.2f está abaixo do limiar %.2f", q.Confidence, q.Threshold)
	default:
		return "regra disparada"
	}
}

// LocalizeDocType maps stored document type codes to Portuguese labels.
func LocalizeDocType(v string) string {
	switch v {
	case "nfe":
		return "NF-e"
	case "receipt":
		return "recibo"
	case "service_invoice":
		return "fatura de serviço"
	case "other":
		return "outro"
	default:
		return v
	}
}

// LocalizeRouting maps stored routing codes to Portuguese labels.
func LocalizeRouting(v string) string {
	switch v {
	case "fiscal":
		return "setor fiscal"
	case "accounts_payable":
		return "contas a pagar"
	case "review":
		return "revisão"
	case "other":
		return "outro"
	default:
		return v
	}
}

// LocalizeRisk maps stored risk labels to Portuguese.
func LocalizeRisk(v string) string {
	switch v {
	case "low":
		return "baixo"
	case "medium":
		return "médio"
	case "high":
		return "alto"
	default:
		return v
	}
}

func hasFactor(factors []Factor, id string) bool {
	for _, f := range factors {
		if f.ID == id {
			return true
		}
	}

	return false
}
