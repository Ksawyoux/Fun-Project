package mutation_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"archgraph/zone4/internal/deltalog"
	"archgraph/zone4/internal/graphdb"
	"archgraph/zone4/internal/mutation"
	"archgraph/zone4/internal/schema"
)

func newTestAPI(t *testing.T) (*mutation.API, *graphdb.Store) {
	t.Helper()
	store, err := graphdb.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return mutation.New(store), store
}

func ent(name, ns string) *schema.Entity {
	return &schema.Entity{
		Type:          schema.EntityService,
		CanonicalName: name,
		Namespace:     ns,
		Confidence:    0.9,
		IsActive:      true,
		ValidFrom:     time.Now(),
	}
}

func TestApplyBatch_CreateEntityWritesGraphAndLog(t *testing.T) {
	ctx := context.Background()
	api, store := newTestAPI(t)

	e := ent("payment-service", "acme")
	res, err := api.ApplyBatch(ctx, []mutation.Mutation{
		{Kind: mutation.KindUpsertEntity, Entity: e},
	})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if res.Status != "SUCCESS" || res.Results[0].Operation != mutation.OpCreated {
		t.Fatalf("unexpected result: %+v", res)
	}

	got, err := store.GetEntity(ctx, res.Results[0].EntityID)
	if err != nil {
		t.Fatalf("get entity: %v", err)
	}
	if got.CanonicalName != "payment-service" {
		t.Errorf("expected payment-service, got %q", got.CanonicalName)
	}
	if got.Version != 1 {
		t.Errorf("expected v1, got v%d", got.Version)
	}

	entries, err := deltalog.Read(ctx, store.DB(), deltalog.ReadOpts{EntityID: got.ID})
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 log entry, got %d", len(entries))
	}
	if entries[0].MutationType != deltalog.MutEntityCreated {
		t.Errorf("expected ENTITY_CREATED, got %s", entries[0].MutationType)
	}
}

func TestApplyBatch_UpsertEntity_UpdatesAndBumpsVersion(t *testing.T) {
	ctx := context.Background()
	api, store := newTestAPI(t)

	e := ent("auth", "acme")
	if _, err := api.ApplyBatch(ctx, []mutation.Mutation{
		{Kind: mutation.KindUpsertEntity, Entity: e},
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	// Mutate a property and upsert again.
	e2 := ent("auth", "acme")
	e2.Confidence = 0.95
	res, err := api.ApplyBatch(ctx, []mutation.Mutation{
		{Kind: mutation.KindUpsertEntity, Entity: e2},
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if res.Results[0].Operation != mutation.OpUpdated {
		t.Fatalf("expected UPDATED, got %s", res.Results[0].Operation)
	}
	got, _ := store.GetEntity(ctx, res.Results[0].EntityID)
	if got.Version != 2 {
		t.Errorf("expected v2, got v%d", got.Version)
	}
	if got.Confidence != 0.95 {
		t.Errorf("expected confidence 0.95, got %v", got.Confidence)
	}
}

func TestApplyBatch_NoChangeUpsertSkipsLog(t *testing.T) {
	ctx := context.Background()
	api, store := newTestAPI(t)

	e := ent("idempotent-svc", "acme")
	if _, err := api.ApplyBatch(ctx, []mutation.Mutation{
		{Kind: mutation.KindUpsertEntity, Entity: e},
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	// Re-upsert identical content.
	res, err := api.ApplyBatch(ctx, []mutation.Mutation{
		{Kind: mutation.KindUpsertEntity, Entity: ent("idempotent-svc", "acme")},
	})
	if err != nil {
		t.Fatalf("noop upsert: %v", err)
	}
	if res.Results[0].Operation != mutation.OpNoChange {
		t.Errorf("expected NO_CHANGE, got %s", res.Results[0].Operation)
	}
	entries, _ := deltalog.Read(ctx, store.DB(), deltalog.ReadOpts{EntityID: res.Results[0].EntityID})
	if len(entries) != 1 {
		t.Errorf("expected 1 log entry (just the create), got %d", len(entries))
	}
}

func TestApplyBatch_RelationshipRequiresEndpoints(t *testing.T) {
	ctx := context.Background()
	api, _ := newTestAPI(t)

	rel := &schema.Relationship{
		Type:       schema.RelDependsOn,
		FromID:     "ent_nonexistent_a",
		ToID:       "ent_nonexistent_b",
		Confidence: 0.9,
		IsActive:   true,
		ValidFrom:  time.Now(),
	}
	_, err := api.ApplyBatch(ctx, []mutation.Mutation{
		{Kind: mutation.KindUpsertRelationship, Relationship: rel},
	})
	if !errors.Is(err, schema.ErrUnknownEntity) {
		t.Fatalf("expected ErrUnknownEntity, got %v", err)
	}
}

func TestApplyBatch_EntityAndRelationshipAtomic(t *testing.T) {
	ctx := context.Background()
	api, store := newTestAPI(t)

	a := ent("svc-a", "acme")
	b := ent("svc-b", "acme")
	// IDs derived deterministically so we can reference them in the relationship.
	a.ID = a.DeterministicID()
	b.ID = b.DeterministicID()

	rel := &schema.Relationship{
		Type:       schema.RelDependsOn,
		FromID:     a.ID,
		ToID:       b.ID,
		Confidence: 0.85,
		IsActive:   true,
		ValidFrom:  time.Now(),
	}

	res, err := api.ApplyBatch(ctx, []mutation.Mutation{
		{Kind: mutation.KindUpsertEntity, Entity: a},
		{Kind: mutation.KindUpsertEntity, Entity: b},
		{Kind: mutation.KindUpsertRelationship, Relationship: rel},
	})
	if err != nil {
		t.Fatalf("batch: %v", err)
	}
	if res.Status != "SUCCESS" || len(res.Results) != 3 {
		t.Fatalf("unexpected result: %+v", res)
	}

	nb, err := store.Neighborhood(ctx, a.ID, 1, graphdb.DirOutbound)
	if err != nil {
		t.Fatalf("neighborhood: %v", err)
	}
	if len(nb.Edges) != 1 || nb.Edges[0].ToID != b.ID {
		t.Fatalf("expected one outbound edge to %s, got %+v", b.ID, nb.Edges)
	}
}

func TestApplyBatch_SoftDeleteThenQuery(t *testing.T) {
	ctx := context.Background()
	api, store := newTestAPI(t)

	e := ent("ephemeral", "acme")
	cr, err := api.ApplyBatch(ctx, []mutation.Mutation{
		{Kind: mutation.KindUpsertEntity, Entity: e},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	id := cr.Results[0].EntityID

	if _, err := api.ApplyBatch(ctx, []mutation.Mutation{
		{Kind: mutation.KindSoftDeleteEntity, TargetID: id, Reason: "test"},
	}); err != nil {
		t.Fatalf("delete: %v", err)
	}

	// By-ID still returns the row, marked inactive.
	got, err := store.GetEntity(ctx, id)
	if err != nil {
		t.Fatalf("get after delete: %v", err)
	}
	if got.IsActive {
		t.Error("expected is_active=false after soft delete")
	}
	if got.LifecycleStage != schema.LifecycleDeleted {
		t.Errorf("expected lifecycle=DELETED, got %s", got.LifecycleStage)
	}

	// By-name should NOT return it (only active rows).
	_, err = store.GetEntityByName(ctx, "acme", "ephemeral")
	if !errors.Is(err, graphdb.ErrNotFound) {
		t.Errorf("expected ErrNotFound by name after soft delete, got %v", err)
	}
}

func TestApplyBatch_VersionConflict(t *testing.T) {
	ctx := context.Background()
	api, _ := newTestAPI(t)

	e := ent("locked", "acme")
	cr, err := api.ApplyBatch(ctx, []mutation.Mutation{
		{Kind: mutation.KindUpsertEntity, Entity: e},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// Stale version supplied by caller.
	stale := ent("locked", "acme")
	stale.ID = cr.Results[0].EntityID
	stale.Version = 99
	stale.Confidence = 0.5

	_, err = api.ApplyBatch(ctx, []mutation.Mutation{
		{Kind: mutation.KindUpsertEntity, Entity: stale},
	})
	if !errors.Is(err, graphdb.ErrVersionConflict) {
		t.Fatalf("expected version conflict, got %v", err)
	}
}
