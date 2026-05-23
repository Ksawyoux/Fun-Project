# Zone 5 MVP — Design

Date: 2026-05-23
Status: Implemented (initial)

## Scope

A single-process Go service implementing the Intelligence & Serving Layer
described in `Zone5.md`. Sits on top of Zone 4 and consumes its REST API.
Defers anything that needs infra Zone 4 doesn't yet have: snapshots,
runtime metrics store, vector embeddings, Redis cache, GraphQL/WebSockets,
real LLM call.

## Decisions

| # | Decision | Rationale |
|---|----------|-----------|
| 1 | Separate Go module `archgraph/zone5` | Mirrors zone4's layout; enforces the service boundary the spec implies. |
| 2 | HTTP client to Zone 4 (no shared library) | Zone 4's data types live under `internal/`. Going over HTTP avoids the temptation to leak schema and matches the production deployment shape. |
| 3 | Stub LLM by default, behind an `LLM` interface | Tests and offline usage need a deterministic path. A real Anthropic adapter is one file away. |
| 4 | Keyword intent classifier | No embeddings infra. The five archetypes in the spec are easy to discriminate with priority-ordered keyword rules. |
| 5 | Score-on-path retrieval, not full PageRank | The spec's formula Σ_path weight·confidence captures the right shape without eigenvector iteration. |
| 6 | One Neighborhood call per BFS step in Impact | Zone 4 has no batch-traverse endpoint. Acceptable for MVP; production pushes the BFS into Zone 4. |
| 7 | Tarjan's SCC for cycle detection | Spec names it. O(V+E), gives SCCs directly. |
| 8 | Evolution Tracker = delta-log replay (no snapshots) | We don't have a snapshot store. Replaying matches the spec's "delta log is truth" principle anyway. |
| 9 | Add `ListByNamespace` to Zone 4 | Health Auditor needs a whole-workspace view; without it, Tarjan can't run. |

## What's deferred

- Vector-embedding intent router.
- Federated runtime metrics joins (no metric store exists).
- Snapshot-based diffs (use delta-log replay for now).
- Redis edge cache + invalidation events.
- GraphQL, WebSockets.
- Multi-tenant beyond the `namespace` field.
- Mermaid syntax validator (we pass the LLM output through unchanged).

## Component map

```
zone5/
├── cmd/zone5d/main.go                 HTTP entry point
├── internal/
│   ├── zone4client/                   HTTP client + JSON DTOs
│   ├── intent/                        Keyword-based archetype classifier
│   ├── planner/                       Archetype → Plan (action + params)
│   ├── retriever/                     Subgraph fetch + score-based pruning
│   ├── assembler/                     Subgraph → Markdown context
│   ├── reasoner/                      LLM interface + StubLLM
│   ├── analytics/
│   │   ├── impact.go                  Blast radius BFS with decay
│   │   ├── health.go                  Tarjan + supernode + shared-DB detection
│   │   ├── tarjan.go                  SCC implementation
│   │   └── evolution.go               Delta-log replay diff
│   └── server/handlers.go             REST routes
└── docs/superpowers/specs/            this file
```

## Request flow — `/v1/ask`

```
POST /v1/ask {question, namespace, ...}
        │
        ▼
   planner.Make
        │
        ▼
  classification → action
        │
        ├─ IMPACT      → analytics.CalculateBlastRadius
        ├─ GOVERNANCE  → analytics.Audit
        ├─ TEMPORAL    → analytics.ComputeEvolution
        └─ STRUCTURAL/RUNTIME
                │
                ▼
        retriever.Retrieve
                │
                ▼
        assembler.Assemble
                │
                ▼
        reasoner.Answer  (stub or real LLM)
                │
                ▼
          JSON response
```

## HTTP API

| Method | Path | Body | Purpose |
|--------|------|------|---------|
| POST | `/v1/ask` | `{question, namespace, entity_name?, depth?}` | NL Q&A path; routes to engines or reasoner |
| POST | `/v1/blast-radius` | `{entity_id\|entity_name+namespace, max_depth?}` | Direct Impact Analyzer |
| POST | `/v1/health-audit` | `{namespace}` | Whole-namespace smell scan |
| POST | `/v1/diff` | `{namespace, from, to}` | Evolution via delta-log replay |
| GET | `/v1/entities/{id}` | — | Passthrough to Zone 4 (+ optional `?depth=N`) |
| GET | `/v1/health` | — | Liveness |

## Algorithms

### Retriever scoring
$$Score(E) = \max_{\text{paths } O \to E} \prod_{e \in path} w(\text{type}(e)) \cdot \text{conf}(e)$$

Implemented as iterative relaxation (≤ 6 passes, in practice ≤ depth).
Edges where the score on either endpoint would fall below `0.15` are dropped.
If the result set still exceeds 200 nodes, the retriever decrements the depth
and re-fetches (the spec's "Context Safety Guard").

### Blast Radius
Direct port of Zone5.md §3.2 pseudocode. BFS from origin, decay per edge type,
prune below probability 0.10. Sorted by impact descending.

### Tarjan SCC
Standard recursive implementation. Used by Health Auditor; nodes that
appear in an SCC of size ≥ 2 (or in a 1-node SCC with a self-loop) are
flagged CRITICAL.

### Supernode detection
Per spec §3.3.C: flag entities whose total degree exceeds `mean + 4σ`.
Requires ≥ 4 entities to be meaningful; small namespaces are skipped.

### Evolution
Replay delta log between `from` and `to`, tally by mutation type. Emit a
drift alert for each new `RUNTIME_CALLS` edge (placeholder rule — real
config-driven rule engine deferred).

## Tests

- `intent`: priority ordering of archetypes; governance beats impact when
  both keywords appear.
- `planner`: structural needs an entity; governance needs a namespace; impact
  bumps depth to 5.
- `analytics/tarjan`: 3-cycle detection; self-loops; DAGs.

End-to-end tests against a live `zone4d` are deferred until Go is installed
and we can stand the two daemons up together.

## Known gaps

1. **HTTP round-trip per BFS expansion** in blast radius is O(affected
   nodes). Fine for hundreds; doesn't scale to thousands.
2. **No retries / circuit breaker** between Zone 5 and Zone 4 — a single
   timeout fails the request.
3. **Evolution Tracker fetches up to 1000 log entries at once** with no
   pagination loop. Wider windows silently truncate.
4. **Citations in the stub answer are pass-through.** We don't currently
   validate that the LLM cited real IDs from the assembled context.
5. **No streaming.** The spec mentions WebSocket token streaming for `/ask`;
   MVP responds with one JSON blob.
