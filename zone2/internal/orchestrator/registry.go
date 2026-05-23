// Package orchestrator wires ingestors, checkpoint store, ledger, DLQ, and
// the delivery sink into a single Run pipeline.
//
// Two pieces:
//   - Registry: holds every Ingestor by its metadata ID, validates configs at
//     registration time so misconfiguration fails on boot, not mid-run.
//   - Runner: topologically schedules registered ingestors and executes them,
//     with partial-failure isolation per Zone 2 §6 ("A failed ingestor should
//     never block unrelated ingestors").
package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"

	"archgraph/zone2/internal/ingestor"
)

// Registry is a name→ingestor map with config validation on insert.
type Registry struct {
	mu  sync.RWMutex
	ing map[string]ingestor.Ingestor
}

func NewRegistry() *Registry {
	return &Registry{ing: map[string]ingestor.Ingestor{}}
}

// Register validates the ingestor's config and stores it. Returns an error if
// the ID is already in use or the config rejects.
func (r *Registry) Register(i ingestor.Ingestor) error {
	if i == nil {
		return errors.New("nil ingestor")
	}
	md := i.Identify()
	if md.ID == "" {
		return errors.New("ingestor metadata: id required")
	}
	if err := i.ValidateConfig(); err != nil {
		return fmt.Errorf("validate config for %s: %w", md.ID, err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.ing[md.ID]; ok {
		return fmt.Errorf("ingestor %s already registered", md.ID)
	}
	r.ing[md.ID] = i
	return nil
}

func (r *Registry) Get(id string) (ingestor.Ingestor, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	i, ok := r.ing[id]
	return i, ok
}

// List returns the ingestors in deterministic ID order — used by the HTTP
// /v1/health and /v1/runs handlers.
func (r *Registry) List() []ingestor.Ingestor {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]ingestor.Ingestor, 0, len(r.ing))
	for _, i := range r.ing {
		out = append(out, i)
	}
	sort.Slice(out, func(a, b int) bool {
		return out[a].Identify().ID < out[b].Identify().ID
	})
	return out
}

// Metadata returns just the metadata view, in stable order. Cheap snapshot
// for the HTTP layer.
func (r *Registry) Metadata() []ingestor.Metadata {
	list := r.List()
	out := make([]ingestor.Metadata, len(list))
	for i, ing := range list {
		out[i] = ing.Identify()
	}
	return out
}

// CheckConnectivity calls every ingestor's CheckConnectivity in parallel and
// returns the first error encountered keyed by ingestor ID, or nil if all
// succeed. The HTTP /v1/health handler surfaces this.
func (r *Registry) CheckConnectivity(ctx context.Context) map[string]error {
	list := r.List()
	results := make(map[string]error, len(list))
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, ing := range list {
		wg.Add(1)
		go func(i ingestor.Ingestor) {
			defer wg.Done()
			err := i.CheckConnectivity(ctx)
			mu.Lock()
			results[i.Identify().ID] = err
			mu.Unlock()
		}(ing)
	}
	wg.Wait()
	return results
}
