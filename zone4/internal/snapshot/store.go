package snapshot

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"archgraph/zone4/internal/deltalog"
	"archgraph/zone4/internal/graphdb"
	"archgraph/zone4/schema"
)

type GraphState struct {
	Entities      []*schema.Entity       `json:"entities"`
	Relationships []*schema.Relationship `json:"relationships"`
}

type SnapshotMetadata struct {
	SnapshotID     string    `json:"snapshot_id"`
	SnapshotAt     time.Time `json:"snapshot_at"`
	CreatedAt      time.Time `json:"created_at"`
	LastLogEntryID int64     `json:"last_log_entry_id"`
	NodeCount      int       `json:"node_count"`
	EdgeCount      int       `json:"edge_count"`
	ActiveServices int       `json:"active_services"`
	Checksum       string    `json:"checksum"`
}

type SnapshotStore struct {
	db *sql.DB
}

func New(db *sql.DB) *SnapshotStore {
	return &SnapshotStore{db: db}
}

func (s *SnapshotStore) CreateSnapshot(ctx context.Context, snapshotID string, snapshotAt time.Time) (*SnapshotMetadata, error) {
	if snapshotID == "" {
		snapshotID = fmt.Sprintf("snap_%d", time.Now().UnixNano())
	}
	if snapshotAt.IsZero() {
		snapshotAt = time.Now()
	}

	// 1. Get max log entry ID up to snapshotAt
	var lastLogEntryID int64
	var maxID sql.NullInt64
	err := s.db.QueryRowContext(ctx, `
        SELECT MAX(entry_id) FROM delta_log WHERE occurred_at <= ?
    `, snapshotAt.UTC().Format(time.RFC3339Nano)).Scan(&maxID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("find max log entry: %w", err)
	}
	if maxID.Valid {
		lastLogEntryID = maxID.Int64
	}

	// 2. Fetch all entities
	rows, err := s.db.QueryContext(ctx, `
        SELECT id, type, sub_type, canonical_name, namespace, confidence,
               is_active, lifecycle_stage, valid_from, valid_to, version,
               properties, created_at, updated_at
          FROM entities
    `)
	if err != nil {
		return nil, fmt.Errorf("fetch entities for snapshot: %w", err)
	}
	defer rows.Close()

	var state GraphState
	activeServices := 0
	for rows.Next() {
		e, err := graphdb.ScanEntityRow(rows)
		if err != nil {
			return nil, err
		}
		state.Entities = append(state.Entities, e)
		if e.Type == schema.EntityService && e.IsActive {
			activeServices++
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// 3. Fetch all relationships
	relRows, err := s.db.QueryContext(ctx, `
        SELECT id, type, from_id, to_id, confidence, is_active,
               valid_from, valid_to, version, properties, created_at, updated_at
          FROM relationships
    `)
	if err != nil {
		return nil, fmt.Errorf("fetch relationships for snapshot: %w", err)
	}
	defer relRows.Close()

	for relRows.Next() {
		r, err := graphdb.ScanRelationshipRow(relRows)
		if err != nil {
			return nil, err
		}
		state.Relationships = append(state.Relationships, r)
	}
	if err := relRows.Err(); err != nil {
		return nil, err
	}

	// 4. Compress graph data
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	if err := json.NewEncoder(gw).Encode(state); err != nil {
		return nil, fmt.Errorf("encode graph state: %w", err)
	}
	gw.Close()
	compressedBytes := buf.Bytes()

	// 5. Compute Checksum
	sum := sha256.Sum256(compressedBytes)
	checksum := hex.EncodeToString(sum[:])

	// 6. Stats & Save
	nodeCount := len(state.Entities)
	edgeCount := len(state.Relationships)
	stats := map[string]int{
		"node_count":      nodeCount,
		"edge_count":      edgeCount,
		"active_services": activeServices,
	}
	statsJSON, err := json.Marshal(stats)
	if err != nil {
		return nil, fmt.Errorf("marshal stats: %w", err)
	}

	now := time.Now().UTC()
	_, err = s.db.ExecContext(ctx, `
        INSERT INTO snapshots (
            snapshot_id, snapshot_at, created_at, last_log_entry_id, statistics, graph_data, checksum
        ) VALUES (?, ?, ?, ?, ?, ?, ?)
    `,
		snapshotID,
		snapshotAt.UTC().Format(time.RFC3339Nano),
		now.Format(time.RFC3339Nano),
		lastLogEntryID,
		string(statsJSON),
		compressedBytes,
		checksum,
	)
	if err != nil {
		return nil, fmt.Errorf("insert snapshot: %w", err)
	}

	return &SnapshotMetadata{
		SnapshotID:     snapshotID,
		SnapshotAt:     snapshotAt,
		CreatedAt:      now,
		LastLogEntryID: lastLogEntryID,
		NodeCount:      nodeCount,
		EdgeCount:      edgeCount,
		ActiveServices: activeServices,
		Checksum:       checksum,
	}, nil
}

func (s *SnapshotStore) RestoreGraph(ctx context.Context, asOf time.Time) (*GraphState, error) {
	if asOf.IsZero() {
		asOf = time.Now()
	}

	// 1. Find nearest snapshot before asOf
	var (
		snapshotID     string
		lastLogEntryID int64
		compressedData []byte
		checksum       string
	)
	err := s.db.QueryRowContext(ctx, `
        SELECT snapshot_id, last_log_entry_id, graph_data, checksum
          FROM snapshots
         WHERE snapshot_at <= ?
         ORDER BY snapshot_at DESC
         LIMIT 1
    `, asOf.UTC().Format(time.RFC3339Nano)).Scan(&snapshotID, &lastLogEntryID, &compressedData, &checksum)

	state := &GraphState{
		Entities:      make([]*schema.Entity, 0),
		Relationships: make([]*schema.Relationship, 0),
	}

	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("find nearest snapshot: %w", err)
		}
		// No snapshot found, replay all delta logs from entry_id >= 1
		lastLogEntryID = 0
	} else {
		// Verify checksum
		sum := sha256.Sum256(compressedData)
		if hex.EncodeToString(sum[:]) != checksum {
			return nil, fmt.Errorf("snapshot %q checksum mismatch", snapshotID)
		}

		// Decompress graph data
		gr, err := gzip.NewReader(bytes.NewReader(compressedData))
		if err != nil {
			return nil, fmt.Errorf("decompress snapshot graph_data: %w", err)
		}
		defer gr.Close()

		if err := json.NewDecoder(gr).Decode(state); err != nil {
			return nil, fmt.Errorf("decode snapshot state: %w", err)
		}
	}

	// 2. Fetch delta logs from lastLogEntryID up to asOf
	rows, err := s.db.QueryContext(ctx, `
        SELECT entry_id, transaction_id, mutation_type, entity_id, relationship_id,
               before_state, after_state, occurred_at, recorded_at
          FROM delta_log
         WHERE entry_id > ? AND occurred_at <= ?
         ORDER BY entry_id ASC
    `, lastLogEntryID, asOf.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, fmt.Errorf("query delta log for replay: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			e         deltalog.Entry
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
		e.MutationType = deltalog.MutationType(mutationT)
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

		if err := replayEntry(state, e); err != nil {
			return nil, fmt.Errorf("replay entry %d: %w", e.EntryID, err)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return state, nil
}

func replayEntry(state *GraphState, entry deltalog.Entry) error {
	switch entry.MutationType {
	case deltalog.MutEntityCreated:
		var e schema.Entity
		if err := json.Unmarshal(entry.AfterState, &e); err != nil {
			return err
		}
		state.Entities = append(state.Entities, &e)

	case deltalog.MutEntityUpdated, deltalog.MutEntitySoftDeleted, deltalog.MutEntityRestored:
		var e schema.Entity
		if err := json.Unmarshal(entry.AfterState, &e); err != nil {
			return err
		}
		found := false
		for i, existing := range state.Entities {
			if existing.ID == e.ID {
				state.Entities[i] = &e
				found = true
				break
			}
		}
		if !found {
			state.Entities = append(state.Entities, &e)
		}

	case deltalog.MutRelationshipCreated:
		var r schema.Relationship
		if err := json.Unmarshal(entry.AfterState, &r); err != nil {
			return err
		}
		state.Relationships = append(state.Relationships, &r)

	case deltalog.MutRelationshipUpdated, deltalog.MutRelationshipDeleted:
		var r schema.Relationship
		if err := json.Unmarshal(entry.AfterState, &r); err != nil {
			return err
		}
		found := false
		for i, existing := range state.Relationships {
			if existing.ID == r.ID {
				state.Relationships[i] = &r
				found = true
				break
			}
		}
		if !found {
			state.Relationships = append(state.Relationships, &r)
		}
	}
	return nil
}
