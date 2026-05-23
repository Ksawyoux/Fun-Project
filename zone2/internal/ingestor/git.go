package ingestor

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"archgraph/nif"
)

// GitConfig is what one Git source needs to know.
//
// SubProjects is optional: a monorepo lists its sub-projects so each maps
// to its own MODULE/SERVICE entity. If empty, the whole repo becomes one
// entity. The spec calls this out as load-bearing for monorepos (§3.1).
type GitConfig struct {
	SourceID    string   `json:"source_id"`    // stable name for this configured source
	RepoPath    string   `json:"repo_path"`    // local clone path
	Namespace   string   `json:"namespace"`    // org/team scope
	SubProjects []string `json:"sub_projects,omitempty"` // sub-paths relative to RepoPath
}

// Git is a pull ingestor. It shells out to the system `git` binary — pure
// Go alternatives (go-git) are heavyweight and we can rely on git being
// present everywhere a dev would run this MVP.
type Git struct {
	cfg GitConfig
}

func NewGit(cfg GitConfig) *Git { return &Git{cfg: cfg} }

func (g *Git) Identify() Metadata {
	return Metadata{
		ID:            "git:" + g.cfg.SourceID,
		Name:          "Git Ingestor (" + g.cfg.SourceID + ")",
		SourceType:    "git",
		ConnectorType: "pull",
		Version:       "0.1.0",
	}
}

func (g *Git) ValidateConfig() error {
	if g.cfg.SourceID == "" {
		return fmt.Errorf("git: source_id required")
	}
	if g.cfg.RepoPath == "" {
		return fmt.Errorf("git: repo_path required")
	}
	if g.cfg.Namespace == "" {
		return fmt.Errorf("git: namespace required")
	}
	return nil
}

func (g *Git) CheckConnectivity(ctx context.Context) error {
	// "Connectivity" for a local repo is "is this a git repo".
	_, err := g.runGit(ctx, "rev-parse", "--git-dir")
	if err != nil {
		return fmt.Errorf("git: %s is not a git repository: %w", g.cfg.RepoPath, err)
	}
	return nil
}

// Fetch produces:
//   - One MODULE entity per sub-project (or one for the whole repo).
//   - One MODULE entity per source file present at HEAD.
//   - One IMPORTS-style DEPENDS_ON relationship is left to the AST ingestor —
//     the Git layer only knows about file presence, not code semantics.
//
// Checkpoint = last processed HEAD SHA. On first run (empty checkpoint), we
// emit everything at HEAD; on subsequent runs we only emit changed files
// since `checkpoint..HEAD`.
func (g *Git) Fetch(ctx context.Context, runID, checkpoint string) (*nif.Batch, string, error) {
	headOut, err := g.runGit(ctx, "rev-parse", "HEAD")
	if err != nil {
		return nil, checkpoint, fmt.Errorf("git rev-parse HEAD: %w", err)
	}
	head := strings.TrimSpace(headOut)
	now := time.Now().UTC()

	batch := &nif.Batch{}

	// --- One MODULE per sub-project (or the whole repo) ---
	projects := g.cfg.SubProjects
	if len(projects) == 0 {
		projects = []string{"."}
	}
	projectEntities := map[string]*nif.Entity{}
	for _, sub := range projects {
		name := strings.Trim(sub, "/")
		if name == "" || name == "." {
			name = filepath.Base(g.cfg.RepoPath)
		}
		ent := &nif.Entity{
			ID:        nif.DeterministicEntityID("git", g.cfg.SourceID, nif.EntityModule, name, g.cfg.Namespace),
			Type:      nif.EntityModule,
			SubType:   "project",
			Name:      name,
			RawName:   sub,
			Namespace: g.cfg.Namespace,
			Source: nif.SourceInfo{
				SourceType: "git",
				SourceID:   g.cfg.SourceID,
				SourceRef:  g.cfg.RepoPath + ":" + sub,
				ObservedAt: now,
			},
			Properties: map[string]any{
				"head_sha":  head,
				"sub_path":  sub,
			},
			Confidence:   0.99,
			IsPartial:    false,
			IngestionRun: runID,
		}
		batch.Entities = append(batch.Entities, ent)
		projectEntities[sub] = ent
	}

	// --- One MODULE per source file ---
	var paths []string
	if checkpoint == "" {
		// First run: enumerate everything at HEAD.
		out, err := g.runGit(ctx, "ls-tree", "-r", "--name-only", "HEAD")
		if err != nil {
			return nil, checkpoint, fmt.Errorf("git ls-tree: %w", err)
		}
		paths = splitLines(out)
	} else {
		// Incremental: only files changed between checkpoint and HEAD.
		out, err := g.runGit(ctx, "diff", "--name-only", checkpoint+"..HEAD")
		if err != nil {
			// `checkpoint` may be unknown if the user reset history.
			// Fall back to ls-tree HEAD so we don't silently miss data.
			out, err = g.runGit(ctx, "ls-tree", "-r", "--name-only", "HEAD")
			if err != nil {
				return nil, checkpoint, fmt.Errorf("git diff fallback ls-tree: %w", err)
			}
		}
		paths = splitLines(out)
	}

	for _, path := range paths {
		if path == "" {
			continue
		}
		sub := projectForPath(path, projects)
		fileEnt := &nif.Entity{
			ID:        nif.DeterministicEntityID("git", g.cfg.SourceID, nif.EntityModule, path, g.cfg.Namespace),
			Type:      nif.EntityModule,
			SubType:   "file",
			Name:      path,
			RawName:   path,
			Namespace: g.cfg.Namespace,
			Source: nif.SourceInfo{
				SourceType: "git",
				SourceID:   g.cfg.SourceID,
				SourceRef:  g.cfg.RepoPath + ":" + path,
				ObservedAt: now,
			},
			Properties: map[string]any{
				"sub_project": sub,
				"head_sha":    head,
			},
			Confidence:   0.95,
			IsPartial:    false,
			IngestionRun: runID,
		}
		batch.Entities = append(batch.Entities, fileEnt)

		// File "belongs to" its sub-project — DEPENDS_ON points from file
		// to project. We use DEPENDS_ON since OWNS in Zone 4 is reserved
		// for team-owns-service. A future Ownership ingestor will add the
		// team→service edges.
		if proj, ok := projectEntities[sub]; ok && proj.ID != fileEnt.ID {
			rel := &nif.Relationship{
				ID:           nif.DeterministicRelationshipID(nif.RelDependsOn, fileEnt.ID, proj.ID, "git"),
				Type:         nif.RelDependsOn,
				FromEntityID: fileEnt.ID,
				ToEntityID:   proj.ID,
				Source: nif.SourceInfo{
					SourceType: "git",
					SourceID:   g.cfg.SourceID,
					SourceRef:  g.cfg.RepoPath + ":" + path,
					ObservedAt: now,
				},
				Confidence:   0.95,
				IsInferred:   false,
				IngestionRun: runID,
				Properties:   map[string]any{"reason": "file resides in sub-project"},
			}
			batch.Relationships = append(batch.Relationships, rel)
		}
	}

	return batch, head, nil
}

// projectForPath finds the longest matching sub-project prefix for a file.
// Falls back to "." if none match — that's the whole-repo case.
func projectForPath(path string, projects []string) string {
	best := "."
	bestLen := -1
	for _, sub := range projects {
		s := strings.Trim(sub, "/")
		if s == "" || s == "." {
			continue
		}
		if strings.HasPrefix(path, s+"/") || path == s {
			if len(s) > bestLen {
				best = sub
				bestLen = len(s)
			}
		}
	}
	return best
}

func (g *Git) runGit(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = g.cfg.RepoPath
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w (stderr: %s)", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

func splitLines(s string) []string {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}
