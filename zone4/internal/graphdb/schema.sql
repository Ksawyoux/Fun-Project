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
