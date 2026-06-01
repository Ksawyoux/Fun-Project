// Command zone4d is the Zone 4 graph storage daemon.
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

	"archgraph/zone4/internal/graphdb"
	"archgraph/zone4/internal/mutation"
	"archgraph/zone4/internal/search"
	"archgraph/zone4/internal/server"
	"archgraph/zone4/internal/snapshot"
)

func main() {
	var (
		dbPath = flag.String("db", "zone4.db", "SQLite database path (use :memory: for ephemeral)")
		addr   = flag.String("addr", ":8080", "HTTP listen address")
	)
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	store, err := graphdb.Open(*dbPath)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer store.Close()

	indexer := search.New(store.DB())
	indexer.Start(ctx)
	defer indexer.Stop()

	snapshotStore := snapshot.New(store.DB())

	api := mutation.New(store, indexer)
	srv := server.New(store, api, indexer, snapshotStore)

	httpSrv := &http.Server{
		Addr:              *addr,
		Handler:           srv.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		log.Printf("zone4d listening on %s (db=%s)", *addr, *dbPath)
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
