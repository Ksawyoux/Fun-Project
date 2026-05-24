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

- `-root` — project root containing `zone2/`, `zone3/`, `zone4/`, and `zone5/` (default `.`)
- `-zone2-port` (default `8083`), `-zone3-port` (default `8082`), `-zone4-port` (default `8080`), `-zone5-port` (default `8081`)
- `-db` — SQLite path passed to zone4d (default `zone4.db`)
- `-zone3-db` — SQLite registry path passed to zone3d (default `zone3.db`)
- `-zone2-config` — source config passed to zone2d (empty = scan supervisor CWD)
- `-ready-timeout` — how long to wait for zones to become healthy
  (default `30s`)

## What it does

1. Starts `zone4d` via `go run ./cmd/zone4d` in the `zone4/` dir.
2. Polls `http://localhost:8080/v1/health` until 200.
3. Starts `zone3d`, pointed at the running Zone 4.
4. Starts `zone2d`, pointed at Zone 3 for the default ingestion path.
5. Starts `zone5d`, pointed at the running Zone 4.
6. Forwards daemon stdout/stderr with `[zone2]`, `[zone3]`, `[zone4]`, and `[zone5]` prefixes.
7. On Ctrl+C, sends SIGTERM to all children and waits up to 5s for
   graceful shutdown before killing.

If Zone 4 or Zone 3 dies unexpectedly the supervisor shuts the stack down too.
