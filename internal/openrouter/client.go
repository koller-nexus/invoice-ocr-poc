// Package openrouter calls DeepSeek V4 Flash through the OpenRouter chat API.
package openrouter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/williamkoller/tesseract-poc-go/internal/extract"
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
}

// NewClient builds an OpenRouter client. Empty apiKey disables Assist.
func NewClient(baseURL, apiKey, model string, timeout time.Duration) *Client {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = defaultBaseURL
	}

	if strings.TrimSpace(model) == "" {
		model = defaultModel
	}

	if timeout <= 0 {
		timeout = 45 * time.Second
	}

	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  strings.TrimSpace(apiKey),
		model:   model,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
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
func (c *Client) Assist(ctx context.Context, ocrText string, hint extract.Result) (extract.Result, string, error) {
	if !c.Enabled() {
		return hint, "", nil
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
	})
	if err != nil {
		return hint, "", fmt.Errorf("marshal openrouter request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return hint, "", fmt.Errorf("new openrouter request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("HTTP-Referer", "https://github.com/williamkoller/tesseract-poc-go")
	req.Header.Set("X-Title", "tesseract-poc-go")

	slog.Info("openrouter.assist.request",
		"step", "openrouter.assist.request",
		"engine", "openrouter",
		"model", c.model,
		"ocr_chars", len(ocrText),
	)

	started := time.Now()
	resp, err := c.httpClient.Do(req)
	if err != nil {
		slog.Error("openrouter.assist.response",
			"step", "openrouter.assist.response",
			"engine", "openrouter",
			"model", c.model,
			"duration_ms", time.Since(started).Milliseconds(),
			"err", err,
		)
		return hint, "", fmt.Errorf("openrouter request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return hint, "", fmt.Errorf("read openrouter body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return hint, "", fmt.Errorf("openrouter status %d: %s", resp.StatusCode, bytes.TrimSpace(body))
	}

	var parsed chatResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return hint, "", fmt.Errorf("decode openrouter response: %w", err)
	}

	if len(parsed.Choices) == 0 {
		return hint, "", fmt.Errorf("openrouter returned no choices")
	}

	slog.Info("openrouter.assist.response",
		"step", "openrouter.assist.response",
		"engine", "openrouter",
		"model", c.model,
		"status", resp.StatusCode,
		"duration_ms", time.Since(started).Milliseconds(),
	)

	return parseAssist(parsed.Choices[0].Message.Content, hint)
}

type chatRequest struct {
	Model          string            `json:"model"`
	Messages       []chatMessage     `json:"messages"`
	Temperature    float64           `json:"temperature"`
	ResponseFormat map[string]string `json:"response_format"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
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
