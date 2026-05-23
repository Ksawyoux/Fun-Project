// Command archgraph is a process supervisor that brings up every zone in
// the right order and tears them down cleanly on shutdown.
//
// Why a supervisor and not one fused binary: Zone 4 and Zone 5 are
// independent Go modules whose internal packages can't be cross-imported.
// Running them as siblings under one parent matches the production shape
// (each zone is a service) and keeps the local dev story to "one command".
//
// Order:
//   1. zone4d on :8080 — graph storage daemon
//   2. wait for /v1/health to return 200
//   3. zone5d on :8081 — intelligence layer, pointed at zone4d
//
// Output from each child is prefixed with [zone4] / [zone5] so two streams
// interleave readably in the parent terminal. Ctrl+C (or SIGTERM) is
// propagated to both children; they each have their own graceful shutdown.
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
		rootDir   = flag.String("root", ".", "Project root containing zone3/, zone4/, and zone5/")
		zone4Port = flag.String("zone4-port", "8080", "Port for zone4d")
		zone3Port = flag.String("zone3-port", "8082", "Port for zone3d")
		zone5Port = flag.String("zone5-port", "8081", "Port for zone5d")
		zone2Port = flag.String("zone2-port", "8083", "Port for zone2d")
		dbPath    = flag.String("db", "zone4.db", "SQLite database path passed to zone4d")
		zone3Db   = flag.String("zone3-db", "zone3.db", "SQLite database path passed to zone3d")
		zone2State = flag.String("zone2-state", "zone2-state", "State directory for zone2d (checkpoints, ledger, DLQ)")
		zone2Config = flag.String("zone2-config", "", "Path to zone2d config JSON; empty = scan supervisor CWD as one source")
		readyWait = flag.Duration("ready-timeout", 30*time.Second, "Time to wait for zones to become healthy before giving up")
	)
	flag.Parse()

	absRoot, err := filepath.Abs(*rootDir)
	if err != nil {
		log.Fatalf("resolve root: %v", err)
	}
	zone2Dir := filepath.Join(absRoot, "zone2")
	zone3Dir := filepath.Join(absRoot, "zone3")
	zone4Dir := filepath.Join(absRoot, "zone4")
	zone5Dir := filepath.Join(absRoot, "zone5")
	if !dirExists(zone2Dir) || !dirExists(zone3Dir) || !dirExists(zone4Dir) || !dirExists(zone5Dir) {
		log.Fatalf("expected zone2/, zone3/, zone4/ and zone5/ under %s — pass -root if running from elsewhere", absRoot)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var wg sync.WaitGroup

	zone4Addr := ":" + *zone4Port
	zone3Addr := ":" + *zone3Port
	zone5Addr := ":" + *zone5Port
	zone2Addr := ":" + *zone2Port
	zone4URL := "http://localhost:" + *zone4Port
	zone3URL := "http://localhost:" + *zone3Port
	zone2URL := "http://localhost:" + *zone2Port

	// --- Boot zone4d ---
	zone4Cmd, err := startZone(ctx, "zone4", zone4Dir, "./cmd/zone4d",
		"-addr", zone4Addr, "-db", *dbPath)
	if err != nil {
		log.Fatalf("start zone4: %v", err)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := zone4Cmd.Wait(); err != nil && !isSignalErr(err) {
			log.Printf("[archgraph] zone4 exited: %v", err)
			stop() // bring everything down if zone4 dies unexpectedly
		}
	}()

	// --- Wait for zone4 health ---
	if err := waitHealthy(ctx, zone4URL+"/v1/health", *readyWait); err != nil {
		log.Printf("[archgraph] zone4 never became healthy: %v", err)
		stop()
		wg.Wait()
		os.Exit(1)
	}
	log.Printf("[archgraph] zone4 is healthy at %s", zone4URL)

	// --- Boot zone3d ---
	zone3Cmd, err := startZone(ctx, "zone3", zone3Dir, "./cmd/zone3d",
		"-addr", zone3Addr, "-db", *zone3Db, "-zone4", zone4URL)
	if err != nil {
		log.Printf("[archgraph] start zone3: %v", err)
		stop()
		wg.Wait()
		os.Exit(1)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := zone3Cmd.Wait(); err != nil && !isSignalErr(err) {
			log.Printf("[archgraph] zone3 exited: %v", err)
			stop() // bring everything down if zone3 dies unexpectedly
		}
	}()

	// --- Wait for zone3 health ---
	if err := waitHealthy(ctx, zone3URL+"/v1/health", *readyWait); err != nil {
		log.Printf("[archgraph] zone3 never became healthy: %v", err)
		stop()
		wg.Wait()
		os.Exit(1)
	}
	log.Printf("[archgraph] zone3 is healthy at %s", zone3URL)

	// --- Boot zone2d ---
	zone2Args := []string{"-addr", zone2Addr, "-state", *zone2State, "-zone4", zone4URL}
	if *zone2Config != "" {
		zone2Args = append(zone2Args, "-config", *zone2Config)
	}
	zone2Cmd, err := startZone(ctx, "zone2", zone2Dir, "./cmd/zone2d", zone2Args...)
	if err != nil {
		log.Printf("[archgraph] start zone2: %v", err)
		stop()
		wg.Wait()
		os.Exit(1)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := zone2Cmd.Wait(); err != nil && !isSignalErr(err) {
			log.Printf("[archgraph] zone2 exited: %v", err)
		}
	}()

	if err := waitHealthy(ctx, zone2URL+"/v1/health", *readyWait); err != nil {
		log.Printf("[archgraph] zone2 never became healthy: %v", err)
		// non-fatal: zone2 might just have no ingestors yet
	} else {
		log.Printf("[archgraph] zone2 is healthy at %s", zone2URL)
	}

	// --- Boot zone5d ---
	zone5Cmd, err := startZone(ctx, "zone5", zone5Dir, "./cmd/zone5d",
		"-addr", zone5Addr, "-zone4", zone4URL)
	if err != nil {
		log.Printf("[archgraph] start zone5: %v", err)
		stop()
		wg.Wait()
		os.Exit(1)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := zone5Cmd.Wait(); err != nil && !isSignalErr(err) {
			log.Printf("[archgraph] zone5 exited: %v", err)
		}
	}()

	log.Printf("[archgraph] all zones up — zone2 on :%s, zone3 on :%s, zone5 on :%s (talks to zone4 at %s)", *zone2Port, *zone3Port, *zone5Port, zone4URL)

	// Block until signal or a child dies and triggered stop().
	<-ctx.Done()
	log.Printf("[archgraph] shutdown signal received; sending SIGTERM to children")

	// Best-effort graceful kill. Both zoneNd binaries handle SIGTERM and
	// run their own http.Server.Shutdown.
	if zone5Cmd != nil && zone5Cmd.Process != nil {
		_ = zone5Cmd.Process.Signal(syscall.SIGTERM)
	}
	if zone2Cmd != nil && zone2Cmd.Process != nil {
		_ = zone2Cmd.Process.Signal(syscall.SIGTERM)
	}
	if zone3Cmd != nil && zone3Cmd.Process != nil {
		_ = zone3Cmd.Process.Signal(syscall.SIGTERM)
	}
	if zone4Cmd != nil && zone4Cmd.Process != nil {
		_ = zone4Cmd.Process.Signal(syscall.SIGTERM)
	}

	// Give them up to 5s to exit cleanly, then kill.
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
		log.Printf("[archgraph] all zones stopped cleanly")
	case <-time.After(5 * time.Second):
		log.Printf("[archgraph] timed out waiting for shutdown; killing")
		if zone5Cmd != nil && zone5Cmd.Process != nil {
			_ = zone5Cmd.Process.Kill()
		}
		if zone2Cmd != nil && zone2Cmd.Process != nil {
			_ = zone2Cmd.Process.Kill()
		}
		if zone3Cmd != nil && zone3Cmd.Process != nil {
			_ = zone3Cmd.Process.Kill()
		}
		if zone4Cmd != nil && zone4Cmd.Process != nil {
			_ = zone4Cmd.Process.Kill()
		}
		<-done
	}
}

// startZone launches `go run <pkg> <args...>` in `dir` and returns the
// started *exec.Cmd. Output is forwarded to the parent stdout/stderr with a
// per-zone prefix so two daemons interleave readably.
//
// `go run` (not `go build` + exec) is deliberate: it keeps the supervisor
// stateless and avoids stale-binary footguns. The trade-off is a slow first
// start (the Go compiler runs); subsequent starts hit the build cache.
func startZone(ctx context.Context, label, dir, pkg string, args ...string) (*exec.Cmd, error) {
	cmdArgs := append([]string{"run", pkg}, args...)
	cmd := exec.CommandContext(ctx, "go", cmdArgs...)
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
