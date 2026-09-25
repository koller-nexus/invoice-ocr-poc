# Performance

Wall-clock for one receipt dropped from about **1 minute** to about **15 seconds** with the initial optimizations. With the migration to **PostgreSQL + Redis**, processing time dropped further from **11s to 9s** (~18% improvement).

## Database and Cache Backends

The system supports multiple database and cache backends for different deployment scenarios.

### Performance Comparison

| Stack | First-seen Receipt | Cache Hit |
|-------|-------------------|-----------|
| SQLite + Memory | ~11s | ~100ms |
| **PostgreSQL + Redis** | **~9s** | ~100ms |

The 2-second improvement comes from:
- PostgreSQL parallel writes (no single-writer lock)
- Connection pooling with prepared statements
- Redis persistence eliminates cold-start penalty after restarts

### Database Backends

| Backend | Use Case | Write Latency | Concurrency |
|---------|----------|---------------|-------------|
| **SQLite** (default) | Development, single instance | ~1-5ms | Single writer |
| **PostgreSQL** | Production, multiple instances | ~2-10ms | Parallel writes |

PostgreSQL is configured with write-optimized settings:
- `synchronous_commit=off` — Fast writes with slight durability risk on crash
- `SkipDefaultTransaction` — No transaction wrapper for single writes
- `PrepareStmt` — Cached prepared statements
- Connection pooling with configurable limits

### Cache Backends

| Backend | Use Case | Persistence | Sharing |
|---------|----------|-------------|---------|
| **Memory** (default) | Development, single instance | ❌ Lost on restart | ❌ Per-process |
| **Redis** | Production, multiple instances | ✅ Survives restart | ✅ Shared across instances |

Cache hit rate determines how often expensive API calls (OpenRouter, Jev) are skipped. A Redis cache persists across restarts and can be shared between multiple API instances.

### Configuration

```bash
# Database: "sqlite" or "postgres"
DB_BACKEND=postgres
PG_HOST=localhost
PG_PORT=5432
PG_SYNC_COMMIT=off  # Fast writes

# Cache: "memory" or "redis"
CACHE_BACKEND=redis
REDIS_ADDR=localhost:6379
OCR_CACHE_TTL=1h
```

### Local Development

```bash
# Start PostgreSQL and Redis
docker-compose up -d postgres redis

# Run with both backends
DB_BACKEND=postgres CACHE_BACKEND=redis go run ./cmd/api
```

## What was slow

The hot path used to be sequential and always paid the expensive hops:

1. Resize and re-encode the upload.
2. Tesseract.
3. Ollama `glm-ocr:latest` even when Tesseract had already found line items. On CPU that hop alone is often 30–60 seconds.
4. OpenRouter vision, DeepSeek assist, and Jev, one after another.

A single receipt therefore stacked local VLM time on top of the API calls.

## What changed

### Skip Ollama when Tesseract already has items

`recognizeCascade` still escalates when Tesseract fails, the text is too short, or the parse has no items. If Tesseract returns at least one line item, the job logs `ocr.skip_ollama` (`reason=has_items`) and keeps that text.

OpenRouter vision still runs later, and only when the extract is still weak. The local VLM is reserved for scans Tesseract cannot read. That is the change that removes most of the minute.

### Reuse the client image

`preprocess.Passthrough` returns the uploaded file. The client already compresses. The backend no longer decodes, resizes, or re-encodes before OCR, and every engine reads the same `ImgPath`.

### Cache identical OCR text

After Tesseract, the service hashes the OCR text (`OCR_CACHE_ENABLED`, default on, `OCR_CACHE_TTL` default `1h`). A hit skips Ollama, OpenRouter, Assist, and Jev and applies the stored parse and judgment. A repeat of the same text is Tesseract plus a map lookup, on the order of 100 ms. Different receipts do not hit.

### Keep local work off the API pool

`WORKER_COUNT_LOCAL` runs Tesseract and Ollama. `WORKER_COUNT_API` runs OpenRouter, Assist, and Jev. Buffered channels between stages let one invoice sit on the network while another is still in OCR. That raises throughput. It does not shorten a single Ollama call.

### Shorter network setup

Ollama, OpenRouter, and Jev share one `http.Client` with keep-alive (`MaxIdleConns=32`, `MaxIdleConnsPerHost=8`). Each hop has its own `context.WithTimeout` (`OCR_TIMEOUT`, `ASSIST_TIMEOUT`, `JEV_TIMEOUT`) so a stuck call fails instead of holding the job until `JOB_TIMEOUT`.

With PostgreSQL + Redis, the database layer no longer serializes behind a single writer, and connection pooling keeps latency low.

## Where the 9 seconds go

On a first-seen receipt that Tesseract can parse (PostgreSQL + Redis stack):

| Hop | Runs | Typical Time |
| --- | --- | --- |
| Passthrough | yes, no image work | <1ms |
| Tesseract | yes | ~500ms |
| Ollama `glm-ocr` | no, when items exist | skipped |
| OpenRouter vision | only if the extract is still weak | ~2-3s |
| Assist | only if items are still incomplete | ~1-2s |
| Jev | yes | ~3-4s |
| PostgreSQL write | yes | ~2-5ms |

The remaining time is Tesseract plus the API hops that still have to run. A second upload of the same OCR text should drop to the cache path (~100ms).

Timings are on the `job.done` log: `prep_ms`, `ocr_ms`, `assist_ms`, `jev_ms`, `db_ms`, `cache_hit`.
