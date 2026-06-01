-- Zone 4 MVP schema.
-- One SQLite file holds three logical stores:
--   entities        — current graph nodes
--   relationships   — current graph edges
--   delta_log       — append-only history (source of truth)
--
-- All mutations write to entities/relationships AND delta_log in the same
-- SQLite transaction. SQLite gives us cross-table atomicity for free.

PRAGMA journal_mode = WAL;
PRAGMA foreign_keys = ON;
PRAGMA synchronous  = NORMAL;

CREATE TABLE IF NOT EXISTS entities (
    id              TEXT    PRIMARY KEY,
    type            TEXT    NOT NULL,
    sub_type        TEXT    NOT NULL DEFAULT '',
    canonical_name  TEXT    NOT NULL,
    namespace       TEXT    NOT NULL,
    confidence      REAL    NOT NULL,
    is_active       INTEGER NOT NULL DEFAULT 1,
    lifecycle_stage TEXT    NOT NULL DEFAULT 'ACTIVE',
    valid_from      TEXT    NOT NULL,
    valid_to        TEXT,
    version         INTEGER NOT NULL DEFAULT 1,
    properties      TEXT    NOT NULL DEFAULT '{}',  -- JSON
    created_at      TEXT    NOT NULL,
    updated_at      TEXT    NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_entities_ns_name ON entities(namespace, canonical_name);
CREATE INDEX IF NOT EXISTS idx_entities_type    ON entities(namespace, type);
CREATE INDEX IF NOT EXISTS idx_entities_active  ON entities(is_active);

CREATE TABLE IF NOT EXISTS relationships (
    id         TEXT    PRIMARY KEY,
    type       TEXT    NOT NULL,
    from_id    TEXT    NOT NULL REFERENCES entities(id),
    to_id      TEXT    NOT NULL REFERENCES entities(id),
    confidence REAL    NOT NULL,
    is_active  INTEGER NOT NULL DEFAULT 1,
    valid_from TEXT    NOT NULL,
    valid_to   TEXT,
    version    INTEGER NOT NULL DEFAULT 1,
    properties TEXT    NOT NULL DEFAULT '{}',
    created_at TEXT    NOT NULL,
    updated_at TEXT    NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_rel_from ON relationships(from_id, type, is_active);
CREATE INDEX IF NOT EXISTS idx_rel_to   ON relationships(to_id,   type, is_active);

CREATE TABLE IF NOT EXISTS delta_log (
    entry_id        INTEGER PRIMARY KEY AUTOINCREMENT,  -- monotonic
    transaction_id  TEXT    NOT NULL,
    mutation_type   TEXT    NOT NULL,
    entity_id       TEXT,
    relationship_id TEXT,
    before_state    TEXT,
    after_state     TEXT,
    occurred_at     TEXT    NOT NULL,
    recorded_at     TEXT    NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_log_txn      ON delta_log(transaction_id);
CREATE INDEX IF NOT EXISTS idx_log_entity   ON delta_log(entity_id);
CREATE INDEX IF NOT EXISTS idx_log_rel      ON delta_log(relationship_id);
CREATE INDEX IF NOT EXISTS idx_log_occurred ON delta_log(occurred_at);

-- Search Index table (Standard SQL fallback for compatibility)
CREATE TABLE IF NOT EXISTS entity_search (
    entity_id            TEXT PRIMARY KEY,
    canonical_name       TEXT NOT NULL,
    aliases              TEXT NOT NULL,
    entity_type          TEXT NOT NULL,
    sub_type             TEXT NOT NULL,
    namespace            TEXT NOT NULL,
    owner_team           TEXT NOT NULL,
    criticality          TEXT NOT NULL,
    maturity             TEXT NOT NULL,
    velocity             TEXT NOT NULL,
    is_active            INTEGER NOT NULL,
    architectural_smells TEXT NOT NULL,
    tags                 TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_search_ns   ON entity_search(namespace);
CREATE INDEX IF NOT EXISTS idx_search_type ON entity_search(entity_type);
CREATE INDEX IF NOT EXISTS idx_search_name ON entity_search(canonical_name);


-- Snapshots table
CREATE TABLE IF NOT EXISTS snapshots (
    snapshot_id        TEXT PRIMARY KEY,
    snapshot_at        TEXT NOT NULL,
    created_at         TEXT NOT NULL,
    last_log_entry_id  INTEGER NOT NULL,
    statistics         TEXT NOT NULL, -- JSON
    graph_data         BLOB NOT NULL, -- Compressed Gzip JSON/GOB
    checksum           TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_snapshots_at ON snapshots(snapshot_at);

-- Runtime metrics store tables
CREATE TABLE IF NOT EXISTS entity_metrics (
    entity_id           TEXT NOT NULL,
    timestamp           TEXT NOT NULL, -- RFC3339
    request_rate        REAL NOT NULL DEFAULT 0.0,
    error_rate          REAL NOT NULL DEFAULT 0.0,
    p50_latency_ms      REAL NOT NULL DEFAULT 0.0,
    p95_latency_ms      REAL NOT NULL DEFAULT 0.0,
    p99_latency_ms      REAL NOT NULL DEFAULT 0.0,
    cpu_utilization     REAL NOT NULL DEFAULT 0.0,
    memory_utilization  REAL NOT NULL DEFAULT 0.0,
    PRIMARY KEY (entity_id, timestamp)
);

CREATE TABLE IF NOT EXISTS relationship_metrics (
    relationship_id     TEXT NOT NULL,
    timestamp           TEXT NOT NULL, -- RFC3339
    call_rate           REAL NOT NULL DEFAULT 0.0,
    error_rate          REAL NOT NULL DEFAULT 0.0,
    p99_latency_ms      REAL NOT NULL DEFAULT 0.0,
    data_volume_bytes   INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (relationship_id, timestamp)
);

CREATE INDEX IF NOT EXISTS idx_entity_metrics_ts ON entity_metrics(entity_id, timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_rel_metrics_ts    ON relationship_metrics(relationship_id, timestamp DESC);

