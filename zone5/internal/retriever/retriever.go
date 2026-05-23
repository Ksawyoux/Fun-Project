// Package retriever fetches a relevant subgraph for a query and prunes it
// to fit the LLM context budget.
//
// The spec describes a PageRank-style propagation. For MVP we use the
// score-on-the-path formula directly (Score(E) = Π weight(edge) × confidence(E))
// which captures the same shape without the eigenvector iteration: nodes
// connected by strong typed edges (EXPOSES, RUNTIME_CALLS) survive pruning;
// nodes reached only via weak edges (CALLS at symbol level) drop out.
package retriever

import (
	"context"
	"fmt"
	"sort"

	"archgraph/zone5/internal/zone4client"
)

// EdgeWeights are the per-edge-type defaults from Zone5.md §1.3.
var EdgeWeights = map[string]float64{
	"EXPOSES":             1.0,
	"RUNTIME_CALLS":       0.9,
	"IMPORTS":             0.7,
	"OWNS":                0.6,
	"DEPENDS_ON":          0.5,
	"CHANGE_COUPLED_WITH": 0.4,
	"CALLS":               0.2,
}

const (
	defaultWeight = 0.3 // For relationship types not in the table.
	dropThreshold = 0.15
	// Token budget for the assembled context, approximate. We use the rough
	// rule of thumb of 4 chars per token; this gets enforced by the
	// assembler. The retriever caps node count first because that's the
	// dimension we can measure cheaply.
	maxNodes = 200
)

// Subgraph is the retriever's output: the origin plus scored, pruned nodes
// and the edges that connect them.
type Subgraph struct {
	Origin *zone4client.Entity      `json:"origin"`
	Nodes  []*ScoredNode            `json:"nodes"`
	Edges  []*zone4client.Relationship `json:"edges"`
	// Stats for observability / debugging.
	Stats RetrieveStats `json:"stats"`
}

type ScoredNode struct {
	*zone4client.Entity
	Score float64 `json:"score"`
}

type RetrieveStats struct {
	NodesBeforePrune int `json:"nodes_before_prune"`
	NodesAfterPrune  int `json:"nodes_after_prune"`
	EdgesKept        int `json:"edges_kept"`
	RequestedDepth   int `json:"requested_depth"`
	AppliedDepth     int `json:"applied_depth"`
}

// Retrieve pulls the n-hop neighborhood around `originID` and scores every
// reachable node. If the size still exceeds maxNodes, depth is reduced by 1
// and the call retries — mirroring the spec's "Context Safety Guard".
func Retrieve(ctx context.Context, cl *zone4client.Client, originID string, depth int, dir zone4client.Direction) (*Subgraph, error) {
	if depth < 1 {
		depth = 1
	}
	if depth > 5 {
		depth = 5
	}
	requested := depth

	for {
		nb, err := cl.Neighborhood(ctx, originID, depth, dir)
		if err != nil {
			return nil, fmt.Errorf("neighborhood: %w", err)
		}
		sub := scoreAndPrune(nb)
		sub.Stats.RequestedDepth = requested
		sub.Stats.AppliedDepth = depth

		if len(sub.Nodes) <= maxNodes || depth <= 1 {
			return sub, nil
		}
		depth-- // shrink and retry — the spec's depth-back-off rule.
	}
}

func scoreAndPrune(nb *zone4client.Neighborhood) *Subgraph {
	// Index nodes by ID for quick lookup. The origin is always score=1.0.
	byID := map[string]*ScoredNode{
		nb.Origin.ID: {Entity: nb.Origin, Score: 1.0},
	}
	for _, n := range nb.Nodes {
		byID[n.ID] = &ScoredNode{Entity: n, Score: 0}
	}

	// Propagate scores along edges. One pass is enough for the spec's path
	// formula (multiplicative along edges) — we just relax until stable, in
	// practice ≤ depth iterations.
	for iter := 0; iter < 6; iter++ {
		changed := false
		for _, e := range nb.Edges {
			w := EdgeWeights[e.Type]
			if w == 0 {
				w = defaultWeight
			}
			// Score flows in both directions for retrieval purposes.
			if from, ok := byID[e.FromID]; ok {
				if to, ok2 := byID[e.ToID]; ok2 {
					candidate := from.Score * w * e.Confidence
					if candidate > to.Score {
						to.Score = candidate
						changed = true
					}
					candidate = to.Score * w * e.Confidence
					if candidate > from.Score && from.Entity.ID != nb.Origin.ID {
						from.Score = candidate
						changed = true
					}
				}
			}
		}
		if !changed {
			break
		}
	}

	// Collect, prune, sort.
	stats := RetrieveStats{NodesBeforePrune: len(byID) - 1} // -origin
	var kept []*ScoredNode
	keptIDs := map[string]struct{}{nb.Origin.ID: {}}
	for id, n := range byID {
		if id == nb.Origin.ID {
			continue
		}
		if n.Score < dropThreshold {
			continue
		}
		kept = append(kept, n)
		keptIDs[id] = struct{}{}
	}
	sort.Slice(kept, func(i, j int) bool { return kept[i].Score > kept[j].Score })

	// Hard cap on count — already pruned by score, this is the size safety.
	if len(kept) > maxNodes {
		for _, n := range kept[maxNodes:] {
			delete(keptIDs, n.ID)
		}
		kept = kept[:maxNodes]
	}
	stats.NodesAfterPrune = len(kept)

	// Keep only edges whose endpoints both survived pruning.
	var keptEdges []*zone4client.Relationship
	for _, e := range nb.Edges {
		_, fromOK := keptIDs[e.FromID]
		_, toOK := keptIDs[e.ToID]
		if fromOK && toOK {
			keptEdges = append(keptEdges, e)
		}
	}
	stats.EdgesKept = len(keptEdges)

	return &Subgraph{
		Origin: nb.Origin,
		Nodes:  kept,
		Edges:  keptEdges,
		Stats:  stats,
	}
}
