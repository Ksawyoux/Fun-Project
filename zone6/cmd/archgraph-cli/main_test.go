package main

import "testing"

func TestResolveEntityByFileAndLine_ExactFileMatch(t *testing.T) {
	entities := []*Entity{
		{
			ID:            "file-1",
			CanonicalName: "cmd/app/main.go",
			Properties:    map[string]any{"source_ref": "/repo/cmd/app/main.go"},
		},
	}

	got, err := resolveEntityByFileAndLine(entities, "cmd/app/main.go", 0)
	if err != nil {
		t.Fatalf("resolveEntityByFileAndLine() error = %v", err)
	}
	if got != "file-1" {
		t.Fatalf("resolved id = %s, want file-1", got)
	}
}

func TestResolveEntityByFileAndLine_SuffixAndLineMatch(t *testing.T) {
	entities := []*Entity{
		{
			ID:            "pkg-1",
			CanonicalName: "example.com/app/pkg/service",
			Properties:    map[string]any{"source_ref": "/repo/pkg/service/service.go"},
		},
		{
			ID:            "fn-1",
			CanonicalName: "example.com/app/pkg/service.Handle",
			Properties: map[string]any{
				"file": "pkg/service/service.go",
				"line": float64(42),
			},
		},
	}

	got, err := resolveEntityByFileAndLine(entities, "service/service.go", 42)
	if err != nil {
		t.Fatalf("resolveEntityByFileAndLine() error = %v", err)
	}
	if got != "fn-1" {
		t.Fatalf("resolved id = %s, want fn-1", got)
	}
}

func TestResolveEntityByFileAndLine_NoMatch(t *testing.T) {
	entities := []*Entity{
		{
			ID:            "file-1",
			CanonicalName: "cmd/app/main.go",
			Properties:    map[string]any{"file": "cmd/app/main.go"},
		},
	}

	if _, err := resolveEntityByFileAndLine(entities, "missing.go", 0); err == nil {
		t.Fatalf("resolveEntityByFileAndLine() error = nil, want error")
	}
}
