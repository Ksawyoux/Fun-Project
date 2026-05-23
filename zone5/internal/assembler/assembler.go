// Package assembler turns a retrieved Subgraph into a Markdown context
// payload suitable for handing to an LLM.
//
// Format follows the example in Zone5.md §2.1 — hierarchical sections with
// explicit IDs the LLM can cite. We embed entity IDs as inline code so the
// Answer Formatter can re-link them after generation.
package assembler

import (
	"fmt"
	"sort"
	"strings"

	"archgraph/zone5/internal/retriever"
	"archgraph/zone5/internal/zone4client"
)

// Assemble produces the final markdown payload + a citation index the
// Answer Formatter consumes. The citation index lets the formatter rewrite
// `[id]` references back into resolvable links even if the LLM hallucinates
// IDs that weren't in the context.
type Output struct {
	Markdown  string                 `json:"markdown"`
	Citations map[string]CitationRef `json:"citations"`
}

type CitationRef struct {
	ID            string `json:"id"`
	CanonicalName string `json:"canonical_name"`
	Type          string `json:"type"`
	Namespace     string `json:"namespace"`
}

// Approximate budget for the assembled context. Hard cap — beyond this we
// truncate the lowest-scoring nodes. Uses the rule-of-thumb 4-chars/token
// from the spec.
const maxChars = 64_000 * 4

func Assemble(sub *retriever.Subgraph) Output {
	var b strings.Builder
	cites := map[string]CitationRef{}

	if sub.Origin != nil {
		writeEntitySection(&b, "TARGET ENTITY", sub.Origin, 1.0)
		cites[sub.Origin.ID] = refOf(sub.Origin)
	}

	// Group edges by direction relative to the origin so we can present
	// "callers / consumers" vs "dependencies" cleanly.
	var inbound, outbound []*zone4client.Relationship
	for _, e := range sub.Edges {
		if sub.Origin != nil && e.ToID == sub.Origin.ID {
			inbound = append(inbound, e)
		} else if sub.Origin != nil && e.FromID == sub.Origin.ID {
			outbound = append(outbound, e)
		}
	}

	nodeByID := map[string]*retriever.ScoredNode{}
	for _, n := range sub.Nodes {
		nodeByID[n.ID] = n
		cites[n.ID] = refOf(n.Entity)
	}

	if len(inbound) > 0 {
		b.WriteString("\n### ACTIVE CALLERS / DEPENDENTS\n")
		writeEdgeList(&b, inbound, nodeByID, "from")
	}
	if len(outbound) > 0 {
		b.WriteString("\n### OUTBOUND DEPENDENCIES\n")
		writeEdgeList(&b, outbound, nodeByID, "to")
	}

	// Anything else in the subgraph that's neither inbound nor outbound
	// (multi-hop neighbors) — list them as related context.
	directIDs := map[string]struct{}{}
	for _, e := range inbound {
		directIDs[e.FromID] = struct{}{}
	}
	for _, e := range outbound {
		directIDs[e.ToID] = struct{}{}
	}
	var related []*retriever.ScoredNode
	for _, n := range sub.Nodes {
		if _, ok := directIDs[n.ID]; ok {
			continue
		}
		related = append(related, n)
	}
	if len(related) > 0 {
		sort.Slice(related, func(i, j int) bool { return related[i].Score > related[j].Score })
		b.WriteString("\n### RELATED CONTEXT (multi-hop)\n")
		for _, n := range related {
			fmt.Fprintf(&b, "- `%s` — %s/%s (type: %s, score: %.2f)\n",
				n.ID, n.Namespace, n.CanonicalName, n.Type, n.Score)
		}
	}

	out := b.String()
	if len(out) > maxChars {
		out = out[:maxChars] + "\n\n…(truncated)"
	}
	return Output{Markdown: out, Citations: cites}
}

func writeEntitySection(b *strings.Builder, header string, e *zone4client.Entity, score float64) {
	fmt.Fprintf(b, "### %s\n", header)
	fmt.Fprintf(b, "- **ID:** `%s`\n", e.ID)
	fmt.Fprintf(b, "- **Type:** %s", e.Type)
	if e.SubType != "" {
		fmt.Fprintf(b, " (%s)", e.SubType)
	}
	fmt.Fprintln(b)
	fmt.Fprintf(b, "- **Canonical Name:** `%s`\n", e.CanonicalName)
	fmt.Fprintf(b, "- **Namespace:** `%s`\n", e.Namespace)
	fmt.Fprintf(b, "- **Confidence:** %.2f\n", e.Confidence)
	if !e.IsActive {
		fmt.Fprintf(b, "- **Status:** SOFT-DELETED (lifecycle: %s)\n", e.LifecycleStage)
	}
	if len(e.Properties) > 0 {
		fmt.Fprintln(b, "- **Properties:**")
		for k, v := range e.Properties {
			fmt.Fprintf(b, "  - %s: %v\n", k, v)
		}
	}
}

func writeEdgeList(b *strings.Builder, edges []*zone4client.Relationship, nodeByID map[string]*retriever.ScoredNode, peerField string) {
	// Sort edges by peer score descending for readability.
	sort.SliceStable(edges, func(i, j int) bool {
		var si, sj float64
		if peerField == "from" {
			if n, ok := nodeByID[edges[i].FromID]; ok {
				si = n.Score
			}
			if n, ok := nodeByID[edges[j].FromID]; ok {
				sj = n.Score
			}
		} else {
			if n, ok := nodeByID[edges[i].ToID]; ok {
				si = n.Score
			}
			if n, ok := nodeByID[edges[j].ToID]; ok {
				sj = n.Score
			}
		}
		return si > sj
	})
	for _, e := range edges {
		peerID := e.ToID
		if peerField == "from" {
			peerID = e.FromID
		}
		peer, ok := nodeByID[peerID]
		if !ok {
			fmt.Fprintf(b, "- (orphan) `%s` via %s (confidence %.2f)\n", peerID, e.Type, e.Confidence)
			continue
		}
		fmt.Fprintf(b, "- **Entity:** `%s` (`%s`)\n", peer.CanonicalName, peer.ID)
		fmt.Fprintf(b, "  - **Relationship:** %s (confidence %.2f)\n", e.Type, e.Confidence)
		if len(e.Properties) > 0 {
			fmt.Fprintln(b, "  - **Edge Properties:**")
			for k, v := range e.Properties {
				fmt.Fprintf(b, "    - %s: %v\n", k, v)
			}
		}
	}
}

func refOf(e *zone4client.Entity) CitationRef {
	return CitationRef{
		ID:            e.ID,
		CanonicalName: e.CanonicalName,
		Type:          e.Type,
		Namespace:     e.Namespace,
	}
}
