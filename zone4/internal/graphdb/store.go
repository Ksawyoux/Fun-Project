// Package graphdb owns the SQLite-backed projection of the delta log.
//
// It exposes:
//   - Open / Close — connection lifecycle and migration.
//   - GetEntity / GetEntityByName — by-ID and by-name lookups.
//   - Neighborhood — N-hop traversal in either direction.
//   - Lower-level Insert/UpdateEntity (and relationship equivalents) that the
//     mutation package calls inside a transaction.
//
// All write helpers take a *sql.Tx so the mutation package can keep the graph
// write and the delta-log append in the same SQLite transaction.
package graphdb

import (
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"archgraph/zone4/internal/schema"

	_ "github.com/mattn/go-sqlite3"
)

//go:embed schema.sql
var schemaDDL string

// Store wraps the SQLite connection. It is safe for concurrent use because
// *sql.DB is goroutine-safe; individual transactions are not, but the mutation
// package serializes those.
type Store struct {
	db *sql.DB
}

// Open opens (or creates) the SQLite database at the given path and applies
// the schema migration. Pass ":memory:" for ephemeral in-process storage.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// SQLite handles concurrent reads fine but a single writer is the safe
	// default. The mutation API serializes writes anyway.
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schemaDDL); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	return &Store{db: db}, nil
}

// DB exposes the underlying *sql.DB so the mutation package can BEGIN a
// transaction that spans graph + delta-log writes.
func (s *Store) DB() *sql.DB { return s.db }

func (s *Store) Close() error { return s.db.Close() }

// ErrNotFound is returned by GetEntity/GetEntityByName/GetRelationship when
// the requested row doesn't exist.
var ErrNotFound = errors.New("not found")

// ErrVersionConflict is returned when an optimistic-lock check fails.
// The caller (mutation package) catches this and surfaces it as a 409.
var ErrVersionConflict = errors.New("version conflict")

// --- Entity CRUD ---------------------------------------------------------

// InsertEntity inserts a new entity row. Caller must have validated `e`
// and assigned an ID. Version is set to 1; created_at = updated_at = now.
func InsertEntity(tx *sql.Tx, e *schema.Entity) error {
	props, err := json.Marshal(coalesceProps(e.Properties))
	if err != nil {
		return fmt.Errorf("marshal properties: %w", err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if e.Version == 0 {
		e.Version = 1
	}
	if e.LifecycleStage == "" {
		e.LifecycleStage = schema.LifecycleActive
	}
	_, err = tx.Exec(`
		INSERT INTO entities
		    (id, type, sub_type, canonical_name, namespace, confidence,
		     is_active, lifecycle_stage, valid_from, valid_to, version,
		     properties, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		e.ID, string(e.Type), e.SubType, e.CanonicalName, e.Namespace,
		e.Confidence, boolToInt(e.IsActive), string(e.LifecycleStage),
		e.ValidFrom.UTC().Format(time.RFC3339Nano), nullTime(e.ValidTo),
		e.Version, string(props), now, now,
	)
	if err != nil {
		return fmt.Errorf("insert entity: %w", err)
	}
	return nil
}

// UpdateEntity overwrites an existing entity row using optimistic locking
// against the supplied previous version. Returns ErrVersionConflict if the
// row's current version doesn't match `prevVersion`.
func UpdateEntity(tx *sql.Tx, e *schema.Entity, prevVersion int) error {
	props, err := json.Marshal(coalesceProps(e.Properties))
	if err != nil {
		return fmt.Errorf("marshal properties: %w", err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := tx.Exec(`
		UPDATE entities
		   SET type = ?, sub_type = ?, canonical_name = ?, namespace = ?,
		       confidence = ?, is_active = ?, lifecycle_stage = ?,
		       valid_from = ?, valid_to = ?, version = ?, properties = ?,
		       updated_at = ?
		 WHERE id = ? AND version = ?
	`,
		string(e.Type), e.SubType, e.CanonicalName, e.Namespace,
		e.Confidence, boolToInt(e.IsActive), string(e.LifecycleStage),
		e.ValidFrom.UTC().Format(time.RFC3339Nano), nullTime(e.ValidTo),
		e.Version, string(props), now,
		e.ID, prevVersion,
	)
	if err != nil {
		return fmt.Errorf("update entity: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return ErrVersionConflict
	}
	return nil
}

// SoftDeleteEntity marks an entity inactive, sets its lifecycle to DELETED,
// and stamps valid_to. Relationships referencing the entity are left in place
// so historical queries continue to work; they're implicitly stale.
func SoftDeleteEntity(tx *sql.Tx, id string, prevVersion int) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := tx.Exec(`
		UPDATE entities
		   SET is_active = 0,
		       lifecycle_stage = ?,
		       valid_to = ?,
		       version = version + 1,
		       updated_at = ?
		 WHERE id = ? AND version = ?
	`, string(schema.LifecycleDeleted), now, now, id, prevVersion)
	if err != nil {
		return fmt.Errorf("soft delete entity: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrVersionConflict
	}
	return nil
}

// EntityExists is used by the conflict detector to verify relationship
// endpoints before insertion.
func EntityExists(tx *sql.Tx, id string) (bool, error) {
	var one int
	err := tx.QueryRow(`SELECT 1 FROM entities WHERE id = ?`, id).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// --- Relationship CRUD ---------------------------------------------------

func InsertRelationship(tx *sql.Tx, r *schema.Relationship) error {
	props, err := json.Marshal(coalesceProps(r.Properties))
	if err != nil {
		return fmt.Errorf("marshal properties: %w", err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if r.Version == 0 {
		r.Version = 1
	}
	_, err = tx.Exec(`
		INSERT INTO relationships
		    (id, type, from_id, to_id, confidence, is_active,
		     valid_from, valid_to, version, properties, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		r.ID, string(r.Type), r.FromID, r.ToID, r.Confidence,
		boolToInt(r.IsActive), r.ValidFrom.UTC().Format(time.RFC3339Nano),
		nullTime(r.ValidTo), r.Version, string(props), now, now,
	)
	if err != nil {
		return fmt.Errorf("insert relationship: %w", err)
	}
	return nil
}

func UpdateRelationship(tx *sql.Tx, r *schema.Relationship, prevVersion int) error {
	props, err := json.Marshal(coalesceProps(r.Properties))
	if err != nil {
		return fmt.Errorf("marshal properties: %w", err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := tx.Exec(`
		UPDATE relationships
		   SET type = ?, from_id = ?, to_id = ?, confidence = ?,
		       is_active = ?, valid_from = ?, valid_to = ?, version = ?,
		       properties = ?, updated_at = ?
		 WHERE id = ? AND version = ?
	`,
		string(r.Type), r.FromID, r.ToID, r.Confidence,
		boolToInt(r.IsActive), r.ValidFrom.UTC().Format(time.RFC3339Nano),
		nullTime(r.ValidTo), r.Version, string(props), now,
		r.ID, prevVersion,
	)
	if err != nil {
		return fmt.Errorf("update relationship: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrVersionConflict
	}
	return nil
}

func SoftDeleteRelationship(tx *sql.Tx, id string, prevVersion int) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := tx.Exec(`
		UPDATE relationships
		   SET is_active = 0, valid_to = ?, version = version + 1, updated_at = ?
		 WHERE id = ? AND version = ?
	`, now, now, id, prevVersion)
	if err != nil {
		return fmt.Errorf("soft delete relationship: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrVersionConflict
	}
	return nil
}

// --- Helpers -------------------------------------------------------------

func coalesceProps(p map[string]any) map[string]any {
	if p == nil {
		return map[string]any{}
	}
	return p
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func nullTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UTC().Format(time.RFC3339Nano)
}
