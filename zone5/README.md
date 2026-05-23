# Zone 5 — Intelligence & Serving Layer (MVP)

The reasoning brain that sits on top of Zone 4. Translates structural and
historical graph data into actionable architectural intelligence.

## What's here

- **Query Engine** — intent classifier, planner, subgraph retriever with
  weight-based pruning.
- **Reasoning Engine** — context assembler + LLM interface (stub by default;
  swap in Claude API later).
- **Analytical Engines** — blast radius, health audit (Tarjan SCC,
  supernodes), evolution tracker (delta-log replay).
- **REST Serving Layer** — `/v1/ask`, `/v1/blast-radius`,
  `/v1/health-audit`, `/v1/diff`.

## What's deferred

- Vector embeddings / semantic intent classification (keyword router for now).
- Federated runtime metrics (no metric store yet).
- Snapshot-based diffs (replay the delta log instead).
- GraphQL, WebSockets, Redis cache, multi-tenant isolation beyond namespace.
- Real LLM integration (stub returns deterministic structured answers).

## Run

Requires a Zone 4 daemon already running.

```
go run ./cmd/zone5d -zone4 http://localhost:8080 -addr :8081
curl http://localhost:8081/v1/ask -d '{"question":"What depends on payment-service?","namespace":"acme"}'
```

## Architecture

See `docs/superpowers/specs/2026-05-23-zone5-mvp-design.md`.
