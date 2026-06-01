package search_test

import (
	"context"
	"testing"
	"time"

	"archgraph/zone4/internal/graphdb"
	"archgraph/zone4/internal/search"
	"archgraph/zone4/schema"
)

func TestSearch_IndexAndQuery(t *testing.T) {
	ctx := context.Background()
	store, err := graphdb.Open(":memory:")
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer store.Close()

	// Insert test entities
	tx, err := store.DB().Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	e1 := &schema.Entity{
		ID:            "ent_payment_001",
		Type:          schema.EntityService,
		CanonicalName: "payment-service",
		Namespace:     "acme",
		Confidence:    0.9,
		IsActive:      true,
		ValidFrom:     time.Now(),
		Properties: map[string]any{
			"aliases":      []any{"payment-gateway", "pay-api"},
			"owner_team":   "team-payments",
			"criticality":  "HIGH",
			"tags":         []any{"prod", "billing"},
		},
	}
	e2 := &schema.Entity{
		ID:            "ent_auth_002",
		Type:          schema.EntityService,
		CanonicalName: "auth-service",
		Namespace:     "acme",
		Confidence:    0.95,
		IsActive:      true,
		ValidFrom:     time.Now(),
		Properties: map[string]any{
			"owner_team":   "team-auth",
			"criticality":  "CRITICAL",
			"tags":         []any{"prod", "security"},
		},
	}
	if err := graphdb.InsertEntity(tx, e1); err != nil {
		t.Fatalf("insert e1: %v", err)
	}
	if err := graphdb.InsertEntity(tx, e2); err != nil {
		t.Fatalf("insert e2: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	indexer := search.New(store.DB())
	indexer.Start(ctx)
	defer indexer.Stop()

	// Index them (run synchronously in test to avoid races)
	indexer.Enqueue(e1.ID)
	indexer.Enqueue(e2.ID)

	// A short sleep to allow the background worker to consume the channel
	time.Sleep(100 * time.Millisecond)

	// Query 1: Search by text in name
	res, err := indexer.Search(ctx, search.SearchOptions{Query: "payment"})
	if err != nil {
		t.Fatalf("search payment: %v", err)
	}
	if len(res) != 1 || res[0].ID != e1.ID {
		t.Errorf("expected 1 result (payment-service), got %d results", len(res))
	}

	// Query 2: Search by text in alias
	res, err = indexer.Search(ctx, search.SearchOptions{Query: "gateway"})
	if err != nil {
		t.Fatalf("search gateway: %v", err)
	}
	if len(res) != 1 || res[0].ID != e1.ID {
		t.Errorf("expected 1 result (payment-service by alias), got %d results", len(res))
	}

	// Query 3: Search with facet filter (criticality = CRITICAL)
	res, err = indexer.Search(ctx, search.SearchOptions{Criticality: "CRITICAL"})
	if err != nil {
		t.Fatalf("search criticality: %v", err)
	}
	if len(res) != 1 || res[0].ID != e2.ID {
		t.Errorf("expected 1 result (auth-service), got %d results", len(res))
	}

	// Query 4: Search with multiple filters (owner_team = team-payments, namespace = acme)
	res, err = indexer.Search(ctx, search.SearchOptions{OwnerTeam: "team-payments", Namespace: "acme"})
	if err != nil {
		t.Fatalf("search combined: %v", err)
	}
	if len(res) != 1 || res[0].ID != e1.ID {
		t.Errorf("expected 1 result (payment-service), got %d results", len(res))
	}
}
