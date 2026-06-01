package mutation

import (
	"context"
	"database/sql"
	"reflect"

	"archgraph/zone4/internal/graphdb"
	"archgraph/zone4/schema"
)

// getEntityTx / getRelationshipTx do the same job as graphdb.GetEntity but
// inside an open transaction. We need this so the read-modify-write sequence
// inside ApplyBatch is consistent — SQLite holds a write lock on the tx so
// the read can't race with another writer.
//
// We open these as raw SELECTs rather than reusing the package methods so we
// don't end up taking two connections from the pool (which would deadlock at
// SetMaxOpenConns(1)).
func getEntityTx(ctx context.Context, tx *sql.Tx, id string) (*schema.Entity, error) {
	row := tx.QueryRowContext(ctx, `SELECT
        id, type, sub_type, canonical_name, namespace, confidence,
        is_active, lifecycle_stage, valid_from, valid_to, version,
        properties, created_at, updated_at
        FROM entities WHERE id = ?`, id)
	return graphdb.ScanEntityRow(row)
}

func getRelationshipTx(ctx context.Context, tx *sql.Tx, id string) (*schema.Relationship, error) {
	row := tx.QueryRowContext(ctx, `SELECT
        id, type, from_id, to_id, confidence, is_active,
        valid_from, valid_to, version, properties, created_at, updated_at
        FROM relationships WHERE id = ?`, id)
	return graphdb.ScanRelationshipRow(row)
}

// entityEqualForUpdate decides whether two entity snapshots are "effectively
// the same" — used to short-circuit no-op upserts so we don't pollute the
// delta log with phantom updates.
//
// Fields excluded from comparison:
//   - Version, ValidFrom, ValidTo: bookkeeping, set by the writer.
//   - ID: identity, equal by construction.
func entityEqualForUpdate(a, b *schema.Entity) bool {
	return a.Type == b.Type &&
		a.SubType == b.SubType &&
		a.CanonicalName == b.CanonicalName &&
		a.Namespace == b.Namespace &&
		a.Confidence == b.Confidence &&
		a.IsActive == b.IsActive &&
		a.LifecycleStage == b.LifecycleStage &&
		mapsEqual(a.Properties, b.Properties)
}

func relEqualForUpdate(a, b *schema.Relationship) bool {
	return a.Type == b.Type &&
		a.FromID == b.FromID &&
		a.ToID == b.ToID &&
		a.Confidence == b.Confidence &&
		a.IsActive == b.IsActive &&
		mapsEqual(a.Properties, b.Properties)
}

func mapsEqual(a, b map[string]any) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	return reflect.DeepEqual(a, b)
}
