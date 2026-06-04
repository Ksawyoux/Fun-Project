package wiki

import (
	"context"
	"strings"
	"testing"

	"archgraph/serving/internal/storageclient"
)

func TestRewriteCitations_DeepLinks(t *testing.T) {
	cites := map[string]Citation{
		"c1": {Handle: "c1", Name: "reasoner.Answer", File: "serving/internal/reasoner/reasoner.go", Line: 56},
		"c2": {Handle: "c2", Name: "StubLLM", File: "serving/internal/reasoner/reasoner.go"}, // no line
		"c3": {Handle: "c3", Name: "NoFile"},                                                 // no file → plain text
	}
	in := "The [[c1]] method drives [[c2]], unlike [[c3]] and the unknown [[c9]]."
	out, used := rewriteCitations(in, cites)

	if !strings.Contains(out, "[reasoner.Answer](serving/internal/reasoner/reasoner.go#L56)") {
		t.Errorf("c1 should become a deep link with line: %q", out)
	}
	if !strings.Contains(out, "[StubLLM](serving/internal/reasoner/reasoner.go)") {
		t.Errorf("c2 should become a file link without line: %q", out)
	}
	if strings.Contains(out, "[[c3]]") || !strings.Contains(out, "NoFile") {
		t.Errorf("c3 (no file) should be plain text: %q", out)
	}
	if !strings.Contains(out, "[[c9]]") {
		t.Errorf("unknown handle c9 should be left untouched: %q", out)
	}
	if len(used) != 2 {
		t.Errorf("expected 2 resolved code links, got %d (%v)", len(used), used)
	}
}

func TestSplitSummary(t *testing.T) {
	s, body := splitSummary("SUMMARY: The serving layer answers queries.\n\n## Overview\nstuff")
	if s != "The serving layer answers queries." {
		t.Errorf("summary parse: %q", s)
	}
	if !strings.HasPrefix(body, "## Overview") {
		t.Errorf("body should start after summary line: %q", body)
	}

	s2, body2 := splitSummary("No summary marker here. More text.")
	if s2 == "" {
		t.Errorf("fallback summary should be non-empty")
	}
	if !strings.Contains(body2, "No summary marker") {
		t.Errorf("body should retain content when no marker: %q", body2)
	}
}

// fakeLLM echoes a deterministic page that cites the first two handles, so the
// generation pipeline can be exercised without a real model.
type fakeLLM struct{}

func (fakeLLM) Complete(_ context.Context, _, user string) (string, error) {
	// Pull two handles out of the catalog to prove citations get rewritten.
	h1, h2 := "c1", "c2"
	return "SUMMARY: A test subsystem.\n\n## Overview\nUses [[" + h1 + "]] and [[" + h2 + "]].\n\n## Architecture\n```mermaid\ngraph TD\n  A-->B\n```\n", nil
}

// errLLM always fails, to exercise graceful degradation.
type errLLM struct{}

func (errLLM) Complete(_ context.Context, _, _ string) (string, error) {
	return "", context.DeadlineExceeded
}

func TestGeneratePage_FallbackOnLLMError(t *testing.T) {
	topic := Topic{ID: "storage", Title: "Storage", Dir: "storage", EntityIDs: []string{"id-a", "id-b"}}
	index := map[string]*storageclient.Entity{
		"id-a": {ID: "id-a", Type: "FUNCTION", CanonicalName: "Put", Properties: map[string]any{"file": "storage/db.go", "line": float64(7)}},
		"id-b": {ID: "id-b", Type: "STRUCT", CanonicalName: "Store", Properties: map[string]any{"file": "storage/db.go"}},
	}
	g := NewGenerator(nil, errLLM{}, "claude-cli", "")
	page, fallback := g.generatePage(context.Background(), topic, index, nil, "h")
	if !fallback {
		t.Fatalf("expected fallback when LLM errors")
	}
	if !strings.Contains(page.Markdown, "storage/db.go#L7") {
		t.Errorf("fallback should still contain deep code links: %q", page.Markdown)
	}
	if !strings.Contains(page.UsedLLM, "fallback") {
		t.Errorf("fallback page should be marked: %q", page.UsedLLM)
	}
	if page.Summary == "" {
		t.Errorf("fallback should have a summary")
	}
}

func TestGeneratePage_EndToEnd(t *testing.T) {
	topic := Topic{ID: "serving", Title: "Serving", Dir: "serving", EntityIDs: []string{"id-a", "id-b"}}
	index := map[string]*storageclient.Entity{
		"id-a": {ID: "id-a", Type: "FUNCTION", CanonicalName: "Answer", Properties: map[string]any{"file": "serving/x.go", "line": float64(10)}},
		"id-b": {ID: "id-b", Type: "STRUCT", CanonicalName: "Server", Properties: map[string]any{"file": "serving/y.go"}},
	}
	g := NewGenerator(nil, fakeLLM{}, "fake", "")
	page, fallback := g.generatePage(context.Background(), topic, index, nil, "hash123")
	if fallback {
		t.Fatalf("expected a real LLM page, got fallback")
	}
	if page.Summary != "A test subsystem." {
		t.Errorf("summary: %q", page.Summary)
	}
	if page.Mermaid == "" || !strings.Contains(page.Mermaid, "graph TD") {
		t.Errorf("mermaid not extracted: %q", page.Mermaid)
	}
	if !strings.Contains(page.Markdown, "serving/x.go#L10") {
		t.Errorf("expected deep code link in body: %q", page.Markdown)
	}
	if len(page.Citations) == 0 {
		t.Errorf("expected resolved citations recorded")
	}
}
