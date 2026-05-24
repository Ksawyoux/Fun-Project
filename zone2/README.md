# Zone 2 — Ingestion Subsystem (MVP)

Reaches into signal sources (Zone 1), normalizes records into NIF
(Normalized Ingestion Format), and delivers them to Zone 3 by default
(or to Zone 4 directly for debug/dev, or to a file for testing). Source isolation: nothing downstream knows about git,
filesystems, or AST nodes — only NIF.

## What's here

- **Connectors** — Pull only for now.
- **Ingestors** — Git (system `git` CLI), Go AST (`go/parser`).
- **Normalization** — NIF types + deterministic SHA-256 IDs + schema validator.
- **Orchestration** — In-process topological DAG runner with concurrent
  independent branches and partial-failure isolation.
- **Delivery** — `Zone3Sink` (POST `/v1/ingest`) by default, `Zone4Sink` (POST `/v1/mutations`) for direct debug/dev writes, or `FileSink` (JSONL).
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

Requires `zone3d` listening for the default path (see top-level `cmd/archgraph`).

```
go run ./cmd/zone2d \
  -zone3 http://localhost:8082 \
  -addr :8083 \
  -state ./zone2-state \
  -config sources.json

# Trigger a run
curl -X POST localhost:8083/v1/runs -d '{"trigger":"manual"}'
```

For direct debug/dev writes to Zone 4, pass `-zone4 http://localhost:8080`.

## Architecture

See `docs/superpowers/specs/2026-05-23-zone2-mvp-design.md`.
