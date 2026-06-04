package registry

import (
	"context"
	"testing"
	"time"
)

func TestRegistryUpsertAndGet(t *testing.T) {
	ctx := context.Background()

	// Use in-memory SQLite for testing
	reg, err := Open(":memory:")
	if err != nil {
		t.Fatalf("failed to open registry: %v", err)
	}
	defer reg.Close()

	now := time.Now().UTC()
	ent := &RegistryEntity{
		CanonicalID:   "ent_12345",
		EntityType:    "SERVICE",
		SubType:       "MICROSERVICE",
		CanonicalName: "payment-service",
		Namespace:     "acme-prod",
		Confidence:    0.98,
		FirstSeen:     now,
		LastConfirmed: now,
		Aliases: []AliasInfo{
			{Name: "payment_svc", SourceType: "git", SourceID: "repo1", Confidence: 0.95, AddedAt: now},
		},
		SourceContributions: []SourceContribution{
			{SourceType: "git", SourceID: "repo1", SourceRef: "main", LastSeen: now},
		},
	}

	// 1. Upsert
	if err := reg.Upsert(ctx, ent); err != nil {
		t.Fatalf("upsert failed: %v", err)
	}

	// 2. GetByID
	got, err := reg.GetByID(ctx, "ent_12345")
	if err != nil {
		t.Fatalf("get by ID failed: %v", err)
	}
	if got.CanonicalName != "payment-service" {
		t.Errorf("expected canonical name 'payment-service', got %q", got.CanonicalName)
	}
	if len(got.Aliases) != 1 || got.Aliases[0].Name != "payment_svc" {
		t.Errorf("alias not restored correctly: %v", got.Aliases)
	}

	// 3. GetByCanonicalName
	got, err = reg.GetByCanonicalName(ctx, "payment-service", "acme-prod")
	if err != nil {
		t.Fatalf("get by name failed: %v", err)
	}
	if got.CanonicalID != "ent_12345" {
		t.Errorf("expected ID 'ent_12345', got %q", got.CanonicalID)
	}

	// 4. GetByAlias
	got, err = reg.GetByAlias(ctx, "payment_svc", "acme-prod")
	if err != nil {
		t.Fatalf("get by alias failed: %v", err)
	}
	if got.CanonicalID != "ent_12345" {
		t.Errorf("expected ID 'ent_12345' from alias lookup, got %q", got.CanonicalID)
	}

	// 5. Get missing entity
	_, err = reg.GetByID(ctx, "missing")
	if err == nil {
		t.Error("expected error for missing entity, got nil")
	}
}
