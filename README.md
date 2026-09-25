# tesseract-poc-go

Proof of concept HTTP API: upload a receipt, optionally preprocess, then call **only HTTP APIs**.

Default OCR in `.env.example` is **Ollama** (`glm-ocr:latest` via `POST /api/generate`). OpenRouter vision remains available.

Then DeepSeek (OpenRouter) extracts line items and TypeSafe Jev judges type, routing, and risk.

## Requirements

- Go 1.27.1
- Ollama with `glm-ocr:latest` (`ollama pull glm-ocr:latest`)
- `OPENROUTER_API_KEY` — optional structured extract (and OCR if `OCR_ENGINE=openrouter`)
- `TYPESAFE_API_KEY` — Jev
- Optional: `go build -tags gocv` for OpenCV preprocess

## Run

```bash
cp configs/.env.example .env
# set OPENROUTER_API_KEY and TYPESAFE_API_KEY
go run ./cmd/api
```

`OCR_ENGINE=ollama` (example), `openrouter`, or `tesseract`.

`LOG_FORMAT=text` (default, one line per event) or `json`.

## API

- `GET /health`
- `POST /api/v1/image/processor`
- `GET /api/v1/invoices/:id`
- `GET /api/v1/invoices/:id/analysis`
- `GET /api/v1/invoices`

```bash
curl -s -X POST http://localhost:8080/api/v1/image/processor \
  -F "image=@./receipt.png;type=image/png"
```

Flow: preprocess → Ollama GLM-OCR (or OpenRouter) → DeepSeek JSON → Jev HTTP.

## Accuracy harness

```bash
go test -tags evaluation ./test/
```
