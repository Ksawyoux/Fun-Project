# archgraph supervisor

Brings up every implemented zone in dependency order with one command.

## Why a supervisor (not one fused binary)

Zones 2 through 5 are independent Go modules — their `internal/` packages
can't be cross-imported. Running them as siblings under one parent matches
the production shape (each zone is its own service) and keeps the local dev
story to one command.

## Run

```
cd cmd/archgraph
go run . -root ../..
```

Flags:

- `-root` — project root containing `ingestion/`, `pipeline/`, `storage/`, and `serving/` (default `.`)
- `-zone2-port` (default `8083`), `-zone3-port` (default `8082`), `-zone4-port` (default `8080`), `-zone5-port` (default `8081`)
- `-db` — SQLite path passed to storaged (default `storage.db`)
- `-pipeline-db` — SQLite registry path passed to pipelined (default `pipeline.db`)
- `-zone2-config` — source config passed to ingestiond (empty = scan supervisor CWD)
- `-ready-timeout` — how long to wait for zones to become healthy
  (default `30s`)

## What it does

1. Starts `storaged` via `go run ./cmd/storaged` in the `storage/` dir.
2. Polls `http://localhost:8080/v1/health` until 200.
3. Starts `pipelined`, pointed at the running Storage.
4. Starts `ingestiond`, pointed at Pipeline for the default ingestion path.
5. Starts `servingd`, pointed at the running Storage.
6. Forwards daemon stdout/stderr with `[zone2]`, `[zone3]`, `[zone4]`, and `[zone5]` prefixes.
7. On Ctrl+C, sends SIGTERM to all children and waits up to 5s for
   graceful shutdown before killing.

If Storage or Pipeline dies unexpectedly the supervisor shuts the stack down too.
