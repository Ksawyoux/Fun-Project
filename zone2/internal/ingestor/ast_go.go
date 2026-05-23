package ingestor

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"time"

	"archgraph/nif"
)

// GoASTConfig configures one AST ingestor over one (possibly nested) Go
// codebase.
type GoASTConfig struct {
	SourceID  string `json:"source_id"`
	RootPath  string `json:"root_path"` // directory to walk
	Namespace string `json:"namespace"`
	// IgnoreDirs are skipped during the walk — vendor/, node_modules/, etc.
	IgnoreDirs []string `json:"ignore_dirs,omitempty"`
}

// GoAST parses Go source files and emits:
//   - One MODULE entity per Go package (canonical name = import path).
//   - One FUNCTION entity per top-level function declaration.
//   - IMPORTS relationships from package → package.
//   - CALLS relationships are deferred — call graph extraction needs
//     resolution beyond go/parser (it would need types via go/packages),
//     which is overkill for MVP.
//
// Per-file isolation: each parse runs in a recovered block; a syntax error
// in one file flags that file as partial but doesn't stop the walk.
type GoAST struct {
	cfg GoASTConfig
}

func NewGoAST(cfg GoASTConfig) *GoAST { return &GoAST{cfg: cfg} }

func (a *GoAST) Identify() Metadata {
	return Metadata{
		ID:            "ast-go:" + a.cfg.SourceID,
		Name:          "Go AST Ingestor (" + a.cfg.SourceID + ")",
		SourceType:    "ast",
		ConnectorType: "pull",
		Version:       "0.1.0",
		// Depends on a Git ingestor with the same source_id having run
		// first. The orchestrator uses this string match to order the DAG.
		Dependencies: []string{"git:" + a.cfg.SourceID},
	}
}

func (a *GoAST) ValidateConfig() error {
	if a.cfg.SourceID == "" {
		return fmt.Errorf("ast-go: source_id required")
	}
	if a.cfg.RootPath == "" {
		return fmt.Errorf("ast-go: root_path required")
	}
	if a.cfg.Namespace == "" {
		return fmt.Errorf("ast-go: namespace required")
	}
	return nil
}

func (a *GoAST) CheckConnectivity(ctx context.Context) error {
	info, err := fsStat(a.cfg.RootPath)
	if err != nil {
		return fmt.Errorf("ast-go: root unreachable: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("ast-go: root_path is not a directory")
	}
	return nil
}

// Fetch ignores `checkpoint` for MVP and always walks the full tree.
// Incremental AST analysis needs file-mtime-vs-package-cache logic that
// isn't load-bearing for a first cut. The new "checkpoint" we return is a
// timestamp so the staleness monitor still gets a fresh signal.
func (a *GoAST) Fetch(ctx context.Context, runID, _ string) (*nif.Batch, string, error) {
	now := time.Now().UTC()
	batch := &nif.Batch{}

	// First pass: walk filesystem, collect Go files grouped by directory.
	// Each directory == one package (the standard Go layout convention).
	pkgFiles := map[string][]string{}
	ignore := normalizeIgnore(a.cfg.IgnoreDirs)
	walkErr := filepath.WalkDir(a.cfg.RootPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable nodes silently; better partial than fail
		}
		if d.IsDir() {
			name := d.Name()
			if _, skip := ignore[name]; skip {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".go") || strings.HasSuffix(d.Name(), "_test.go") {
			return nil
		}
		dir := filepath.Dir(path)
		pkgFiles[dir] = append(pkgFiles[dir], path)
		return nil
	})
	if walkErr != nil {
		return nil, "", fmt.Errorf("walk %s: %w", a.cfg.RootPath, walkErr)
	}

	// Per-package processing.
	fset := token.NewFileSet()
	pkgEntities := map[string]*nif.Entity{} // dir → entity
	for dir, files := range pkgFiles {
		pkg, partial := parsePackage(fset, files)
		if pkg == nil {
			continue
		}
		relDir, _ := filepath.Rel(a.cfg.RootPath, dir)
		if relDir == "" {
			relDir = "."
		}
		importPath := strings.ReplaceAll(relDir, string(filepath.Separator), "/")

		pkgEnt := &nif.Entity{
			ID:        nif.DeterministicEntityID("ast", a.cfg.SourceID, nif.EntityModule, importPath, a.cfg.Namespace),
			Type:      nif.EntityModule,
			SubType:   "go_package",
			Name:      importPath,
			RawName:   pkg.Name,
			Namespace: a.cfg.Namespace,
			Source: nif.SourceInfo{
				SourceType: "ast",
				SourceID:   a.cfg.SourceID,
				SourceRef:  dir,
				ObservedAt: now,
			},
			Properties: map[string]any{
				"package_name": pkg.Name,
				"file_count":   len(files),
			},
			Confidence:   0.95,
			IsPartial:    partial,
			IngestionRun: runID,
		}
		batch.Entities = append(batch.Entities, pkgEnt)
		pkgEntities[dir] = pkgEnt
	}

	// Second pass: per-file functions + imports.
	for dir, files := range pkgFiles {
		pkgEnt, ok := pkgEntities[dir]
		if !ok {
			continue
		}
		for _, path := range files {
			emitFileRecords(fset, path, dir, pkgEnt, batch, a.cfg.SourceID, a.cfg.Namespace, runID, now, pkgEntities)
		}
	}

	return batch, now.Format(time.RFC3339Nano), nil
}

// parsePackage parses every file in a package and returns the *ast.Package
// best-effort. `partial` is true if any file failed.
func parsePackage(fset *token.FileSet, files []string) (*ast.Package, bool) {
	pkgs := map[string]*ast.Package{}
	partial := false
	for _, f := range files {
		file, err := parser.ParseFile(fset, f, nil, parser.ImportsOnly)
		if err != nil {
			partial = true
			continue
		}
		p, ok := pkgs[file.Name.Name]
		if !ok {
			p = &ast.Package{Name: file.Name.Name, Files: map[string]*ast.File{}}
			pkgs[file.Name.Name] = p
		}
		p.Files[f] = file
	}
	// Pick the largest package (handles foo + foo_test in one dir).
	var best *ast.Package
	for _, p := range pkgs {
		if best == nil || len(p.Files) > len(best.Files) {
			best = p
		}
	}
	return best, partial
}

// emitFileRecords runs once per .go file. It re-parses with full mode (to
// get FuncDecls) and emits one FUNCTION entity per top-level function plus
// one IMPORTS edge per import line.
func emitFileRecords(fset *token.FileSet, path, dir string, pkgEnt *nif.Entity, batch *nif.Batch, sourceID, namespace, runID string, now time.Time, pkgEntities map[string]*nif.Entity) {
	file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
	if err != nil {
		return // already counted as partial during parsePackage
	}

	relPath, _ := filepath.Rel(filepath.Dir(dir), path)
	if relPath == "" {
		relPath = filepath.Base(path)
	}

	// --- Functions ---
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name == nil {
			continue
		}
		fqName := pkgEnt.Name + "." + fn.Name.Name
		fnEnt := &nif.Entity{
			ID:        nif.DeterministicEntityID("ast", sourceID, nif.EntityFunction, fqName, namespace),
			Type:      nif.EntityFunction,
			SubType:   "go_function",
			Name:      fqName,
			RawName:   fn.Name.Name,
			Namespace: namespace,
			Source: nif.SourceInfo{
				SourceType: "ast",
				SourceID:   sourceID,
				SourceRef:  path + ":" + fmtLine(fset, fn.Pos()),
				ObservedAt: now,
			},
			Properties: map[string]any{
				"package": pkgEnt.Name,
				"file":    relPath,
				"line":    fset.Position(fn.Pos()).Line,
			},
			Confidence:   0.95,
			IngestionRun: runID,
		}
		batch.Entities = append(batch.Entities, fnEnt)
		// Function depends on the package it lives in.
		batch.Relationships = append(batch.Relationships, &nif.Relationship{
			ID:           nif.DeterministicRelationshipID(nif.RelDependsOn, fnEnt.ID, pkgEnt.ID, "ast"),
			Type:         nif.RelDependsOn,
			FromEntityID: fnEnt.ID,
			ToEntityID:   pkgEnt.ID,
			Source: nif.SourceInfo{
				SourceType: "ast",
				SourceID:   sourceID,
				SourceRef:  path,
				ObservedAt: now,
			},
			Confidence:   0.99,
			IngestionRun: runID,
		})
	}

	// --- Imports ---
	for _, imp := range file.Imports {
		importPath := strings.Trim(imp.Path.Value, `"`)
		// Only emit IMPORTS edges to packages we've seen in THIS run (i.e.,
		// internal imports). External imports are noise without a real
		// dependency resolver — deferred.
		var target *nif.Entity
		for _, e := range pkgEntities {
			if e.Name == importPath {
				target = e
				break
			}
		}
		if target == nil || target.ID == pkgEnt.ID {
			continue
		}
		batch.Relationships = append(batch.Relationships, &nif.Relationship{
			ID:           nif.DeterministicRelationshipID(nif.RelImports, pkgEnt.ID, target.ID, "ast"),
			Type:         nif.RelImports,
			FromEntityID: pkgEnt.ID,
			ToEntityID:   target.ID,
			Source: nif.SourceInfo{
				SourceType: "ast",
				SourceID:   sourceID,
				SourceRef:  path,
				ObservedAt: now,
			},
			Confidence:   0.99,
			IngestionRun: runID,
		})
	}
}

func fmtLine(fset *token.FileSet, pos token.Pos) string {
	p := fset.Position(pos)
	return fmt.Sprintf("%d", p.Line)
}

func normalizeIgnore(in []string) map[string]struct{} {
	defaults := []string{".git", "vendor", "node_modules", ".idea", ".vscode"}
	out := map[string]struct{}{}
	for _, d := range append(defaults, in...) {
		out[d] = struct{}{}
	}
	return out
}

// fsStat exists so tests can fake the filesystem if they ever need to.
var fsStat = func(p string) (fs.FileInfo, error) {
	return osStat(p)
}
