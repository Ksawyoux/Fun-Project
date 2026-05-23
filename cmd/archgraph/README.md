# archgraph supervisor

Brings up every implemented zone in dependency order with one command.

## Why a supervisor (not one fused binary)

Zone 4 and Zone 5 are independent Go modules — their `internal/` packages
can't be cross-imported. Running them as siblings under one parent matches
the production shape (each zone is its own service) and keeps the local dev
story to one command.

## Run

```
cd cmd/archgraph
go run . -root ../..
```

Flags:

- `-root` — project root containing `zone4/` and `zone5/` (default `.`)
- `-zone4-port` (default `8080`), `-zone5-port` (default `8081`)
- `-db` — SQLite path passed to zone4d (default `zone4.db`)
- `-ready-timeout` — how long to wait for zone4 to become healthy
  (default `30s`)

## What it does

1. Starts `zone4d` via `go run ./cmd/zone4d` in the `zone4/` dir.
2. Polls `http://localhost:8080/v1/health` until 200.
3. Starts `zone5d`, pointed at the running zone4.
4. Forwards both daemons' stdout/stderr with `[zone4]` / `[zone5]` prefixes.
5. On Ctrl+C, sends SIGTERM to both children and waits up to 5s for
   graceful shutdown before killing.

If zone4 dies unexpectedly the supervisor shuts zone5 down too.
