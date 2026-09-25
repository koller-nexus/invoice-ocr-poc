# invoice-ocr-poc

Monorepo: `backend/` (Go HTTP API) and `frontend/` (placeholder).

Proof of concept HTTP API: upload a receipt image, enqueue processing, then poll SQLite-backed results.

OCR is one of **Ollama** (`glm-ocr:latest`), **OpenRouter** vision, or local **Tesseract**. DeepSeek (OpenRouter) optionally extracts line items. TypeSafe Jev judges document type, routing, and risk.

## Architecture

Upload returns as soon as the file is on disk and the invoice is queued. A worker pool runs preprocess, OCR, optional DeepSeek assist, and Jev. Results stay in SQLite. Logs go to stdout (`zap`).

```mermaid
flowchart LR
  client[HTTP client] --> gin[Gin API]
  gin -->|"POST /api/v1/image/processor"| svc[invoice.Service]
  svc --> disk[Upload files]
  svc --> db[(SQLite)]
  svc --> pool[worker.Pool]
  pool --> job[ProcessJob]
  job --> prep[preprocess]
  prep --> ocr[OCR engine]
  ocr --> ollama[Ollama]
  ocr --> orVision[OpenRouter vision]
  ocr --> tess[Tesseract]
  ocr --> parse[extract.Parse]
  parse --> assist[DeepSeek assist]
  assist --> jev[TypeSafe Jev]
  jev --> db
```

`OCR_ENGINE` selects one OCR backend (`ollama`, `openrouter`, or `tesseract`). Assist is skipped when `OPENROUTER_API_KEY` is empty.

## Requirements

- Go 1.27.1
- Ollama with `glm-ocr:latest` (`ollama pull glm-ocr:latest`) when `OCR_ENGINE=ollama`
- `OPENROUTER_API_KEY` — optional structured extract (and OCR if `OCR_ENGINE=openrouter`)
- `TYPESAFE_API_KEY` — Jev
- Tesseract on `PATH` when `OCR_ENGINE=tesseract`
- Optional: `go build -tags gocv` for OpenCV preprocess

## Run

```bash
cd backend
cp configs/.env.example configs/.env
# set OPENROUTER_API_KEY and TYPESAFE_API_KEY
go run ./cmd/api
```

Config is read from `.env` or `configs/.env` (relative to the process working directory).

`OCR_ENGINE=ollama` (example), `openrouter`, or `tesseract`.

`LOG_FORMAT=text` (default, console) or `json`.

## API

- `GET /health`
- `POST /api/v1/image/processor`
- `GET /api/v1/invoices/:id`
- `GET /api/v1/invoices/:id/analysis`
- `GET /api/v1/invoices`

```bash
curl -s -X POST http://localhost:8080/api/v1/image/processor \
  -F "image=@./path/to/receipt.png;type=image/png"
```

Flow: preprocess → OCR (Ollama, OpenRouter, or Tesseract) → extract / optional DeepSeek → Jev HTTP.

## Accuracy harness

```bash
cd backend
go test -tags evaluation ./test/
```
