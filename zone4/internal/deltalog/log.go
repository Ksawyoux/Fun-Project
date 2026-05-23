// Package deltalog owns the append-only history that is the system's source
// of truth. The graph DB is a projection of this log; if the graph is lost it
// can be rebuilt by replaying entries in entry_id order.
//
// Append takes a *sql.Tx so callers can keep the graph write and the log
// append in the same SQLite transaction. Read is read-only and runs on the
// *sql.DB directly.
package deltalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type MutationType string

const (
	MutEntityCreated       MutationType = "ENTITY_CREATED"
	MutEntityUpdated       MutationType = "ENTITY_UPDATED"
	MutEntitySoftDeleted   MutationType = "ENTITY_SOFT_DELETED"
	MutEntityRestored      MutationType = "ENTITY_RESTORED"
	MutRelationshipCreated MutationType = "RELATIONSHIP_CREATED"
	MutRelationshipUpdated MutationType = "RELATIONSHIP_UPDATED"
	MutRelationshipDeleted MutationType = "RELATIONSHIP_DELETED"
)

// Entry mirrors the spec's Delta Log Entry. Before/After states are stored as
// raw JSON so we can serialize whatever struct the mutation captured.
type Entry struct {
	EntryID        int64           `json:"entry_id"`
	TransactionID  string          `json:"transaction_id"`
	MutationType   MutationType    `json:"mutation_type"`
	EntityID       string          `json:"entity_id,omitempty"`
	RelationshipID string          `json:"relationship_id,omitempty"`
	BeforeState    json.RawMessage `json:"before_state,omitempty"`
	AfterState     json.RawMessage `json:"after_state,omitempty"`
	OccurredAt     time.Time       `json:"occurred_at"`
	RecordedAt     time.Time       `json:"recorded_at"`
}

// Append writes a log entry inside the caller's transaction. EntryID is
// populated on success. Setting OccurredAt is optional; if zero it defaults
// to now (RecordedAt is always now).
func Append(tx *sql.Tx, e *Entry) (int64, error) {
	now := time.Now().UTC()
	if e.OccurredAt.IsZero() {
		e.OccurredAt = now
	}
	e.RecordedAt = now

	res, err := tx.Exec(`
        INSERT INTO delta_log
            (transaction_id, mutation_type, entity_id, relationship_id,
             before_state, after_state, occurred_at, recorded_at)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?)
    `,
		e.TransactionID,
		string(e.MutationType),
		nullStr(e.EntityID),
		nullStr(e.RelationshipID),
		nullRaw(e.BeforeState),
		nullRaw(e.AfterState),
		e.OccurredAt.UTC().Format(time.RFC3339Nano),
		e.RecordedAt.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return 0, fmt.Errorf("append log entry: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("log entry id: %w", err)
	}
	e.EntryID = id
	return id, nil
}

// ReadOpts filters log reads. Zero values mean "no filter on that field".
type ReadOpts struct {
	FromEntryID    int64
	EntityID       string
	RelationshipID string
	TransactionID  string
	Limit          int
}

// Read returns entries matching the filter, in monotonic entry_id order.
func Read(ctx context.Context, db *sql.DB, opts ReadOpts) ([]Entry, error) {
	var (
		clauses []string
		args    []any
	)
	if opts.FromEntryID > 0 {
		clauses = append(clauses, "entry_id >= ?")
		args = append(args, opts.FromEntryID)
	}
	if opts.EntityID != "" {
		clauses = append(clauses, "entity_id = ?")
		args = append(args, opts.EntityID)
	}
	if opts.RelationshipID != "" {
		clauses = append(clauses, "relationship_id = ?")
		args = append(args, opts.RelationshipID)
	}
	if opts.TransactionID != "" {
		clauses = append(clauses, "transaction_id = ?")
		args = append(args, opts.TransactionID)
	}
	where := ""
	if len(clauses) > 0 {
		where = " WHERE " + strings.Join(clauses, " AND ")
	}
	limit := opts.Limit
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	args = append(args, limit)

	q := `
        SELECT entry_id, transaction_id, mutation_type, entity_id, relationship_id,
               before_state, after_state, occurred_at, recorded_at
          FROM delta_log` + where + `
         ORDER BY entry_id ASC
         LIMIT ?`

	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("query log: %w", err)
	}
	defer rows.Close()

	var out []Entry
	for rows.Next() {
		var (
			e         Entry
			entID     sql.NullString
			relID     sql.NullString
			before    sql.NullString
			after     sql.NullString
			occurred  string
			recorded  string
			mutationT string
		)
		if err := rows.Scan(&e.EntryID, &e.TransactionID, &mutationT,
			&entID, &relID, &before, &after, &occurred, &recorded); err != nil {
			return nil, fmt.Errorf("scan log entry: %w", err)
		}
		e.MutationType = MutationType(mutationT)
		if entID.Valid {
			e.EntityID = entID.String
		}
		if relID.Valid {
			e.RelationshipID = relID.String
		}
		if before.Valid {
			e.BeforeState = json.RawMessage(before.String)
		}
		if after.Valid {
			e.AfterState = json.RawMessage(after.String)
		}
		if e.OccurredAt, err = time.Parse(time.RFC3339Nano, occurred); err != nil {
			return nil, fmt.Errorf("parse occurred_at: %w", err)
		}
		if e.RecordedAt, err = time.Parse(time.RFC3339Nano, recorded); err != nil {
			return nil, fmt.Errorf("parse recorded_at: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullRaw(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return string(b)
}
