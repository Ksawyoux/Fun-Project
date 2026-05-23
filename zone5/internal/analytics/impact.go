// Package analytics holds the specialized engines that bypass the LLM:
// blast radius, architecture health, evolution diffing.
//
// All three operate on data fetched from Zone 4 via the HTTP client. They
// are deliberately deterministic and return structured reports — the LLM
// can layer narrative on top, but the numbers are computed here.
package analytics

import (
	"context"
	"fmt"
	"sort"

	"archgraph/zone5/internal/zone4client"
)

// PropagationDecay is the per-edge-type decay from Zone5.md §3.2.
var PropagationDecay = map[string]float64{
	"IMPORTS":           0.95,
	"RUNTIME_CALLS":     0.80,
	"DATA_COUPLED_WITH": 0.70,
	"DEPENDS_ON":        0.50,
}

const (
	defaultDecay      = 0.40 // unknown edge types are treated as weakly coupled
	impactPruneThresh = 0.10 // matches the spec's pruning constant
)

// BlastRadius is the report returned by the Impact Analyzer.
type BlastRadius struct {
	Origin   *zone4client.Entity `json:"origin"`
	Affected []AffectedNode      `json:"affected"`
	MaxDepth int                 `json:"max_depth"`
	// Total count is useful when the response is truncated for transport.
	TotalAffected int `json:"total_affected"`
}

type AffectedNode struct {
	NodeID           string  `json:"node_id"`
	CanonicalName    string  `json:"canonical_name"`
	Type             string  `json:"type"`
	Depth            int     `json:"depth"`
	ImpactProbability float64 `json:"impact_probability"`
}

// CalculateBlastRadius is the Go translation of Zone5.md §3.2's algorithm.
// We use zone4's Neighborhood endpoint with direction=in to fetch the
// callers in one round-trip per node — which is wasteful at high depth.
// Production would push the BFS down into Zone 4; for MVP this is fine.
func CalculateBlastRadius(ctx context.Context, cl *zone4client.Client, originID string, maxDepth int) (*BlastRadius, error) {
	if maxDepth < 1 {
		maxDepth = 5
	}
	if maxDepth > 5 {
		maxDepth = 5 // zone4 hard cap; respect it here too
	}

	origin, err := cl.GetEntity(ctx, originID)
	if err != nil {
		return nil, fmt.Errorf("origin lookup: %w", err)
	}

	type frame struct {
		nodeID string
		depth  int
		prob   float64
	}
	visited := map[string]float64{} // node_id → best (highest) probability seen
	queue := []frame{{originID, 0, 1.0}}
	affected := []AffectedNode{}

	for len(queue) > 0 {
		f := queue[0]
		queue = queue[1:]

		if prev, ok := visited[f.nodeID]; ok && prev >= f.prob {
			continue
		}
		visited[f.nodeID] = f.prob

		// Fetch this node and its inbound edges. The Neighborhood endpoint
		// with depth=1 gives us both at once.
		nb, err := cl.Neighborhood(ctx, f.nodeID, 1, zone4client.DirIn)
		if err != nil {
			return nil, fmt.Errorf("expand %s: %w", f.nodeID, err)
		}

		// Record the node itself (skipping the origin, which doesn't count
		// as "affected" — it's the cause).
		if f.nodeID != originID {
			affected = append(affected, AffectedNode{
				NodeID:            f.nodeID,
				CanonicalName:     nb.Origin.CanonicalName,
				Type:              nb.Origin.Type,
				Depth:             f.depth,
				ImpactProbability: f.prob,
			})
		}

		if f.depth >= maxDepth {
			continue
		}

		// Propagate to callers (incoming edges).
		for _, e := range nb.Edges {
			if e.ToID != f.nodeID {
				continue
			}
			decay := PropagationDecay[e.Type]
			if decay == 0 {
				decay = defaultDecay
			}
			next := f.prob * decay * e.Confidence
			if next <= impactPruneThresh {
				continue
			}
			queue = append(queue, frame{e.FromID, f.depth + 1, next})
		}
	}

	// Sort by impact descending — highest blast first.
	sort.Slice(affected, func(i, j int) bool {
		return affected[i].ImpactProbability > affected[j].ImpactProbability
	})

	return &BlastRadius{
		Origin:        origin,
		Affected:      affected,
		MaxDepth:      maxDepth,
		TotalAffected: len(affected),
	}, nil
}
