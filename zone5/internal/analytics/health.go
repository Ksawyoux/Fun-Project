package analytics

import (
	"context"
	"fmt"
	"math"

	"archgraph/zone5/internal/zone4client"
)

// HealthReport collects all detected smells in a namespace.
type HealthReport struct {
	Namespace string  `json:"namespace"`
	Smells    []Smell `json:"smells"`
	Summary   struct {
		Entities      int `json:"entities"`
		Relationships int `json:"relationships"`
		CyclesFound   int `json:"cycles_found"`
		Supernodes    int `json:"supernodes"`
	} `json:"summary"`
}

// Smell is a single rule violation. The Severity strings follow the spec:
// CRITICAL > HIGH > MEDIUM > LOW.
type Smell struct {
	Type     string   `json:"type"`
	Severity string   `json:"severity"`
	Message  string   `json:"message"`
	Nodes    []string `json:"nodes,omitempty"` // canonical names for human reading
	NodeIDs  []string `json:"node_ids,omitempty"`
}

// Audit runs every smell rule against a namespace. Pulling the whole
// namespace upfront (one ListNamespace call) is wasteful for very large
// workspaces, but it gives us index-level access — which is the right
// trade for cycle detection (Tarjan needs the full edge set).
func Audit(ctx context.Context, cl *zone4client.Client, namespace string) (*HealthReport, error) {
	listing, err := cl.ListNamespace(ctx, namespace)
	if err != nil {
		return nil, fmt.Errorf("list namespace: %w", err)
	}

	report := &HealthReport{Namespace: namespace}
	report.Summary.Entities = len(listing.Entities)
	report.Summary.Relationships = len(listing.Relationships)

	// Build an adjacency list for cycle detection. We keep both the typed
	// and the symbol-level edges; in production we might filter to only
	// IMPORTS / DEPENDS_ON / RUNTIME_CALLS to avoid false positives from
	// e.g. CHANGE_COUPLED_WITH (which is statistical, not structural).
	adj := map[string][]string{}
	for _, r := range listing.Relationships {
		adj[r.FromID] = append(adj[r.FromID], r.ToID)
	}

	// Index entities by ID for name lookup in smell reports.
	entIdx := map[string]*zone4client.Entity{}
	for _, e := range listing.Entities {
		entIdx[e.ID] = e
	}

	// --- Circular dependencies (Tarjan's SCC, size ≥ 2 or self-loop) ---
	for _, scc := range tarjanSCC(adj) {
		isCycle := len(scc) >= 2 ||
			(len(scc) == 1 && containsSelfLoop(adj, scc[0]))
		if !isCycle {
			continue
		}
		names := make([]string, 0, len(scc))
		for _, id := range scc {
			if e, ok := entIdx[id]; ok {
				names = append(names, e.CanonicalName)
			} else {
				names = append(names, id)
			}
		}
		report.Smells = append(report.Smells, Smell{
			Type:     "circular_dependency",
			Severity: "CRITICAL",
			Message:  fmt.Sprintf("circular dependency of size %d", len(scc)),
			Nodes:    names,
			NodeIDs:  scc,
		})
		report.Summary.CyclesFound++
	}

	// --- Supernode detection: degree > mean + 4σ ---
	if len(listing.Entities) >= 4 {
		degrees := map[string]int{}
		for _, r := range listing.Relationships {
			degrees[r.FromID]++
			degrees[r.ToID]++
		}
		var sum, sumSq float64
		var n float64
		for _, e := range listing.Entities {
			d := float64(degrees[e.ID])
			sum += d
			sumSq += d * d
			n++
		}
		if n > 0 {
			mean := sum / n
			variance := (sumSq / n) - (mean * mean)
			std := 0.0
			if variance > 0 {
				std = math.Sqrt(variance)
			}
			threshold := mean + 4*std
			for _, e := range listing.Entities {
				d := float64(degrees[e.ID])
				if d > threshold && d > 3 { // ignore tiny graphs where threshold collapses
					report.Smells = append(report.Smells, Smell{
						Type:     "supernode",
						Severity: "MEDIUM",
						Message:  fmt.Sprintf("entity has %d edges (mean %.1f, σ %.1f) — possible bottleneck", int(d), mean, std),
						Nodes:    []string{e.CanonicalName},
						NodeIDs:  []string{e.ID},
					})
					report.Summary.Supernodes++
				}
			}
		}
	}

	// --- Shared database coupling ---
	// Two distinct services that both READ_FROM or WRITE_TO the same table.
	type dbAccess struct {
		serviceID string
		access    string
	}
	tableAccess := map[string][]dbAccess{}
	for _, r := range listing.Relationships {
		if r.Type != "READS_FROM" && r.Type != "WRITES_TO" {
			continue
		}
		fromE, ok := entIdx[r.FromID]
		if !ok || fromE.Type != "SERVICE" {
			continue
		}
		tableAccess[r.ToID] = append(tableAccess[r.ToID], dbAccess{serviceID: r.FromID, access: r.Type})
	}
	for tableID, accesses := range tableAccess {
		services := map[string]struct{}{}
		for _, a := range accesses {
			services[a.serviceID] = struct{}{}
		}
		if len(services) < 2 {
			continue
		}
		tbl, ok := entIdx[tableID]
		tableName := tableID
		if ok {
			tableName = tbl.CanonicalName
		}
		names := []string{}
		ids := []string{}
		for sid := range services {
			ids = append(ids, sid)
			if s, ok := entIdx[sid]; ok {
				names = append(names, s.CanonicalName)
			} else {
				names = append(names, sid)
			}
		}
		report.Smells = append(report.Smells, Smell{
			Type:     "shared_database_coupling",
			Severity: "HIGH",
			Message:  fmt.Sprintf("%d services access table %s — violates microservice isolation", len(services), tableName),
			Nodes:    append([]string{tableName}, names...),
			NodeIDs:  append([]string{tableID}, ids...),
		})
	}

	return report, nil
}

func containsSelfLoop(adj map[string][]string, id string) bool {
	for _, n := range adj[id] {
		if n == id {
			return true
		}
	}
	return false
}
