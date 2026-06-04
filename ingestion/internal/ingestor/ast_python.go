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

type PythonASTConfig struct {
	SourceID   string   `json:"source_id"`
	RootPath   string   `json:"root_path"`
	Namespace  string   `json:"namespace"`
	IgnoreDirs []string `json:"ignore_dirs,omitempty"`
}

type PythonAST struct {
	cfg PythonASTConfig
}

func NewPythonAST(cfg PythonASTConfig) *PythonAST {
	return &PythonAST{cfg: cfg}
}

func (p *PythonAST) Identify() Metadata {
	return Metadata{
		ID:            "ast-python:" + p.cfg.SourceID,
		Name:          "Python AST Ingestor (" + p.cfg.SourceID + ")",
		SourceType:    "ast",
		ConnectorType: "pull",
		Version:       "0.1.0",
	}
}

func (p *PythonAST) ValidateConfig() error {
	if p.cfg.SourceID == "" {
		return fmt.Errorf("ast-python: source_id required")
	}
	if p.cfg.RootPath == "" {
		return fmt.Errorf("ast-python: root_path required")
	}
	if p.cfg.Namespace == "" {
		return fmt.Errorf("ast-python: namespace required")
	}
	return nil
}

func (p *PythonAST) CheckConnectivity(ctx context.Context) error {
	info, err := os.Stat(p.cfg.RootPath)
	if err != nil {
		return fmt.Errorf("ast-python: root unreachable: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("ast-python: root_path is not a directory")
	}
	return nil
}

func (p *PythonAST) Fetch(ctx context.Context, runID, _ string) (*nif.Batch, string, error) {
	now := time.Now().UTC()
	batch := &nif.Batch{}
	root, err := filepath.Abs(p.cfg.RootPath)
	if err != nil {
		return nil, "", fmt.Errorf("abs root %s: %w", p.cfg.RootPath, err)
	}

	var pyFiles []string
	ignore := normalizeIgnore(p.cfg.IgnoreDirs)
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
		if strings.HasSuffix(d.Name(), ".py") {
			pyFiles = append(pyFiles, path)
		}
		return nil
	})
	if err != nil {
		return nil, "", fmt.Errorf("walk python files: %w", err)
	}

	importIndex := map[string]*nif.Entity{}
	type pyFileData struct {
		path       string
		moduleName string
		relPath    string
	}
	var fileList []pyFileData

	// 1. Create MODULE entities for all Python files
	for _, path := range pyFiles {
		rel := relPath(root, path)
		modulePath := strings.TrimSuffix(rel, ".py")
		modulePath = strings.ReplaceAll(modulePath, "/", ".")
		if strings.HasSuffix(modulePath, ".__init__") {
			modulePath = strings.TrimSuffix(modulePath, ".__init__")
		}

		modEnt := &nif.Entity{
			ID:        nif.DeterministicEntityID("ast", p.cfg.SourceID, nif.EntityModule, modulePath, p.cfg.Namespace),
			Type:      nif.EntityModule,
			SubType:   "python_module",
			Name:      modulePath,
			RawName:   filepath.Base(path),
			Namespace: p.cfg.Namespace,
			Source: nif.SourceInfo{
				SourceType: "ast",
				SourceID:   p.cfg.SourceID,
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
		fileList = append(fileList, pyFileData{path: path, moduleName: modulePath, relPath: rel})
	}

	// 2. Parse imports and declarations inside files
	defRegex := regexp.MustCompile(`^def\s+([a-zA-Z_][a-zA-Z0-9_]*)\s*\(`)
	classRegex := regexp.MustCompile(`^class\s+([a-zA-Z_][a-zA-Z0-9_]*)\s*[:\(]`)

	for _, fd := range fileList {
		f, err := os.Open(fd.path)
		if err != nil {
			continue // Skip unreadable files
		}

		scanner := bufio.NewScanner(f)
		lineNum := 0
		modEnt := importIndex[fd.moduleName]

		for scanner.Scan() {
			lineNum++
			line := strings.TrimSpace(scanner.Text())
			rawLine := scanner.Text()

			// Match functions
			if strings.HasPrefix(rawLine, "def ") {
				matches := defRegex.FindStringSubmatch(rawLine)
				if len(matches) > 1 {
					funcName := matches[1]
					fqName := fd.moduleName + "." + funcName
					fnEnt := &nif.Entity{
						ID:        nif.DeterministicEntityID("ast", p.cfg.SourceID, nif.EntityFunction, fqName, p.cfg.Namespace),
						Type:      nif.EntityFunction,
						SubType:   "python_function",
						Name:      fqName,
						RawName:   funcName,
						Namespace: p.cfg.Namespace,
						Source: nif.SourceInfo{
							SourceType: "ast",
							SourceID:   p.cfg.SourceID,
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
							SourceID:   p.cfg.SourceID,
							SourceRef:  fd.path,
							ObservedAt: now,
						},
						Confidence:   0.99,
						IngestionRun: runID,
					})
				}
			}

			// Match classes
			if strings.HasPrefix(rawLine, "class ") {
				matches := classRegex.FindStringSubmatch(rawLine)
				if len(matches) > 1 {
					className := matches[1]
					fqName := fd.moduleName + "." + className
					clsEnt := &nif.Entity{
						ID:        nif.DeterministicEntityID("ast", p.cfg.SourceID, nif.EntityClass, fqName, p.cfg.Namespace),
						Type:      nif.EntityClass,
						SubType:   "python_class",
						Name:      fqName,
						RawName:   className,
						Namespace: p.cfg.Namespace,
						Source: nif.SourceInfo{
							SourceType: "ast",
							SourceID:   p.cfg.SourceID,
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
					batch.Entities = append(batch.Entities, clsEnt)
					// Class depends on module
					batch.Relationships = append(batch.Relationships, &nif.Relationship{
						ID:           nif.DeterministicRelationshipID(nif.RelDependsOn, clsEnt.ID, modEnt.ID, "ast"),
						Type:         nif.RelDependsOn,
						FromEntityID: clsEnt.ID,
						ToEntityID:   modEnt.ID,
						Source: nif.SourceInfo{
							SourceType: "ast",
							SourceID:   p.cfg.SourceID,
							SourceRef:  fd.path,
							ObservedAt: now,
						},
						Confidence:   0.99,
						IngestionRun: runID,
					})
				}
			}

			// Match imports
			var importedModules []string
			if strings.HasPrefix(line, "import ") {
				parts := strings.Split(strings.TrimPrefix(line, "import "), ",")
				for _, part := range parts {
					// handle "import a as b"
					term := strings.Fields(part)
					if len(term) > 0 {
						importedModules = append(importedModules, term[0])
					}
				}
			} else if strings.HasPrefix(line, "from ") {
				parts := strings.Split(strings.TrimPrefix(line, "from "), " import ")
				if len(parts) > 0 {
					basePkg := strings.TrimSpace(parts[0])
					if len(parts) > 1 {
						subParts := strings.Split(parts[1], ",")
						for _, sp := range subParts {
							term := strings.Fields(sp)
							if len(term) > 0 {
								importedModules = append(importedModules, basePkg+"."+term[0])
								importedModules = append(importedModules, basePkg) // also import base package
							}
						}
					} else {
						importedModules = append(importedModules, basePkg)
					}
				}
			}

			// Add relationship for found internal imports
			for _, imp := range importedModules {
				target, ok := importIndex[imp]
				if ok && target.ID != modEnt.ID {
					batch.Relationships = append(batch.Relationships, &nif.Relationship{
						ID:           nif.DeterministicRelationshipID(nif.RelImports, modEnt.ID, target.ID, "ast"),
						Type:         nif.RelImports,
						FromEntityID: modEnt.ID,
						ToEntityID:   target.ID,
						Source: nif.SourceInfo{
							SourceType: "ast",
							SourceID:   p.cfg.SourceID,
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
