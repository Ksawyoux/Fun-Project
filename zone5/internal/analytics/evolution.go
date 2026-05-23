package analytics

import (
	"context"
	"fmt"
	"sort"
	"time"

	"archgraph/zone5/internal/zone4client"
)

// EvolutionReport summarizes what changed in a time window. The spec's
// reference algorithm diffs two graph snapshots. We don't have snapshots
// yet, so we approximate by replaying the delta log between t1 and t2.
// The shape of the report matches the spec's example output.
type EvolutionReport struct {
	From         time.Time     `json:"from"`
	To           time.Time     `json:"to"`
	DiffSummary  DiffSummary   `json:"diff_summary"`
	DriftAlerts  []DriftAlert  `json:"drift_alerts,omitempty"`
	RawEntries   int           `json:"raw_entries"`
}

type DiffSummary struct {
	NodesAdded    int `json:"nodes_added"`
	NodesRemoved  int `json:"nodes_removed"`
	NodesUpdated  int `json:"nodes_updated"`
	EdgesAdded    int `json:"edges_added"`
	EdgesRemoved  int `json:"edges_removed"`
	EdgesModified int `json:"edges_modified"`
}

// DriftAlert is the high-level annotation on top of raw counts. For MVP we
// emit one alert per CRITICAL pattern; in production this would be a
// dedicated rule engine.
type DriftAlert struct {
	Severity string `json:"severity"`
	Message  string `json:"message"`
	EntityID string `json:"entity_id,omitempty"`
}

// ComputeEvolution scans the delta log entries in [from, to] and tallies
// what was added/removed/updated. The Zone 4 log is monotonic — entry_ids
// only grow — so the order is reliable.
//
// `namespace` filters the report to a single workspace. Because the log
// stores transaction IDs but not namespaces, we filter after fetching the
// referenced entity. For MVP we accept the extra round-trips; production
// would denormalize namespace onto the log row.
func ComputeEvolution(ctx context.Context, cl *zone4client.Client, namespace string, from, to time.Time) (*EvolutionReport, error) {
	entries, err := cl.ReadLog(ctx, zone4client.ReadLogOpts{Limit: 1000})
	if err != nil {
		return nil, fmt.Errorf("read log: %w", err)
	}

	report := &EvolutionReport{From: from, To: to, RawEntries: len(entries)}

	for _, e := range entries {
		if !from.IsZero() && e.OccurredAt.Before(from) {
			continue
		}
		if !to.IsZero() && e.OccurredAt.After(to) {
			continue
		}
		// Filter by namespace when possible. The after_state usually carries
		// the namespace; fall back to before_state for deletes.
		if namespace != "" && !entryMatchesNamespace(e, namespace) {
			continue
		}

		switch e.MutationType {
		case "ENTITY_CREATED":
			report.DiffSummary.NodesAdded++
		case "ENTITY_UPDATED":
			report.DiffSummary.NodesUpdated++
		case "ENTITY_SOFT_DELETED":
			report.DiffSummary.NodesRemoved++
		case "RELATIONSHIP_CREATED":
			report.DiffSummary.EdgesAdded++
			if alert := relationshipDriftAlert(e); alert != nil {
				report.DriftAlerts = append(report.DriftAlerts, *alert)
			}
		case "RELATIONSHIP_UPDATED":
			report.DiffSummary.EdgesModified++
		case "RELATIONSHIP_DELETED":
			report.DiffSummary.EdgesRemoved++
		}
	}

	// Stable order for diff alerts so the response is deterministic.
	sort.SliceStable(report.DriftAlerts, func(i, j int) bool {
		return report.DriftAlerts[i].Severity < report.DriftAlerts[j].Severity
	})

	return report, nil
}

func entryMatchesNamespace(e zone4client.LogEntry, namespace string) bool {
	for _, src := range []map[string]any{e.AfterState, e.BeforeState} {
		if src == nil {
			continue
		}
		if ns, ok := src["namespace"].(string); ok && ns == namespace {
			return true
		}
	}
	return false
}

// relationshipDriftAlert spots one CRITICAL pattern: a new RUNTIME_CALLS
// edge appearing between services in the same namespace bypasses any
// gateway/topology rules. Production would have a config-driven rule set.
func relationshipDriftAlert(e zone4client.LogEntry) *DriftAlert {
	if e.AfterState == nil {
		return nil
	}
	if t, _ := e.AfterState["type"].(string); t != "RUNTIME_CALLS" {
		return nil
	}
	return &DriftAlert{
		Severity: "HIGH",
		Message:  "new RUNTIME_CALLS edge introduced — verify it isn't bypassing the gateway",
		EntityID: e.RelationshipID,
	}
}
