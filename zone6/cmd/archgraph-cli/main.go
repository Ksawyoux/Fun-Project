package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

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
	Text           string                 `json:"text"`
	MermaidDiagram string                 `json:"mermaid_diagram,omitempty"`
	Confidence     float64                `json:"confidence"`
	UsedLLM        string                 `json:"used_llm"`
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
	From        time.Time   `json:"from"`
	To          time.Time   `json:"to"`
	DiffSummary DiffSummary `json:"diff_summary"`
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
	ID             string         `json:"id"`
	Type           string         `json:"type"`
	SubType        string         `json:"sub_type,omitempty"`
	CanonicalName  string         `json:"canonical_name"`
	Namespace      string         `json:"namespace"`
	Confidence     float64        `json:"confidence"`
	IsActive       bool           `json:"is_active"`
	Properties     map[string]any `json:"properties,omitempty"`
	Source         SourceInfo     `json:"source"`
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

	// Normalize target path
	normPath := filepath.Clean(path)

	// First pass: look for exact matches on path or relative path
	for _, e := range entities {
		// 1. Check canonical_name (some files are stored with name=path)
		if strings.HasSuffix(filepath.Clean(e.CanonicalName), normPath) || strings.HasSuffix(normPath, filepath.Clean(e.CanonicalName)) {
			if line == 0 {
				return e.ID, nil
			}
		}

		// 2. Check source ref
		if strings.HasSuffix(filepath.Clean(e.Source.SourceRef), normPath) || strings.HasSuffix(normPath, filepath.Clean(e.Source.SourceRef)) {
			if line == 0 {
				return e.ID, nil
			}
		}

		// 3. Check properties
		for _, key := range []string{"path", "filepath", "file"} {
			if val, ok := e.Properties[key].(string); ok {
				if strings.HasSuffix(filepath.Clean(val), normPath) || strings.HasSuffix(normPath, filepath.Clean(val)) {
					if line == 0 {
						return e.ID, nil
					}
				}
			}
		}
	}

	// Second pass: if line number specified, look for symbol entities containing this line
	if line > 0 {
		for _, e := range entities {
			// Ensure it belongs to the target file first
			fileMatch := false
			if strings.HasSuffix(filepath.Clean(e.Source.SourceRef), normPath) || strings.HasSuffix(normPath, filepath.Clean(e.Source.SourceRef)) {
				fileMatch = true
			}
			if !fileMatch {
				for _, key := range []string{"path", "filepath", "file"} {
					if val, ok := e.Properties[key].(string); ok {
						if strings.HasSuffix(filepath.Clean(val), normPath) || strings.HasSuffix(normPath, filepath.Clean(val)) {
							fileMatch = true
							break
						}
					}
				}
			}

			if fileMatch {
				// Check line number range
				startL, hasStart := e.Properties["start_line"]
				endL, hasEnd := e.Properties["end_line"]
				exactL, hasExact := e.Properties["line"]

				if hasExact {
					if lNum, ok := exactL.(float64); ok && int(lNum) == line {
						return e.ID, nil
					}
					if lNum, ok := exactL.(int); ok && lNum == line {
						return e.ID, nil
					}
				}

				if hasStart && hasEnd {
					sVal, ok1 := startL.(float64)
					eVal, ok2 := endL.(float64)
					if ok1 && ok2 && line >= int(sVal) && line <= int(eVal) {
						return e.ID, nil
					}
					sValI, ok1I := startL.(int)
					eValI, ok2I := endL.(int)
					if ok1I && ok2I && line >= sValI && line <= eValI {
						return e.ID, nil
					}
				}
			}
		}
	}

	// Third pass fallback: look for any entity where the path is just a substring
	for _, e := range entities {
		if strings.Contains(e.CanonicalName, path) || strings.Contains(e.Source.SourceRef, path) {
			return e.ID, nil
		}
	}

	return "", fmt.Errorf("no entity found for file %s (line %d)", path, line)
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
		
		// Define nodes
		for _, e := range listing.Entities {
			cleanName := strings.ReplaceAll(e.CanonicalName, "\"", "\\\"")
			fmt.Printf("    %s[\"%s (%s)\"]\n", e.ID, cleanName, e.Type)
		}
		
		// Define connections
		for _, r := range listing.Relationships {
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
