# Agent Instructions

Go 1.27 OCR PoC. Prefer the local skills under `.agents/skills/` over generic advice. Code, comments, API errors, and commit messages are English. Do not change `VERSION` unless the user explicitly requests that version bump.

## Skills

| Need | Skill |
| --- | --- |
| Style, naming, docs | `golang-code-style`, `golang-naming`, `golang-documentation` |
| Context, concurrency, errors | `golang-context`, `golang-concurrency`, `golang-error-handling` |
| Tests, lint, security, CI | `golang-testing`, `golang-lint`, `golang-security`, `golang-continuous-integration` |
| Modernize, libraries, pkg.go.dev | `golang-modernize`, `golang-popular-libraries`, `golang-pkg-go-dev` |
| OCR / Tesseract | `ocr-document-processor` |
| TypeSafe app design | `typesafe-ai` |
| Jev question design | Jev skill — read it before every `judge` call |

## Go

Target the `go` directive in `go.mod` (currently 1.27). Prefer the standard library. Vet a new dependency with `godig` (`overview`, `vulns`, `imported-by`) before adding it.

### Layout and style

- `cmd/` entrypoints, `internal/` app logic, `pkg/` shared utilities, `api/` transport, `configs/` configuration, `test/` fixtures.
- Interfaces at the consumer. Inject dependencies through constructors. No global mutable state.
- MixedCaps identifiers. Packages are a single lowercase word. Errors: `ErrNotFound`, types `PathError`.
- `context.Context` is the first parameter, named `ctx`. Propagate the same context. Never store it on a struct. Call `cancel()` on every path.
- Functions stay short. More than four parameters becomes an options struct. Handle errors and edges first.
- Initialize slices and maps. Use named fields in composite literals. Break calls with four or more arguments one per line.

### Errors, concurrency, security

- Check every error. Wrap with `fmt.Errorf("context: %w", err)`. Lowercase error strings, no trailing punctuation.
- Log or return, never both. Use `errors.Is` / `errors.As` / `errors.AsType`. Prefer `log/slog`.
- Every goroutine has an owner, an exit, and `ctx.Done()` in `select`. Sender closes the channel. Default to unbuffered. Run `go test -race`.
- Parameterized queries, `exec.Command` with separate args, `os.Root` for user paths, `crypto/rand` for tokens. No secrets in source.

### Tests, lint, CI

- Table-driven tests with named `t.Run` cases. One `_test.go` per source file. Mock interfaces, not concretes.
- Integration tests use `//go:build integration`. Use `goleak` where goroutines run. `t.Context()` and `testing/synctest` for time-sensitive concurrency.
- Every project needs `.golangci.yml`. Run `golangci-lint run ./...`. `//nolint:linter // reason` only; never bare `//nolint` on security linters.
- CI must run `go test -race`, `golangci-lint`, and `govulncheck`. Never commit directly to `develop`; use a feature branch and a PR.

### Verification

```bash
gofmt -l .
go vet ./...
go test ./...
go test -race ./...
golangci-lint run ./...
govulncheck ./...
```

Prefix those commands with `rtk` when available (`rtk go test`, `rtk git status`).

## OCR / Tesseract

Use `.agents/skills/ocr-document-processor`. Core path: `scripts/ocr_processor.py`. Receipts: `scripts/receipt_scanner.py`. Cards: `scripts/business_card_scanner.py`. Requires Tesseract plus `pytesseract`; OpenCV for preprocess; PyMuPDF for PDFs.

1. Decide plain OCR, structured extraction, or a specialized parser.
2. Preprocess skewed, noisy, or shadowed inputs (`deskew`, `denoise`, `threshold`, `remove_shadows`).
3. Set an explicit language (`eng`, `por`, `eng+por`). Do not rely on the default when accuracy matters.
4. Honor `timeout` and `min_confidence` (default 60). Preserve page index, bbox, and language in structured output.
5. Return confidence caveats for low-quality, rotated, handwritten, or multilingual sources. Weak OCR is a hypothesis, not a fact.
6. Route born-digital (non-scanned) PDFs to a document converter, not OCR.

Do not claim extracted fields are exact when confidence is weak. Validate file type and optional-dependency presence before running. Batch work stays isolated per file.

## TypeSafe and Jev

Code owns the workflow. Jev supplies typed judgments (`choice`, `noul`, `score`) where ordinary code needs semantic understanding. Live docs at https://docs.typesafe.ai/llms.txt are the source of truth before any integration. Read `typesafe-ai` for application design; read the Jev skill before calling `judge`.

In this PoC, use Jev to:

- Route document type (receipt, card, generic scan, unsupported).
- Check whether an extracted field is actually supported by the OCR text.
- Score OCR quality and escalate uncertain cases instead of writing bad fields.

Question rules: one factor per question; fan out over the same `state`; include a no-match option on every `choice`; rank items with per-item `score`; compose the verdict in code. Do not send credentials, unnecessary PII, or session history. Confidence is not permission to act. A noul near 0.5 means Jev cannot tell.

`TYPESAFE_API_KEY` stays in the environment. Never commit it.
