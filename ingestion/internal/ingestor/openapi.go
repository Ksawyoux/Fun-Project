package ingestor

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"archgraph/nif"
)

type OpenAPIConfig struct {
	SourceID   string   `json:"source_id"`
	RootPath   string   `json:"root_path"`
	Namespace  string   `json:"namespace"`
	IgnoreDirs []string `json:"ignore_dirs,omitempty"`
}

type OpenAPI struct {
	cfg OpenAPIConfig
}

func NewOpenAPI(cfg OpenAPIConfig) *OpenAPI {
	return &OpenAPI{cfg: cfg}
}

func (o *OpenAPI) Identify() Metadata {
	return Metadata{
		ID:            "openapi:" + o.cfg.SourceID,
		Name:          "OpenAPI Spec Ingestor (" + o.cfg.SourceID + ")",
		SourceType:    "api",
		ConnectorType: "pull",
		Version:       "0.1.0",
	}
}

func (o *OpenAPI) ValidateConfig() error {
	if o.cfg.SourceID == "" {
		return fmt.Errorf("openapi: source_id required")
	}
	if o.cfg.RootPath == "" {
		return fmt.Errorf("openapi: root_path required")
	}
	if o.cfg.Namespace == "" {
		return fmt.Errorf("openapi: namespace required")
	}
	return nil
}

func (o *OpenAPI) CheckConnectivity(ctx context.Context) error {
	info, err := os.Stat(o.cfg.RootPath)
	if err != nil {
		return fmt.Errorf("openapi: root unreachable: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("openapi: root_path is not a directory")
	}
	return nil
}

func (o *OpenAPI) Fetch(ctx context.Context, runID, _ string) (*nif.Batch, string, error) {
	now := time.Now().UTC()
	batch := &nif.Batch{}
	root, err := filepath.Abs(o.cfg.RootPath)
	if err != nil {
		return nil, "", fmt.Errorf("abs root %s: %w", o.cfg.RootPath, err)
	}

	var specFiles []string
	ignore := normalizeIgnore(o.cfg.IgnoreDirs)
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
		name := strings.ToLower(d.Name())
		if name == "openapi.json" || name == "openapi.yaml" || name == "openapi.yml" ||
			strings.HasSuffix(name, ".openapi.json") || strings.HasSuffix(name, ".openapi.yaml") || strings.HasSuffix(name, ".openapi.yml") {
			specFiles = append(specFiles, path)
		}
		return nil
	})
	if err != nil {
		return nil, "", fmt.Errorf("walk openapi specs: %w", err)
	}

	for _, path := range specFiles {
		endpoints, err := parseSpecFile(path)
		if err != nil {
			continue // Skip invalid spec files
		}
		rel := relPath(root, path)

		for _, ep := range endpoints {
			fqName := ep.Method + " " + ep.Path
			epEnt := &nif.Entity{
				ID:        nif.DeterministicEntityID("api", o.cfg.SourceID, nif.EntityAPIEndpoint, fqName, o.cfg.Namespace),
				Type:      nif.EntityAPIEndpoint,
				SubType:   "http_route",
				Name:      fqName,
				RawName:   ep.Path,
				Namespace: o.cfg.Namespace,
				Source: nif.SourceInfo{
					SourceType: "api",
					SourceID:   o.cfg.SourceID,
					SourceRef:  path,
					ObservedAt: now,
				},
				Properties: map[string]any{
					"method": ep.Method,
					"path":   ep.Path,
					"file":   rel,
				},
				Confidence:   0.95,
				IngestionRun: runID,
			}
			batch.Entities = append(batch.Entities, epEnt)
		}
	}

	return batch, now.Format(time.RFC3339Nano), nil
}

type endpoint struct {
	Method string
	Path   string
}

func parseSpecFile(path string) ([]endpoint, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	// Try JSON first
	var endpoints []endpoint
	var data map[string]any
	if err := json.Unmarshal(b, &data); err == nil {
		if paths, ok := data["paths"].(map[string]any); ok {
			for p, pathVal := range paths {
				if pathObj, ok := pathVal.(map[string]any); ok {
					for m := range pathObj {
						mUpper := strings.ToUpper(m)
						if isHTTPMethod(mUpper) {
							endpoints = append(endpoints, endpoint{Method: mUpper, Path: p})
						}
					}
				}
			}
		}
		return endpoints, nil
	}

	// Fallback to YAML line scanning
	return parseYAMLSpec(b)
}

func parseYAMLSpec(b []byte) ([]endpoint, error) {
	var endpoints []endpoint
	scanner := bufio.NewScanner(strings.NewReader(string(b)))
	inPaths := false
	var currentPath string

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		indent := len(line) - len(strings.TrimLeft(line, " "))

		// Detect entering 'paths:' block
		if indent == 0 && strings.HasPrefix(trimmed, "paths:") {
			inPaths = true
			continue
		}

		if inPaths {
			// If we exit the indented block under paths, reset
			if indent == 0 && !strings.HasPrefix(trimmed, "paths:") {
				inPaths = false
				continue
			}

			// Path key (starts with two spaces and a slash)
			if indent == 2 && strings.HasPrefix(trimmed, "/") {
				currentPath = strings.TrimSuffix(trimmed, ":")
				continue
			}

			// Method key under a path (starts with four spaces)
			if indent == 4 && currentPath != "" {
				method := strings.ToUpper(strings.TrimSuffix(trimmed, ":"))
				if isHTTPMethod(method) {
					endpoints = append(endpoints, endpoint{Method: method, Path: currentPath})
				}
			}
		}
	}
	return endpoints, nil
}

func isHTTPMethod(m string) bool {
	switch m {
	case "GET", "POST", "PUT", "DELETE", "PATCH", "OPTIONS", "HEAD":
		return true
	}
	return false
}
