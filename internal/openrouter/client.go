// Package openrouter calls DeepSeek V4 Flash through the OpenRouter chat API.
package openrouter

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
	"github.com/williamkoller/invoice-ocr-poc/internal/ocr"
	"go.uber.org/zap"
)

const (
	defaultModel   = "deepseek/deepseek-v4-flash"
	defaultBaseURL = "https://openrouter.ai/api/v1"
)

// Client talks to OpenRouter chat completions.
type Client struct {
	baseURL    string
	apiKey     string
	model      string
	httpClient *http.Client
	log        *zap.SugaredLogger
}

// NewClient builds an OpenRouter client. Empty apiKey disables Assist.
// Timeouts belong on the request context, not on the shared client.
func NewClient(baseURL, apiKey, model string, httpClient *http.Client) *Client {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = defaultBaseURL
	}

	if strings.TrimSpace(model) == "" {
		model = defaultModel
	}

	if httpClient == nil {
		httpClient = &http.Client{}
	}

	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		apiKey:     strings.TrimSpace(apiKey),
		model:      model,
		httpClient: httpClient,
		log:        zap.NewNop().Sugar(),
	}
}

// WithLogger sets the structured logger. A nil logger keeps the nop logger.
func (c *Client) WithLogger(log *zap.SugaredLogger) *Client {
	if c == nil {
		return c
	}

	if log != nil {
		c.log = log
	}

	return c
}

// Enabled reports whether the client can call OpenRouter.
func (c *Client) Enabled() bool {
	return c != nil && c.apiKey != ""
}

// Model is the OpenRouter model slug.
func (c *Client) Model() string {
	if c == nil {
		return ""
	}

	return c.model
}

// Assist asks DeepSeek to recover line items from OCR text. Code still owns totals.
func (c *Client) Assist(ctx context.Context, ocrText string, hint extract.Result) (extract.Result, string, ocr.Usage, error) {
	if !c.Enabled() {
		return hint, "", ocr.Usage{}, nil
	}

	payload, err := json.Marshal(chatRequest{
		Model: c.model,
		Messages: []chatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt(ocrText, hint)},
		},
		Temperature: 0.2,
		ResponseFormat: map[string]string{
			"type": "json_object",
		},
		Usage: ocr.UsageInclude{Include: true},
	})
	if err != nil {
		return hint, "", ocr.Usage{}, fmt.Errorf("marshal openrouter request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return hint, "", ocr.Usage{}, fmt.Errorf("new openrouter request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("HTTP-Referer", "https://github.com/williamkoller/invoice-ocr-poc")
	req.Header.Set("X-Title", "invoice-ocr-poc")

	c.log.Infow("openrouter.assist.request",
		"step", "openrouter.assist.request",
		"engine", "openrouter",
		"model", c.model,
		"ocr_chars", len(ocrText),
	)

	started := time.Now()
	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.log.Errorw("openrouter.assist.response",
			"step", "openrouter.assist.response",
			"engine", "openrouter",
			"model", c.model,
			"duration_ms", time.Since(started).Milliseconds(),
			"err", err,
		)
		return hint, "", ocr.Usage{DurationMs: time.Since(started).Milliseconds()}, fmt.Errorf("openrouter request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return hint, "", ocr.Usage{DurationMs: time.Since(started).Milliseconds()}, fmt.Errorf("read openrouter body: %w", err)
	}

	durationMs := time.Since(started).Milliseconds()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return hint, "", ocr.Usage{DurationMs: durationMs}, fmt.Errorf("openrouter status %d: %s", resp.StatusCode, bytes.TrimSpace(body))
	}

	var parsed chatResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return hint, "", ocr.Usage{DurationMs: durationMs}, fmt.Errorf("decode openrouter response: %w", err)
	}

	usage := parsed.Usage.ToUsage(durationMs)

	if len(parsed.Choices) == 0 {
		return hint, "", usage, fmt.Errorf("openrouter returned no choices")
	}

	c.log.Infow("openrouter.assist.response",
		"step", "openrouter.assist.response",
		"engine", "openrouter",
		"model", c.model,
		"status", resp.StatusCode,
		"duration_ms", durationMs,
		"total_tokens", usage.TotalTokens,
		"cost_usd", usage.CostUSD,
	)

	result, notes, err := parseAssist(parsed.Choices[0].Message.Content, hint)
	return result, notes, usage, err
}

type chatRequest struct {
	Model          string            `json:"model"`
	Messages       []chatMessage     `json:"messages"`
	Temperature    float64           `json:"temperature"`
	ResponseFormat map[string]string `json:"response_format"`
	Usage          ocr.UsageInclude  `json:"usage"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatChoice struct {
	Message chatMessage `json:"message"`
}

type chatResponse struct {
	Choices []chatChoice   `json:"choices"`
	Usage   ocr.TokenUsage `json:"usage"`
}

type assistJSON struct {
	Items          []assistItem `json:"items"`
	EstimatedTotal float64      `json:"total_estimado"`
	LegacyTotal    float64      `json:"estimated_total"`
	SomaItens      float64      `json:"soma_itens"`
	SomaConfere    bool         `json:"soma_confere"`
	Notes          string       `json:"notas"`
	LegacyNotes    string       `json:"notes"`
}

type assistItem struct {
	Produto     string  `json:"produto"`
	Description string  `json:"description"`
	Quantidade  float64 `json:"quantidade"`
	Quantity    float64 `json:"quantity"`
	ValorUnit   float64 `json:"valor_unitario"`
	UnitAmount  float64 `json:"unit_amount"`
	LineTotal   float64 `json:"line_total"`
}

const systemPrompt = `Você extrai campos de um cupom ou nota fiscal a partir de texto OCR já reconhecido.
Responda APENAS um JSON com:
{"items":[{"produto":"...","quantidade":1,"valor_unitario":0,"line_total":0}],"total_estimado":0,"soma_itens":0,"soma_confere":true,"notas":"..."}
Não invente produtos. Ignore CNPJ, IE, chave de acesso e forma de pagamento como itens.
soma_confere deve ser true só se a soma dos line_total for compatível com total_estimado.
notas em português sobre incertezas do OCR.
Não classifique tipo de documento, roteamento nem risco.`

func userPrompt(ocrText string, _ extract.Result) string {
	return "Texto OCR (fonte da verdade; extraia só o que estiver escrito):\n" + ocrText
}

func parseAssist(content string, fallback extract.Result) (extract.Result, string, error) {
	content = strings.TrimSpace(content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)

	var out assistJSON
	if err := json.Unmarshal([]byte(content), &out); err != nil {
		return fallback, "", fmt.Errorf("decode assist json: %w", err)
	}

	notes := strings.TrimSpace(out.Notes)
	if notes == "" {
		notes = strings.TrimSpace(out.LegacyNotes)
	}

	if out.SomaConfere {
		if notes != "" {
			notes += " "
		}

		notes += "Modelo: soma dos itens confere com o total estimado."
	}

	items := make([]extract.Item, 0, len(out.Items))
	computed := 0.0

	for _, raw := range out.Items {
		it := extract.Item{
			Description: firstNonEmpty(raw.Produto, raw.Description),
			Quantity:    firstNonZero(raw.Quantidade, raw.Quantity),
			UnitAmount:  firstNonZero(raw.ValorUnit, raw.UnitAmount),
			LineTotal:   raw.LineTotal,
		}
		if it.Quantity == 0 {
			it.Quantity = 1
		}

		if it.LineTotal == 0 {
			it.LineTotal = it.Quantity * it.UnitAmount
		}

		if it.Description == "" && it.LineTotal == 0 {
			continue
		}

		computed += it.LineTotal
		items = append(items, it)
	}

	if len(items) == 0 {
		return fallback, notes, nil
	}

	estimated := firstNonZero(out.EstimatedTotal, out.LegacyTotal)
	if estimated == 0 {
		estimated = firstNonZero(out.SomaItens, computed)
	}

	return extract.Result{
		Items:              items,
		EstimatedTotal:     estimated,
		ComputedItemsTotal: round2(computed),
	}, notes, nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}

	return ""
}

func firstNonZero(vals ...float64) float64 {
	for _, v := range vals {
		if v != 0 {
			return v
		}
	}

	return 0
}

func round2(v float64) float64 {
	return float64(int(v*100+0.5)) / 100
}
