// zone2d is the Zone 2 daemon — ingestion supervisor with HTTP control plane.
//
// It loads a JSON config that describes which sources to ingest (Git + Go
// AST for MVP), registers each as an Ingestor, and exposes /v1/runs to
// trigger an end-to-end fetch → publish cycle.
//
// Output landing zone defaults to Zone 4 at http://localhost:8080; use
// -file-sink to write JSONL to disk instead (useful when zone4d isn't up).
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"archgraph/zone2/internal/checkpoint"
	"archgraph/zone2/internal/delivery"
	"archgraph/zone2/internal/dlq"
	"archgraph/zone2/internal/ingestor"
	"archgraph/zone2/internal/ledger"
	"archgraph/zone2/internal/orchestrator"
	"archgraph/zone2/internal/server"
)

// Config is what -config points to. One Git block per repo, one Go AST block
// per Go codebase. If left empty, the daemon falls back to a single default
// source rooted at the current working directory — handy for a quick demo.
type Config struct {
	Git        []ingestor.GitConfig           `json:"git"`
	GoAST      []ingestor.GoASTConfig         `json:"ast_go"`
	PythonAST  []ingestor.PythonASTConfig     `json:"ast_python"`
	TypeScript []ingestor.TypeScriptASTConfig `json:"ast_ts"`
	OpenAPI    []ingestor.OpenAPIConfig       `json:"openapi"`
}

func main() {
	var (
		addr        = flag.String("addr", ":8083", "HTTP listen address")
		stateDir    = flag.String("state", "./zone2-state", "Directory for checkpoints, ledger, DLQ")
		zone3URL    = flag.String("zone3", "http://localhost:8082", "Zone 3 base URL (default ingestion pipeline)")
		zone4URL    = flag.String("zone4", "", "Zone 4 base URL (direct mutations sink, debug/dev only)")
		fileSinkDir = flag.String("file-sink", "", "If set, write to JSONL under this dir instead of HTTP sink")
		configPath  = flag.String("config", "", "Path to zone2 config JSON; if empty, scans CWD as one source")
	)
	flag.Parse()

	if err := os.MkdirAll(*stateDir, 0o755); err != nil {
		if os.IsPermission(err) {
			log.Fatalf("state dir %q: %v (tip: if deploying on Render, you must attach a Persistent Disk mounted at the target directory, or use a relative path)", *stateDir, err)
		}
		log.Fatalf("state dir: %v", err)
	}

	ckpt, err := checkpoint.New(filepath.Join(*stateDir, "checkpoints"))
	if err != nil {
		log.Fatalf("checkpoint store: %v", err)
	}
	led, err := ledger.Open(filepath.Join(*stateDir, "ledger"))
	if err != nil {
		log.Fatalf("ledger: %v", err)
	}
	defer led.Close()
	deadQ, err := dlq.Open(filepath.Join(*stateDir, "dlq"))
	if err != nil {
		log.Fatalf("dlq: %v", err)
	}
	defer deadQ.Close()

	var sink delivery.Sink
	if *fileSinkDir != "" {
		fs, err := delivery.NewFileSink(*fileSinkDir)
		if err != nil {
			log.Fatalf("file sink: %v", err)
		}
		sink = fs
		log.Printf("[zone2] sink: file → %s", *fileSinkDir)
	} else if *zone4URL != "" {
		sink = delivery.NewZone4Sink(*zone4URL)
		log.Printf("[zone2] sink: zone4 (debug/dev direct) → %s", *zone4URL)
	} else {
		sink = delivery.NewZone3Sink(*zone3URL)
		log.Printf("[zone2] sink: zone3 → %s", *zone3URL)
	}

	cfg, err := loadConfig(*configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	reg := orchestrator.NewRegistry()
	for _, g := range cfg.Git {
		if err := reg.Register(ingestor.NewGit(g)); err != nil {
			log.Fatalf("register git %s: %v", g.SourceID, err)
		}
		log.Printf("[zone2] registered git:%s (%s)", g.SourceID, g.RepoPath)
	}
	for _, a := range cfg.GoAST {
		if err := reg.Register(ingestor.NewGoAST(a)); err != nil {
			log.Fatalf("register ast-go %s: %v", a.SourceID, err)
		}
		log.Printf("[zone2] registered ast-go:%s (%s)", a.SourceID, a.RootPath)
	}
	for _, p := range cfg.PythonAST {
		if err := reg.Register(ingestor.NewPythonAST(p)); err != nil {
			log.Fatalf("register ast-python %s: %v", p.SourceID, err)
		}
		log.Printf("[zone2] registered ast-python:%s (%s)", p.SourceID, p.RootPath)
	}
	for _, t := range cfg.TypeScript {
		if err := reg.Register(ingestor.NewTypeScriptAST(t)); err != nil {
			log.Fatalf("register ast-ts %s: %v", t.SourceID, err)
		}
		log.Printf("[zone2] registered ast-ts:%s (%s)", t.SourceID, t.RootPath)
	}
	for _, o := range cfg.OpenAPI {
		if err := reg.Register(ingestor.NewOpenAPI(o)); err != nil {
			log.Fatalf("register openapi %s: %v", o.SourceID, err)
		}
		log.Printf("[zone2] registered openapi:%s (%s)", o.SourceID, o.RootPath)
	}

	runner := &orchestrator.Runner{
		Registry:    reg,
		Checkpoint:  ckpt,
		Ledger:      led,
		DLQ:         deadQ,
		Sink:        sink,
		Concurrency: 4,
	}

	srv := server.New(runner, reg)
	httpServer := &http.Server{
		Addr:              *addr,
		Handler:           srv.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Printf("[zone2] listening on %s", *addr)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("http: %v", err)
		}
	}()

	<-ctx.Done()
	log.Printf("[zone2] shutdown")
	shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = httpServer.Shutdown(shutCtx)
}

// loadConfig reads the JSON file at path, or — if path is empty — returns a
// single-source default that scans the current working directory.
//
// The default convenience config is what makes "go run ./cmd/zone2d" work
// without ceremony. For real use the operator passes -config pointing at a
// JSON file with explicit sources.
func loadConfig(path string) (Config, error) {
	if path == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return Config{}, err
		}
		log.Printf("[zone2] no -config; scanning %s as one default source", cwd)
		return Config{
			Git: []ingestor.GitConfig{{
				SourceID: "default", RepoPath: cwd, Namespace: "local",
			}},
			GoAST: []ingestor.GoASTConfig{{
				SourceID: "default", RootPath: cwd, Namespace: "local",
			}},
		}, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read %s: %w", path, err)
	}
	var c Config
	if err := json.Unmarshal(b, &c); err != nil {
		return Config{}, fmt.Errorf("parse %s: %w", path, err)
	}
	return c, nil
}
