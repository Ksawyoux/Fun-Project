# Zone 4 MVP — Design

Date: 2026-05-23
Status: Implemented (initial)

## Scope

A single-process, SQLite-backed implementation of Zone 4 (Graph Storage) per
the architecture in `Zone4.md`. Honors the spec's central principle —
**delta log is truth, graph is projection** — using SQLite's cross-table
transactions for atomicity. Defers operational scale features (snapshots,
metrics store, partitioning, multi-layer cache, search index) and temporal
replay.

## Decisions

| # | Decision | Rationale |
|---|----------|-----------|
| 1 | Approach A: one SQLite file, log + graph tables side-by-side | Cross-table atomicity for free; no two-phase commit dance. |
| 2 | Pure Go (Go 1.22) + `modernc.org/sqlite` | Cross-compile-friendly, no CGO/clang dependency. |
| 3 | Single write entry point (`mutation.API`) | Spec-mandated. Every write validated, versioned, and logged. |
| 4 | Optimistic locking via integer `version` column | No global locks. Caller supplies expected version or accepts whatever's in the DB. |
| 5 | Soft delete only (no hard delete) | Spec principle: "history is not optional." |
| 6 | BFS neighborhood traversal in Go, not recursive SQL CTE | Easier to bound by depth and to scan full row data alongside. |
| 7 | Read by-name returns active rows only; by-ID returns soft-deleted too | Historical lookups still work; name-based lookup hides tombstones. |
| 8 | Deterministic IDs from `hash(type + canonical_name + namespace)` | Spec recipe minus `source_*` (no Zone 2 yet). Two reports of the same entity collide to the same ID — intended. |
| 9 | No-op upsert short-circuits without writing log entry | Prevents phantom updates polluting the audit trail. |

## What's deferred

- Snapshot store + point-in-time replay.
- Runtime metrics store + time-series writes.
- Search index (using SQL indexes on `(namespace, canonical_name)` for now).
- Multi-layer cache.
- Entity merge/split operations.
- Multi-tenant isolation beyond the `namespace` column.

## Component map

```
zone4/
├── cmd/zone4d/main.go              HTTP server entry point
├── internal/
│   ├── schema/                     Entity, Relationship, validation
│   ├── deltalog/                   Append-only log: Append(tx), Read
│   ├── graphdb/                    SQLite projection
│   │   ├── schema.sql              DDL applied on Open
│   │   ├── store.go                Insert/Update/SoftDelete (tx-scoped)
│   │   └── query.go                GetEntity, GetEntityByName, Neighborhood
│   ├── mutation/                   Single write entry point
│   │   ├── api.go                  ApplyBatch + per-kind handlers
│   │   └── conflict.go             tx-scoped reads + change-detection helpers
│   └── server/handlers.go          HTTP routes
└── docs/superpowers/specs/         this file
```

## Data flow (write path)

```
HTTP POST /v1/mutations
    │
    ▼
server.handleMutations
    │
    ▼
mutation.ApplyBatch
    │
    ├─ BEGIN sqlite tx
    │
    ├─ per mutation:
    │     validate → version check → graphdb insert/update → deltalog.Append
    │
    └─ COMMIT (or ROLLBACK on any error)
```

Both the graph row and the log entry write through the same `*sql.Tx`. SQLite
guarantees they commit atomically; if anything in the batch fails, both roll
back together.

## HTTP API

| Method | Path | Body / Params | Purpose |
|--------|------|---------------|---------|
| POST | `/v1/mutations` | `{mutations:[...]}` | Apply a batch |
| GET | `/v1/entities/{id}` | — | By-ID lookup (returns soft-deleted) |
| GET | `/v1/entities?namespace=&name=` | — | By-name lookup (active only) |
| GET | `/v1/entities/{id}/neighborhood?depth=&direction=` | depth 0–5; direction in\|out\|both | BFS traversal |
| GET | `/v1/log?entity_id=&relationship_id=&transaction_id=&from_entry_id=&limit=` | — | Read delta log |
| GET | `/v1/health` | — | Liveness |

## Error mapping

| Internal | HTTP |
|----------|------|
| `*schema.ValidationError` | 400 validation_error |
| `schema.ErrUnknownEntity` | 422 unknown_entity |
| `graphdb.ErrVersionConflict` | 409 version_conflict |
| `graphdb.ErrNotFound` | 404 not_found |
| other | 500 internal_error |

## Tests

- `schema`: validation rules, deterministic ID stability, self-loop rejection.
- `mutation`: end-to-end create / update / no-op / soft-delete / version-conflict /
  relationship endpoint check / batch-atomic entity+edge writes.

## Known gaps

1. **`SetMaxOpenConns(1)`** serializes all DB ops through one connection.
   Fine for MVP throughput; replaces a more nuanced reader-writer scheme.
2. **No retry on `SQLITE_BUSY`.** With a single connection it shouldn't
   happen, but a more permissive pool would need backoff.
3. **No properties-equality fast path.** `reflect.DeepEqual` on properties is
   fine for typical 5–20 keys, slow if maps get big.
4. **No tombstone propagation.** Soft-deleting an entity leaves its
   relationships untouched. Spec calls this out as a downstream cleanup
   problem; deferred.
