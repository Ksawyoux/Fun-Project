package main

import (
	"bufio"
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

//go:embed web/*
var webFS embed.FS

// API models mirroring Zone 4 and Zone 5 schemas
type AskReq struct {
	Question   string `json:"question"`
	Namespace  string `json:"namespace,omitempty"`
	EntityName string `json:"entity_name,omitempty"`
	Depth      int    `json:"depth,omitempty"`
}

type AskResp struct {
	Plan         Plan          `json:"plan"`
	Answer       *Answer       `json:"answer,omitempty"`
	BlastRadius  *BlastRadius  `json:"blast_radius,omitempty"`
	HealthReport *HealthReport `json:"health_report,omitempty"`
	Evolution    *Evolution    `json:"evolution,omitempty"`
}

type Plan struct {
	Action     string `json:"action"`
	EntityName string `json:"entity_name,omitempty"`
	Namespace  string `json:"namespace,omitempty"`
	Depth      int    `json:"depth,omitempty"`
}

type Answer struct {
	Text           string  `json:"text"`
	MermaidDiagram string  `json:"mermaid_diagram,omitempty"`
	Confidence     float64 `json:"confidence"`
	UsedLLM        string  `json:"used_llm"`
}

type BlastRadius struct {
	Origin        *Entity        `json:"origin"`
	Affected      []AffectedNode `json:"affected"`
	MaxDepth      int            `json:"max_depth"`
	TotalAffected int            `json:"total_affected"`
}

type AffectedNode struct {
	NodeID            string  `json:"node_id"`
	CanonicalName     string  `json:"canonical_name"`
	Type              string  `json:"type"`
	Depth             int     `json:"depth"`
	ImpactProbability float64 `json:"impact_probability"`
}

type HealthReport struct {
	Namespace string  `json:"namespace"`
	Smells    []Smell `json:"smells"`
	Summary   struct {
		Entities      int `json:"entities"`
		Relationships int `json:"relationships"`
		CyclesFound   int `json:"cycles_found"`
		Supernodes    int `json:"supernodes"`
	} `json:"summary"`
}

type Smell struct {
	Type     string   `json:"type"`
	Severity string   `json:"severity"`
	Message  string   `json:"message"`
	Nodes    []string `json:"nodes,omitempty"`
	NodeIDs  []string `json:"node_ids,omitempty"`
}

type Evolution struct {
	From        time.Time    `json:"from"`
	To          time.Time    `json:"to"`
	DiffSummary DiffSummary  `json:"diff_summary"`
	DriftAlerts []DriftAlert `json:"drift_alerts,omitempty"`
}

type DiffSummary struct {
	NodesAdded    int `json:"nodes_added"`
	NodesRemoved  int `json:"nodes_removed"`
	NodesUpdated  int `json:"nodes_updated"`
	EdgesAdded    int `json:"edges_added"`
	EdgesRemoved  int `json:"edges_removed"`
	EdgesModified int `json:"edges_modified"`
}

type DriftAlert struct {
	Severity string `json:"severity"`
	Message  string `json:"message"`
	EntityID string `json:"entity_id,omitempty"`
}

type Entity struct {
	ID            string         `json:"id"`
	Type          string         `json:"type"`
	SubType       string         `json:"sub_type,omitempty"`
	CanonicalName string         `json:"canonical_name"`
	Namespace     string         `json:"namespace"`
	Confidence    float64        `json:"confidence"`
	IsActive      bool           `json:"is_active"`
	Properties    map[string]any `json:"properties,omitempty"`
	Source        SourceInfo     `json:"source"`
}

type SourceInfo struct {
	SourceType string    `json:"source_type"`
	SourceID   string    `json:"source_id"`
	SourceRef  string    `json:"source_ref"`
	ObservedAt time.Time `json:"observed_at"`
}

type Relationship struct {
	ID         string         `json:"id"`
	Type       string         `json:"type"`
	FromID     string         `json:"from_id"`
	ToID       string         `json:"to_id"`
	Confidence float64        `json:"confidence"`
	IsActive   bool           `json:"is_active"`
	Properties map[string]any `json:"properties,omitempty"`
}

type NamespaceListing struct {
	Namespace     string          `json:"namespace"`
	Entities      []*Entity       `json:"entities"`
	Relationships []*Relationship `json:"relationships"`
}

// Config maps .archgraph.yaml
type Config struct {
	Version    string           `yaml:"version"`
	Namespace  string           `yaml:"namespace"`
	Governance GovernanceConfig `yaml:"governance"`
}

type GovernanceConfig struct {
	Rules []RuleConfig `yaml:"rules"`
}

type RuleConfig struct {
	ID       string `yaml:"id"`
	Name     string `yaml:"name"`
	Severity string `yaml:"severity"`
	Scope    string `yaml:"scope"`
}

func main() {
	// Base configuration
	zone4Addr := flag.String("zone4", "http://localhost:8080", "Zone 4 Base URL")
	zone5Addr := flag.String("zone5", "http://localhost:8081", "Zone 5 Base URL")
	configPath := flag.String("config", ".archgraph.yaml", "Path to .archgraph.yaml file")
	flag.Parse()

	args := flag.Args()
	if len(args) < 1 {
		printUsage()
		os.Exit(1)
	}

	subcommand := args[0]
	subArgs := args[1:]

	cfg, err := loadConfig(*configPath)
	if err != nil {
		// Fallback defaults if config file is not found
		cfg = &Config{
			Namespace: "acme",
		}
	}

	ctx := context.Background()

	switch subcommand {
	case "query":
		handleQuery(ctx, *zone5Addr, cfg.Namespace, subArgs)
	case "diff":
		handleDiff(ctx, *zone5Addr, cfg.Namespace, subArgs)
	case "impact":
		handleImpact(ctx, *zone4Addr, *zone5Addr, cfg.Namespace, subArgs)
	case "validate":
		handleValidate(ctx, *zone4Addr, *zone5Addr, cfg, subArgs)
	case "graph":
		handleGraph(ctx, *zone4Addr, cfg.Namespace, subArgs)
	case "document":
		handleDocument(ctx, *zone4Addr, *zone5Addr, cfg, subArgs)
	case "mcp":
		handleMCP(ctx, *zone4Addr, *zone5Addr, cfg.Namespace)
	case "dashboard":
		handleDashboard(ctx, *zone4Addr, *zone5Addr, cfg.Namespace, subArgs)
	case "analyze-pr":
		handleAnalyzePR(ctx, *zone4Addr, *zone5Addr, cfg, subArgs)
	default:
		fmt.Printf("Unknown subcommand: %s\n", subcommand)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println("Usage: archgraph [flags] <subcommand> [args]")
	fmt.Println("\nFlags:")
	fmt.Println("  -zone4  string  Zone 4 base URL (default: http://localhost:8080)")
	fmt.Println("  -zone5  string  Zone 5 base URL (default: http://localhost:8081)")
	fmt.Println("  -config string  Path to governance config (default: .archgraph.yaml)")
	fmt.Println("\nSubcommands:")
	fmt.Println("  graph                               Draw visual dependency graph in the terminal")
	fmt.Println("  query \"<question>\"                  Run natural-language queries against serving layer")
	fmt.Println("  diff <commit_sha_1> <commit_sha_2>  Compare structural changes between two Git commits")
	fmt.Println("  impact --file <path> [--line <num>] Calculate blast radius of a file or code block")
	fmt.Println("  impact <entity_id>                  Calculate blast radius of a canonical entity ID")
	fmt.Println("  validate [--detail]                  Validate codebase structures against governance rules")
	fmt.Println("  document [--out <file>]             Generate markdown documentation for the codebase")
	fmt.Println("  mcp                                 Run as a Model Context Protocol (MCP) server over stdio")
	fmt.Println("  dashboard [--port <port>]           Launch the interactive Web Dashboard")
	fmt.Println("  analyze-pr [--base-ref <ref>] [--head-ref <ref>] Evaluate pull request changes")
}

func loadConfig(path string) (*Config, error) {
	finalPath := path
	if path == ".archgraph.yaml" {
		dir, err := os.Getwd()
		if err == nil {
			for {
				candidate := filepath.Join(dir, ".archgraph.yaml")
				if _, err := os.Stat(candidate); err == nil {
					finalPath = candidate
					break
				}
				parent := filepath.Dir(dir)
				if parent == dir {
					break
				}
				dir = parent
			}
		}
	}

	data, err := os.ReadFile(finalPath)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// Subcommand handlers

func handleQuery(ctx context.Context, zone5Addr, namespace string, args []string) {
	if len(args) < 1 {
		fmt.Println("Error: Query subcommand requires a question argument.")
		fmt.Println("Usage: archgraph query \"<question>\"")
		os.Exit(1)
	}
	question := args[0]

	reqBody, _ := json.Marshal(AskReq{
		Question:  question,
		Namespace: namespace,
	})

	resp, err := postJSON(ctx, zone5Addr+"/v1/ask", reqBody)
	if err != nil {
		fmt.Printf("Error contacting Zone 5: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		fmt.Printf("Error from serving layer (status %d): %s\n", resp.StatusCode, body)
		os.Exit(1)
	}

	var askResp AskResp
	if err := json.NewDecoder(resp.Body).Decode(&askResp); err != nil {
		fmt.Printf("Error decoding response: %v\n", err)
		os.Exit(1)
	}

	if askResp.Answer != nil {
		fmt.Println("🤖 Answer:")
		fmt.Println(askResp.Answer.Text)
		if askResp.Answer.MermaidDiagram != "" {
			fmt.Println("\n📊 Structure Visualization:")
			fmt.Printf("```mermaid\n%s\n```\n", askResp.Answer.MermaidDiagram)
		}
		fmt.Printf("\n[LLM: %s, Confidence: %.2f]\n", askResp.Answer.UsedLLM, askResp.Answer.Confidence)
	} else if askResp.BlastRadius != nil {
		printBlastRadiusReport(askResp.BlastRadius)
	} else if askResp.HealthReport != nil {
		printHealthReport(askResp.HealthReport)
	} else {
		fmt.Println("Query executed successfully. (No narrative answer returned)")
	}
}

func handleDiff(ctx context.Context, zone5Addr, namespace string, args []string) {
	if len(args) < 2 {
		fmt.Println("Error: Diff subcommand requires two Git commit references.")
		fmt.Println("Usage: archgraph diff <commit_sha_1> <commit_sha_2>")
		os.Exit(1)
	}
	refA := args[0]
	refB := args[1]

	timeA, err := getCommitTime(refA)
	if err != nil {
		fmt.Printf("Error resolving commit %s: %v\n", refA, err)
		os.Exit(1)
	}

	timeB, err := getCommitTime(refB)
	if err != nil {
		fmt.Printf("Error resolving commit %s: %v\n", refB, err)
		os.Exit(1)
	}

	reqBody, _ := json.Marshal(map[string]any{
		"namespace": namespace,
		"from":      timeA,
		"to":        timeB,
	})

	resp, err := postJSON(ctx, zone5Addr+"/v1/diff", reqBody)
	if err != nil {
		fmt.Printf("Error contacting Zone 5: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		fmt.Printf("Error from serving layer (status %d): %s\n", resp.StatusCode, body)
		os.Exit(1)
	}

	var evo Evolution
	if err := json.NewDecoder(resp.Body).Decode(&evo); err != nil {
		fmt.Printf("Error decoding diff response: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("🔄 Architecture Diff [%s ➔ %s]\n", refA, refB)
	fmt.Printf("Time Range: %s to %s\n\n", evo.From.Format(time.RFC3339), evo.To.Format(time.RFC3339))
	fmt.Println("Diff Summary:")
	fmt.Printf("  Nodes Added:    %d\n", evo.DiffSummary.NodesAdded)
	fmt.Printf("  Nodes Removed:  %d\n", evo.DiffSummary.NodesRemoved)
	fmt.Printf("  Nodes Updated:  %d\n", evo.DiffSummary.NodesUpdated)
	fmt.Printf("  Edges Added:    %d\n", evo.DiffSummary.EdgesAdded)
	fmt.Printf("  Edges Removed:  %d\n", evo.DiffSummary.EdgesRemoved)
	fmt.Printf("  Edges Modified: %d\n", evo.DiffSummary.EdgesModified)

	if len(evo.DriftAlerts) > 0 {
		fmt.Println("\n⚠️ Drift Alerts:")
		for _, alert := range evo.DriftAlerts {
			fmt.Printf("  [%s] %s (Entity ID: %s)\n", alert.Severity, alert.Message, alert.EntityID)
		}
	} else {
		fmt.Println("\n✅ No architecture drift alerts detected.")
	}
}

func handleImpact(ctx context.Context, zone4Addr, zone5Addr, namespace string, args []string) {
	var fileArg string
	var lineArg int
	var maxDepth int

	fs := flag.NewFlagSet("impact", flag.ExitOnError)
	fs.StringVar(&fileArg, "file", "", "Code file path")
	fs.IntVar(&lineArg, "line", 0, "Line number within the file")
	fs.IntVar(&maxDepth, "depth", 3, "Maximum traversal depth")
	_ = fs.Parse(args)

	var entityID string

	// Check if positional argument is given as entity ID
	if fileArg == "" && len(fs.Args()) > 0 {
		entityID = fs.Args()[0]
	}

	if fileArg != "" {
		resolvedID, err := findEntityByFileAndLine(ctx, zone4Addr, namespace, fileArg, lineArg)
		if err != nil {
			fmt.Printf("Error resolving file %s to entity: %v\n", fileArg, err)
			os.Exit(1)
		}
		entityID = resolvedID
		fmt.Printf("Resolved file %s (line %d) to canonical Entity ID: %s\n", fileArg, lineArg, entityID)
	}

	if entityID == "" {
		fmt.Println("Error: Must specify either an entity ID or a --file path.")
		fmt.Println("Usage: archgraph impact <entity_id>")
		fmt.Println("   or: archgraph impact --file <path> [--line <number>]")
		os.Exit(1)
	}

	reqBody, _ := json.Marshal(map[string]any{
		"entity_id": entityID,
		"max_depth": maxDepth,
	})

	resp, err := postJSON(ctx, zone5Addr+"/v1/blast-radius", reqBody)
	if err != nil {
		fmt.Printf("Error contacting Zone 5: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		fmt.Printf("Error from serving layer (status %d): %s\n", resp.StatusCode, body)
		os.Exit(1)
	}

	var br BlastRadius
	if err := json.NewDecoder(resp.Body).Decode(&br); err != nil {
		fmt.Printf("Error decoding blast radius response: %v\n", err)
		os.Exit(1)
	}

	printBlastRadiusReport(&br)
}

func handleValidate(ctx context.Context, zone4Addr, zone5Addr string, cfg *Config, args []string) {
	var detail bool
	fs := flag.NewFlagSet("validate", flag.ExitOnError)
	fs.BoolVar(&detail, "detail", false, "Show detailed rule breakdown")
	_ = fs.Parse(args)

	// 1. Fetch Smells from Zone 5 Health Audit
	reqBody, _ := json.Marshal(map[string]string{
		"namespace": cfg.Namespace,
	})

	resp, err := postJSON(ctx, zone5Addr+"/v1/health-audit", reqBody)
	if err != nil {
		fmt.Printf("Error contacting Zone 5 audit: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		fmt.Printf("Error from serving layer (status %d): %s\n", resp.StatusCode, body)
		os.Exit(1)
	}

	var report HealthReport
	if err := json.NewDecoder(resp.Body).Decode(&report); err != nil {
		fmt.Printf("Error decoding audit report: %v\n", err)
		os.Exit(1)
	}

	// 2. Fetch all entities in namespace to run require-service-owners check
	allEntities, err := fetchAllEntities(ctx, zone4Addr, cfg.Namespace)
	if err != nil {
		fmt.Printf("Warning: Could not fetch namespace entities: %v\n", err)
	}

	// Check require-service-owners (RULE-02)
	for _, e := range allEntities {
		if e.Type == "SERVICE" {
			owner, hasOwner := e.Properties["owner"]
			primaryOwner, hasPrimaryOwner := e.Properties["primary_owner"]

			// If both are missing or empty
			isMissing := true
			if hasOwner {
				if str, ok := owner.(string); ok && str != "" {
					isMissing = false
				}
			}
			if hasPrimaryOwner && isMissing {
				if m, ok := primaryOwner.(map[string]any); ok {
					if t, ok := m["team_name"].(string); ok && t != "" {
						isMissing = false
					}
				}
			}

			if isMissing {
				report.Smells = append(report.Smells, Smell{
					Type:     "require-service-owners",
					Severity: "HIGH",
					Message:  fmt.Sprintf("Service %s has no declared owner", e.CanonicalName),
					Nodes:    []string{e.CanonicalName},
					NodeIDs:  []string{e.ID},
				})
			}
		}
	}

	// 3. Map smells to governance config
	ruleMap := make(map[string]*RuleConfig)
	for i := range cfg.Governance.Rules {
		r := &cfg.Governance.Rules[i]
		ruleMap[r.Name] = r
	}

	failed := false
	var violations []string

	fmt.Println("🛡️  Checking Architecture Boundaries...")

	for _, smell := range report.Smells {
		ruleName := ""
		switch smell.Type {
		case "circular_dependency":
			ruleName = "no-circular-dependencies"
		case "shared_database_coupling":
			ruleName = "shared-database-table"
		case "require-service-owners":
			ruleName = "require-service-owners"
		}

		severity := smell.Severity
		ruleID := "UNKNOWN"
		if ruleName != "" {
			if r, ok := ruleMap[ruleName]; ok {
				severity = r.Severity
				ruleID = r.ID
			}
		}

		if severity == "FAIL" {
			failed = true
			violations = append(violations, fmt.Sprintf("❌ FAIL [%s] (%s): %s on nodes: %v", ruleID, ruleName, smell.Message, smell.Nodes))
		} else if severity == "WARN" {
			violations = append(violations, fmt.Sprintf("⚠️ WARN [%s] (%s): %s on nodes: %v", ruleID, ruleName, smell.Message, smell.Nodes))
		}
	}

	// Output
	if len(violations) > 0 {
		fmt.Printf("\nFound %d governance violations:\n", len(violations))
		for _, v := range violations {
			fmt.Println(v)
		}
	} else {
		fmt.Println("\n✅ Architecture validation passed. No violations.")
	}

	if detail {
		fmt.Printf("\nGovernance Audit Details:\n")
		fmt.Printf("  Namespace:     %s\n", report.Namespace)
		fmt.Printf("  Total Nodes:   %d\n", report.Summary.Entities)
		fmt.Printf("  Total Edges:   %d\n", report.Summary.Relationships)
		fmt.Printf("  Cycles Found:  %d\n", report.Summary.CyclesFound)
		fmt.Printf("  Supernodes:    %d\n", report.Summary.Supernodes)
	}

	if failed {
		fmt.Println("\n❌ Commit rejected: Your changes introduce critical architectural smells.")
		os.Exit(1)
	}
	fmt.Println("\n✅ Ready to commit.")
}

// Helpers

func postJSON(ctx context.Context, endpoint string, body []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return http.DefaultClient.Do(req)
}

func getCommitTime(ref string) (time.Time, error) {
	cmd := exec.Command("git", "log", "-1", "--format=%cI", ref)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return time.Time{}, fmt.Errorf("git error: %s: %w", strings.TrimSpace(stderr.String()), err)
	}
	tStr := strings.TrimSpace(stdout.String())
	return time.Parse(time.RFC3339, tStr)
}

func fetchAllEntities(ctx context.Context, zone4Addr, namespace string) ([]*Entity, error) {
	u := fmt.Sprintf("%s/v1/entities?namespace=%s", zone4Addr, url.QueryEscape(namespace))
	resp, err := http.Get(u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("http error %d: %s", resp.StatusCode, b)
	}

	var listing NamespaceListing
	if err := json.NewDecoder(resp.Body).Decode(&listing); err != nil {
		return nil, err
	}
	return listing.Entities, nil
}

func findEntityByFileAndLine(ctx context.Context, zone4Addr, namespace string, path string, line int) (string, error) {
	entities, err := fetchAllEntities(ctx, zone4Addr, namespace)
	if err != nil {
		return "", err
	}
	return resolveEntityByFileAndLine(entities, path, line)
}

func resolveEntityByFileAndLine(entities []*Entity, path string, line int) (string, error) {
	normPath := filepath.Clean(path)

	// First pass: file-level match when no line was requested.
	for _, e := range entities {
		if line == 0 && entityMatchesPath(e, normPath) {
			return e.ID, nil
		}
	}

	// Second pass: if a line was requested, prefer symbol entities in that file.
	if line > 0 {
		for _, e := range entities {
			if entityMatchesPath(e, normPath) && entityMatchesLine(e, line) {
				return e.ID, nil
			}
		}
	}

	// Third pass fallback: look for any entity where the path is just a substring
	for _, e := range entities {
		if entityContainsPath(e, path) {
			return e.ID, nil
		}
	}

	return "", fmt.Errorf("no entity found for file %s (line %d)", path, line)
}

func entityMatchesPath(e *Entity, normPath string) bool {
	for _, candidate := range entityPathCandidates(e) {
		clean := filepath.Clean(candidate)
		if clean == "." || clean == "" {
			continue
		}
		if clean == normPath || strings.HasSuffix(clean, string(filepath.Separator)+normPath) || strings.HasSuffix(normPath, string(filepath.Separator)+clean) {
			return true
		}
	}
	return false
}

func entityContainsPath(e *Entity, path string) bool {
	for _, candidate := range entityPathCandidates(e) {
		if candidate == "" {
			continue
		}
		if strings.Contains(candidate, path) || strings.Contains(path, candidate) {
			return true
		}
	}
	return false
}

func entityPathCandidates(e *Entity) []string {
	candidates := []string{e.CanonicalName}
	if e.Source.SourceRef != "" {
		candidates = append(candidates, e.Source.SourceRef)
	}
	for _, key := range []string{"source_ref", "file", "path", "filepath"} {
		if val, ok := e.Properties[key].(string); ok && val != "" {
			candidates = append(candidates, val)
		}
	}
	return candidates
}

func entityMatchesLine(e *Entity, line int) bool {
	if exact, ok := intProperty(e.Properties["line"]); ok && exact == line {
		return true
	}
	start, hasStart := intProperty(e.Properties["start_line"])
	end, hasEnd := intProperty(e.Properties["end_line"])
	return hasStart && hasEnd && line >= start && line <= end
}

func intProperty(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	case json.Number:
		i, err := n.Int64()
		if err == nil {
			return int(i), true
		}
	}
	return 0, false
}

func printBlastRadiusReport(br *BlastRadius) {
	fmt.Printf("🛡️  Blast Radius Report for: %s (Type: %s)\n", br.Origin.CanonicalName, br.Origin.Type)
	fmt.Printf("Max Traversal Depth: %d, Total Affected Downstreams: %d\n\n", br.MaxDepth, br.TotalAffected)
	if len(br.Affected) == 0 {
		fmt.Println("No downstream systems are affected.")
		return
	}
	fmt.Println("Affected Downstream Nodes:")
	for _, n := range br.Affected {
		fmt.Printf("  - %s (Type: %s, Depth: %d, Probability: %.1f%%)\n",
			n.CanonicalName, n.Type, n.Depth, n.ImpactProbability*100)
	}
}

func printHealthReport(hr *HealthReport) {
	fmt.Printf("🏥 Health Report for Namespace: %s\n", hr.Namespace)
	fmt.Printf("Summary: Nodes=%d, Edges=%d, Cycles=%d, Supernodes=%d\n\n",
		hr.Summary.Entities, hr.Summary.Relationships, hr.Summary.CyclesFound, hr.Summary.Supernodes)
	if len(hr.Smells) == 0 {
		fmt.Println("✅ No architectural smells detected.")
		return
	}
	fmt.Println("Smells Detected:")
	for _, smell := range hr.Smells {
		fmt.Printf("  [%s] %s: %s (Nodes: %v)\n", smell.Severity, smell.Type, smell.Message, smell.Nodes)
	}
}

func handleGraph(ctx context.Context, zone4Addr, namespace string, args []string) {
	var format string
	fs := flag.NewFlagSet("graph", flag.ExitOnError)
	fs.StringVar(&format, "format", "tree", "Output format: tree | mermaid")
	_ = fs.Parse(args)

	listing, err := fetchNamespaceListing(ctx, zone4Addr, namespace)
	if err != nil {
		fmt.Printf("Error fetching namespace graph: %v\n", err)
		os.Exit(1)
	}

	if len(listing.Entities) == 0 {
		fmt.Println("No entities found in this namespace.")
		return
	}

	if format == "mermaid" {
		fmt.Println("```mermaid")
		fmt.Println("flowchart TD")

		// Create a lookup map for entities by ID
		entitiesByID := make(map[string]*Entity)
		for _, e := range listing.Entities {
			entitiesByID[e.ID] = e
		}

		// Helper to check if a relationship is containment (file belongs to project)
		isContainment := func(r *Relationship) bool {
			if r.Type != "DEPENDS_ON" {
				return false
			}
			target, ok := entitiesByID[r.ToID]
			if !ok {
				return false
			}
			source, ok := entitiesByID[r.FromID]
			if !ok {
				return false
			}
			return target.SubType == "project" && source.Type == "MODULE"
		}

		// Helper to sanitize path for Mermaid node ID
		sanitizePathForID := func(path string) string {
			var sb strings.Builder
			sb.WriteString("dir_")
			for _, r := range path {
				if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
					sb.WriteRune(r)
				} else {
					sb.WriteRune('_')
				}
			}
			res := sb.String()
			for strings.Contains(res, "__") {
				res = strings.ReplaceAll(res, "__", "_")
			}
			return res
		}

		// Tracks folders we've created
		folderNodes := make(map[string]string) // path -> folderNodeID
		folderLabels := make(map[string]string) // folderNodeID -> folderName
		type linkKey struct {
			from string
			to   string
		}
		parentChildLinks := make(map[linkKey]bool)

		// Set of leaf nodes that are placed inside the tree structure
		inTree := make(map[string]bool)

		// 1. Trace all containment relationships and build directory tree structure
		for _, r := range listing.Relationships {
			if isContainment(r) {
				fileEnt := entitiesByID[r.FromID]
				projEnt := entitiesByID[r.ToID]

				parts := strings.Split(fileEnt.CanonicalName, "/")
				prevID := projEnt.ID
				var currentPath string

				for i := 0; i < len(parts)-1; i++ {
					if currentPath == "" {
						currentPath = parts[i]
					} else {
						currentPath = currentPath + "/" + parts[i]
					}

					dirID := sanitizePathForID(currentPath)
					if _, exists := folderNodes[currentPath]; !exists {
						folderNodes[currentPath] = dirID
						folderLabels[dirID] = parts[i]
					}

					parentChildLinks[linkKey{from: prevID, to: dirID}] = true
					prevID = dirID
				}

				// Connect the last directory node (or root) to the file node itself
				parentChildLinks[linkKey{from: prevID, to: fileEnt.ID}] = true
				inTree[fileEnt.ID] = true
			}
		}

		// 2. Define folder nodes
		for dirID, folderName := range folderLabels {
			fmt.Printf("    %s[\"📂 %s\"]\n", dirID, folderName)
		}

		// 3. Define original entity nodes
		for _, e := range listing.Entities {
			cleanName := strings.ReplaceAll(e.CanonicalName, "\"", "\\\"")
			// If the entity is inside the tree, we only display the basename for readability
			if inTree[e.ID] {
				parts := strings.Split(e.CanonicalName, "/")
				cleanName = parts[len(parts)-1]
				cleanName = strings.ReplaceAll(cleanName, "\"", "\\\"")
			}
			fmt.Printf("    %s[\"%s (%s)\"]\n", e.ID, cleanName, e.Type)
		}

		// 4. Output the directory tree structure links
		for link := range parentChildLinks {
			fmt.Printf("    %s --> %s\n", link.from, link.to)
		}

		// 5. Output other relationships (cross-file dependencies, calls, etc.)
		for _, r := range listing.Relationships {
			if isContainment(r) {
				continue
			}
			fmt.Printf("    %s -->|%s| %s\n", r.FromID, r.Type, r.ToID)
		}

		fmt.Println("```")
		return
	}

	entMap := make(map[string]*Entity)
	for _, e := range listing.Entities {
		entMap[e.ID] = e
	}

	outRels := make(map[string][]*Relationship)
	for _, r := range listing.Relationships {
		outRels[r.FromID] = append(outRels[r.FromID], r)
	}

	fmt.Printf("🏗️  Visualizing Architecture Graph for Namespace: \033[1;35m%s\033[0m\n", namespace)
	fmt.Println(strings.Repeat("=", 60))

	isCalled := make(map[string]bool)
	for _, r := range listing.Relationships {
		isCalled[r.ToID] = true
	}

	var startNodes []*Entity
	for _, e := range listing.Entities {
		if e.Type == "SERVICE" && !isCalled[e.ID] {
			startNodes = append(startNodes, e)
		}
	}

	if len(startNodes) == 0 {
		for _, e := range listing.Entities {
			if e.Type == "SERVICE" {
				startNodes = append(startNodes, e)
			}
		}
	}

	if len(startNodes) == 0 {
		startNodes = listing.Entities
	}

	for _, start := range startNodes {
		visited := make(map[string]bool)
		printNodeTree(start, entMap, outRels, visited, 0, "", "")
		fmt.Println()
	}
}

func fetchNamespaceListing(ctx context.Context, zone4Addr, namespace string) (*NamespaceListing, error) {
	u := fmt.Sprintf("%s/v1/entities?namespace=%s", zone4Addr, url.QueryEscape(namespace))
	resp, err := http.Get(u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("http error %d: %s", resp.StatusCode, b)
	}

	var listing NamespaceListing
	if err := json.NewDecoder(resp.Body).Decode(&listing); err != nil {
		return nil, err
	}
	return &listing, nil
}

func printNodeTree(n *Entity, entMap map[string]*Entity, outRels map[string][]*Relationship, visited map[string]bool, depth int, printPrefix string, childPrefix string) {
	cyan := "\033[36m"
	green := "\033[32m"
	reset := "\033[0m"
	red := "\033[31;1m"
	gray := "\033[90m"

	color := reset
	if n.Type == "SERVICE" {
		color = cyan
	} else if n.Type == "DATABASE_TABLE" {
		color = green
	}

	fmt.Printf("%s%s[%s]%s %s(%s)%s", printPrefix, color, n.CanonicalName, reset, gray, n.Type, reset)

	if visited[n.ID] {
		fmt.Printf(" %s[CYCLE]%s\n", red, reset)
		return
	}
	fmt.Println()

	rels := outRels[n.ID]
	if len(rels) == 0 {
		return
	}

	newVisited := make(map[string]bool)
	for k, v := range visited {
		newVisited[k] = v
	}
	newVisited[n.ID] = true

	for i, r := range rels {
		target, ok := entMap[r.ToID]
		if !ok {
			continue
		}

		isLastRel := (i == len(rels)-1)

		var nextPrintPrefix string
		var nextChildPrefix string

		yellow := "\033[33m"
		if isLastRel {
			nextPrintPrefix = childPrefix + "└── " + yellow + fmt.Sprintf("(%s)", r.Type) + reset + " ──► "
			nextChildPrefix = childPrefix + "    "
		} else {
			nextPrintPrefix = childPrefix + "├── " + yellow + fmt.Sprintf("(%s)", r.Type) + reset + " ──► "
			nextChildPrefix = childPrefix + "│   "
		}

		printNodeTree(target, entMap, outRels, newVisited, depth+1, nextPrintPrefix, nextChildPrefix)
	}
}

// MCP JSON-RPC 2.0 Types
type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type JSONRPCResponse struct {
	JSONRPC string `json:"jsonrpc"`
	ID      any    `json:"id"`
	Result  any    `json:"result,omitempty"`
	Error   *Error `json:"error,omitempty"`
}

type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type InitializeResult struct {
	ProtocolVersion string             `json:"protocolVersion"`
	Capabilities    ServerCapabilities `json:"capabilities"`
	ServerInfo      ServerInfo         `json:"serverInfo"`
}

type ServerCapabilities struct {
	Tools     struct{} `json:"tools"`
	Resources struct{} `json:"resources"`
}

type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type ToolsListResult struct {
	Tools []MCPTool `json:"tools"`
}

type MCPTool struct {
	Name        string     `json:"name"`
	Description string     `json:"description"`
	InputSchema JSONSchema `json:"inputSchema"`
}

type JSONSchema struct {
	Type       string                `json:"type"`
	Properties map[string]SchemaProp `json:"properties"`
	Required   []string              `json:"required,omitempty"`
}

type SchemaProp struct {
	Type        string `json:"type"`
	Description string `json:"description"`
}

type ToolCallResult struct {
	Content []ToolContent `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

type ToolContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type ResourcesListResult struct {
	Resources []MCPResource `json:"resources"`
}

type MCPResource struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	MimeType    string `json:"mimeType,omitempty"`
}

type ResourceReadResult struct {
	Contents []ResourceContent `json:"contents"`
}

type ResourceContent struct {
	URI      string `json:"uri"`
	MimeType string `json:"mimeType,omitempty"`
	Text     string `json:"text"`
}

var mcpWriteMu sync.Mutex

func sendResult(id any, result any) {
	mcpWriteMu.Lock()
	defer mcpWriteMu.Unlock()
	resp := JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	}
	_ = json.NewEncoder(os.Stdout).Encode(resp)
}

func sendError(id any, code int, message string, data any) {
	mcpWriteMu.Lock()
	defer mcpWriteMu.Unlock()
	resp := JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error: &Error{
			Code:    code,
			Message: message,
		},
	}
	_ = json.NewEncoder(os.Stdout).Encode(resp)
}

func handleMCP(ctx context.Context, zone4Addr, zone5Addr, defaultNamespace string) {
	// Redirect any standard logging to stderr so we do not pollute stdout
	logFile := os.Stderr
	fmt.Fprintln(logFile, "Starting ArchGraph MCP server on stdio...")

	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var req JSONRPCRequest
		if err := json.Unmarshal(line, &req); err != nil {
			sendError(nil, -32700, "Parse error", nil)
			continue
		}

		go handleRequest(ctx, zone4Addr, zone5Addr, defaultNamespace, req)
	}
}

func handleRequest(ctx context.Context, zone4Addr, zone5Addr, defaultNamespace string, req JSONRPCRequest) {
	switch req.Method {
	case "initialize":
		res := InitializeResult{
			ProtocolVersion: "2024-11-05",
			Capabilities:    ServerCapabilities{},
			ServerInfo: ServerInfo{
				Name:    "archgraph",
				Version: "1.0",
			},
		}
		sendResult(req.ID, res)

	case "notifications/initialized":
		// no-op

	case "tools/list":
		res := ToolsListResult{
			Tools: []MCPTool{
				{
					Name:        "archgraph_audit",
					Description: "Audit codebase topology for dependency cycle loops, supernodes, and database coupling.",
					InputSchema: JSONSchema{
						Type: "object",
						Properties: map[string]SchemaProp{
							"namespace": {Type: "string", Description: "Namespace of the codebase to audit (defaults to configuration namespace)."},
						},
					},
				},
				{
					Name:        "archgraph_get_diff",
					Description: "Compare architectural changes between two Git commits/references.",
					InputSchema: JSONSchema{
						Type: "object",
						Properties: map[string]SchemaProp{
							"referenceA": {Type: "string", Description: "First Git commit SHA or branch name."},
							"referenceB": {Type: "string", Description: "Second Git commit SHA or branch name."},
							"namespace":  {Type: "string", Description: "Namespace of the codebase."},
						},
						Required: []string{"referenceA", "referenceB", "namespace"},
					},
				},
				{
					Name:        "archgraph_suggestions",
					Description: "Generate architectural refactoring recommendations based on smells in the codebase.",
					InputSchema: JSONSchema{
						Type: "object",
						Properties: map[string]SchemaProp{
							"namespace": {Type: "string", Description: "Namespace of the codebase."},
						},
						Required: []string{"namespace"},
					},
				},
				{
					Name:        "archgraph_blast_radius",
					Description: "Calculate downstream impact of a file or entity change.",
					InputSchema: JSONSchema{
						Type: "object",
						Properties: map[string]SchemaProp{
							"file":      {Type: "string", Description: "File path to analyze (optional)."},
							"line":      {Type: "integer", Description: "Line number within the file to analyze (optional)."},
							"entityId":  {Type: "string", Description: "Canonical entity ID (optional)."},
							"namespace": {Type: "string", Description: "Namespace of the codebase."},
						},
						Required: []string{"namespace"},
					},
				},
				{
					Name:        "archgraph_ask",
					Description: "Submit a natural-language structural/architectural question to the Serving Layer.",
					InputSchema: JSONSchema{
						Type: "object",
						Properties: map[string]SchemaProp{
							"question":  {Type: "string", Description: "The architectural question (e.g. 'Are there database couplings?')."},
							"namespace": {Type: "string", Description: "Namespace of the codebase."},
						},
						Required: []string{"question", "namespace"},
					},
				},
				{
					Name:        "archgraph_document",
					Description: "Generate structured markdown documentation for the codebase using the knowledge graph.",
					InputSchema: JSONSchema{
						Type: "object",
						Properties: map[string]SchemaProp{
							"namespace": {Type: "string", Description: "Namespace of the codebase to document (defaults to configuration namespace)."},
						},
					},
				},
			},
		}
		sendResult(req.ID, res)

	case "tools/call":
		var params struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			sendError(req.ID, -32602, "Invalid params", nil)
			return
		}

		res, err := callTool(ctx, zone4Addr, zone5Addr, defaultNamespace, params.Name, params.Arguments)
		if err != nil {
			sendResult(req.ID, ToolCallResult{
				Content: []ToolContent{{Type: "text", Text: fmt.Sprintf("Error: %v", err)}},
				IsError: true,
			})
		} else {
			sendResult(req.ID, res)
		}

	case "resources/list":
		res := ResourcesListResult{
			Resources: []MCPResource{
				{URI: "archgraph://schema", Name: "NIF Schema Definition", Description: "Supported entity and relationship types", MimeType: "text/plain"},
				{URI: "archgraph://health/summary", Name: "Namespace Health Summary", Description: "Tally of active graph elements and detected smells", MimeType: "text/plain"},
				{URI: "archgraph://drift/log", Name: "Delta Log Activity", Description: "Activity sequence feed", MimeType: "text/plain"},
			},
		}
		sendResult(req.ID, res)

	case "resources/read":
		var params struct {
			URI string `json:"uri"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			sendError(req.ID, -32602, "Invalid params", nil)
			return
		}

		res, err := readResource(ctx, zone4Addr, zone5Addr, defaultNamespace, params.URI)
		if err != nil {
			sendError(req.ID, -32603, err.Error(), nil)
		} else {
			sendResult(req.ID, res)
		}

	default:
		sendError(req.ID, -32601, "Method not found", nil)
	}
}

func callTool(ctx context.Context, zone4Addr, zone5Addr, defaultNamespace string, name string, args json.RawMessage) (*ToolCallResult, error) {
	switch name {
	case "archgraph_audit":
		var m struct {
			Namespace string `json:"namespace"`
		}
		_ = json.Unmarshal(args, &m)
		ns := m.Namespace
		if ns == "" {
			ns = defaultNamespace
		}

		reqBody, _ := json.Marshal(map[string]string{"namespace": ns})
		resp, err := postJSON(ctx, zone5Addr+"/v1/health-audit", reqBody)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		var hr HealthReport
		if err := json.NewDecoder(resp.Body).Decode(&hr); err != nil {
			return nil, err
		}

		var b bytes.Buffer
		fmt.Fprintf(&b, "🏥 Health Audit for Namespace: %s\n", hr.Namespace)
		fmt.Fprintf(&b, "Summary: Nodes=%d, Edges=%d, Cycles=%d, Supernodes=%d\n\n",
			hr.Summary.Entities, hr.Summary.Relationships, hr.Summary.CyclesFound, hr.Summary.Supernodes)
		if len(hr.Smells) > 0 {
			fmt.Fprintln(&b, "Detected Smells:")
			for _, s := range hr.Smells {
				fmt.Fprintf(&b, "  - [%s] %s: %s (Nodes: %v)\n", s.Severity, s.Type, s.Message, s.Nodes)
			}
		} else {
			fmt.Fprintln(&b, "✅ No smells detected.")
		}

		return &ToolCallResult{Content: []ToolContent{{Type: "text", Text: b.String()}}}, nil

	case "archgraph_get_diff":
		var m struct {
			ReferenceA string `json:"referenceA"`
			ReferenceB string `json:"referenceB"`
			Namespace  string `json:"namespace"`
		}
		if err := json.Unmarshal(args, &m); err != nil {
			return nil, err
		}
		ns := m.Namespace
		if ns == "" {
			ns = defaultNamespace
		}

		tA, err := getCommitTime(m.ReferenceA)
		if err != nil {
			return nil, fmt.Errorf("resolve refA: %w", err)
		}
		tB, err := getCommitTime(m.ReferenceB)
		if err != nil {
			return nil, fmt.Errorf("resolve refB: %w", err)
		}

		reqBody, _ := json.Marshal(map[string]any{
			"namespace": ns,
			"from":      tA,
			"to":        tB,
		})
		resp, err := postJSON(ctx, zone5Addr+"/v1/diff", reqBody)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		var evo Evolution
		if err := json.NewDecoder(resp.Body).Decode(&evo); err != nil {
			return nil, err
		}

		var b bytes.Buffer
		fmt.Fprintf(&b, "🔄 Architecture Diff [%s ➔ %s]\n", m.ReferenceA, m.ReferenceB)
		fmt.Fprintf(&b, "Time Range: %s to %s\n\n", evo.From.Format(time.RFC3339), evo.To.Format(time.RFC3339))
		fmt.Fprintf(&b, "Nodes Added: %d, Removed: %d, Updated: %d\n", evo.DiffSummary.NodesAdded, evo.DiffSummary.NodesRemoved, evo.DiffSummary.NodesUpdated)
		fmt.Fprintf(&b, "Edges Added: %d, Removed: %d, Modified: %d\n", evo.DiffSummary.EdgesAdded, evo.DiffSummary.EdgesRemoved, evo.DiffSummary.EdgesModified)

		if len(evo.DriftAlerts) > 0 {
			fmt.Fprintln(&b, "\nDrift Alerts:")
			for _, alert := range evo.DriftAlerts {
				fmt.Fprintf(&b, "  - [%s] %s (Entity ID: %s)\n", alert.Severity, alert.Message, alert.EntityID)
			}
		}

		return &ToolCallResult{Content: []ToolContent{{Type: "text", Text: b.String()}}}, nil

	case "archgraph_suggestions":
		var m struct {
			Namespace string `json:"namespace"`
		}
		_ = json.Unmarshal(args, &m)
		ns := m.Namespace
		if ns == "" {
			ns = defaultNamespace
		}

		reqBody, _ := json.Marshal(map[string]string{"namespace": ns})
		resp, err := postJSON(ctx, zone5Addr+"/v1/health-audit", reqBody)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		var hr HealthReport
		if err := json.NewDecoder(resp.Body).Decode(&hr); err != nil {
			return nil, err
		}

		var suggestions []string
		for _, s := range hr.Smells {
			switch s.Type {
			case "circular_dependency":
				suggestions = append(suggestions, fmt.Sprintf("- **Break circular coupling**: A dependency cycle of size %d exists between nodes %v. Refactor these to move shared logic to a common helper module or use event-based decoupling.", len(s.Nodes), s.Nodes))
			case "shared_database_coupling":
				suggestions = append(suggestions, fmt.Sprintf("- **Isolate database access**: Multiple services %v are accessing database table %s. To satisfy microservice boundary isolation, wrap database queries in a single owning service and expose APIs for others.", s.Nodes[1:], s.Nodes[0]))
			case "supernode":
				suggestions = append(suggestions, fmt.Sprintf("- **Decouple bottleneck supernode**: The node %v has excessively high degree. Consider dividing its responsibilities or introducing caching/queues.", s.Nodes))
			}
		}

		entities, err := fetchAllEntities(ctx, zone4Addr, ns)
		if err == nil {
			for _, e := range entities {
				if e.Type == "SERVICE" {
					owner, ok1 := e.Properties["owner"]
					pOwner, ok2 := e.Properties["primary_owner"]
					missing := true
					if ok1 {
						if s, ok := owner.(string); ok && s != "" {
							missing = false
						}
					}
					if ok2 && missing {
						if m, ok := pOwner.(map[string]any); ok {
							if s, ok := m["team_name"].(string); ok && s != "" {
								missing = false
							}
						}
					}
					if missing {
						suggestions = append(suggestions, fmt.Sprintf("- **Assign owner**: Service `%s` has no declared owner. Update its metadata in code configuration to assign an owner team.", e.CanonicalName))
					}
				}
			}
		}

		var b bytes.Buffer
		fmt.Fprintf(&b, "💡 Refactoring Suggestions for Namespace: %s\n\n", ns)
		if len(suggestions) > 0 {
			for _, s := range suggestions {
				fmt.Fprintln(&b, s)
			}
		} else {
			fmt.Fprintln(&b, "✅ Codebase is in excellent architectural health! No recommendations needed.")
		}

		return &ToolCallResult{Content: []ToolContent{{Type: "text", Text: b.String()}}}, nil

	case "archgraph_blast_radius":
		var m struct {
			File      string `json:"file"`
			Line      int    `json:"line"`
			EntityID  string `json:"entityId"`
			Namespace string `json:"namespace"`
		}
		if err := json.Unmarshal(args, &m); err != nil {
			return nil, err
		}
		ns := m.Namespace
		if ns == "" {
			ns = defaultNamespace
		}

		entityID := m.EntityID
		if entityID == "" && m.File != "" {
			resolved, err := findEntityByFileAndLine(ctx, zone4Addr, ns, m.File, m.Line)
			if err != nil {
				return nil, err
			}
			entityID = resolved
		}

		if entityID == "" {
			return nil, errors.New("must supply entityId or file path")
		}

		reqBody, _ := json.Marshal(map[string]any{
			"entity_id": entityID,
			"max_depth": 3,
		})
		resp, err := postJSON(ctx, zone5Addr+"/v1/blast-radius", reqBody)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		var br BlastRadius
		if err := json.NewDecoder(resp.Body).Decode(&br); err != nil {
			return nil, err
		}

		var b bytes.Buffer
		fmt.Fprintf(&b, "🛡️ Blast Radius Report for: %s (Type: %s)\n\n", br.Origin.CanonicalName, br.Origin.Type)
		if len(br.Affected) > 0 {
			fmt.Fprintln(&b, "Affected Downstream Callers:")
			for _, n := range br.Affected {
				fmt.Fprintf(&b, "  - %s (Type: %s, Depth: %d, Probability: %.1f%%)\n", n.CanonicalName, n.Type, n.Depth, n.ImpactProbability*100)
			}
		} else {
			fmt.Fprintln(&b, "No downstream caller is affected.")
		}

		return &ToolCallResult{Content: []ToolContent{{Type: "text", Text: b.String()}}}, nil

	case "archgraph_ask":
		var m struct {
			Question  string `json:"question"`
			Namespace string `json:"namespace"`
		}
		if err := json.Unmarshal(args, &m); err != nil {
			return nil, err
		}
		ns := m.Namespace
		if ns == "" {
			ns = defaultNamespace
		}

		reqBody, _ := json.Marshal(AskReq{
			Question:  m.Question,
			Namespace: ns,
		})
		resp, err := postJSON(ctx, zone5Addr+"/v1/ask", reqBody)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		var askResp AskResp
		if err := json.NewDecoder(resp.Body).Decode(&askResp); err != nil {
			return nil, err
		}

		text := "No answer was returned."
		if askResp.Answer != nil {
			text = askResp.Answer.Text
		}

		return &ToolCallResult{Content: []ToolContent{{Type: "text", Text: text}}}, nil

	case "archgraph_document":
		var m struct {
			Namespace string `json:"namespace"`
		}
		_ = json.Unmarshal(args, &m)
		ns := m.Namespace
		if ns == "" {
			ns = defaultNamespace
		}

		docText, err := generateDocumentation(ctx, zone4Addr, zone5Addr, ns)
		if err != nil {
			return nil, err
		}
		return &ToolCallResult{Content: []ToolContent{{Type: "text", Text: docText}}}, nil

	default:
		return nil, fmt.Errorf("unknown tool %s", name)
	}
}

func readResource(ctx context.Context, zone4Addr, zone5Addr, defaultNamespace string, uri string) (*ResourceReadResult, error) {
	switch uri {
	case "archgraph://schema":
		schemaText := `Entities:
  SERVICE               - A microservice or web backend.
  DATABASE_TABLE        - A relational database table.
  TEAM                  - An organizational owning unit.
  FILE                  - Code source files.
  FUNCTION              - Code function blocks.
Relationships:
  CALLS                 - Synchronous runtime call or invocation.
  READS_FROM / WRITES_TO - Database read/write queries.
  CHANGE_COUPLED_WITH   - Implicitly coupled database resources.
  DEPENDS_ON            - Structural dependencies.`
		return &ResourceReadResult{
			Contents: []ResourceContent{
				{URI: uri, MimeType: "text/plain", Text: schemaText},
			},
		}, nil

	case "archgraph://health/summary":
		reqBody, _ := json.Marshal(map[string]string{"namespace": defaultNamespace})
		resp, err := postJSON(ctx, zone5Addr+"/v1/health-audit", reqBody)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		var hr HealthReport
		if err := json.NewDecoder(resp.Body).Decode(&hr); err != nil {
			return nil, err
		}

		summaryText := fmt.Sprintf("Namespace: %s\nTotal Nodes: %d\nTotal Edges: %d\nCircular Cycles Detected: %d\nBottleneck Supernodes: %d",
			hr.Namespace, hr.Summary.Entities, hr.Summary.Relationships, hr.Summary.CyclesFound, hr.Summary.Supernodes)
		return &ResourceReadResult{
			Contents: []ResourceContent{
				{URI: uri, MimeType: "text/plain", Text: summaryText},
			},
		}, nil

	case "archgraph://drift/log":
		resp, err := http.Get(zone4Addr + "/v1/log?limit=20")
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		body, _ := io.ReadAll(resp.Body)
		return &ResourceReadResult{
			Contents: []ResourceContent{
				{URI: uri, MimeType: "text/plain", Text: string(body)},
			},
		}, nil

	default:
		return nil, fmt.Errorf("unknown resource %s", uri)
	}
}

func handleDocument(ctx context.Context, zone4Addr, zone5Addr string, cfg *Config, args []string) {
	var outPath string
	fs := flag.NewFlagSet("document", flag.ExitOnError)
	fs.StringVar(&outPath, "out", "", "Path to write the markdown documentation")
	_ = fs.Parse(args)

	doc, err := generateDocumentation(ctx, zone4Addr, zone5Addr, cfg.Namespace)
	if err != nil {
		fmt.Printf("Error generating documentation: %v\n", err)
		os.Exit(1)
	}

	if outPath != "" {
		err := os.WriteFile(outPath, []byte(doc), 0644)
		if err != nil {
			fmt.Printf("Error writing documentation to %s: %v\n", outPath, err)
			os.Exit(1)
		}
		fmt.Printf("✅ Documentation successfully written to %s\n", outPath)
	} else {
		fmt.Println(doc)
	}
}

func findWorkspaceRoot() string {
	dir, err := os.Getwd()
	if err != nil {
		return "."
	}
	for {
		candidate := filepath.Join(dir, ".archgraph.yaml")
		if _, err := os.Stat(candidate); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	d, _ := os.Getwd()
	return d
}

func generateDirTree(dirPath string, prefix string) string {
	var sb strings.Builder
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return ""
	}

	var filtered []os.DirEntry
	for _, entry := range entries {
		name := entry.Name()
		if name == ".git" || name == "node_modules" || name == "vendor" || name == "bin" || name == ".gemini" || strings.HasPrefix(name, ".") {
			continue
		}
		filtered = append(filtered, entry)
	}

	for i, entry := range filtered {
		isLast := (i == len(filtered)-1)
		var connector string
		if isLast {
			connector = "└── "
		} else {
			connector = "├── "
		}

		sb.WriteString(prefix + connector + entry.Name() + "\n")

		if entry.IsDir() {
			var newPrefix string
			if isLast {
				newPrefix = prefix + "    "
			} else {
				newPrefix = prefix + "│   "
			}
			sb.WriteString(generateDirTree(filepath.Join(dirPath, entry.Name()), newPrefix))
		}
	}
	return sb.String()
}

func hoistREADME(entityPath string) string {
	if entityPath == "" {
		return ""
	}
	dir := filepath.Dir(entityPath)
	candidates := []string{
		filepath.Join(dir, "README.md"),
		filepath.Join(dir, "readme.md"),
		filepath.Join(dir, "README"),
	}
	for _, cand := range candidates {
		if data, err := os.ReadFile(cand); err == nil {
			content := string(data)
			lines := strings.Split(content, "\n")
			if len(lines) > 25 {
				return strings.Join(lines[:25], "\n") + "\n\n*(Truncated. Read full description in " + cand + ")*"
			}
			return content
		}
	}
	return ""
}

func getExecutiveSummary(ctx context.Context, zone5Addr, namespace string, entities []*Entity, relationships []*Relationship) string {
	reqBody, _ := json.Marshal(AskReq{
		Question:  fmt.Sprintf("Summarize the architecture of namespace %s in 3 sentences.", namespace),
		Namespace: namespace,
	})

	client := &http.Client{Timeout: 3 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, zone5Addr+"/v1/ask", bytes.NewReader(reqBody))
	if err == nil {
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err == nil {
			defer resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				var askResp AskResp
				if json.NewDecoder(resp.Body).Decode(&askResp) == nil && askResp.Answer != nil && askResp.Answer.Text != "" && !strings.Contains(askResp.Answer.Text, "No answer") {
					return askResp.Answer.Text
				}
			}
		}
	}

	services := 0
	dbs := 0
	for _, e := range entities {
		if e.Type == "SERVICE" {
			services++
		} else if e.Type == "DATABASE_TABLE" {
			dbs++
		}
	}
	return fmt.Sprintf("The %s system is a microservices-based application containing %d services and %d database tables. The primary entry points coordinate downstream service invocations and database access. The network graph consists of %d entities and %d relationships, reflecting decoupled database operations and service communications.",
		namespace, services, dbs, len(entities), len(relationships))
}

func computeLocalBlastRadius(startID string, entities []*Entity, outRels map[string][]*Relationship, inRels map[string][]*Relationship) ([]string, float64) {
	queue := []string{startID}
	visited := map[string]bool{startID: true}
	depthMap := map[string]int{startID: 0}

	var affected []string
	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]

		currDepth := depthMap[curr]
		if currDepth >= 3 {
			continue
		}

		for _, rel := range inRels[curr] {
			parentID := rel.FromID
			if !visited[parentID] {
				visited[parentID] = true
				depthMap[parentID] = currDepth + 1
				queue = append(queue, parentID)
				affected = append(affected, parentID)
			}
		}
	}

	total := len(entities)
	percentage := 0.0
	if total > 1 {
		percentage = (float64(len(affected)) / float64(total-1)) * 100.0
	}
	return affected, percentage
}

func generateDocumentation(ctx context.Context, zone4Addr, zone5Addr, namespace string) (string, error) {
	listing, err := fetchNamespaceListing(ctx, zone4Addr, namespace)
	if err != nil {
		return "", fmt.Errorf("fetch namespace: %w", err)
	}

	reqBody, _ := json.Marshal(map[string]string{"namespace": namespace})
	resp, err := postJSON(ctx, zone5Addr+"/v1/health-audit", reqBody)
	var healthReport HealthReport
	if err == nil {
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			_ = json.NewDecoder(resp.Body).Decode(&healthReport)
		}
	}

	inDegree := make(map[string]int)
	outDegree := make(map[string]int)
	inRels := make(map[string][]*Relationship)
	outRels := make(map[string][]*Relationship)
	for _, r := range listing.Relationships {
		outDegree[r.FromID]++
		inDegree[r.ToID]++
		inRels[r.ToID] = append(inRels[r.ToID], r)
		outRels[r.FromID] = append(outRels[r.FromID], r)
	}

	entMap := make(map[string]*Entity)
	for _, e := range listing.Entities {
		entMap[e.ID] = e
	}

	var entryPoints []*Entity
	for _, e := range listing.Entities {
		if e.Type == "SERVICE" {
			hasInboundCall := false
			for _, rel := range inRels[e.ID] {
				if rel.Type == "CALLS" {
					hasInboundCall = true
					break
				}
			}
			if !hasInboundCall {
				entryPoints = append(entryPoints, e)
			}
		}
	}

	var orphans []*Entity
	for _, e := range listing.Entities {
		if e.Type != "SERVICE" && inDegree[e.ID] == 0 {
			orphans = append(orphans, e)
		}
	}

	execSummary := getExecutiveSummary(ctx, zone5Addr, namespace, listing.Entities, listing.Relationships)
	wsRoot := findWorkspaceRoot()

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# 🏗️  System Architecture Blueprint: %s\n\n", namespace))

	sb.WriteString("## 📊 Project Metadata & Graph Health\n")
	sb.WriteString(fmt.Sprintf("- **Namespace:** `%s`\n", namespace))
	sb.WriteString(fmt.Sprintf("- **Workspace Directory:** `%s`\n", wsRoot))
	sb.WriteString(fmt.Sprintf("- **Total Registered Entities:** %d\n", len(listing.Entities)))
	sb.WriteString(fmt.Sprintf("- **Active Relationships:** %d\n", len(listing.Relationships)))
	if healthReport.Summary.Entities > 0 {
		sb.WriteString(fmt.Sprintf("- **Dependency Cycles Found:** %d\n", healthReport.Summary.CyclesFound))
		sb.WriteString(fmt.Sprintf("- **Detected Architectural Smells:** %d\n", len(healthReport.Smells)))
	} else {
		sb.WriteString("- **Dependency Cycles Found:** 0\n")
		sb.WriteString("- **Detected Architectural Smells:** 0\n")
	}
	sb.WriteString("\n")

	sb.WriteString("## 📝 Executive Summary\n")
	sb.WriteString(execSummary + "\n\n")

	sb.WriteString("## 🚪 Entry Points\n")
	if len(entryPoints) > 0 {
		sb.WriteString("The following services act as system entry points (having no inbound callers):\n")
		for _, ep := range entryPoints {
			pathStr := ep.Properties["path"]
			if pathStr == nil {
				pathStr = ep.Source.SourceRef
			}
			sb.WriteString(fmt.Sprintf("- **%s** (Type: `%s`, Source: `%v`)\n", ep.CanonicalName, ep.Type, pathStr))
		}
	} else {
		sb.WriteString("No entry points detected (all services have inbound call relationships).\n")
	}
	sb.WriteString("\n")

	sb.WriteString("## 📁 Codebase Directory Structure\n")
	sb.WriteString("```\n")
	sb.WriteString(filepath.Base(wsRoot) + "/\n")
	sb.WriteString(generateDirTree(wsRoot, ""))
	sb.WriteString("```\n\n")

	sb.WriteString("## 🗺️  Detailed Module Breakdown\n")
	for _, e := range listing.Entities {
		sb.WriteString(fmt.Sprintf("### 📦 %s (`%s`)\n", e.CanonicalName, e.Type))

		purpose := "No purpose declared."
		if desc, ok := e.Properties["description"].(string); ok && desc != "" {
			purpose = desc
		} else if e.Type == "SERVICE" {
			purpose = fmt.Sprintf("Microservice coordinating %s operations.", e.CanonicalName)
		} else if e.Type == "DATABASE_TABLE" {
			purpose = fmt.Sprintf("Relational database table storing raw %s data.", e.CanonicalName)
		}
		sb.WriteString(fmt.Sprintf("- **Purpose:** %s\n", purpose))

		owner := "Unassigned"
		if o, ok := e.Properties["owner"].(string); ok && o != "" {
			owner = o
		}
		sb.WriteString(fmt.Sprintf("- **Owner:** %s\n", owner))

		pathStr := e.Properties["path"]
		if pathStr == nil {
			pathStr = e.Source.SourceRef
		}
		if pathStr != "" {
			sb.WriteString(fmt.Sprintf("- **Source Path:** `%v`\n", pathStr))
		}

		outEdges := outRels[e.ID]
		if len(outEdges) > 0 {
			sb.WriteString("- **Key Outbound Dependencies:**\n")
			for _, edge := range outEdges {
				targetNode, ok := entMap[edge.ToID]
				targetName := edge.ToID
				if ok {
					targetName = targetNode.CanonicalName
				}
				sb.WriteString(fmt.Sprintf("  - `-- (%s) -->` **%s**\n", edge.Type, targetName))
			}
		}

		if pStr, ok := pathStr.(string); ok && pStr != "" {
			fullPath := filepath.Join(wsRoot, pStr)
			readmeContent := hoistREADME(fullPath)
			if readmeContent != "" {
				sb.WriteString("\n#### Hoisted Submodule Documentation:\n")
				sb.WriteString(readmeContent + "\n")
			}
		}
		sb.WriteString("\n")
	}

	sb.WriteString("## ⚡ Component API Reference & Contracts\n")
	sb.WriteString("This section documents the operations and contracts registered across components.\n\n")

	hasContracts := false
	for _, e := range listing.Entities {
		if e.Type == "SERVICE" {
			dbReads := []string{}
			dbWrites := []string{}
			calls := []string{}

			for _, edge := range outRels[e.ID] {
				target, ok := entMap[edge.ToID]
				if !ok {
					continue
				}
				if target.Type == "DATABASE_TABLE" {
					if edge.Type == "READS_FROM" {
						dbReads = append(dbReads, target.CanonicalName)
					} else if edge.Type == "WRITES_TO" {
						dbWrites = append(dbWrites, target.CanonicalName)
					}
				} else if edge.Type == "CALLS" {
					calls = append(calls, target.CanonicalName)
				}
			}

			if len(dbReads) > 0 || len(dbWrites) > 0 || len(calls) > 0 {
				hasContracts = true
				sb.WriteString(fmt.Sprintf("### %s API Reference\n", e.CanonicalName))
				if len(calls) > 0 {
					sb.WriteString("- **Downstream Service Contracts:**\n")
					for _, c := range calls {
						sb.WriteString(fmt.Sprintf("  - Invokes `%s` synchronously over HTTP/gRPC\n", c))
					}
				}
				if len(dbReads) > 0 || len(dbWrites) > 0 {
					sb.WriteString("- **Database Contracts & Side Effects:**\n")
					for _, tbl := range dbReads {
						sb.WriteString(fmt.Sprintf("  - Reads from Table `%s`\n", tbl))
					}
					for _, tbl := range dbWrites {
						sb.WriteString(fmt.Sprintf("  - Writes to Table `%s` (Side Effect: DB Mutation)\n", tbl))
					}
				}
				sb.WriteString("\n")
			}
		}
	}
	if !hasContracts {
		sb.WriteString("No component contracts (calls or database operations) are currently tracked.\n\n")
	}

	sb.WriteString("## 🛡️  Cross-Cutting Concerns\n\n")

	sb.WriteString("### Authentication & Authorization\n")
	var authProtected []string
	for _, e := range listing.Entities {
		if e.Type == "SERVICE" && e.CanonicalName != "auth-service" {
			for _, edge := range outRels[e.ID] {
				target := entMap[edge.ToID]
				if target != nil && target.CanonicalName == "auth-service" {
					authProtected = append(authProtected, e.CanonicalName)
					break
				}
			}
		}
	}
	if len(authProtected) > 0 {
		sb.WriteString("The following services have direct dependency links to `auth-service`, enforcing caller validation:\n")
		for _, ap := range authProtected {
			sb.WriteString(fmt.Sprintf("- `%s`\n", ap))
		}
	} else {
		sb.WriteString("No services currently require routing via `auth-service` directly in the dependency graph.\n")
	}
	sb.WriteString("\n")

	sb.WriteString("### State Management & Databases\n")
	var dbs []*Entity
	for _, e := range listing.Entities {
		if e.Type == "DATABASE_TABLE" {
			dbs = append(dbs, e)
		}
	}
	if len(dbs) > 0 {
		sb.WriteString("The system source-of-truth tables and their controlling services are:\n")
		for _, db := range dbs {
			var readers []string
			var writers []string
			for _, r := range listing.Relationships {
				if r.ToID == db.ID {
					caller := entMap[r.FromID]
					if caller != nil {
						if r.Type == "READS_FROM" {
							readers = append(readers, caller.CanonicalName)
						} else if r.Type == "WRITES_TO" {
							writers = append(writers, caller.CanonicalName)
						}
					}
				}
			}
			sb.WriteString(fmt.Sprintf("- **Table `%s`**:\n", db.CanonicalName))
			sb.WriteString(fmt.Sprintf("  - *Writers (Mutators):* %v\n", writers))
			sb.WriteString(fmt.Sprintf("  - *Readers (Consumers):* %v\n", readers))
		}
	} else {
		sb.WriteString("No shared database nodes registered.\n")
	}
	sb.WriteString("\n")

	sb.WriteString("## 🧠  The \"Archigraph\" Special Sauce\n\n")

	sb.WriteString("### Centrality Analysis (Dependency Hubs)\n")
	sb.WriteString("Centrality highlights \"Hub\" entities that have a high number of inbound or outbound links, marking potential single points of failure:\n\n")

	type centrality struct {
		name   string
		t      string
		degree int
		in     int
		out    int
	}
	var cents []centrality
	for _, e := range listing.Entities {
		cents = append(cents, centrality{
			name:   e.CanonicalName,
			t:      e.Type,
			degree: inDegree[e.ID] + outDegree[e.ID],
			in:     inDegree[e.ID],
			out:    outDegree[e.ID],
		})
	}

	for i := 0; i < len(cents); i++ {
		for j := i + 1; j < len(cents); j++ {
			if cents[i].degree < cents[j].degree {
				cents[i], cents[j] = cents[j], cents[i]
			}
		}
	}

	sb.WriteString("| Entity Name | Entity Type | Degree Centrality (In + Out) | Inbound Links | Outbound Links |\n")
	sb.WriteString("|-------------|-------------|----------------------------|---------------|----------------|\n")
	limit := 5
	if len(cents) < limit {
		limit = len(cents)
	}
	for i := 0; i < limit; i++ {
		c := cents[i]
		sb.WriteString(fmt.Sprintf("| **`%s`** | %s | %d | %d | %d |\n", c.name, c.t, c.degree, c.in, c.out))
	}
	sb.WriteString("\n")

	sb.WriteString("### Orphan Detection (Potential Dead Code)\n")
	if len(orphans) > 0 {
		sb.WriteString("The following static modules or tables are registered in the graph but have zero incoming relationships, which may indicate dead code:\n")
		for _, o := range orphans {
			sb.WriteString(fmt.Sprintf("- **`%s`** (Type: `%s`)\n", o.CanonicalName, o.Type))
		}
	} else {
		sb.WriteString("✅ No orphans or dead modules detected. Every entity has inbound references.\n")
	}
	sb.WriteString("\n")

	sb.WriteString("### Change Impact Radius\n")
	sb.WriteString("Computed blast radius showing the percentage of caller systems affected if a module is modified:\n\n")
	for _, e := range listing.Entities {
		if e.Type == "SERVICE" {
			affected, percent := computeLocalBlastRadius(e.ID, listing.Entities, outRels, inRels)
			affectedNames := []string{}
			for _, affID := range affected {
				if node, ok := entMap[affID]; ok {
					affectedNames = append(affectedNames, "`"+node.CanonicalName+"`")
				}
			}
			sb.WriteString(fmt.Sprintf("- **`%s`**: Impact Radius: **%.1f%%**\n", e.CanonicalName, percent))
			if len(affectedNames) > 0 {
				sb.WriteString(fmt.Sprintf("  - *Downstream Callers Affected:* %s\n", strings.Join(affectedNames, ", ")))
			}
		}
	}
	sb.WriteString("\n")

	sb.WriteString("## 📖  Glossary\n")
	sb.WriteString("- **SERVICE:** A microservice or web backend performing domain logic.\n")
	sb.WriteString("- **DATABASE_TABLE:** A relational database table storing schema records.\n")
	sb.WriteString("- **CALLS:** A runtime dependency indicating synchronous HTTP or RPC call.\n")
	sb.WriteString("- **READS_FROM:** Data operation reading database records.\n")
	sb.WriteString("- **WRITES_TO:** Data operation modifying database records.\n")
	sb.WriteString("- **CHANGE_COUPLED_WITH:** Relationship inferring structural or data coupling from shared resources.\n")

	return sb.String(), nil
}

func handleDashboard(ctx context.Context, zone4Addr, zone5Addr, namespace string, args []string) {
	port := "8084"
	fsCmd := flag.NewFlagSet("dashboard", flag.ExitOnError)
	fsCmd.StringVar(&port, "port", "8084", "Dashboard web server port")
	_ = fsCmd.Parse(args)

	// Create sub-FS for the web files
	subFS, err := fs.Sub(webFS, "web")
	if err != nil {
		fmt.Printf("Error creating web filesystem sub-fs: %v\n", err)
		os.Exit(1)
	}

	mux := http.NewServeMux()
	// Serve dynamic config
	mux.HandleFunc("/api/config", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"zone4":     zone4Addr,
			"zone5":     zone5Addr,
			"namespace": namespace,
		})
	})
	// Serve static files
	mux.Handle("/", http.FileServer(http.FS(subFS)))

	serverAddr := ":" + port
	fmt.Printf("🚀 Starting Web Dashboard on http://localhost:%s ...\n", port)
	fmt.Printf("Connecting to Zone 5 at %s\n", zone5Addr)

	// Attempt to open the web browser automatically
	go func() {
		time.Sleep(500 * time.Millisecond)
		openBrowser("http://localhost:" + port)
	}()

	if err := http.ListenAndServe(serverAddr, mux); err != nil {
		fmt.Printf("Dashboard server failed: %v\n", err)
		os.Exit(1)
	}
}

func openBrowser(url string) {
	// Attempt to run open on macOS
	if exec.Command("open", url).Start() == nil {
		return
	}
	// Attempt to run xdg-open on Linux
	if exec.Command("xdg-open", url).Start() == nil {
		return
	}
	// Fallback to cmd on Windows
	_ = exec.Command("cmd", "/c", "start", url).Start()
}

func handleAnalyzePR(ctx context.Context, zone4Addr, zone5Addr string, cfg *Config, args []string) {
	var baseRef string
	var headRef string
	var outFile string

	fsCmd := flag.NewFlagSet("analyze-pr", flag.ExitOnError)
	fsCmd.StringVar(&baseRef, "base-ref", "origin/main", "Base Git reference (e.g. origin/main)")
	fsCmd.StringVar(&headRef, "head-ref", "HEAD", "Head Git reference (e.g. HEAD)")
	fsCmd.StringVar(&outFile, "out", "", "Output file path for the markdown comment (prints to stdout if empty)")
	_ = fsCmd.Parse(args)

	// 1. Get modified files from git diff
	modifiedFiles, err := getModifiedFiles(baseRef, headRef)
	if err != nil {
		fmt.Printf("Error getting modified files: %v\n", err)
		os.Exit(1)
	}

	if len(modifiedFiles) == 0 {
		fmt.Println("No modified files detected between references.")
		return
	}

	// 2. Resolve entities and compute blast radius
	type prImpact struct {
		File     string
		EntityID string
		Entity   *Entity
		Radius   *BlastRadius
	}
	var impacts []prImpact

	for _, file := range modifiedFiles {
		resolvedID, err := findEntityByFileAndLine(ctx, zone4Addr, cfg.Namespace, file, 0)
		if err != nil {
			// File might not map to a graph entity, skip it
			continue
		}
		// Fetch entity details from Zone 5
		u := fmt.Sprintf("%s/v1/entities/%s", zone5Addr, url.PathEscape(resolvedID))
		resp, err := http.Get(u)
		if err != nil {
			continue
		}
		var ent Entity
		if json.NewDecoder(resp.Body).Decode(&ent) == nil {
			// Get blast radius
			reqBody, _ := json.Marshal(map[string]any{
				"entity_id": resolvedID,
				"max_depth": 3,
			})
			respBR, err := postJSON(ctx, zone5Addr+"/v1/blast-radius", reqBody)
			if err == nil {
				var br BlastRadius
				if json.NewDecoder(respBR.Body).Decode(&br) == nil {
					impacts = append(impacts, prImpact{
						File:     file,
						EntityID: resolvedID,
						Entity:   &ent,
						Radius:   &br,
					})
				}
				respBR.Body.Close()
			}
		}
		resp.Body.Close()
	}

	// 3. Get health audit smells
	var smells []Smell
	reqBody, _ := json.Marshal(map[string]string{"namespace": cfg.Namespace})
	resp, err := postJSON(ctx, zone5Addr+"/v1/health-audit", reqBody)
	if err == nil {
		var hr HealthReport
		if json.NewDecoder(resp.Body).Decode(&hr) == nil {
			smells = hr.Smells
		}
		resp.Body.Close()
	}

	// 4. Generate Markdown
	var sb strings.Builder
	sb.WriteString("## 🏗️ ArchGraph Pull Request Analysis\n\n")
	sb.WriteString("This PR was analyzed by **ArchGraph** to assess architectural risks and dependency impact.\n\n")

	if len(impacts) > 0 {
		sb.WriteString("### 🛡️ Downstream Blast Radius\n")
		sb.WriteString("The following table shows the downstream systems and services impacted by files modified in this PR:\n\n")
		sb.WriteString("| Modified File | Resolved Entity | Impacted Downstreams | Risk Rating |\n")
		sb.WriteString("|---|---|---|---|\n")

		for _, imp := range impacts {
			impactedCount := imp.Radius.TotalAffected
			risk := "Low"
			if impactedCount > 5 {
				risk = "🔴 High"
			} else if impactedCount > 2 {
				risk = "🟡 Medium"
			} else {
				risk = "🟢 Low"
			}

			downstreamNames := []string{}
			for _, node := range imp.Radius.Affected {
				if node.Type == "SERVICE" || node.Type == "API_ENDPOINT" {
					downstreamNames = append(downstreamNames, fmt.Sprintf("`%s` (%s)", node.CanonicalName, node.Type))
				}
			}
			downstreamStr := strings.Join(downstreamNames, ", ")
			if downstreamStr == "" {
				downstreamStr = "None"
			}
			if len(downstreamStr) > 80 {
				downstreamStr = downstreamStr[:77] + "..."
			}

			sb.WriteString(fmt.Sprintf("| `%s` | `%s` (%s) | %s | %s |\n", imp.File, imp.Entity.CanonicalName, imp.Entity.Type, downstreamStr, risk))
		}
		sb.WriteString("\n")
	} else {
		sb.WriteString("✅ **No architectural entities affected**: The modified files do not impact any registered service or module boundaries.\n\n")
	}

	// Filter smells related to the modified files/entities
	var relevantSmells []Smell
	for _, smell := range smells {
		isRelevant := false
		for _, nodeName := range smell.Nodes {
			for _, imp := range impacts {
				if strings.Contains(imp.Entity.CanonicalName, nodeName) || strings.Contains(nodeName, imp.Entity.CanonicalName) {
					isRelevant = true
					break
				}
			}
			if isRelevant {
				break
			}
		}
		if isRelevant {
			relevantSmells = append(relevantSmells, smell)
		}
	}

	if len(relevantSmells) > 0 {
		sb.WriteString("### ⚠️ Architectural Violations & Smells\n")
		sb.WriteString("The changes in this PR affect entities with existing architectural smells or introduce new structural risks:\n\n")
		for _, smell := range relevantSmells {
			severityEmoji := "⚠️"
			if smell.Severity == "FAIL" || smell.Severity == "HIGH" {
				severityEmoji = "❌"
			}
			sb.WriteString(fmt.Sprintf("- %s **[%s]**: %s (Affects nodes: %v)\n", severityEmoji, smell.Type, smell.Message, smell.Nodes))
		}
		sb.WriteString("\n")
	} else {
		sb.WriteString("✅ **Architecture Governance Clean**: No structural smells or policy violations detected for modified components.\n\n")
	}

	sb.WriteString("*(For a full interactive graph representation and timeline playback, launch the **ArchGraph Dashboard** locally via `archgraph dashboard`.)*\n")

	mdContent := sb.String()
	if outFile != "" {
		err := os.WriteFile(outFile, []byte(mdContent), 0644)
		if err != nil {
			fmt.Printf("Error writing out file: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("PR impact report written to %s\n", outFile)
	} else {
		fmt.Println(mdContent)
	}
}

func getModifiedFiles(base, head string) ([]string, error) {
	cmd := exec.Command("git", "diff", "--name-only", base+"..."+head)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		// Fallback to git diff base head if base...head fails
		cmd = exec.Command("git", "diff", "--name-only", base, head)
		out.Reset()
		cmd.Stdout = &out
		if err := cmd.Run(); err != nil {
			return nil, err
		}
	}
	var files []string
	scanner := bufio.NewScanner(&out)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			files = append(files, line)
		}
	}
	return files, nil
}
