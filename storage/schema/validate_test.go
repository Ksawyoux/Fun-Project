package schema

import (
	"errors"
	"testing"
	"time"
)

func validEntity() *Entity {
	return &Entity{
		Type:          EntityService,
		CanonicalName: "payment-service",
		Namespace:     "acme",
		Confidence:    0.9,
		IsActive:      true,
		ValidFrom:     time.Now(),
	}
}

func TestValidateEntity_OK(t *testing.T) {
	if err := ValidateEntity(validEntity()); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestValidateEntity_Errors(t *testing.T) {
	cases := []struct {
		name  string
		mut   func(*Entity)
		field string
	}{
		{"bad type", func(e *Entity) { e.Type = "NOPE" }, "type"},
		{"empty name", func(e *Entity) { e.CanonicalName = "" }, "canonical_name"},
		{"empty namespace", func(e *Entity) { e.Namespace = "" }, "namespace"},
		{"confidence < 0", func(e *Entity) { e.Confidence = -0.1 }, "confidence"},
		{"confidence > 1", func(e *Entity) { e.Confidence = 1.5 }, "confidence"},
		{"zero valid_from", func(e *Entity) { e.ValidFrom = time.Time{} }, "valid_from"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := validEntity()
			c.mut(e)
			err := ValidateEntity(e)
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			var verr *ValidationError
			if !errors.As(err, &verr) {
				t.Fatalf("expected *ValidationError, got %T", err)
			}
			if verr.Field != c.field {
				t.Errorf("expected field %q, got %q", c.field, verr.Field)
			}
		})
	}
}

func TestValidateRelationship_RejectsSelfLoop(t *testing.T) {
	r := &Relationship{
		Type:       RelDependsOn,
		FromID:     "ent_abc",
		ToID:       "ent_abc",
		Confidence: 0.9,
		ValidFrom:  time.Now(),
	}
	err := ValidateRelationship(r)
	if err == nil {
		t.Fatal("expected error for self-loop")
	}
	var verr *ValidationError
	if !errors.As(err, &verr) || verr.Field != "from_id/to_id" {
		t.Errorf("expected self-loop validation error, got %v", err)
	}
}

func TestDeterministicID_StableAcrossInstances(t *testing.T) {
	a := &Entity{Type: EntityService, CanonicalName: "x", Namespace: "y"}
	b := &Entity{Type: EntityService, CanonicalName: "x", Namespace: "y"}
	if a.DeterministicID() != b.DeterministicID() {
		t.Fatalf("ids differ: %s vs %s", a.DeterministicID(), b.DeterministicID())
	}
	c := &Entity{Type: EntityService, CanonicalName: "x", Namespace: "other"}
	if a.DeterministicID() == c.DeterministicID() {
		t.Fatal("ids should differ for different namespaces")
	}
}
