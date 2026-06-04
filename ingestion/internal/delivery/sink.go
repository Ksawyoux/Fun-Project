// Package delivery is the boundary between Zone 2 and whatever consumes
// NIF records — Zone 3 by default, Zone 4 only for direct debug/dev writes.
//
// The Sink interface keeps that boundary swappable. Two concrete sinks for
// MVP: Zone3Sink (HTTP to zone3d), Zone4Sink (direct debug/dev path), and
// FileSink (JSONL on disk, useful for tests).
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
