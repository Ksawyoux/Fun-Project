// Command archgraph is a process supervisor that brings up the services in
// the right order and tears them down cleanly on shutdown.
//
// Why a supervisor and not one fused binary: Storage and Serving are
// independent Go modules whose internal packages can't be cross-imported.
// Running them as siblings under one parent matches the production shape
// (each module is a service) and keeps the local dev story to "one command".
//
// Order:
//   1. storaged on :8080 — graph storage daemon
//   2. wait for /v1/health to return 200
//   3. servingd on :8081 — intelligence/serving layer, pointed at storaged
//
// Output from each child is prefixed with [storage] / [serving] so two streams
// interleave readably in the parent terminal. Ctrl+C (or SIGTERM) is
// propagated to all children; they each have their own graceful shutdown.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

func main() {
	var (
		rootDir         = flag.String("root", ".", "Project root containing pipeline/, storage/, and serving/")
		storagePort     = flag.String("storage-port", "8080", "Port for storaged")
		pipelinePort    = flag.String("pipeline-port", "8082", "Port for pipelined")
		servingPort     = flag.String("serving-port", "8081", "Port for servingd")
		ingestionPort   = flag.String("ingestion-port", "8083", "Port for ingestiond")
		dbPath          = flag.String("db", "storage.db", "SQLite database path passed to storaged")
		pipelineDb      = flag.String("pipeline-db", "pipeline.db", "SQLite database path passed to pipelined")
		ingestionState  = flag.String("ingestion-state", "ingestion-state", "State directory for ingestiond (checkpoints, ledger, DLQ)")
		ingestionConfig = flag.String("ingestion-config", "", "Path to ingestiond config JSON; empty = scan supervisor CWD as one source")
		readyWait       = flag.Duration("ready-timeout", 30*time.Second, "Time to wait for services to become healthy before giving up")
	)
	flag.Parse()

	absRoot, err := filepath.Abs(*rootDir)
	if err != nil {
		log.Fatalf("resolve root: %v", err)
	}
	ingestionDir := filepath.Join(absRoot, "ingestion")
	pipelineDir := filepath.Join(absRoot, "pipeline")
	storageDir := filepath.Join(absRoot, "storage")
	servingDir := filepath.Join(absRoot, "serving")
	if !dirExists(ingestionDir) || !dirExists(pipelineDir) || !dirExists(storageDir) || !dirExists(servingDir) {
		log.Fatalf("expected ingestion/, pipeline/, storage/ and serving/ under %s — pass -root if running from elsewhere", absRoot)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var wg sync.WaitGroup

	storageAddr := ":" + *storagePort
	pipelineAddr := ":" + *pipelinePort
	servingAddr := ":" + *servingPort
	ingestionAddr := ":" + *ingestionPort
	storageURL := "http://localhost:" + *storagePort
	pipelineURL := "http://localhost:" + *pipelinePort
	ingestionURL := "http://localhost:" + *ingestionPort

	// --- Boot storaged ---
	storageCmd, err := startZone(ctx, "storage", storageDir, "./cmd/storaged",
		"-addr", storageAddr, "-db", *dbPath)
	if err != nil {
		log.Fatalf("start storage: %v", err)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := storageCmd.Wait(); err != nil && !isSignalErr(err) {
			log.Printf("[archgraph] storage exited: %v", err)
			stop() // bring everything down if storage dies unexpectedly
		}
	}()

	// --- Wait for storage health ---
	if err := waitHealthy(ctx, storageURL+"/v1/health", *readyWait); err != nil {
		log.Printf("[archgraph] storage never became healthy: %v", err)
		stop()
		wg.Wait()
		os.Exit(1)
	}
	log.Printf("[archgraph] storage is healthy at %s", storageURL)

	// --- Boot pipelined ---
	pipelineCmd, err := startZone(ctx, "pipeline", pipelineDir, "./cmd/pipelined",
		"-addr", pipelineAddr, "-db", *pipelineDb, "-storage", storageURL)
	if err != nil {
		log.Printf("[archgraph] start pipeline: %v", err)
		stop()
		wg.Wait()
		os.Exit(1)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := pipelineCmd.Wait(); err != nil && !isSignalErr(err) {
			log.Printf("[archgraph] pipeline exited: %v", err)
			stop() // bring everything down if pipeline dies unexpectedly
		}
	}()

	// --- Wait for pipeline health ---
	if err := waitHealthy(ctx, pipelineURL+"/v1/health", *readyWait); err != nil {
		log.Printf("[archgraph] pipeline never became healthy: %v", err)
		stop()
		wg.Wait()
		os.Exit(1)
	}
	log.Printf("[archgraph] pipeline is healthy at %s", pipelineURL)

	// --- Boot ingestiond ---
	ingestionArgs := []string{"-addr", ingestionAddr, "-state", *ingestionState, "-pipeline", pipelineURL}
	if *ingestionConfig != "" {
		ingestionArgs = append(ingestionArgs, "-config", *ingestionConfig)
	}
	ingestionCmd, err := startZone(ctx, "ingestion", ingestionDir, "./cmd/ingestiond", ingestionArgs...)
	if err != nil {
		log.Printf("[archgraph] start ingestion: %v", err)
		stop()
		wg.Wait()
		os.Exit(1)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := ingestionCmd.Wait(); err != nil && !isSignalErr(err) {
			log.Printf("[archgraph] ingestion exited: %v", err)
		}
	}()

	if err := waitHealthy(ctx, ingestionURL+"/v1/health", *readyWait); err != nil {
		log.Printf("[archgraph] ingestion never became healthy: %v", err)
		// non-fatal: ingestion might just have no ingestors yet
	} else {
		log.Printf("[archgraph] ingestion is healthy at %s", ingestionURL)
	}

	// --- Boot servingd ---
	servingCmd, err := startZone(ctx, "serving", servingDir, "./cmd/servingd",
		"-addr", servingAddr, "-storage", storageURL)
	if err != nil {
		log.Printf("[archgraph] start serving: %v", err)
		stop()
		wg.Wait()
		os.Exit(1)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := servingCmd.Wait(); err != nil && !isSignalErr(err) {
			log.Printf("[archgraph] serving exited: %v", err)
		}
	}()

	log.Printf("[archgraph] all services up — ingestion on :%s, pipeline on :%s, serving on :%s (talks to storage at %s)", *ingestionPort, *pipelinePort, *servingPort, storageURL)

	// Block until signal or a child dies and triggered stop().
	<-ctx.Done()
	log.Printf("[archgraph] shutdown signal received; sending SIGTERM to children")

	// Best-effort graceful kill. Both ingestiond/pipelined/storaged/servingd binaries handle SIGTERM and
	// run their own http.Server.Shutdown.
	if servingCmd != nil && servingCmd.Process != nil {
		_ = servingCmd.Process.Signal(syscall.SIGTERM)
	}
	if ingestionCmd != nil && ingestionCmd.Process != nil {
		_ = ingestionCmd.Process.Signal(syscall.SIGTERM)
	}
	if pipelineCmd != nil && pipelineCmd.Process != nil {
		_ = pipelineCmd.Process.Signal(syscall.SIGTERM)
	}
	if storageCmd != nil && storageCmd.Process != nil {
		_ = storageCmd.Process.Signal(syscall.SIGTERM)
	}

	// Give them up to 5s to exit cleanly, then kill.
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
		log.Printf("[archgraph] all services stopped cleanly")
	case <-time.After(5 * time.Second):
		log.Printf("[archgraph] timed out waiting for shutdown; killing")
		if servingCmd != nil && servingCmd.Process != nil {
			_ = servingCmd.Process.Kill()
		}
		if ingestionCmd != nil && ingestionCmd.Process != nil {
			_ = ingestionCmd.Process.Kill()
		}
		if pipelineCmd != nil && pipelineCmd.Process != nil {
			_ = pipelineCmd.Process.Kill()
		}
		if storageCmd != nil && storageCmd.Process != nil {
			_ = storageCmd.Process.Kill()
		}
		<-done
	}
}

func startZone(ctx context.Context, label, dir, pkg string, args ...string) (*exec.Cmd, error) {
	binaryName := filepath.Base(pkg)
	binaryPath := filepath.Join(dir, binaryName)
	if !fileExists(binaryPath) {
		if fileExists(binaryPath + ".exe") {
			binaryName += ".exe"
			binaryPath += ".exe"
		}
	}

	var cmd *exec.Cmd
	if fileExists(binaryPath) {
		cmd = exec.CommandContext(ctx, "./"+binaryName, args...)
	} else {
		cmdArgs := append([]string{"run", pkg}, args...)
		cmd = exec.CommandContext(ctx, "go", cmdArgs...)
	}
	cmd.Dir = dir

	// Filter out Go module environment variables to prevent module resolution conflicts in child go processes
	var env []string
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "GOMOD=") && !strings.HasPrefix(e, "GOWORK=") {
			env = append(env, e)
		}
	}
	cmd.Env = env

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("%s stdout pipe: %w", label, err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("%s stderr pipe: %w", label, err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("%s start: %w", label, err)
	}
	go forwardLines(stdout, os.Stdout, "["+label+"] ")
	go forwardLines(stderr, os.Stderr, "["+label+"] ")
	return cmd, nil
}

// forwardLines copies r into w, prefixing each line. Closes silently when
// the source EOFs (the child has exited).
func forwardLines(r io.Reader, w io.Writer, prefix string) {
	scanner := bufio.NewScanner(r)
	// Match Go's default scanner buffer ceiling so long log lines don't
	// silently truncate.
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		fmt.Fprintln(w, prefix+scanner.Text())
	}
}

// waitHealthy polls url until it returns 200 or the deadline elapses.
// The poll interval starts at 100ms and caps at 500ms — we want fast
// detection on the happy path without hammering during a slow Go build.
func waitHealthy(ctx context.Context, url string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	interval := 100 * time.Millisecond
	client := &http.Client{Timeout: 2 * time.Second}

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if time.Now().After(deadline) {
			return errors.New("timed out waiting for /v1/health")
		}
		resp, err := client.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		select {
		case <-time.After(interval):
		case <-ctx.Done():
			return ctx.Err()
		}
		if interval < 500*time.Millisecond {
			interval += 100 * time.Millisecond
		}
	}
}

func dirExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}

func fileExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}

// isSignalErr returns true if the error came from the child being signalled
// — which is normal during a shutdown, not a crash worth logging.
func isSignalErr(err error) bool {
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return false
	}
	if ws, ok := exitErr.Sys().(syscall.WaitStatus); ok {
		return ws.Signaled()
	}
	return false
}
