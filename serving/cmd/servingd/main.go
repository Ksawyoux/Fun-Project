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

	"archgraph/serving/internal/reasoner"
	"archgraph/serving/internal/server"
	"archgraph/serving/internal/storageclient"
	"archgraph/serving/internal/wiki"
)

func main() {
	var (
		storageURL = flag.String("storage", "http://localhost:8080", "Storage base URL")
		addr       = flag.String("addr", ":8081", "HTTP listen address")
		wikiCache  = flag.String("wiki-cache", "wiki-cache", "Directory for cached generated wiki pages")
	)
	flag.Parse()

	cl := storageclient.New(*storageURL)

	// LLM adapter: deterministic stub by default. Operators can opt into the
	// local `claude` CLI with ARCHGRAPH_ENABLE_CLAUDE_CLI=1 when the host has an
	// interactive Claude login available.
	var (
		llm     reasoner.LLM = reasoner.StubLLM{}
		llmName              = "stub"
	)
	if os.Getenv("ARCHGRAPH_ENABLE_CLAUDE_CLI") == "1" && os.Getenv("ARCHGRAPH_FORCE_STUB") == "" {
		if cli, err := reasoner.NewClaudeCLI(os.Getenv("CLAUDE_BIN")); err == nil {
			// Wiki pages have larger prompts than interactive Q&A; give the CLI
			// a longer budget so big subsystems don't time out.
			cli.SetTimeout(180 * time.Second)
			llm = cli
			llmName = "claude-cli"
		} else {
			log.Printf("claude CLI not available, using stub reasoner: %v", err)
		}
	}
	reason := reasoner.New(llm, llmName)

	// The wiki generator reuses the same LLM adapter as the reasoner. With the
	// stub LLM it still produces a structured (if terse) wiki; with the claude
	// CLI it produces narrative, code-linked pages like Code Wiki.
	wikiGen := wiki.NewGenerator(cl, llm, llmName, *wikiCache)

	srv := server.New(cl, reason, wikiGen)
	httpSrv := &http.Server{
		Addr:              *addr,
		Handler:           srv.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Printf("servingd listening on %s (storage=%s)", *addr, *storageURL)
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
