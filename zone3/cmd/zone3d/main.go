// Command zone3d is the Zone 3 processing pipeline daemon.
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

	"archgraph/zone3/internal/pipeline"
	"archgraph/zone3/internal/registry"
	"archgraph/zone3/internal/server"
	"archgraph/zone3/internal/z4client"
)

func main() {
	var (
		dbPath   = flag.String("db", "zone3.db", "SQLite path for local Entity Registry")
		zone4URL = flag.String("zone4", "http://localhost:8080", "Zone 4 base URL")
		addr     = flag.String("addr", ":8082", "HTTP listen address")
	)
	flag.Parse()

	// 1. Open the local Entity Registry SQLite database
	reg, err := registry.Open(*dbPath)
	if err != nil {
		log.Fatalf("open registry db: %v", err)
	}
	defer reg.Close()

	// 2. Initialize Zone 4 HTTP Client
	z4 := z4client.New(*zone4URL)

	// 3. Initialize Pipeline & Server
	pl := pipeline.New(reg, z4)
	srv := server.New(pl, z4)

	httpSrv := &http.Server{
		Addr:              *addr,
		Handler:           srv.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Printf("zone3d listening on %s (db=%s, zone4=%s)", *addr, *dbPath, *zone4URL)
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
