// Command zone5d is the Zone 5 intelligence & serving daemon. It speaks
// JSON over HTTP and talks to zone4d as its data source.
package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"archgraph/zone5/internal/reasoner"
	"archgraph/zone5/internal/server"
	"archgraph/zone5/internal/zone4client"
)

func main() {
	var (
		zone4URL = flag.String("zone4", "http://localhost:8080", "Zone 4 base URL")
		addr     = flag.String("addr", ":8081", "HTTP listen address")
	)
	flag.Parse()

	cl := zone4client.New(*zone4URL)

	// LLM adapter: prefer the local `claude` CLI (Claude Code) so we get
	// real model power using whatever login the host already has — no API
	// key required. Fall back to the deterministic stub if the binary
	// isn't installed, so the pipeline still works offline / in tests.
	// Set ARCHGRAPH_FORCE_STUB=1 to force the stub even when claude exists.
	var (
		llm     reasoner.LLM = reasoner.StubLLM{}
		llmName              = "stub"
	)
	if os.Getenv("ARCHGRAPH_FORCE_STUB") == "" {
		if cli, err := reasoner.NewClaudeCLI(os.Getenv("CLAUDE_BIN")); err == nil {
			llm = cli
			llmName = "claude-cli"
		} else {
			log.Printf("claude CLI not available, using stub reasoner: %v", err)
		}
	}
	reason := reasoner.New(llm, llmName)

	srv := server.New(cl, reason)
	httpSrv := &http.Server{
		Addr:              *addr,
		Handler:           srv.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Printf("zone5d listening on %s (zone4=%s)", *addr, *zone4URL)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("http server: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("shutdown requested")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		log.Printf("http shutdown: %v", err)
	}
}
