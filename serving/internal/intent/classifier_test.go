package intent

import "testing"

func TestClassify_KeywordPriority(t *testing.T) {
	cases := []struct {
		question string
		want     Archetype
	}{
		{"What are the dependencies of `payment-service`?", ArchStructural},
		{"What breaks if I delete UserTable?", ArchImpact},
		{"Do we have any circular dependencies in `acme`?", ArchGovernance},
		{"How did billing change in the last month?", ArchTemporal},
		{"Which endpoint of auth-service is slowest by p99?", ArchRuntime},
		{"audit my workspace please", ArchGovernance},
		{"blast radius of removing the orders queue", ArchImpact},
		// Ambiguous structural fallback
		{"tell me about checkout", ArchStructural},
	}
	for _, c := range cases {
		got := Classify(c.question).Archetype
		if got != c.want {
			t.Errorf("Classify(%q): got %s, want %s", c.question, got, c.want)
		}
	}
}

func TestClassify_GovernanceBeatsImpact(t *testing.T) {
	// "circular" should win over "if I delete" — governance is more specific.
	q := "if I delete a service, do we have circular dependencies"
	if got := Classify(q).Archetype; got != ArchGovernance {
		t.Errorf("expected GOVERNANCE for %q, got %s", q, got)
	}
}
