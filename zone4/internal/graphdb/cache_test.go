package graphdb_test

import (
	"context"
	"testing"
	"time"

	"archgraph/zone4/internal/graphdb"
	"archgraph/zone4/schema"
)

func TestCache_HitsMissesAndInvalidations(t *testing.T) {
	ctx := context.Background()
	store, err := graphdb.Open(":memory:")
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer store.Close()

	// Create test entities
	tx, err := store.DB().Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	e1 := &schema.Entity{
		ID:            "ent_a",
		Type:          schema.EntityService,
		CanonicalName: "service-a",
		Namespace:     "default",
		Confidence:    1.0,
		IsActive:      true,
		ValidFrom:     time.Now(),
	}
	e2 := &schema.Entity{
		ID:            "ent_b",
		Type:          schema.EntityService,
		CanonicalName: "service-b",
		Namespace:     "default",
		Confidence:    1.0,
		IsActive:      true,
		ValidFrom:     time.Now(),
	}
	rel := &schema.Relationship{
		ID:         "rel_a_b",
		Type:       schema.RelDependsOn,
		FromID:     "ent_a",
		ToID:       "ent_b",
		Confidence: 0.9,
		IsActive:   true,
		ValidFrom:  time.Now(),
	}
	if err := graphdb.InsertEntity(tx, e1); err != nil {
		t.Fatalf("insert entity: %v", err)
	}
	if err := graphdb.InsertEntity(tx, e2); err != nil {
		t.Fatalf("insert entity: %v", err)
	}
	if err := graphdb.InsertRelationship(tx, rel); err != nil {
		t.Fatalf("insert rel: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	// 1. Warm cache on first read
	got, err := store.GetEntity(ctx, "ent_a")
	if err != nil {
		t.Fatalf("GetEntity failed: %v", err)
	}
	if got.ID != "ent_a" {
		t.Errorf("expected ent_a, got %v", got)
	}

	// 2. Fetch Neighborhood to warm Layer 2 Cache
	nb, err := store.Neighborhood(ctx, "ent_a", 1, graphdb.DirOutbound)
	if err != nil {
		t.Fatalf("Neighborhood query failed: %v", err)
	}
	if len(nb.Edges) != 1 {
		t.Errorf("expected 1 edge, got %d", len(nb.Edges))
	}

	// 3. Verify Neighborhood Cache Hit by making database connection closeable (we can just check the cache exists)
	cacheKey := graphdb.NeighborhoodKey("ent_a", 1, graphdb.DirOutbound)
	_, ok := store.Cache().GetNeighborhood(cacheKey)
	if !ok {
		t.Error("expected neighborhood cache hit")
	}

	// 4. Invalidate entity cache
	store.InvalidateCache("ent_a")

	// Cache should be cleared for both entity-a and neighborhood-a
	_, ok = store.Cache().GetEntity("ent_a")
	if ok {
		t.Error("expected entity cache miss after invalidation")
	}
	_, ok = store.Cache().GetNeighborhood(cacheKey)
	if ok {
		t.Error("expected neighborhood cache miss after entity invalidation")
	}

	// 5. Rewarm neighborhood cache
	_, _ = store.Neighborhood(ctx, "ent_a", 1, graphdb.DirOutbound)
	_, ok = store.Cache().GetNeighborhood(cacheKey)
	if !ok {
		t.Error("expected neighborhood cache hit after rewarming")
	}

	// 6. Invalidate by relationship (should clear neighborhood-a because relationship has endpoints ent_a, ent_b)
	store.InvalidateRelCache("ent_a", "ent_b")
	_, ok = store.Cache().GetNeighborhood(cacheKey)
	if ok {
		t.Error("expected neighborhood cache miss after relationship invalidation")
	}
}
