package ocr

import "strings"

// EngineOptions selects the OCR backend. Default is OpenRouter vision.
type EngineOptions struct {
	Name       string
	OpenRouter OpenRouterOptions
	Ollama     OllamaOptions
	TessLang   string
}

// NewEngine picks OpenRouter (default), Ollama, or Tesseract.
func NewEngine(opts EngineOptions) Engine {
	switch strings.ToLower(strings.TrimSpace(opts.Name)) {
	case "tesseract":
		return NewTesseract(opts.TessLang)
	case "ollama", "glm-ocr":
		return NewOllama(opts.Ollama)
	default:
		return NewOpenRouter(opts.OpenRouter)
	}
}
