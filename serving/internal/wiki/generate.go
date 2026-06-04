package wiki

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"archgraph/serving/internal/reasoner"
	"archgraph/serving/internal/storageclient"
)

// Page is one rendered subsystem article. Markdown is the LLM-authored, code-
// linked body (mermaid block kept inline so it renders in place); Mermaid
// duplicates the first diagram as a structured field for API consumers.
type Page struct {
	TopicID    string     `json:"topic_id"`
	Title      string     `json:"title"`
	Summary    string     `json:"summary"`
	Markdown   string     `json:"markdown"`
	Mermaid    string     `json:"mermaid,omitempty"`
	Citations  []Citation `json:"citations,omitempty"`
	EntityHash string     `json:"entity_hash"`
	UsedLLM    string     `json:"used_llm"`
	Cached     bool       `json:"cached"`
}

// Wiki is the full generated artifact: a table of contents (Topics) plus one
// Page per topic.
type Wiki struct {
	Namespace   string          `json:"namespace"`
	GeneratedAt time.Time       `json:"generated_at"`
	Topics      []Topic         `json:"topics"`
	Pages       map[string]Page `json:"pages"`
}

// Generator owns the dependencies needed to turn the graph into a wiki: a
// storage client to read the namespace, an LLM to author prose, and a cache
// dir so unchanged topics aren't re-generated (each page is one LLM call).
type Generator struct {
	cl       *storageclient.Client
	llm      reasoner.LLM
	llmName  string
	cacheDir string
	opts     SegmentOptions
}

func NewGenerator(cl *storageclient.Client, llm reasoner.LLM, llmName, cacheDir string) *Generator {
	return &Generator{cl: cl, llm: llm, llmName: llmName, cacheDir: cacheDir}
}

// WikiSystemPrompt steers the LLM toward Code-Wiki house style: a narrative
// subsystem article, grounded strictly in the provided catalog, with deep code
// links via citation handles and exactly one architecture diagram.
const WikiSystemPrompt = `You are a senior software architect writing a documentation wiki for a codebase.
You will be given a CATALOG of the components in one subsystem, drawn from a knowledge graph.
Write a clear, engaging wiki page that explains this subsystem to a new engineer.

Rules:
1. First line MUST be exactly "SUMMARY: <one sentence describing the subsystem>". Nothing before it.
2. After the summary line, write the article in Markdown with these sections:
   - "## Overview": 1-2 paragraphs on the subsystem's responsibility and how its pieces fit together.
   - "## Key Components": describe the most important components in prose (not a bare list).
   - "## Architecture": exactly one ` + "```mermaid```" + ` diagram showing the main components and their relationships.
3. Grounding: use ONLY facts from the catalog. Do not invent components, files, or behavior. If unsure, say less.
4. Citations: whenever you mention a component that has a citation handle in the catalog, cite it inline using its handle in double brackets, e.g. [[c3]]. These become links to the source code. Cite generously but only with handles that appear in the catalog.
5. Tone: technical, concrete, and concise. Explain *why* boundaries exist and how data flows.`

// maxContextChars bounds the catalog handed to the LLM (rule of thumb 4
// chars/token). Beyond it we drop the lowest-priority entities.
const maxContextChars = 48_000

// maxComponentsPerPage bounds how many entities are catalogued (and citable)
// per page so prompts stay fast even for very large subsystems.
const maxComponentsPerPage = 120

// Generate builds (or loads from cache) the full wiki for a namespace.
func (g *Generator) Generate(ctx context.Context, namespace string) (*Wiki, error) {
	listing, err := g.cl.ListNamespace(ctx, namespace)
	if err != nil {
		return nil, fmt.Errorf("list namespace: %w", err)
	}
	topics := Segment(listing, g.opts)

	index := make(map[string]*storageclient.Entity, len(listing.Entities))
	for _, e := range listing.Entities {
		if e != nil {
			index[e.ID] = e
		}
	}

	wiki := &Wiki{
		Namespace:   namespace,
		GeneratedAt: time.Now().UTC(),
		Pages:       make(map[string]Page, len(topics)),
	}

	for i := range topics {
		topic := topics[i]
		hash := entityHash(topic.EntityIDs, index)

		// Cache hit: identical entity set → reuse the page, skip the LLM call.
		if cached, ok := g.loadPage(namespace, topic.ID); ok && cached.EntityHash == hash {
			cached.Cached = true
			wiki.Pages[topic.ID] = *cached
			topics[i].Summary = cached.Summary
			continue
		}

		page, fallback := g.generatePage(ctx, topic, index, listing.Relationships, hash)
		// Cache only real LLM pages — fallbacks should be retried next run.
		if !fallback {
			g.savePage(namespace, *page)
		}
		wiki.Pages[topic.ID] = *page
		topics[i].Summary = page.Summary
	}

	wiki.Topics = topics
	return wiki, nil
}

// generatePage authors one subsystem page. It never returns an error: if the
// LLM fails (timeout, not configured, etc.) it degrades to a deterministic
// outline so the wiki always renders. The bool reports whether the result is
// such a fallback (fallbacks are not cached, so they retry next run).
func (g *Generator) generatePage(ctx context.Context, topic Topic, index map[string]*storageclient.Entity, rels []*storageclient.Relationship, hash string) (*Page, bool) {
	catalog, cites := buildCatalog(topic, index, rels)
	user := fmt.Sprintf("## Subsystem: %s (directory: %s)\n\n%s", topic.Title, topic.Dir, catalog)

	raw, err := g.llm.Complete(ctx, WikiSystemPrompt, user)
	if err != nil {
		return fallbackPage(topic, cites, hash, g.llmName, err), true
	}

	summary, body := splitSummary(raw)
	body, usedHandles := rewriteCitations(body, cites)

	page := &Page{
		TopicID:    topic.ID,
		Title:      topic.Title,
		Summary:    summary,
		Markdown:   body,
		Mermaid:    extractMermaid(body),
		Citations:  usedCitations(cites, usedHandles),
		EntityHash: hash,
		UsedLLM:    g.llmName,
	}
	return page, false
}

// fallbackPage renders a deterministic outline (components grouped by type,
// each a deep code link) when the LLM is unavailable — graceful degradation so
// a single timeout never blanks the wiki.
func fallbackPage(topic Topic, cites map[string]Citation, hash, llmName string, cause error) *Page {
	byType := map[string][]Citation{}
	var resolved []Citation
	for _, c := range cites {
		byType[c.Type] = append(byType[c.Type], c)
		if codeLinkOf(c) != "" {
			resolved = append(resolved, c)
		}
	}
	types := make([]string, 0, len(byType))
	for t := range byType {
		types = append(types, t)
	}
	sort.Strings(types)

	var b strings.Builder
	fmt.Fprintf(&b, "## Overview\n\nAuto-generated outline for the **%s** subsystem (%d components). ", topic.Title, len(cites))
	b.WriteString("Narrative prose was unavailable for this page; the components and their source locations are listed below.\n")
	if cause != nil {
		fmt.Fprintf(&b, "\n> _LLM unavailable: %s_\n", cause)
	}
	b.WriteString("\n## Components\n")
	for _, t := range types {
		rows := byType[t]
		sort.Slice(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })
		fmt.Fprintf(&b, "\n### %s\n", t)
		for _, c := range rows {
			if link := codeLinkOf(c); link != "" {
				fmt.Fprintf(&b, "- [%s](%s)\n", c.Name, link)
			} else {
				fmt.Fprintf(&b, "- %s\n", c.Name)
			}
		}
	}
	sort.Slice(resolved, func(i, j int) bool { return resolved[i].Handle < resolved[j].Handle })
	return &Page{
		TopicID:    topic.ID,
		Title:      topic.Title,
		Summary:    fmt.Sprintf("%d components in %s (outline).", len(cites), topic.Title),
		Markdown:   b.String(),
		Citations:  resolved,
		EntityHash: hash,
		UsedLLM:    llmName + " (fallback)",
	}
}

// buildCatalog renders a deterministic markdown catalog of a topic's entities
// (grouped by type, each with a short citation handle and source location) plus
// the relationships among them — the grounding material the LLM authors from.
func buildCatalog(topic Topic, index map[string]*storageclient.Entity, rels []*storageclient.Relationship) (string, map[string]Citation) {
	cites := map[string]Citation{}
	inTopic := map[string]bool{}
	for _, id := range topic.EntityIDs {
		inTopic[id] = true
	}

	// Assign deterministic handles c1, c2, … in entity-ID order.
	ids := append([]string(nil), topic.EntityIDs...)
	sort.Strings(ids)
	handleByID := make(map[string]string, len(ids))
	type row struct {
		handle string
		e      *storageclient.Entity
	}
	byType := map[string][]row{}
	n := 0
	for _, id := range ids {
		e := index[id]
		if e == nil {
			continue
		}
		if n >= maxComponentsPerPage {
			break // keep prompts bounded on very large subsystems
		}
		n++
		h := fmt.Sprintf("c%d", n)
		handleByID[id] = h
		cites[h] = citationOf(h, e)
		byType[e.Type] = append(byType[e.Type], row{h, e})
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Subsystem **%s** contains %d components.\n\n", topic.Title, len(handleByID))
	b.WriteString("### Components (cite these with their handle)\n")

	types := make([]string, 0, len(byType))
	for t := range byType {
		types = append(types, t)
	}
	sort.Strings(types)
	for _, t := range types {
		fmt.Fprintf(&b, "\n**%s:**\n", t)
		for _, r := range byType[t] {
			loc := strProp(r.e.Properties, "file", "path")
			if ln := intProp(r.e.Properties, "line", "start_line", "lineno"); ln > 0 && loc != "" {
				loc = fmt.Sprintf("%s:%d", loc, ln)
			}
			if loc != "" {
				fmt.Fprintf(&b, "- [[%s]] `%s` — %s\n", r.handle, r.e.CanonicalName, loc)
			} else {
				fmt.Fprintf(&b, "- [[%s]] `%s`\n", r.handle, r.e.CanonicalName)
			}
		}
	}

	// Internal + outbound relationships give the LLM material for the diagram.
	var internal, outbound []string
	for _, rel := range rels {
		if rel == nil {
			continue
		}
		fromIn, toIn := inTopic[rel.FromID], inTopic[rel.ToID]
		switch {
		case fromIn && toIn:
			if fh, th := handleByID[rel.FromID], handleByID[rel.ToID]; fh != "" && th != "" {
				internal = append(internal, fmt.Sprintf("- [[%s]] %s [[%s]]", fh, rel.Type, th))
			}
		case fromIn && !toIn:
			if fh := handleByID[rel.FromID]; fh != "" {
				if peer := index[rel.ToID]; peer != nil {
					outbound = append(outbound, fmt.Sprintf("- [[%s]] %s `%s` (external)", fh, rel.Type, peer.CanonicalName))
				}
			}
		}
	}
	sort.Strings(internal)
	sort.Strings(outbound)
	if len(internal) > 0 {
		b.WriteString("\n### Internal relationships\n")
		b.WriteString(strings.Join(capStrings(internal, 120), "\n"))
		b.WriteString("\n")
	}
	if len(outbound) > 0 {
		b.WriteString("\n### Outbound dependencies\n")
		b.WriteString(strings.Join(capStrings(outbound, 60), "\n"))
		b.WriteString("\n")
	}

	out := b.String()
	if len(out) > maxContextChars {
		out = out[:maxContextChars] + "\n\n…(catalog truncated)"
	}
	return out, cites
}

func capStrings(s []string, n int) []string {
	if len(s) <= n {
		return s
	}
	return append(s[:n:n], "- …(more omitted)")
}

// splitSummary pulls the leading "SUMMARY: …" line out of the model output.
// Falls back to the first non-empty line if the model didn't comply.
func splitSummary(raw string) (summary, body string) {
	raw = strings.TrimSpace(raw)
	lines := strings.SplitN(raw, "\n", 2)
	first := strings.TrimSpace(lines[0])
	if strings.HasPrefix(first, "SUMMARY:") {
		summary = strings.TrimSpace(strings.TrimPrefix(first, "SUMMARY:"))
		if len(lines) > 1 {
			body = strings.TrimSpace(lines[1])
		}
		return summary, body
	}
	// Non-compliant model: synthesize a summary from the first sentence.
	summary = firstSentence(raw)
	return summary, raw
}

func firstSentence(s string) string {
	s = strings.TrimSpace(s)
	for _, cut := range []string{". ", ".\n", "\n"} {
		if i := strings.Index(s, cut); i > 0 && i < 240 {
			return strings.TrimSpace(s[:i+1])
		}
	}
	if len(s) > 200 {
		return s[:200] + "…"
	}
	return s
}

func usedCitations(cites map[string]Citation, handles []string) []Citation {
	out := make([]Citation, 0, len(handles))
	for _, h := range handles {
		if c, ok := cites[h]; ok {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Handle < out[j].Handle })
	return out
}
