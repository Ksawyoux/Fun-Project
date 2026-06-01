package ingestor

import (
	"bufio"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"archgraph/nif"
)

type TypeScriptASTConfig struct {
	SourceID   string   `json:"source_id"`
	RootPath   string   `json:"root_path"`
	Namespace  string   `json:"namespace"`
	IgnoreDirs []string `json:"ignore_dirs,omitempty"`
}

type TypeScriptAST struct {
	cfg TypeScriptASTConfig
}

func NewTypeScriptAST(cfg TypeScriptASTConfig) *TypeScriptAST {
	return &TypeScriptAST{cfg: cfg}
}

func (t *TypeScriptAST) Identify() Metadata {
	return Metadata{
		ID:            "ast-ts:" + t.cfg.SourceID,
		Name:          "TypeScript/JS AST Ingestor (" + t.cfg.SourceID + ")",
		SourceType:    "ast",
		ConnectorType: "pull",
		Version:       "0.1.0",
	}
}

func (t *TypeScriptAST) ValidateConfig() error {
	if t.cfg.SourceID == "" {
		return fmt.Errorf("ast-ts: source_id required")
	}
	if t.cfg.RootPath == "" {
		return fmt.Errorf("ast-ts: root_path required")
	}
	if t.cfg.Namespace == "" {
		return fmt.Errorf("ast-ts: namespace required")
	}
	return nil
}

func (t *TypeScriptAST) CheckConnectivity(ctx context.Context) error {
	info, err := os.Stat(t.cfg.RootPath)
	if err != nil {
		return fmt.Errorf("ast-ts: root unreachable: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("ast-ts: root_path is not a directory")
	}
	return nil
}

func (t *TypeScriptAST) Fetch(ctx context.Context, runID, _ string) (*nif.Batch, string, error) {
	now := time.Now().UTC()
	batch := &nif.Batch{}
	root, err := filepath.Abs(t.cfg.RootPath)
	if err != nil {
		return nil, "", fmt.Errorf("abs root %s: %w", t.cfg.RootPath, err)
	}

	var tsFiles []string
	ignore := normalizeIgnore(t.cfg.IgnoreDirs)
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if _, skip := ignore[d.Name()]; skip {
				return filepath.SkipDir
			}
			return nil
		}
		ext := filepath.Ext(d.Name())
		if ext == ".ts" || ext == ".tsx" || ext == ".js" || ext == ".jsx" {
			// Skip declaration files (.d.ts) and test files
			name := d.Name()
			if strings.HasSuffix(name, ".d.ts") || strings.Contains(name, ".test.") || strings.Contains(name, ".spec.") {
				return nil
			}
			tsFiles = append(tsFiles, path)
		}
		return nil
	})
	if err != nil {
		return nil, "", fmt.Errorf("walk typescript files: %w", err)
	}

	importIndex := map[string]*nif.Entity{}
	type tsFileData struct {
		path       string
		moduleName string
		relPath    string
	}
	var fileList []tsFileData

	// 1. Create MODULE entities for all TS/JS files
	for _, path := range tsFiles {
		rel := relPath(root, path)
		ext := filepath.Ext(path)
		modulePath := strings.TrimSuffix(rel, ext)
		modulePath = strings.ReplaceAll(modulePath, "/", ".")

		modEnt := &nif.Entity{
			ID:        nif.DeterministicEntityID("ast", t.cfg.SourceID, nif.EntityModule, modulePath, t.cfg.Namespace),
			Type:      nif.EntityModule,
			SubType:   "typescript_module",
			Name:      modulePath,
			RawName:   filepath.Base(path),
			Namespace: t.cfg.Namespace,
			Source: nif.SourceInfo{
				SourceType: "ast",
				SourceID:   t.cfg.SourceID,
				SourceRef:  path,
				ObservedAt: now,
			},
			Properties: map[string]any{
				"file": rel,
			},
			Confidence:   0.95,
			IngestionRun: runID,
		}
		batch.Entities = append(batch.Entities, modEnt)
		importIndex[modulePath] = modEnt
		fileList = append(fileList, tsFileData{path: path, moduleName: modulePath, relPath: rel})
	}

	// Regexes to extract functions and imports
	// e.g. function test() or const test = () =>
	funcRegex := regexp.MustCompile(`(?:export\s+)?(?:async\s+)?function\s+([a-zA-Z_][a-zA-Z0-9_]*)`)
	arrowFuncRegex := regexp.MustCompile(`(?:export\s+)?const\s+([a-zA-Z_][a-zA-Z0-9_]*)\s*=\s*(?:\(.*?\)|[a-zA-Z_][a-zA-Z0-9_]*)\s*=>`)

	// e.g. import { x } from './target' or import './target' or const x = require('./target')
	importFromRegex := regexp.MustCompile(`import\s+.*?\s+from\s+['"](.*?)['"]`)
	importDirectRegex := regexp.MustCompile(`import\s+['"](.*?)['"]`)
	requireRegex := regexp.MustCompile(`require\s*\(\s*['"](.*?)['"]\s*\)`)

	for _, fd := range fileList {
		f, err := os.Open(fd.path)
		if err != nil {
			continue
		}

		scanner := bufio.NewScanner(f)
		lineNum := 0
		modEnt := importIndex[fd.moduleName]

		for scanner.Scan() {
			lineNum++
			line := strings.TrimSpace(scanner.Text())

			// Match functions
			var funcName string
			if matches := funcRegex.FindStringSubmatch(line); len(matches) > 1 {
				funcName = matches[1]
			} else if matches := arrowFuncRegex.FindStringSubmatch(line); len(matches) > 1 {
				funcName = matches[1]
			}

			if funcName != "" {
				fqName := fd.moduleName + "." + funcName
				fnEnt := &nif.Entity{
					ID:        nif.DeterministicEntityID("ast", t.cfg.SourceID, nif.EntityFunction, fqName, t.cfg.Namespace),
					Type:      nif.EntityFunction,
					SubType:   "typescript_function",
					Name:      fqName,
					RawName:   funcName,
					Namespace: t.cfg.Namespace,
					Source: nif.SourceInfo{
						SourceType: "ast",
						SourceID:   t.cfg.SourceID,
						SourceRef:  fd.path + fmt.Sprintf(":%d", lineNum),
						ObservedAt: now,
					},
					Properties: map[string]any{
						"file": fd.relPath,
						"line": lineNum,
					},
					Confidence:   0.95,
					IngestionRun: runID,
				}
				batch.Entities = append(batch.Entities, fnEnt)
				// Function depends on module
				batch.Relationships = append(batch.Relationships, &nif.Relationship{
					ID:           nif.DeterministicRelationshipID(nif.RelDependsOn, fnEnt.ID, modEnt.ID, "ast"),
					Type:         nif.RelDependsOn,
					FromEntityID: fnEnt.ID,
					ToEntityID:   modEnt.ID,
					Source: nif.SourceInfo{
						SourceType: "ast",
						SourceID:   t.cfg.SourceID,
						SourceRef:  fd.path,
						ObservedAt: now,
					},
					Confidence:   0.99,
					IngestionRun: runID,
				})
			}

			// Match imports
			var targetImport string
			if matches := importFromRegex.FindStringSubmatch(line); len(matches) > 1 {
				targetImport = matches[1]
			} else if matches := importDirectRegex.FindStringSubmatch(line); len(matches) > 1 {
				targetImport = matches[1]
			} else if matches := requireRegex.FindStringSubmatch(line); len(matches) > 1 {
				targetImport = matches[1]
			}

			if targetImport != "" {
				var resolvedTarget string
				if strings.HasPrefix(targetImport, "./") || strings.HasPrefix(targetImport, "../") {
					// Local import path
					dir := filepath.Dir(fd.relPath)
					absRelPath := filepath.Clean(filepath.Join(dir, targetImport))
					resolvedTarget = strings.ReplaceAll(absRelPath, "/", ".")
				} else {
					// External node_module package, keep as is
					resolvedTarget = targetImport
				}

				targetEnt, ok := importIndex[resolvedTarget]
				if ok && targetEnt.ID != modEnt.ID {
					batch.Relationships = append(batch.Relationships, &nif.Relationship{
						ID:           nif.DeterministicRelationshipID(nif.RelImports, modEnt.ID, targetEnt.ID, "ast"),
						Type:         nif.RelImports,
						FromEntityID: modEnt.ID,
						ToEntityID:   targetEnt.ID,
						Source: nif.SourceInfo{
							SourceType: "ast",
							SourceID:   t.cfg.SourceID,
							SourceRef:  fd.path + fmt.Sprintf(":%d", lineNum),
							ObservedAt: now,
						},
						Confidence:   0.99,
						IngestionRun: runID,
					})
				}
			}
		}
		f.Close()
	}

	return batch, now.Format(time.RFC3339Nano), nil
}
