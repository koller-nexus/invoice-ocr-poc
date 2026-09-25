// Package httpserver exposes the Gin API.
package httpserver

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/williamkoller/invoice-ocr-poc/internal/invoice"
	"github.com/williamkoller/invoice-ocr-poc/internal/jev"
	"github.com/williamkoller/invoice-ocr-poc/internal/ocr"
	"github.com/williamkoller/invoice-ocr-poc/internal/store"
)

// Deps are handler dependencies.
type Deps struct {
	Service            *invoice.Service
	Store              *store.Store
	OCR                ocr.Engine
	Ollama             *ocr.Ollama
	HasAPIKey          bool
	HasOpenRouterKey   bool
	MaxBodyBytes       int64
	OCRName            string
	OllamaModel        string
	OpenRouterModel    string
	OpenRouterOCRModel string
}

// NewRouter builds the Gin engine.
func NewRouter(d Deps) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)

	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery(), cors())
	r.MaxMultipartMemory = d.MaxBodyBytes

	r.GET("/health", d.health)
	api := r.Group("/api/v1")
	api.GET("/runtime", d.runtime)
	api.POST("/image/processor", d.processImage)
	api.GET("/invoices/:id/analysis", d.getAnalysis)
	api.GET("/invoices/:id", d.getInvoice)
	api.GET("/invoices", d.listInvoices)

	return r
}

func (d Deps) health(c *gin.Context) {
	checks := map[string]string{
		"sqlite":     "ok",
		"ocr":        "ok",
		"typesafe":   "ok",
		"openrouter": "ok",
	}
	status := "ok"

	if err := d.Store.Ping(c.Request.Context()); err != nil {
		checks["sqlite"] = "error"
		status = "degraded"
	}

	if d.OCR == nil || !d.OCR.Available() {
		checks["ocr"] = "missing"
		status = "degraded"
	} else if d.OCRName != "" {
		checks["ocr"] = d.OCRName
	}

	if !d.HasAPIKey {
		checks["typesafe"] = "missing_api_key"
		status = "degraded"
	}

	if !d.HasOpenRouterKey {
		checks["openrouter"] = "missing_api_key"
		status = "degraded"
	}

	c.JSON(http.StatusOK, healthResponse{Status: status, Checks: checks})
}

func (d Deps) runtime(c *gin.Context) {
	engine := strings.ToLower(strings.TrimSpace(d.OCRName))
	if engine == "" {
		engine = "openrouter"
	}

	ollamaConfigured := (engine == "ollama" || engine == "glm-ocr") && d.OCR != nil && d.OCR.Available()
	ollama := runtimeOllama{
		Model:      d.OllamaModel,
		Configured: ollamaConfigured,
	}
	if info, ok := d.inspectOllama(c.Request.Context()); ok {
		ollama.Reachable = info.Reachable
		ollama.Loaded = info.Loaded
		ollama.ParameterSize = info.ParameterSize
		ollama.Quantization = info.Quantization
		ollama.ContextLength = info.ContextLength
		ollama.SizeBytes = info.SizeBytes
		ollama.VRAMBytes = info.VRAMBytes
	}

	c.JSON(http.StatusOK, runtimeResponse{
		OCREngine:      engine,
		MaxUploadBytes: d.MaxBodyBytes,
		Ollama:         ollama,
		OpenRouter: runtimeOpenRouter{
			Model:      d.OpenRouterModel,
			OCRModel:   d.OpenRouterOCRModel,
			Configured: d.HasOpenRouterKey,
		},
	})
}

func (d Deps) inspectOllama(ctx context.Context) (ocr.ModelInfo, bool) {
	if d.Ollama == nil {
		return ocr.ModelInfo{}, false
	}

	probeCtx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()

	info, err := d.Ollama.Inspect(probeCtx)
	if err != nil {
		return ocr.ModelInfo{}, true
	}

	return info, true
}

func (d Deps) processImage(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, d.MaxBodyBytes+1024)

	fh, err := c.FormFile("image")
	if err != nil {
		writeError(c, http.StatusBadRequest, "image form field is required")
		return
	}

	src, err := fh.Open()
	if err != nil {
		writeError(c, http.StatusBadRequest, "unable to read uploaded image")
		return
	}
	defer src.Close()

	mimeType := fh.Header.Get("Content-Type")
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}

	inv, err := d.Service.Enqueue(c.Request.Context(), invoice.EnqueueInput{
		OriginalName: fh.Filename,
		MIMEType:     mimeType,
		Body:         src,
	})
	if err != nil {
		if errors.Is(err, invoice.ErrInvalidImage) {
			writeError(c, http.StatusBadRequest, err.Error())
			return
		}

		if errors.Is(err, invoice.ErrQueueFull) {
			writeError(c, http.StatusServiceUnavailable, err.Error())
			return
		}

		writeError(c, http.StatusInternalServerError, "failed to enqueue image")

		return
	}

	c.JSON(http.StatusAccepted, enqueueResponse{
		ID:      inv.ID,
		Status:  inv.Status,
		PollURL: "/api/v1/invoices/" + inv.ID,
	})
}

func (d Deps) getInvoice(c *gin.Context) {
	inv, err := d.Service.Get(c.Request.Context(), c.Param("id"))
	if err != nil {
		if errors.Is(err, invoice.ErrNotFound) {
			writeError(c, http.StatusNotFound, "invoice not found")
			return
		}

		writeError(c, http.StatusInternalServerError, "failed to load invoice")

		return
	}

	c.JSON(http.StatusOK, toInvoiceResponse(inv))
}

func (d Deps) getAnalysis(c *gin.Context) {
	inv, err := d.Service.Get(c.Request.Context(), c.Param("id"))
	if err != nil {
		if errors.Is(err, invoice.ErrNotFound) {
			writeError(c, http.StatusNotFound, "invoice not found")
			return
		}

		writeError(c, http.StatusInternalServerError, "failed to load invoice")

		return
	}

	if inv.Status != store.StatusDone {
		msg := "invoice analysis is not ready"
		if inv.Status == store.StatusFailed && inv.ErrorMessage != "" {
			msg = inv.ErrorMessage
		}

		writeError(c, http.StatusConflict, msg)

		return
	}

	c.JSON(http.StatusOK, jev.Explain(*inv))
}

func (d Deps) listInvoices(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))

	rows, err := d.Service.List(c.Request.Context(), limit, offset)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "failed to list invoices")
		return
	}

	out := make([]invoiceResponse, 0, len(rows))
	for i := range rows {
		out = append(out, toInvoiceResponse(&rows[i]))
	}

	c.JSON(http.StatusOK, gin.H{"items": out})
}

func writeError(c *gin.Context, code int, msg string) {
	c.JSON(code, errorBody{Error: msg})
}
