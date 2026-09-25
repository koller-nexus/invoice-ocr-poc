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
	BaseURL    string
	Model      string
	Timeout    time.Duration
	HTTPClient *http.Client
	Log        *zap.SugaredLogger
}

// NewOllama builds an HTTP client for glm-ocr (or any Ollama vision model).
func NewOllama(opts OllamaOptions) *Ollama {
	if strings.TrimSpace(opts.BaseURL) == "" {
		opts.BaseURL = defaultOllamaURL
	}

	if strings.TrimSpace(opts.Model) == "" {
		opts.Model = defaultOllamaModel
	}

	log := opts.Log
	if log == nil {
		log = zap.NewNop().Sugar()
	}

	httpClient := opts.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{}
	}

	return &Ollama{
		baseURL:    strings.TrimRight(strings.TrimSpace(opts.BaseURL), "/"),
		model:      strings.TrimSpace(opts.Model),
		httpClient: httpClient,
		log:        log,
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
		"timeout", "context",
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

	durationMs := nsToMs(parsed.TotalDuration)
	if durationMs <= 0 {
		durationMs = time.Since(started).Milliseconds()
	}

	usage := Usage{
		DurationMs:           durationMs,
		LoadDurationMs:       nsToMs(parsed.LoadDuration),
		PromptEvalCount:      parsed.PromptEvalCount,
		PromptEvalDurationMs: nsToMs(parsed.PromptEvalDuration),
		EvalCount:            parsed.EvalCount,
		EvalDurationMs:       nsToMs(parsed.EvalDuration),
	}

	o.log.Infow("ollama.response",
		"step", "ollama.response",
		"engine", "ollama",
		"url", url,
		"model", o.model,
		"status", resp.StatusCode,
		"duration_ms", usage.DurationMs,
		"load_duration_ms", usage.LoadDurationMs,
		"eval_count", usage.EvalCount,
		"eval_duration_ms", usage.EvalDurationMs,
		"prompt_eval_count", usage.PromptEvalCount,
		"prompt_eval_duration_ms", usage.PromptEvalDurationMs,
		"text_chars", len(text),
	)

	return Result{
		Text:       text,
		Confidence: 0,
		Usage:      usage,
	}, nil
}

// ModelInfo is a live snapshot of the configured Ollama model.
type ModelInfo struct {
	Reachable     bool
	Loaded        bool
	ParameterSize string
	Quantization  string
	ContextLength int
	SizeBytes     int64
	VRAMBytes     int64
}

// Inspect asks Ollama whether the daemon is up and how the model is loaded.
func (o *Ollama) Inspect(ctx context.Context) (ModelInfo, error) {
	if !o.Available() {
		return ModelInfo{}, fmt.Errorf("ollama ocr url is not configured")
	}

	info := ModelInfo{}
	show, err := o.showModel(ctx)
	if err != nil {
		return info, err
	}

	info.Reachable = true
	info.ParameterSize = show.Details.ParameterSize
	info.Quantization = show.Details.QuantizationLevel
	info.ContextLength = contextLength(show.ModelInfo)

	ps, err := o.runningModels(ctx)
	if err != nil {
		return info, nil
	}

	for _, m := range ps.Models {
		if !sameModel(m.Name, o.model) && !sameModel(m.Model, o.model) {
			continue
		}

		info.Loaded = true
		info.SizeBytes = m.Size
		info.VRAMBytes = m.SizeVRAM
		if m.ContextLength > 0 {
			info.ContextLength = m.ContextLength
		}
		if m.Details.ParameterSize != "" {
			info.ParameterSize = m.Details.ParameterSize
		}
		if m.Details.QuantizationLevel != "" {
			info.Quantization = m.Details.QuantizationLevel
		}
	}

	return info, nil
}

func (o *Ollama) showModel(ctx context.Context) (ollamaShowResponse, error) {
	payload, err := json.Marshal(map[string]string{"model": o.model})
	if err != nil {
		return ollamaShowResponse{}, fmt.Errorf("marshal ollama show: %w", err)
	}

	var parsed ollamaShowResponse
	if err := o.postJSON(ctx, "/api/show", payload, &parsed); err != nil {
		return ollamaShowResponse{}, err
	}

	return parsed, nil
}

func (o *Ollama) runningModels(ctx context.Context) (ollamaPSResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, o.baseURL+"/api/ps", nil)
	if err != nil {
		return ollamaPSResponse{}, fmt.Errorf("new ollama ps request: %w", err)
	}

	resp, err := o.httpClient.Do(req)
	if err != nil {
		return ollamaPSResponse{}, fmt.Errorf("ollama ps: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return ollamaPSResponse{}, fmt.Errorf("read ollama ps: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ollamaPSResponse{}, fmt.Errorf("ollama ps status %d", resp.StatusCode)
	}

	var parsed ollamaPSResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return ollamaPSResponse{}, fmt.Errorf("decode ollama ps: %w", err)
	}

	return parsed, nil
}

func (o *Ollama) postJSON(ctx context.Context, path string, payload []byte, dest any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.baseURL+path, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("new ollama request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := o.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("ollama request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return fmt.Errorf("read ollama body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("ollama status %d: %s", resp.StatusCode, bytes.TrimSpace(body))
	}

	if err := json.Unmarshal(body, dest); err != nil {
		return fmt.Errorf("decode ollama response: %w", err)
	}

	return nil
}

func contextLength(info map[string]any) int {
	for key, raw := range info {
		if !strings.HasSuffix(key, "context_length") {
			continue
		}

		switch n := raw.(type) {
		case float64:
			return int(n)
		case int:
			return n
		}
	}

	return 0
}

func sameModel(a, b string) bool {
	a = strings.TrimSpace(strings.ToLower(a))
	b = strings.TrimSpace(strings.ToLower(b))
	if a == b {
		return true
	}

	return strings.TrimSuffix(a, ":latest") == strings.TrimSuffix(b, ":latest")
}

func nsToMs(ns int64) int64 {
	if ns <= 0 {
		return 0
	}

	return ns / 1_000_000
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
	Response           string `json:"response"`
	TotalDuration      int64  `json:"total_duration"`
	LoadDuration       int64  `json:"load_duration"`
	EvalCount          int    `json:"eval_count"`
	EvalDuration       int64  `json:"eval_duration"`
	PromptEvalCount    int    `json:"prompt_eval_count"`
	PromptEvalDuration int64  `json:"prompt_eval_duration"`
	Message            struct {
		Content string `json:"content"`
	} `json:"message"`
}

type ollamaModelDetails struct {
	ParameterSize     string `json:"parameter_size"`
	QuantizationLevel string `json:"quantization_level"`
}

type ollamaShowResponse struct {
	Details   ollamaModelDetails `json:"details"`
	ModelInfo map[string]any     `json:"model_info"`
}

type ollamaPSModel struct {
	Name          string             `json:"name"`
	Model         string             `json:"model"`
	Size          int64              `json:"size"`
	SizeVRAM      int64              `json:"size_vram"`
	ContextLength int                `json:"context_length"`
	Details       ollamaModelDetails `json:"details"`
}

type ollamaPSResponse struct {
	Models []ollamaPSModel `json:"models"`
}
