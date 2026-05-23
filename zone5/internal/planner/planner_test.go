package planner

import (
	"errors"
	"testing"

	"archgraph/zone5/internal/intent"
)

func TestMake_StructuralRequiresEntity(t *testing.T) {
	_, _, err := Make(Request{Question: "tell me about something"})
	if err == nil {
		t.Fatal("expected error when no entity hint can be extracted")
	}
}

func TestMake_StructuralFromBackticks(t *testing.T) {
	p, cls, err := Make(Request{Question: "what depends on `payment-service`?", Namespace: "acme"})
	if err != nil {
		t.Fatalf("Make: %v", err)
	}
	if cls.Archetype != intent.ArchStructural {
		t.Errorf("expected STRUCTURAL, got %s", cls.Archetype)
	}
	if p.EntityName != "payment-service" {
		t.Errorf("expected payment-service, got %q", p.EntityName)
	}
	if p.Action != ActFetchNeighborhood {
		t.Errorf("expected FETCH_NEIGHBORHOOD, got %s", p.Action)
	}
}

func TestMake_GovernanceNeedsNamespace(t *testing.T) {
	_, _, err := Make(Request{Question: "do we have circular dependencies"})
	if !errors.Is(err, ErrMissingNamespace) {
		t.Fatalf("expected ErrMissingNamespace, got %v", err)
	}
}

func TestMake_ImpactPath(t *testing.T) {
	p, _, err := Make(Request{
		Question:   "what breaks if I delete UserTable",
		EntityName: "UserTable",
		Namespace:  "acme",
	})
	if err != nil {
		t.Fatalf("Make: %v", err)
	}
	if p.Action != ActImpactAnalysis {
		t.Errorf("expected IMPACT_ANALYSIS, got %s", p.Action)
	}
	if p.Depth < 5 {
		t.Errorf("expected impact depth ≥ 5, got %d", p.Depth)
	}
}
