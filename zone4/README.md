# Zone 4 — Graph Storage (MVP)

A single-process, SQLite-backed MVP of the Zone 4 graph storage layer described
in `../Zone4.md`. It implements the spec's central design principle — **the
delta log is the source of truth, the graph is a projection** — while
deferring scale features (snapshots, separate metrics store, multi-layer cache,
partitioning).

## What's in scope

- **Mutation API** — single write entry point with schema enforcement,
  optimistic locking, and atomic graph + delta-log writes.
- **Delta Log** — append-only, monotonic, queryable.
- **Graph DB** — `entities` + `relationships` tables in SQLite.
- **Read path** — get by ID, get by name within namespace, N-hop neighborhood.
- **HTTP server** exposing the above.

## What's deferred

- Snapshot Store, Runtime Metrics Store, Search Index, multi-layer Cache.
- Point-in-time / temporal replay queries.
- Partitioning, multi-tenant isolation beyond `namespace` columns.
- Entity merge/split operations.

## Build & run

```sh
cd zone4
go mod tidy
go build ./...
go test ./...
./zone4d -db zone4.db -addr :8080
```

## Project layout

```
cmd/zone4d/         binary entry point
internal/schema/    NIF types, validation
internal/deltalog/  append-only log
internal/graphdb/   SQLite projection (nodes, edges, queries)
internal/mutation/  single write entry point
internal/server/    HTTP routes
```

## HTTP API

| Method | Path | Purpose |
|--------|------|---------|
| `POST` | `/v1/mutations` | Apply a mutation batch |
| `GET`  | `/v1/entities/{id}` | Get entity by canonical ID |
| `GET`  | `/v1/entities?namespace=X&name=Y` | Get entity by name |
| `GET`  | `/v1/entities/{id}/neighborhood?depth=N` | N-hop neighborhood |
| `GET`  | `/v1/log?from_entry_id=N&limit=M` | Read delta log |

See `docs/superpowers/specs/` for the full design.
