package ingestor

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"archgraph/nif"
)

func TestGoAST_ResolvesImportsUsingGoModModulePaths(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/root\n\ngo 1.22\n")
	writeFile(t, root, "app/main.go", `package app

import (
	"example.com/root/pkg/lib"
	"example.com/tools/tool"
)

func Run() {
	lib.Do()
	tool.Do()
}
`)
	writeFile(t, root, "pkg/lib/lib.go", `package lib

func Do() {}
`)
	writeFile(t, root, "tools/go.mod", "module example.com/tools\n\ngo 1.22\n")
	writeFile(t, root, "tools/tool/tool.go", `package tool

func Do() {}
`)

	ast := NewGoAST(GoASTConfig{
		SourceID:  "fixture",
		RootPath:  root,
		Namespace: "test",
	})
	batch, _, err := ast.Fetch(context.Background(), "run-1", "")
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}

	app := entityByName(batch, "example.com/root/app")
	lib := entityByName(batch, "example.com/root/pkg/lib")
	tool := entityByName(batch, "example.com/tools/tool")
	if app == nil || lib == nil || tool == nil {
		t.Fatalf("missing package entities: app=%v lib=%v tool=%v", app, lib, tool)
	}
	if got := app.Properties["dir"]; got != "app" {
		t.Fatalf("app dir property = %v, want app", got)
	}

	if !hasRelationship(batch, "IMPORTS", app.ID, lib.ID) {
		t.Fatalf("missing IMPORTS relationship app -> lib")
	}
	if !hasRelationship(batch, "IMPORTS", app.ID, tool.ID) {
		t.Fatalf("missing IMPORTS relationship app -> nested module tool")
	}

	fn := entityByName(batch, "example.com/root/app.Run")
	if fn == nil {
		t.Fatalf("missing function entity for app.Run")
	}
	if got := fn.Properties["file"]; got != "app/main.go" {
		t.Fatalf("function file property = %v, want app/main.go", got)
	}
}

func TestGoAST_DoesNotRequireGitIngestor(t *testing.T) {
	ast := NewGoAST(GoASTConfig{SourceID: "plain-folder", RootPath: t.TempDir(), Namespace: "test"})
	if deps := ast.Identify().Dependencies; len(deps) != 0 {
		t.Fatalf("GoAST dependencies = %v, want none", deps)
	}
}

func writeFile(t *testing.T, root, rel, contents string) {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
}

func entityByName(batch *nif.Batch, name string) *nif.Entity {
	for _, e := range batch.Entities {
		if e.Name == name {
			return e
		}
	}
	return nil
}

func hasRelationship(batch *nif.Batch, typ string, fromID, toID string) bool {
	for _, r := range batch.Relationships {
		if string(r.Type) == typ && r.FromEntityID == fromID && r.ToEntityID == toID {
			return true
		}
	}
	return false
}
