// Package jev calls TypeSafe System One over HTTP.
package jev

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/williamkoller/invoice-ocr-poc/internal/extract"
)

const (
	defaultModel = "jev-latest"
	maxRetries   = 3

	// NoulThreshold is the yes/no bar used in compose and Explain.
	NoulThreshold = 0.5
	// ConfidenceFloor is the choice/score bar used in compose and Explain.
	ConfidenceFloor = 0.6
	// TotalDelta is the allowed gap between estimated and computed totals.
	TotalDelta = 0.05
)

// Client talks to POST /v1/systemone.
type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

// NewClient builds an HTTP Jev client.
func NewClient(baseURL, apiKey string, timeout time.Duration) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

// State is the evidence sent to Jev.
type State struct {
	OCRText   string         `json:"ocr_text"`
	Extracted extract.Result `json:"extracted"`
}

// Judgment is the composed application result.
type Judgment struct {
	DocumentType      string
	DocumentTypeConf  float64
	SuggestedRouting  string
	RoutingConfidence float64
	EffectiveRouting  string
	AmountsSupported  float64
	ItemsQtySupported float64
	TotalConsistent   float64
	RiskScore         float64
	RiskLabel         string
	RiskConfidence    float64
	NeedsReview       bool
	ItemsConfirmed    bool
}

// Usage is billed tokens and wall-clock time for one Evaluate call.
type Usage struct {
	InputTokens  int
	OutputTokens int
	TotalTokens  int
	LatencyMs    int64
	Model        string
}

type requestBody struct {
	Model     string         `json:"model"`
	State     State          `json:"state"`
	Questions map[string]any `json:"questions"`
}

type responseBody struct {
	Model   string                    `json:"model"`
	Answers map[string]map[string]any `json:"answers"`
	Usage   tokenUsage                `json:"usage"`
}

type tokenUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// Evaluate sends one fan-out request and composes the verdict in code.
func (c *Client) Evaluate(ctx context.Context, state State) (Judgment, Usage, error) {
	if c.apiKey == "" {
		return Judgment{}, Usage{}, fmt.Errorf("typesafe api key is required")
	}

	body := requestBody{
		Model:     defaultModel,
		State:     state,
		Questions: questions(),
	}

	raw, err := json.Marshal(body)
	if err != nil {
		return Judgment{}, Usage{}, fmt.Errorf("marshal jev request: %w", err)
	}

	started := time.Now()
	respBody, err := c.post(ctx, raw)
	latencyMs := time.Since(started).Milliseconds()
	if err != nil {
		return Judgment{}, Usage{}, err
	}

	judgment, err := compose(state, respBody)
	if err != nil {
		return Judgment{}, Usage{}, err
	}

	return judgment, usageOf(respBody, latencyMs), nil
}

func usageOf(resp responseBody, latencyMs int64) Usage {
	total := resp.Usage.InputTokens + resp.Usage.OutputTokens
	return Usage{
		InputTokens:  resp.Usage.InputTokens,
		OutputTokens: resp.Usage.OutputTokens,
		TotalTokens:  total,
		LatencyMs:    latencyMs,
		Model:        resp.Model,
	}
}

func questions() map[string]any {
	return map[string]any{
		"document_type": map[string]any{
			"type":         "choice",
			"instructions": "What kind of document is `ocr_text`?",
			"criteria": map[string]string{
				"nfe":             "A Brazilian NF-e or similar tax invoice.",
				"receipt":         "A payment receipt or comprovante.",
				"service_invoice": "A service invoice or NFS-e.",
				"other":           "None of the listed document types.",
			},
		},
		"routing": map[string]any{
			"type":         "choice",
			"instructions": "If this document must be routed, which desk should review it given `ocr_text` and `extracted`?",
			"criteria": map[string]string{
				"fiscal":           "Tax / fiscal review.",
				"accounts_payable": "Accounts payable.",
				"other":            "Neither fiscal nor accounts payable, or unclear.",
			},
		},
		"amounts_supported": map[string]any{
			"type":         "noul",
			"instructions": "Are the amounts in `extracted.items` and `extracted.estimated_total` supported by `ocr_text`?",
			"criteria": map[string]string{
				"true":  "The cited amounts appear in the OCR text.",
				"false": "The amounts are missing, invented, or contradicted.",
			},
		},
		"items_qty_supported": map[string]any{
			"type":         "noul",
			"instructions": "Are the line items and quantities in `extracted.items` supported by `ocr_text`?",
			"criteria": map[string]string{
				"true":  "Item descriptions and quantities match the OCR text.",
				"false": "Items or quantities are unsupported or contradicted.",
			},
		},
		"total_consistent": map[string]any{
			"type":         "noul",
			"instructions": "Is `extracted.estimated_total` consistent with `extracted.computed_items_total` and the totals in `ocr_text`?",
			"criteria": map[string]string{
				"true":  "The estimated total agrees with the item sum and OCR totals.",
				"false": "The totals disagree or a total is missing.",
			},
		},
		"risk": map[string]any{
			"type":         "score",
			"instructions": "How risky is trusting `extracted` against `ocr_text`?",
			"criteria": []string{
				"Amounts, quantities, and totals line up with the OCR text.",
				"Some lines are missing, ambiguous, or only partly supported.",
				"Totals or quantities contradict the OCR text or look fabricated.",
			},
		},
	}
}

func (c *Client) post(ctx context.Context, payload []byte) (responseBody, error) {
	var lastErr error

	for attempt := range maxRetries {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return responseBody{}, ctx.Err()
			case <-time.After(time.Duration(1<<attempt) * 200 * time.Millisecond):
			}
		}

		req, err := http.NewRequestWithContext(
			ctx,
			http.MethodPost,
			c.baseURL+"/v1/systemone",
			bytes.NewReader(payload),
		)
		if err != nil {
			return responseBody{}, fmt.Errorf("new jev request: %w", err)
		}

		req.Header.Set("Authorization", "Bearer "+c.apiKey)
		req.Header.Set("Content-Type", "application/json")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("jev request: %w", err)
			continue
		}

		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		closeErr := resp.Body.Close()

		if readErr != nil {
			lastErr = fmt.Errorf("read jev body: %w", readErr)
			continue
		}

		if closeErr != nil {
			lastErr = fmt.Errorf("close jev body: %w", closeErr)
			continue
		}

		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == 529 {
			lastErr = fmt.Errorf("jev retryable status %d: %s", resp.StatusCode, bytes.TrimSpace(body))
			continue
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return responseBody{}, fmt.Errorf("jev status %d: %s", resp.StatusCode, bytes.TrimSpace(body))
		}

		var parsed responseBody
		if err := json.Unmarshal(body, &parsed); err != nil {
			return responseBody{}, fmt.Errorf("decode jev response: %w", err)
		}

		return parsed, nil
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("jev request failed")
	}

	return responseBody{}, lastErr
}

func compose(state State, resp responseBody) (Judgment, error) {
	docType, docConf := choiceOf(resp.Answers["document_type"])
	routing, routeConf := choiceOf(resp.Answers["routing"])
	amounts := noulOf(resp.Answers["amounts_supported"])
	itemsQty := noulOf(resp.Answers["items_qty_supported"])
	totalOK := noulOf(resp.Answers["total_consistent"])
	riskScore, riskConf, riskLabel := scoreOf(resp.Answers["risk"])

	numericMismatch := abs(state.Extracted.EstimatedTotal-state.Extracted.ComputedItemsTotal) > TotalDelta
	needsReview := amounts < NoulThreshold ||
		itemsQty < NoulThreshold ||
		totalOK < NoulThreshold ||
		docConf < ConfidenceFloor ||
		routeConf < ConfidenceFloor ||
		riskConf < ConfidenceFloor ||
		numericMismatch

	effective := routing
	if routing == "other" || routeConf < ConfidenceFloor {
		effective = "review"
	}

	return Judgment{
		DocumentType:      docType,
		DocumentTypeConf:  docConf,
		SuggestedRouting:  routing,
		RoutingConfidence: routeConf,
		EffectiveRouting:  effective,
		AmountsSupported:  amounts,
		ItemsQtySupported: itemsQty,
		TotalConsistent:   totalOK,
		RiskScore:         riskScore,
		RiskLabel:         riskLabel,
		RiskConfidence:    riskConf,
		NeedsReview:       needsReview,
		ItemsConfirmed:    !needsReview,
	}, nil
}

func choiceOf(ans map[string]any) (string, float64) {
	choice, _ := ans["choice"].(string)
	conf, _ := ans["confidence"].(float64)

	return choice, conf
}

func noulOf(ans map[string]any) float64 {
	v, _ := ans["noul"].(float64)
	return v
}

func scoreOf(ans map[string]any) (float64, float64, string) {
	score, _ := ans["score"].(float64)
	conf, _ := ans["confidence"].(float64)
	label := "medium"

	legend, _ := ans["legend"].(map[string]any)
	if legend != nil {
		key := fmt.Sprintf("%d", int(score+0.5))
		if s, ok := legend[key].(string); ok && s != "" {
			label = s
		}
	}

	switch {
	case score < 0.75:
		label = "low"
	case score < 1.5:
		label = "medium"
	default:
		label = "high"
	}

	return score, conf, label
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}

	return v
}
