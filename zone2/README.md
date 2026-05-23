# Zone 2 — Ingestion Subsystem (MVP)

Reaches into signal sources (Zone 1), normalizes records into NIF
(Normalized Ingestion Format), and delivers them to Zone 4 (or to a file
for testing). Source isolation: nothing downstream knows about git,
filesystems, or AST nodes — only NIF.

## What's here

- **Connectors** — Pull only for now.
- **Ingestors** — Git (system `git` CLI), Go AST (`go/parser`).
- **Normalization** — NIF types + deterministic SHA-256 IDs + schema validator.
- **Orchestration** — In-process topological DAG runner with concurrent
  independent branches and partial-failure isolation.
- **Delivery** — `Zone4Sink` (POST /v1/mutations) or `FileSink` (JSONL).
- **Observability** — Append-only ledger, JSONL dead-letter queue,
  on-demand staleness lookup.
- **REST API** — `/v1/runs`, `/v1/ledger`, `/v1/staleness`, `/v1/health`.

## What's deferred

- Push connector (webhook listener) — needs publicly reachable endpoint.
- Stream connector (OTLP) — needs OTel collector.
- Multi-language AST (Python, TS, …).
- Backpressure controller, Kafka event bus.
- Background staleness monitor + health dashboard UI.
- Author/Person entities (requires Zone 4 type extension).

## Run

Requires `zone4d` listening (see top-level `cmd/archgraph`).

```
go run ./cmd/zone2d \
  -zone4 http://localhost:8080 \
  -addr :8082 \
  -data ./data \
  -config sources.json

# Trigger a run
curl -X POST localhost:8082/v1/runs -d '{"source_id":"local-repo"}'
```

## Architecture

See `docs/superpowers/specs/2026-05-23-zone2-mvp-design.md`.
