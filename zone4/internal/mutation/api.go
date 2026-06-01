// Package mutation is the SINGLE write entry point for Zone 4.
//
// Per Zone4.md: nothing writes to the graph except through this API. Every
// mutation is:
//   1. Schema-validated.
//   2. Version-checked (optimistic locking).
//   3. Applied to the graph projection AND appended to the delta log inside
//      one SQLite transaction. The cross-table atomicity SQLite gives us is
//      what makes "delta log is truth, graph is projection" workable.
package mutation

import (
	"context"
	"crypto/rand"
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

// API is the single-write entry point.
type API struct {
	store *graphdb.Store
}

func New(store *graphdb.Store) *API {
	return &API{store: store}
}

// MutationKind controls how an entry in a batch is processed.
type MutationKind string

const (
	KindUpsertEntity       MutationKind = "UPSERT_ENTITY"
	KindUpsertRelationship MutationKind = "UPSERT_RELATIONSHIP"
	KindSoftDeleteEntity   MutationKind = "SOFT_DELETE_ENTITY"
	KindDeleteRelationship MutationKind = "DELETE_RELATIONSHIP"
)

// Mutation is one item in a batch. Exactly one of Entity, Relationship, or
// TargetID is set, depending on Kind.
type Mutation struct {
	Kind         MutationKind         `json:"kind"`
	Entity       *schema.Entity       `json:"entity,omitempty"`
	Relationship *schema.Relationship `json:"relationship,omitempty"`
	TargetID     string               `json:"target_id,omitempty"`
	Reason       string               `json:"reason,omitempty"`
}

// OperationType reports what the apply step actually did.
type OperationType string

const (
	OpCreated     OperationType = "CREATED"
	OpUpdated     OperationType = "UPDATED"
	OpDeleted     OperationType = "DELETED"
	OpNoChange    OperationType = "NO_CHANGE"
	OpFailed      OperationType = "FAILED"
)

// Result is the per-mutation outcome inside a batch result.
type Result struct {
	Index          int           `json:"index"`
	Kind           MutationKind  `json:"kind"`
	Operation      OperationType `json:"operation"`
	EntityID       string        `json:"entity_id,omitempty"`
	RelationshipID string        `json:"relationship_id,omitempty"`
	LogEntryID     int64         `json:"log_entry_id,omitempty"`
	Error          string        `json:"error,omitempty"`
}

// BatchResult is the per-batch return value.
type BatchResult struct {
	TransactionID string   `json:"transaction_id"`
	Status        string   `json:"status"` // SUCCESS | FAILED
	Results       []Result `json:"results"`
}

// ApplyBatch runs an ordered batch of mutations in one SQLite transaction.
// If any mutation fails the entire transaction is rolled back (no partial
// graph writes). The delta log rollback is handled by SQLite for us — every
// log append happens inside the same tx.
//
// Per the spec, on failure we *could* append a compensation record to the log
// instead of rolling back. For the MVP we keep it simple: failed batches
// leave no trace except the returned error.
func (a *API) ApplyBatch(ctx context.Context, mutations []Mutation) (*BatchResult, error) {
	txnID := newTransactionID()
	res := &BatchResult{TransactionID: txnID, Status: "SUCCESS"}

	tx, err := a.store.DB().BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	rollback := true
	defer func() {
		if rollback {
			_ = tx.Rollback()
		}
	}()

	for i, m := range mutations {
		r := Result{Index: i, Kind: m.Kind}
		if err := a.applyOne(ctx, tx, txnID, &m, &r); err != nil {
			r.Operation = OpFailed
			r.Error = err.Error()
			res.Results = append(res.Results, r)
			res.Status = "FAILED"
			return res, err
		}
		res.Results = append(res.Results, r)
	}

	if err := tx.Commit(); err != nil {
		return res, fmt.Errorf("commit: %w", err)
	}
	rollback = false
	return res, nil
}

func (a *API) applyOne(ctx context.Context, tx *sql.Tx, txnID string, m *Mutation, r *Result) error {
	switch m.Kind {
	case KindUpsertEntity:
		return a.upsertEntity(ctx, tx, txnID, m, r)
	case KindUpsertRelationship:
		return a.upsertRelationship(ctx, tx, txnID, m, r)
	case KindSoftDeleteEntity:
		return a.softDeleteEntity(ctx, tx, txnID, m, r)
	case KindDeleteRelationship:
		return a.deleteRelationship(ctx, tx, txnID, m, r)
	default:
		return fmt.Errorf("unknown mutation kind %q", m.Kind)
	}
}

// --- Per-kind handlers ---------------------------------------------------

func (a *API) upsertEntity(ctx context.Context, tx *sql.Tx, txnID string, m *Mutation, r *Result) error {
	e := m.Entity
	if e == nil {
		return errors.New("upsert_entity: missing entity")
	}
	if err := schema.ValidateEntity(e); err != nil {
		return err
	}
	if e.ID == "" {
		e.ID = e.DeterministicID()
	}
	if !e.IsActive {
		// Upsert implies the entity is observed; if caller didn't set it,
		// default to active.
		e.IsActive = true
	}
	if e.LifecycleStage == "" {
		e.LifecycleStage = schema.LifecycleActive
	}
	r.EntityID = e.ID

	existing, err := getEntityTx(ctx, tx, e.ID)
	if err != nil && !errors.Is(err, graphdb.ErrNotFound) {
		return err
	}

	if existing == nil {
		if err := graphdb.InsertEntity(tx, e); err != nil {
			return err
		}
		r.Operation = OpCreated
		return appendEntityLog(tx, txnID, deltalog.MutEntityCreated, e.ID, nil, e, &r.LogEntryID)
	}

	// Optimistic lock: if caller supplied a version it must match.
	if e.Version != 0 && e.Version != existing.Version {
		return fmt.Errorf("%w: have v%d, supplied v%d", graphdb.ErrVersionConflict, existing.Version, e.Version)
	}
	prevVersion := existing.Version
	e.Version = prevVersion + 1

	if entityEqualForUpdate(existing, e) {
		r.Operation = OpNoChange
		return nil
	}
	if err := graphdb.UpdateEntity(tx, e, prevVersion); err != nil {
		return err
	}
	r.Operation = OpUpdated
	return appendEntityLog(tx, txnID, deltalog.MutEntityUpdated, e.ID, existing, e, &r.LogEntryID)
}

func (a *API) upsertRelationship(ctx context.Context, tx *sql.Tx, txnID string, m *Mutation, r *Result) error {
	rel := m.Relationship
	if rel == nil {
		return errors.New("upsert_relationship: missing relationship")
	}
	if err := schema.ValidateRelationship(rel); err != nil {
		return err
	}
	if rel.ID == "" {
		rel.ID = rel.DeterministicID()
	}
	if !rel.IsActive {
		rel.IsActive = true
	}
	r.RelationshipID = rel.ID

	// Endpoints must exist (REL-001).
	if ok, err := graphdb.EntityExists(tx, rel.FromID); err != nil {
		return err
	} else if !ok {
		return fmt.Errorf("%w: from_id %s", schema.ErrUnknownEntity, rel.FromID)
	}
	if ok, err := graphdb.EntityExists(tx, rel.ToID); err != nil {
		return err
	} else if !ok {
		return fmt.Errorf("%w: to_id %s", schema.ErrUnknownEntity, rel.ToID)
	}

	existing, err := getRelationshipTx(ctx, tx, rel.ID)
	if err != nil && !errors.Is(err, graphdb.ErrNotFound) {
		return err
	}

	if existing == nil {
		if err := graphdb.InsertRelationship(tx, rel); err != nil {
			return err
		}
		r.Operation = OpCreated
		return appendRelLog(tx, txnID, deltalog.MutRelationshipCreated, rel.ID, nil, rel, &r.LogEntryID)
	}

	if rel.Version != 0 && rel.Version != existing.Version {
		return fmt.Errorf("%w: have v%d, supplied v%d", graphdb.ErrVersionConflict, existing.Version, rel.Version)
	}
	prevVersion := existing.Version
	rel.Version = prevVersion + 1

	if relEqualForUpdate(existing, rel) {
		r.Operation = OpNoChange
		return nil
	}
	if err := graphdb.UpdateRelationship(tx, rel, prevVersion); err != nil {
		return err
	}
	r.Operation = OpUpdated
	return appendRelLog(tx, txnID, deltalog.MutRelationshipUpdated, rel.ID, existing, rel, &r.LogEntryID)
}

func (a *API) softDeleteEntity(ctx context.Context, tx *sql.Tx, txnID string, m *Mutation, r *Result) error {
	if m.TargetID == "" {
		return errors.New("soft_delete_entity: missing target_id")
	}
	r.EntityID = m.TargetID
	existing, err := getEntityTx(ctx, tx, m.TargetID)
	if err != nil {
		return err
	}
	if !existing.IsActive {
		r.Operation = OpNoChange
		return nil
	}
	if err := graphdb.SoftDeleteEntity(tx, m.TargetID, existing.Version); err != nil {
		return err
	}
	r.Operation = OpDeleted
	after := *existing
	after.IsActive = false
	after.LifecycleStage = schema.LifecycleDeleted
	now := time.Now().UTC()
	after.ValidTo = &now
	after.Version = existing.Version + 1
	return appendEntityLog(tx, txnID, deltalog.MutEntitySoftDeleted, m.TargetID, existing, &after, &r.LogEntryID)
}

func (a *API) deleteRelationship(ctx context.Context, tx *sql.Tx, txnID string, m *Mutation, r *Result) error {
	if m.TargetID == "" {
		return errors.New("delete_relationship: missing target_id")
	}
	r.RelationshipID = m.TargetID
	existing, err := getRelationshipTx(ctx, tx, m.TargetID)
	if err != nil {
		return err
	}
	if !existing.IsActive {
		r.Operation = OpNoChange
		return nil
	}
	if err := graphdb.SoftDeleteRelationship(tx, m.TargetID, existing.Version); err != nil {
		return err
	}
	r.Operation = OpDeleted
	after := *existing
	after.IsActive = false
	now := time.Now().UTC()
	after.ValidTo = &now
	after.Version = existing.Version + 1
	return appendRelLog(tx, txnID, deltalog.MutRelationshipDeleted, m.TargetID, existing, &after, &r.LogEntryID)
}

// --- Delta-log helpers ---------------------------------------------------

func appendEntityLog(tx *sql.Tx, txnID string, mut deltalog.MutationType, id string, before, after *schema.Entity, out *int64) error {
	entry := &deltalog.Entry{
		TransactionID: txnID,
		MutationType:  mut,
		EntityID:      id,
	}
	if before != nil {
		b, err := json.Marshal(before)
		if err != nil {
			return fmt.Errorf("marshal before: %w", err)
		}
		entry.BeforeState = b
	}
	if after != nil {
		b, err := json.Marshal(after)
		if err != nil {
			return fmt.Errorf("marshal after: %w", err)
		}
		entry.AfterState = b
	}
	id64, err := deltalog.Append(tx, entry)
	if err != nil {
		return err
	}
	*out = id64
	return nil
}

func appendRelLog(tx *sql.Tx, txnID string, mut deltalog.MutationType, id string, before, after *schema.Relationship, out *int64) error {
	entry := &deltalog.Entry{
		TransactionID:  txnID,
		MutationType:   mut,
		RelationshipID: id,
	}
	if before != nil {
		b, err := json.Marshal(before)
		if err != nil {
			return fmt.Errorf("marshal before: %w", err)
		}
		entry.BeforeState = b
	}
	if after != nil {
		b, err := json.Marshal(after)
		if err != nil {
			return fmt.Errorf("marshal after: %w", err)
		}
		entry.AfterState = b
	}
	id64, err := deltalog.Append(tx, entry)
	if err != nil {
		return err
	}
	*out = id64
	return nil
}

func newTransactionID() string {
	var b [12]byte
	_, _ = rand.Read(b[:])
	return "txn_" + hex.EncodeToString(b[:])
}
