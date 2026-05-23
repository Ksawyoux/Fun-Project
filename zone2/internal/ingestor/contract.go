// Package ingestor defines the contract every signal source plugs into.
//
// The contract is what makes Zone 2 pluggable: adding a new source means
// implementing this interface, nothing else in the system changes. The
// contract mirrors Zone 2 §3 verbatim, with one Go-ism: Fetch returns a
// Batch rather than an iterator. Iterators are awkward in Go's type system
// and the size of an MVP-scale run (one repo, hundreds of files) fits in
// memory comfortably.
package ingestor

import (
	"context"
	"time"

	"archgraph/zone2/internal/nif"
)

// Metadata is the static description an ingestor advertises to the registry.
type Metadata struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	SourceType    string   `json:"source_type"`
	ConnectorType string   `json:"connector_type"` // pull | push | stream
	Version       string   `json:"version"`
	Dependencies  []string `json:"dependencies,omitempty"` // other ingestor IDs that must run first
}

// Health is what report_health() returns from the spec.
type Health struct {
	Status            string    `json:"status"` // healthy | degraded | failed
	Message           string    `json:"message,omitempty"`
	LastRunAt         time.Time `json:"last_run_at,omitempty"`
	LastSuccessfulAt  time.Time `json:"last_successful_at,omitempty"`
}

// Ingestor is the contract. Each method is small on purpose — the spec is
// explicit that this surface should stay narrow so new sources don't slow
// to add.
type Ingestor interface {
	Identify() Metadata
	// ValidateConfig is called once at registration. Returns nil if the
	// config is usable, an error describing what's missing otherwise.
	ValidateConfig() error
	// CheckConnectivity verifies the source is reachable RIGHT NOW.
	// Cheap: an HTTP HEAD, a "git ls-remote", a filesystem stat.
	CheckConnectivity(ctx context.Context) error
	// Fetch is the work. checkpoint is whatever LoadCheckpoint returned;
	// the ingestor decides what's "since" that checkpoint. The returned
	// Batch carries every entity and relationship observed in this run.
	// The new checkpoint goes in the second return — caller (the runner)
	// persists it ONLY after successful delivery.
	Fetch(ctx context.Context, runID string, checkpoint string) (*nif.Batch, string, error)
}
