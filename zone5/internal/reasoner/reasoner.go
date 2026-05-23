// Package reasoner is the LLM-facing layer. The Reasoner takes assembled
// Markdown context and produces a final answer with citations.
//
// We define LLM as an interface so a real Claude/Anthropic client can be
// plugged in later. The default StubLLM produces a deterministic structured
// answer derived from the context — no model call. Useful for tests, for
// offline operation, and as a baseline the real LLM has to beat.
package reasoner

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"archgraph/zone5/internal/assembler"
)

// LLM is the abstraction every model adapter implements.
type LLM interface {
	Complete(ctx context.Context, systemPrompt, userPrompt string) (string, error)
}

// SystemPrompt is the rules from Zone5.md §2.2 verbatim. Don't tweak it
// casually — the rules around citations and "no hallucination" affect the
// shape of every answer the system produces.
const SystemPrompt = `You are an expert Software Architect analyzing a live AI Codebase Knowledge Graph.
Your task is to answer user queries using ONLY the provided serialized context.

Rules:
1. Citations: You MUST cite the source of every fact. Use markdown links: [Name](file:///path/to/source#line) or node IDs [node-id].
2. Code references: When referencing files or schemas, use the exact file URLs provided in the context.
3. Hallucinations: If the context does not contain enough information to resolve a query, state explicitly: "I do not have the telemetry/code context to answer this."
4. Mermaid diagrams: When explaining structures, generate a clean Mermaid.js diagram to visualize nodes and edges.
5. Analytical Tone: Keep the analysis highly objective and technical. Focus on structural boundaries, failure modes, and coupling.`

// Answer is the public shape returned by the Reasoner.
type Answer struct {
	Text           string                              `json:"text"`
	MermaidDiagram string                              `json:"mermaid_diagram,omitempty"`
	Citations      map[string]assembler.CitationRef    `json:"citations"`
	Confidence     float64                             `json:"confidence"`
	UsedLLM        string                              `json:"used_llm"`
}

// Reasoner ties the LLM, assembled context, and the user's question together.
type Reasoner struct {
	llm     LLM
	llmName string
}

func New(llm LLM, llmName string) *Reasoner {
	return &Reasoner{llm: llm, llmName: llmName}
}

func (r *Reasoner) Answer(ctx context.Context, question string, asm assembler.Output) (*Answer, error) {
	user := fmt.Sprintf("## Question\n%s\n\n## Context\n%s\n", question, asm.Markdown)
	text, err := r.llm.Complete(ctx, SystemPrompt, user)
	if err != nil {
		return nil, fmt.Errorf("llm complete: %w", err)
	}
	mermaid := extractMermaid(text)
	conf := 0.7
	if len(asm.Citations) == 0 {
		conf = 0.3
	}
	return &Answer{
		Text:           text,
		MermaidDiagram: mermaid,
		Citations:      asm.Citations,
		Confidence:     conf,
		UsedLLM:        r.llmName,
	}, nil
}

// extractMermaid pulls the first ```mermaid block out of the answer text so
// the API can return it as a structured field. The Answer Formatter would
// validate the syntax; for MVP we just trust it.
func extractMermaid(s string) string {
	const fence = "```mermaid"
	i := strings.Index(s, fence)
	if i < 0 {
		return ""
	}
	rest := s[i+len(fence):]
	end := strings.Index(rest, "```")
	if end < 0 {
		return ""
	}
	return strings.TrimSpace(rest[:end])
}

// ---------- StubLLM ----------

// StubLLM is the default implementation. It produces a deterministic answer
// by re-summarizing the context section by section. The real LLM does NOT
// have to beat the stub on summarization quality — the stub exists so the
// whole pipeline works end-to-end without an API key, and so tests are
// deterministic.
type StubLLM struct{}

func (StubLLM) Complete(_ context.Context, _, user string) (string, error) {
	target, callers, deps, related := splitContext(user)

	var b strings.Builder
	b.WriteString("## Structural Summary (stub reasoner)\n\n")
	if target != "" {
		b.WriteString("**Target:** ")
		b.WriteString(target)
		b.WriteString("\n\n")
	}
	if callers != "" {
		fmt.Fprintf(&b, "**Inbound dependencies (%d):**\n%s\n", strings.Count(callers, "Entity"), callers)
	}
	if deps != "" {
		fmt.Fprintf(&b, "**Outbound dependencies (%d):**\n%s\n", strings.Count(deps, "Entity"), deps)
	}
	if related != "" {
		fmt.Fprintf(&b, "**Multi-hop related:**\n%s\n", related)
	}
	b.WriteString("\n_This response was produced by the stub reasoner. To get LLM-quality analysis and a Mermaid diagram, configure a real LLM adapter._\n")
	return b.String(), nil
}

// splitContext is a crude parser of the assembled markdown so the stub can
// recompose it without an LLM. It looks for the section headers we know
// the assembler emits.
func splitContext(s string) (target, callers, deps, related string) {
	sections := map[string]*string{
		"### TARGET ENTITY":                  &target,
		"### ACTIVE CALLERS / DEPENDENTS":    &callers,
		"### OUTBOUND DEPENDENCIES":          &deps,
		"### RELATED CONTEXT (multi-hop)":    &related,
	}
	type cut struct {
		header string
		start  int
	}
	var cuts []cut
	for h := range sections {
		if i := strings.Index(s, h); i >= 0 {
			cuts = append(cuts, cut{h, i})
		}
	}
	sort.Slice(cuts, func(i, j int) bool { return cuts[i].start < cuts[j].start })
	for i, c := range cuts {
		end := len(s)
		if i+1 < len(cuts) {
			end = cuts[i+1].start
		}
		body := strings.TrimSpace(s[c.start+len(c.header) : end])
		*sections[c.header] = body
	}
	return
}
