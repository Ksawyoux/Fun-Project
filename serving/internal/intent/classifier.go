// Package intent maps natural-language questions to one of five Query
// Archetypes from the spec.
//
// The spec calls for an embedding-based router. For MVP we use rule-based
// keyword matching — good enough for the canonical example queries and
// trivially swappable behind the same Classify() interface once we have
// embeddings infra.
package intent

import "strings"

type Archetype string

const (
	ArchStructural Archetype = "STRUCTURAL"
	ArchRuntime    Archetype = "RUNTIME"
	ArchTemporal   Archetype = "TEMPORAL"
	ArchImpact     Archetype = "IMPACT"
	ArchGovernance Archetype = "GOVERNANCE"
)

// Classification carries the archetype plus crude parameter extraction.
// Callers (the planner) are responsible for resolving entity names to IDs.
type Classification struct {
	Archetype Archetype `json:"archetype"`
	// Hints the planner uses. All optional — caller may also pass parameters
	// directly via the HTTP request body.
	EntityName string  `json:"entity_name,omitempty"`
	Depth      int     `json:"depth,omitempty"`
	Confidence float64 `json:"confidence"`
}

// Classify is the entry point. The signal is intentionally weak — the spec's
// own examples often map to multiple archetypes. We return the first match
// in priority order: GOVERNANCE > IMPACT > TEMPORAL > RUNTIME > STRUCTURAL.
// This ordering is deliberate: specific archetypes (impact, governance) win
// over generic ones (structural) because they trigger dedicated engines
// with cheaper, more accurate paths.
func Classify(question string) Classification {
	q := strings.ToLower(question)

	switch {
	case hasAny(q, "circular", "cycle", "supernode", "hub-and-spoke", "smell", "audit", "health"):
		return Classification{Archetype: ArchGovernance, Confidence: 0.9}

	case hasAny(q, "blast radius", "what breaks", "what would break", "impact of", "if i delete", "if we delete", "if i remove"):
		return Classification{Archetype: ArchImpact, Confidence: 0.9}

	case hasAny(q, "change", "evolution", "history", "diff", "between commit", "since ", "last month", "last week", "past month", "yesterday", " ago"):
		return Classification{Archetype: ArchTemporal, Confidence: 0.8}

	case hasAny(q, "latency", "slow", "slowest", "p99", "p50", "error rate", "throughput"):
		return Classification{Archetype: ArchRuntime, Confidence: 0.8}

	default:
		// Structural is the catch-all because "what depends on X / what
		// calls X / dependencies of X" is the dominant developer question.
		return Classification{Archetype: ArchStructural, Confidence: 0.6}
	}
}

func hasAny(s string, needles ...string) bool {
	for _, n := range needles {
		if strings.Contains(s, n) {
			return true
		}
	}
	return false
}
