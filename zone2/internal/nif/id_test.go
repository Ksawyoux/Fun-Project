package nif

import "testing"

func TestDeterministicEntityID_Stable(t *testing.T) {
	a := DeterministicEntityID("git", "repo1", EntityModule, "payments", "team-a")
	b := DeterministicEntityID("git", "repo1", EntityModule, "payments", "team-a")
	if a != b {
		t.Fatalf("expected stable ID, got %q vs %q", a, b)
	}
	if a == "" {
		t.Fatal("ID must be non-empty")
	}
}

func TestDeterministicEntityID_Differs(t *testing.T) {
	a := DeterministicEntityID("git", "repo1", EntityModule, "payments", "team-a")
	b := DeterministicEntityID("git", "repo1", EntityModule, "payments", "team-b") // different namespace
	if a == b {
		t.Fatal("expected different namespace to produce different ID")
	}
}

func TestDeterministicRelationshipID_Stable(t *testing.T) {
	a := DeterministicRelationshipID(RelDependsOn, "ent_a", "ent_b", "git")
	b := DeterministicRelationshipID(RelDependsOn, "ent_a", "ent_b", "git")
	if a != b {
		t.Fatalf("expected stable rel ID, got %q vs %q", a, b)
	}
}
