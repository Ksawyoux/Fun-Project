package wiki

import (
	"fmt"
	"regexp"
	"strings"

	"archgraph/serving/internal/storageclient"
)

// Citation is a resolvable reference from wiki prose to a graph entity and its
// source location. Handles (c1, c2, …) are short, LLM-friendly tokens we ask
// the model to cite with; rewriteCitations turns them into deep code links.
type Citation struct {
	Handle string `json:"handle"`
	ID     string `json:"id"`
	Name   string `json:"name"`
	Type   string `json:"type"`
	File   string `json:"file,omitempty"`
	Line   int    `json:"line,omitempty"`
}

// codeLinkOf returns the repo-relative href for an entity's source location,
// e.g. "serving/internal/reasoner/reasoner.go#L42", or "" if the entity has no
// file provenance. The web reader resolves these against a configurable repo
// base (GitHub URL or local path).
func codeLinkOf(c Citation) string {
	if c.File == "" {
		return ""
	}
	if c.Line > 0 {
		return fmt.Sprintf("%s#L%d", c.File, c.Line)
	}
	return c.File
}

func citationOf(handle string, e *storageclient.Entity) Citation {
	c := Citation{
		Handle: handle,
		ID:     e.ID,
		Name:   e.CanonicalName,
		Type:   e.Type,
		File:   strProp(e.Properties, "file", "path"),
	}
	c.Line = intProp(e.Properties, "line", "start_line", "lineno")
	return c
}

func intProp(p map[string]any, keys ...string) int {
	for _, k := range keys {
		v, ok := p[k]
		if !ok {
			continue
		}
		switch n := v.(type) {
		case float64: // JSON numbers decode to float64
			return int(n)
		case int:
			return n
		case int64:
			return int(n)
		}
	}
	return 0
}

var citationToken = regexp.MustCompile(`\[\[([a-zA-Z0-9_-]+)\]\]`)

// rewriteCitations replaces [[handle]] tokens the LLM emitted with markdown
// links to the cited entity's source file (deep code links). Unknown handles
// are left as plain text (brackets stripped) so a hallucinated handle never
// produces a dead [[...]] artifact. Returns the rewritten text and the set of
// handles that actually resolved to a code link.
func rewriteCitations(text string, cites map[string]Citation) (string, []string) {
	used := map[string]bool{}
	out := citationToken.ReplaceAllStringFunc(text, func(tok string) string {
		m := citationToken.FindStringSubmatch(tok)
		if len(m) != 2 {
			return tok
		}
		c, ok := cites[m[1]]
		if !ok {
			return tok // leave unknown tokens untouched
		}
		if link := codeLinkOf(c); link != "" {
			used[m[1]] = true
			return fmt.Sprintf("[%s](%s)", c.Name, link)
		}
		return c.Name // known entity but no source location
	})
	usedList := make([]string, 0, len(used))
	for h := range used {
		usedList = append(usedList, h)
	}
	return out, usedList
}

// extractMermaid pulls the first ```mermaid fenced block out of a page so the
// API can expose it as a structured field. Mirrors reasoner.extractMermaid,
// duplicated here because that one is unexported.
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
