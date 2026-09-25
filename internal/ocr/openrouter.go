package ocr

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.uber.org/zap"
)

const (
	defaultOpenRouterBase = "https://openrouter.ai/api/v1"
	defaultOpenRouterOCR  = "google/gemini-2.5-flash"
)

// OpenRouter reads text from an image through a vision chat model.
type OpenRouter struct {
	baseURL    string
	apiKey     string
	model      string
	httpClient *http.Client
	log        *zap.SugaredLogger
}

// OpenRouterOptions configures the vision OCR client.
type OpenRouterOptions struct {
	BaseURL    string
	APIKey     string
	Model      string
	Timeout    time.Duration
	HTTPClient *http.Client
	Log        *zap.SugaredLogger
}

// NewOpenRouter builds an HTTP vision OCR engine.
func NewOpenRouter(opts OpenRouterOptions) *OpenRouter {
	if strings.TrimSpace(opts.BaseURL) == "" {
		opts.BaseURL = defaultOpenRouterBase
	}

	if strings.TrimSpace(opts.Model) == "" {
		opts.Model = defaultOpenRouterOCR
	}

	log := opts.Log
	if log == nil {
		log = zap.NewNop().Sugar()
	}

	httpClient := opts.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{}
	}

	return &OpenRouter{
		baseURL:    strings.TrimRight(strings.TrimSpace(opts.BaseURL), "/"),
		apiKey:     strings.TrimSpace(opts.APIKey),
		model:      strings.TrimSpace(opts.Model),
		httpClient: httpClient,
		log:        log,
	}
}

// Available reports whether the OpenRouter API key is set.
func (o *OpenRouter) Available() bool {
	return o != nil && o.apiKey != ""
}

// Recognize sends the image as a data URL and asks for raw OCR text only.
func (o *OpenRouter) Recognize(ctx context.Context, imagePath string) (Result, error) {
	if !o.Available() {
		return Result{}, fmt.Errorf("openrouter ocr api key is not configured")
	}

	raw, err := os.ReadFile(imagePath)
	if err != nil {
		return Result{}, fmt.Errorf("read image: %w", err)
	}

	mime := imageMIME(imagePath, raw)
	dataURL := "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(raw)

	payload, err := json.Marshal(orChatRequest{
		Model: o.model,
		Messages: []orChatMessage{
			{
				Role: "user",
				Content: []orContentPart{
					{
						Type: "text",
						Text: "Transcribe every readable character from this receipt or document image. Return plain text only, preserving line breaks. Do not invent missing values. Do not classify the document.",
					},
					{
						Type: "image_url",
						ImageURL: &orImageURL{
							URL: dataURL,
						},
					},
				},
			},
		},
		Temperature: 0.1,
		Usage:       UsageInclude{Include: true},
	})
	if err != nil {
		return Result{}, fmt.Errorf("marshal openrouter ocr request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.baseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return Result{}, fmt.Errorf("new openrouter ocr request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+o.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("HTTP-Referer", "https://github.com/williamkoller/invoice-ocr-poc")
	req.Header.Set("X-Title", "invoice-ocr-poc")

	o.log.Infow("openrouter.ocr.request",
		"step", "openrouter.ocr.request",
		"engine", "openrouter",
		"model", o.model,
		"image_bytes", len(raw),
	)

	started := time.Now()
	resp, err := o.httpClient.Do(req)
	if err != nil {
		o.log.Errorw("openrouter.ocr.response",
			"step", "openrouter.ocr.response",
			"engine", "openrouter",
			"model", o.model,
			"duration_ms", time.Since(started).Milliseconds(),
			"err", err,
		)
		return Result{}, fmt.Errorf("openrouter ocr request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return Result{}, fmt.Errorf("read openrouter ocr body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Result{}, fmt.Errorf("openrouter ocr status %d: %s", resp.StatusCode, bytes.TrimSpace(body))
	}

	var parsed orChatResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return Result{}, fmt.Errorf("decode openrouter ocr response: %w", err)
	}

	if len(parsed.Choices) == 0 {
		return Result{}, fmt.Errorf("openrouter ocr returned no choices")
	}

	text := strings.TrimSpace(parsed.Choices[0].Message.Content)
	if text == "" {
		return Result{}, fmt.Errorf("openrouter ocr returned empty text")
	}

	durationMs := time.Since(started).Milliseconds()
	usage := parsed.Usage.ToUsage(durationMs)

	o.log.Infow("openrouter.ocr.response",
		"step", "openrouter.ocr.response",
		"engine", "openrouter",
		"model", o.model,
		"status", resp.StatusCode,
		"duration_ms", durationMs,
		"total_tokens", usage.TotalTokens,
		"cost_usd", usage.CostUSD,
		"text_chars", len(text),
	)

	return Result{Text: text, Confidence: 0, Usage: usage}, nil
}

func imageMIME(path string, raw []byte) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".webp":
		return "image/webp"
	case ".gif":
		return "image/gif"
	case ".png":
		return "image/png"
	}

	detected := http.DetectContentType(raw)
	if strings.HasPrefix(detected, "image/") {
		return detected
	}

	return "image/png"
}

type orChatRequest struct {
	Model       string          `json:"model"`
	Messages    []orChatMessage `json:"messages"`
	Temperature float64         `json:"temperature"`
	Usage       UsageInclude    `json:"usage"`
}

type orChatMessage struct {
	Role    string          `json:"role"`
	Content []orContentPart `json:"content"`
}

type orContentPart struct {
	Type     string      `json:"type"`
	Text     string      `json:"text,omitempty"`
	ImageURL *orImageURL `json:"image_url,omitempty"`
}

type orImageURL struct {
	URL string `json:"url"`
}

type orChatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Usage TokenUsage `json:"usage"`
}
