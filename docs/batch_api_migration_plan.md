# Gemini Batch API Migration: Draft Plan

**Status:** draft (not implemented) · **Date:** 2026-10-18 · **Constraint (hard):** free models only, paid never.

## Decisions (locked)

- Runtime: `cmd/server` + Wails app dono.
- Grouping: 10 logs per batch request (token amortization).
- Images: include (512px WebP q40 thumbnails, low media resolution).
- Activation: auto-probe + auto-enable when free batch becomes available.
- Free-only invariant: batch path only reaches free-model allowlist (`gemma-*`, free flash-lite); paid model names must be unreachable from any batch code path.

## Current State (baseline)

- Sync path: `internal/ai/gemini.go:298` `AnalyzeBatch` (no request-level retry/failover), default model `gemini-3.5-flash-lite`.
- Server cron: `cmd/server/main.go:57-68`; batch-of-4 loop `processPendingLogs` at `cmd/server/main.go:1704`.
- Wails: one-shot auto-process `app.go:156-166`, `ProcessPendingLogs` `app.go:652` (sequential single-item).
- Queue state: `logs.ai_category` + `ai_retry_count` (`internal/db/db.go:1257`, `internal/db/db.go:1300`); no job tables exist.
- Config: `internal/config/config.go` (`ai_analysis_interval_seconds`, default 3h).

## Key Batch API Facts

- Endpoints: `models/{m}:batchGenerateContent`, `GET /batches/{id}`, `:cancel`, Files API upload/download (resumable).
- JSONL line: `{"key":"log-<id>","request":{GenerateContentRequest}}`; output lines in same order, match by `key`.
- States: `PENDING → RUNNING → SUCCEEDED | FAILED | CANCELLED | EXPIRED`; `batchStats` per-row counts.
- **Paid-only today** (pricing page lists Batch API as a paid tier feature). Free key = request rejected; zero charge possible without a billing account.
- Limits: 100 concurrent jobs, input file 2GB, inline 20MB, results retained 6 weeks, input file + job expire after 48h.
- Create is **not idempotent**; `SUCCEEDED` can contain all-failed rows; `tools` + JSON mode rejected per-row.

## Architecture

Local SQLite = durable source of truth. Remote batch job keeps running server-side even when the app is closed/offline. Worker resumes non-terminal jobs on startup.

### New files

| File | Purpose |
|---|---|
| `internal/ai/batch.go` | REST client: upload/create/get/list/download/cancel; tolerant parser (Operation or direct Batch shape) |
| `internal/ai/batch_prompt.go` | Compact JSONL builder, short keys, sanitize, thumbnails, free-model allowlist + probe |
| `internal/ai/batch_worker.go` | State machine, submit/poll/resume loops, probe (startup + every 24h) |
| `internal/db/batch.go` | Job/item queries + `GetUnanalyzedLogsExcludingBatch()` |
| `internal/ai/batch_test.go`, `internal/ai/batch_prompt_test.go`, `internal/ai/batch_worker_test.go`, `internal/db/batch_test.go` | mock HTTP, golden JSONL, resume, allowlist |

### Edited files

- `internal/db/db.go`: migrate: `ai_batch_jobs`, `ai_batch_items` (SQLite + Postgres).
- `internal/config/config.go` + `.env.example` + `config.yaml.example`: new options.
- `cmd/server/main.go`: worker startup; interactive cron exclusion-aware.
- `app.go`: same wiring (GUI and background daemon both run startup).

## DB Schema

```
ai_batch_jobs(id PK, org_id, model, state, remote_name, input_file_name, output_file_name,
  local_jsonl_path, display_name, request_count, success_count, fail_count, retries,
  next_poll_at, last_error, created_at, updated_at)
  state: draft|submitted|polling|done|failed|expired|cancelled
ai_batch_items(job_id, log_id, item_key, status[pending|success|error], error, PK(job_id,log_id))
```

- Reconcile strategy: `display_name = "get-hike-<jobID>"`; on startup call `GET /batches` to adopt or decide resubmission.
- `GetUnanalyzedLogsExcludingBatch()` prevents race between interactive path and batch items (NOT EXISTS on pending items).

## Worker Flow

1. **Probe** (startup + every 24h): ListModels → free allowlist → 1-item inline batch probe → success: persist `batch_free_model=<name>` (meta) → activate; failure: stay inactive, interactive fallback continues.
2. **Submit** (`auto` mode + probe success): pending ≥ `min_items` (25) → build compact JSONL → save local copy `dataDir/batch/<job>.jsonl` → Files API upload → `:batchGenerateContent` → persist `submitted`.
3. **Poll** (`next_poll_at`, backoff 30s→5m): state check; `SUCCEEDED` → download output → key-match → `UpdateLogAnalysis` + `RecordAPIUsage`; `FAILED`/`CANCELLED`/`EXPIRED` → rows `ai_retry_count++` (cap 10), job terminal; after `EXPIRED`, still-unanalyzed logs are rebuilt into a new job from the local JSONL.
4. **Resume:** on startup list non-terminal jobs from DB and rejoin the poll loop (network/app crash loses nothing).
5. One active job per (key, model); on create timeout reconcile before retry (avoid duplicate cost).

## Token-Lean Prompt

- 10 items/line; input/item: `{i, t(title ≤100 chars, URL/email stripped), e, c, a, ts}`; output/item: `{i, cat, p, conf, r ≤15 words}` via `responseSchema`, `maxOutputTokens ~64/item`, temperature 0.1.
- Image: 512px WebP q40 thumbnail; text+image parts interleaved (`ITEM n: {...}` + image).
- PII never sent. Estimate ~300 tokens per 10 logs (vs current ~600+ per log).

## Config

`ai_batch_mode: off|auto|on` (default `auto`) · `ai_batch_items_per_request=10` · `ai_batch_poll_seconds=45` · `ai_batch_min_items=25` · `GEMINI_BATCH_MODEL` (empty = auto allowlist pick).

## Fallback Chain (paid never)

probing → free-batch active? → batch … otherwise interactive `gemini-3.5-flash-lite` (existing). Probe failure = zero charge (free keys have no billing account).

## Tests

- Mock `httptest` server: upload → create → get → download happy path.
- JSONL golden + sanitization + 10-item grouping.
- Resume-after-restart (temp SQLite, job alive → results applied).
- EXPIRED → rebuild path.
- Allowlist: paid model name rejected; probe failure leaves interactive path untouched.

## Phases

1. DB tables + queries
2. Batch client
3. Prompt builder
4. Worker + probe
5. Config
6. Server wiring
7. Wails wiring
8. Tests
9. `go build ./... && go test ./internal/... && go vet ./...`
10. (optional) `/api/batch/status` + UI badge

## Risks

- Batch is currently paid-only → code stays dormant; probe auto-enables once free availability appears.
- Gemma batch support is uncertain → runtime capability check + interactive fallback.
- Grouped-line partial failure affects up to 10 logs → per-row error map + retry lane.
- Thumbnail generation CPU cost on Termux/Android → generated only at submit time, cached per job.
