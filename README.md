# invoice-ocr-poc

Proof of concept HTTP API: upload a receipt image, enqueue processing, then poll results from **SQLite (default)** or **PostgreSQL**.

OCR is a **confidence cascade**: Tesseract → local Ollama (`glm-ocr:latest`) → OpenRouter vision when still weak. DeepSeek (OpenRouter) optionally extracts line items when the parse is incomplete. TypeSafe Jev judges document type, routing, and risk.

OCR results can be cached by **text hash** in **memory** (default) or **Redis**.

## Architecture

![Architecture](image/architecture.png)

Upload returns as soon as the file is on disk and the invoice is queued. The client compresses the image; the backend **passthrough** reuses that file.

Pipeline stages:

1. **Prep** — mark processing, reuse uploaded bytes
2. **Local pool** (`WORKER_COUNT_LOCAL`) — Tesseract, then Ollama on low confidence / incomplete extract
3. **API pool** (`WORKER_COUNT_API`) — OpenRouter vision if still weak, optional DeepSeek assist, TypeSafe Jev

Persistence and cache are pluggable:

| Concern | Default | Alternative |
|--------|---------|-------------|
| Database (`DB_BACKEND`) | `sqlite` (WAL, single writer) | `postgres` |
| OCR cache (`CACHE_BACKEND`) | `memory` | `redis` |

`docker compose up -d` starts PostgreSQL 16 and Redis 7 (optional pgAdmin / Redis Commander under the `debug` profile).

```mermaid
flowchart LR
  client[HTTP client] --> gin[Gin API]
  gin -->|"POST /api/v1/image/processor"| svc[invoice.Service]
  svc --> disk[Upload files]
  svc --> db[(SQLite or PostgreSQL)]
  svc --> cache[(Memory or Redis OCR cache)]
  svc --> pipe[worker.Pipeline]
  pipe --> local[local pool]
  local --> tess[Tesseract]
  tess -->|low confidence| ollama[Ollama]
  tess -->|high confidence| api[api pool]
  ollama --> api
  api -->|still weak| orVision[OpenRouter vision]
  api --> parse[extract.Parse]
  parse -->|incomplete| assist[DeepSeek assist]
  parse --> jev[TypeSafe Jev]
  jev --> db
  cache -.->|hit skips API hops| jev
```

Sharp images stop on Tesseract. Assist is skipped when `OPENROUTER_API_KEY` is empty or the extract is already complete. A cache hit skips Ollama / OpenRouter / Assist / Jev after Tesseract (~100ms path).

## Requirements

- Go 1.27.1
- Tesseract (`por+eng`) for the first cascade hop
- Ollama with `glm-ocr:latest` (`ollama pull glm-ocr:latest`) for the local fallback
- `OPENROUTER_API_KEY` — optional structured extract and Gemini OCR fallback
- `TYPESAFE_API_KEY` — Jev
- Optional: Docker for PostgreSQL + Redis (`docker compose up -d`)

## Run

```bash
cp configs/.env.example configs/.env
# set OPENROUTER_API_KEY and TYPESAFE_API_KEY
# optional: DB_BACKEND=postgres and CACHE_BACKEND=redis after compose up
go run ./cmd/api
```

Config is read from `.env` or `configs/.env`.

### Database

- `DB_BACKEND=sqlite` (default) — `SQLITE_PATH` (default `data/poc.db`)
- `DB_BACKEND=postgres` — `PG_HOST`, `PG_PORT`, `PG_USER`, `PG_PASSWORD`, `PG_DBNAME`, `PG_SSLMODE`, pool sizes, `PG_SYNC_COMMIT`

### Cache

- `OCR_CACHE_ENABLED=true` (default), TTL `OCR_CACHE_TTL` (default `1h`)
- `CACHE_BACKEND=memory` (default) or `redis` with `REDIS_ADDR`, `REDIS_PASSWORD`, `REDIS_DB`, `REDIS_POOL_SIZE`

### Workers

`WORKER_COUNT_LOCAL` (alias `OCR_WORKERS`) is the Tesseract/Ollama pool. `WORKER_COUNT_API` (alias `JEV_WORKERS`) is OpenRouter/Assist/Jev. `JOB_QUEUE_SIZE` is the in-memory job buffer (default 32). `POST /api/v1/image/processor` returns 503 when the buffer is full.

`LOG_FORMAT=text` (default) or `json`.

Set `DEBUG_PPROF=true` to expose `net/http/pprof` on `DEBUG_PPROF_ADDR` (default `127.0.0.1:6060`). It is not mounted on the public API port.

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

Flow: passthrough upload → Tesseract → Ollama if weak → OpenRouter OCR if still weak → optional DeepSeek → Jev HTTP. Persist to SQLite or PostgreSQL; cache hits short-circuit via memory or Redis.

## OCR Cache

When `OCR_CACHE_ENABLED=true` (default), results are cached by OCR text hash. A cache hit skips Ollama, OpenRouter, Assist, and Jev entirely — only Tesseract runs (~100ms). TTL is `OCR_CACHE_TTL` (default `1h`). Backend is `CACHE_BACKEND` (`memory` or `redis`).

## Profiling with pprof

Set `DEBUG_PPROF=true` to expose `net/http/pprof` on `DEBUG_PPROF_ADDR` (default `127.0.0.1:6060`).

### CPU profile (30 seconds)

```bash
curl -o cpu.prof http://127.0.0.1:6060/debug/pprof/profile?seconds=30
go tool pprof -http=:8081 cpu.prof
```

### Heap

```bash
curl -o heap.prof http://127.0.0.1:6060/debug/pprof/heap
go tool pprof -http=:8081 heap.prof
```

### Goroutines

```bash
curl http://127.0.0.1:6060/debug/pprof/goroutine?debug=2
```

### Execution trace (5 seconds)

```bash
curl -o trace.out http://127.0.0.1:6060/debug/pprof/trace?seconds=5
go tool trace trace.out
```

### Flame graph with pprof web UI

```bash
go tool pprof -http=:8081 http://127.0.0.1:6060/debug/pprof/profile?seconds=30
```

## Accuracy harness

```bash
go test -tags evaluation ./test/
```
