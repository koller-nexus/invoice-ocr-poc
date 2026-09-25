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
	"strings"
	"time"

	"go.uber.org/zap"
)

const (
	defaultOllamaURL   = "http://127.0.0.1:11434"
	defaultOllamaModel = "glm-ocr:latest"
	ollamaOCRPrompt    = "Text Recognition:"
)

// Ollama reads text from an image through the native Ollama generate API.
type Ollama struct {
	baseURL    string
	model      string
	httpClient *http.Client
	log        *zap.SugaredLogger
}

// OllamaOptions configures the local Ollama OCR engine.
type OllamaOptions struct {
	BaseURL string
	Model   string
	Timeout time.Duration
	Log     *zap.SugaredLogger
}

// NewOllama builds an HTTP client for glm-ocr (or any Ollama vision model).
func NewOllama(opts OllamaOptions) *Ollama {
	if strings.TrimSpace(opts.BaseURL) == "" {
		opts.BaseURL = defaultOllamaURL
	}

	if strings.TrimSpace(opts.Model) == "" {
		opts.Model = defaultOllamaModel
	}

	if opts.Timeout <= 0 {
		opts.Timeout = 120 * time.Second
	}

	log := opts.Log
	if log == nil {
		log = zap.NewNop().Sugar()
	}

	return &Ollama{
		baseURL: strings.TrimRight(strings.TrimSpace(opts.BaseURL), "/"),
		model:   strings.TrimSpace(opts.Model),
		httpClient: &http.Client{
			Timeout: opts.Timeout,
		},
		log: log,
	}
}

// Available reports whether the Ollama base URL is set.
func (o *Ollama) Available() bool {
	return o != nil && o.baseURL != ""
}

// Recognize posts the image to POST /api/generate (GLM-OCR vision path).
func (o *Ollama) Recognize(ctx context.Context, imagePath string) (Result, error) {
	if !o.Available() {
		return Result{}, fmt.Errorf("ollama ocr url is not configured")
	}

	raw, err := os.ReadFile(imagePath)
	if err != nil {
		return Result{}, fmt.Errorf("read image: %w", err)
	}

	payload, err := json.Marshal(ollamaGenerateRequest{
		Model:  o.model,
		Prompt: ollamaOCRPrompt,
		Images: []string{base64.StdEncoding.EncodeToString(raw)},
		Stream: false,
	})
	if err != nil {
		return Result{}, fmt.Errorf("marshal ollama request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.baseURL+"/api/generate", bytes.NewReader(payload))
	if err != nil {
		return Result{}, fmt.Errorf("new ollama request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	url := o.baseURL + "/api/generate"
	o.log.Infow("ollama.request",
		"step", "ollama.request",
		"engine", "ollama",
		"url", url,
		"model", o.model,
		"prompt", ollamaOCRPrompt,
		"image_bytes", len(raw),
		"timeout", o.httpClient.Timeout.String(),
	)

	started := time.Now()
	resp, err := o.httpClient.Do(req)
	if err != nil {
		o.log.Errorw("ollama.response",
			"step", "ollama.response",
			"engine", "ollama",
			"url", url,
			"model", o.model,
			"duration_ms", time.Since(started).Milliseconds(),
			"err", err,
		)
		return Result{}, fmt.Errorf("ollama request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return Result{}, fmt.Errorf("read ollama body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		o.log.Errorw("ollama.response",
			"step", "ollama.response",
			"engine", "ollama",
			"url", url,
			"model", o.model,
			"status", resp.StatusCode,
			"duration_ms", time.Since(started).Milliseconds(),
			"err", "non-2xx",
		)
		return Result{}, fmt.Errorf("ollama status %d: %s", resp.StatusCode, bytes.TrimSpace(body))
	}

	var parsed ollamaGenerateResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return Result{}, fmt.Errorf("decode ollama response: %w", err)
	}

	text := strings.TrimSpace(parsed.Response)
	if text == "" {
		text = strings.TrimSpace(parsed.Message.Content)
	}

	text = trimOCRFences(text)
	if text == "" {
		return Result{}, fmt.Errorf("ollama ocr returned empty text")
	}

	o.log.Infow("ollama.response",
		"step", "ollama.response",
		"engine", "ollama",
		"url", url,
		"model", o.model,
		"status", resp.StatusCode,
		"duration_ms", time.Since(started).Milliseconds(),
		"text_chars", len(text),
	)

	return Result{Text: text, Confidence: 0}, nil
}

func trimOCRFences(text string) string {
	text = strings.TrimSpace(text)
	if after, ok := strings.CutPrefix(text, "```markdown"); ok {
		text = strings.TrimSpace(after)
	} else if after, ok := strings.CutPrefix(text, "```"); ok {
		text = strings.TrimSpace(after)
	}

	if i := strings.Index(text, "\n```"); i >= 0 {
		text = strings.TrimSpace(text[:i])
	}

	return strings.TrimSpace(text)
}

type ollamaGenerateRequest struct {
	Model  string   `json:"model"`
	Prompt string   `json:"prompt"`
	Images []string `json:"images"`
	Stream bool     `json:"stream"`
}

type ollamaGenerateResponse struct {
	Response string `json:"response"`
	Message  struct {
		Content string `json:"content"`
	} `json:"message"`
}
