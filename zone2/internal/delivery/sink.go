// Package delivery is the boundary between Zone 2 and whatever consumes
// NIF records — Zone 4 today, possibly Zone 3 (an event bus) later.
//
// The Sink interface keeps that boundary swappable. Two concrete sinks for
// MVP: Zone4Sink (HTTP to zone4d) and FileSink (JSONL on disk, useful for
// tests and for the "no zone4 running" demo case).
package delivery

import (
	"context"

	"archgraph/nif"
)

// PublishResult tells the orchestrator what landed.
type PublishResult struct {
	EntitiesEmitted      int `json:"entities_emitted"`
	RelationshipsEmitted int `json:"relationships_emitted"`
	Failed               int `json:"failed"`
}

// Sink is the delivery contract. Synchronous on purpose for MVP — async
// queuing belongs in a backpressure controller we haven't built.
type Sink interface {
	Publish(ctx context.Context, batch *nif.Batch) (PublishResult, error)
}
