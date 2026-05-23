// Package planner converts a classified intent into a concrete plan that
// the serving layer can execute against zone4 + the analytical engines.
//
// In the full spec this would emit a Cypher query for the graph DB. We don't
// have Cypher — zone4 exposes typed read endpoints (GetEntity, Neighborhood,
// ListNamespace, ReadLog) — so the planner emits a Plan struct that names
// which call to make.
package planner

import (
	"errors"
	"strings"

	"archgraph/zone5/internal/intent"
	"archgraph/zone5/internal/zone4client"
)

type Action string

const (
	ActFetchNeighborhood Action = "FETCH_NEIGHBORHOOD"
	ActListNamespace     Action = "LIST_NAMESPACE"
	ActReadLog           Action = "READ_LOG"
	ActImpactAnalysis    Action = "IMPACT_ANALYSIS"
	ActHealthAudit       Action = "HEALTH_AUDIT"
)

// Plan tells the serving layer what to do. Only fields relevant to the
// chosen Action are populated.
type Plan struct {
	Action         Action               `json:"action"`
	EntityName     string               `json:"entity_name,omitempty"`
	Namespace      string               `json:"namespace,omitempty"`
	Depth          int                  `json:"depth,omitempty"`
	Direction      zone4client.Direction `json:"direction,omitempty"`
	FromEntryID    int64                `json:"from_entry_id,omitempty"`
	Notes          string               `json:"notes,omitempty"`
}

// Request is everything the serving layer passes the planner. The HTTP
// handler builds this from the request body; the planner enriches it via
// the intent classifier and falls back to sensible defaults.
type Request struct {
	Question   string
	Namespace  string
	EntityName string // optional explicit hint
	Depth      int    // optional
}

// ErrMissingNamespace is returned when an archetype needs a namespace but
// the request didn't supply one. We refuse to invent one — the spec is firm
// that multi-tenant isolation is mandatory and a missing namespace would
// scan across tenants.
var ErrMissingNamespace = errors.New("namespace required for this query archetype")

// Plan classifies the question and produces the execution plan.
func Make(req Request) (Plan, intent.Classification, error) {
	cls := intent.Classify(req.Question)

	// Try to lift an entity name from the question if the caller didn't
	// supply one. Heuristic: backticked identifier or quoted phrase.
	entity := req.EntityName
	if entity == "" {
		entity = extractEntityHint(req.Question)
	}

	depth := req.Depth
	if depth == 0 {
		depth = 2
	}

	switch cls.Archetype {
	case intent.ArchImpact:
		if entity == "" {
			return Plan{}, cls, errors.New("impact analysis needs an entity (use entity_name)")
		}
		return Plan{
			Action:     ActImpactAnalysis,
			EntityName: entity,
			Namespace:  req.Namespace,
			Depth:      maxOr(depth, 5),
		}, cls, nil

	case intent.ArchGovernance:
		if req.Namespace == "" {
			return Plan{}, cls, ErrMissingNamespace
		}
		return Plan{Action: ActHealthAudit, Namespace: req.Namespace}, cls, nil

	case intent.ArchTemporal:
		return Plan{
			Action:    ActReadLog,
			Namespace: req.Namespace,
			Notes:     "temporal queries currently replay the delta log; snapshots are not yet wired up",
		}, cls, nil

	case intent.ArchRuntime, intent.ArchStructural:
		fallthrough
	default:
		if entity == "" {
			return Plan{}, cls, errors.New("structural query needs an entity (use entity_name or backtick it in the question)")
		}
		return Plan{
			Action:     ActFetchNeighborhood,
			EntityName: entity,
			Namespace:  req.Namespace,
			Depth:      depth,
			Direction:  zone4client.DirBoth,
		}, cls, nil
	}
}

func maxOr(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// extractEntityHint pulls a `backticked` or "quoted" entity name out of the
// question if present. Returns "" if nothing obvious is there.
func extractEntityHint(q string) string {
	for _, delim := range []string{"`", "\""} {
		if i := strings.Index(q, delim); i >= 0 {
			rest := q[i+len(delim):]
			if j := strings.Index(rest, delim); j > 0 {
				return strings.TrimSpace(rest[:j])
			}
		}
	}
	return ""
}
