package orchestrator

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"sync"
	"time"

	"archgraph/zone2/internal/checkpoint"
	"archgraph/zone2/internal/delivery"
	"archgraph/zone2/internal/dlq"
	"archgraph/zone2/internal/ingestor"
	"archgraph/zone2/internal/ledger"
	"archgraph/nif"
)

// Runner glues an ingestion run together: fetch → validate → DLQ-on-fail →
// publish → checkpoint save → ledger append.
//
// Dependencies (Metadata.Dependencies) are honored as a DAG: an ingestor only
// starts once every dep it lists has finished. Independent ingestors run
// concurrently, capped by `Concurrency`. A failed ingestor marks its own
// ledger entry "failed" but does NOT cancel siblings — the spec is explicit
// that unrelated branches must keep going.
type Runner struct {
	Registry   *Registry
	Checkpoint *checkpoint.Store
	Ledger     *ledger.Ledger
	DLQ        *dlq.Queue
	Sink       delivery.Sink

	// Concurrency caps how many ingestors run in parallel. 0 ⇒ no cap.
	Concurrency int
}

// RunSummary is what /v1/runs returns and what the supervisor logs.
type RunSummary struct {
	RunID     string         `json:"run_id"`
	StartedAt time.Time      `json:"started_at"`
	EndedAt   time.Time      `json:"ended_at"`
	Results   []IngestorRun  `json:"results"`
}

type IngestorRun struct {
	IngestorID string        `json:"ingestor_id"`
	SourceID   string        `json:"source_id"`
	Status     ledger.Status `json:"status"`
	Emitted    int           `json:"emitted"`
	Failed     int           `json:"failed"`
	Skipped    int           `json:"skipped"`
	Duration   time.Duration `json:"duration"`
	Error      string        `json:"error,omitempty"`
}

// RunAll executes every registered ingestor honoring declared dependencies.
// triggerType is recorded in the ledger ("manual", "scheduled", …).
func (r *Runner) RunAll(ctx context.Context, triggerType string) (*RunSummary, error) {
	runID := newRunID()
	started := time.Now().UTC()

	plan, err := topoOrder(r.Registry.List())
	if err != nil {
		return nil, fmt.Errorf("plan: %w", err)
	}

	summary := &RunSummary{RunID: runID, StartedAt: started}

	// State across goroutines.
	var mu sync.Mutex
	done := map[string]ledger.Status{} // ingestor ID → terminal status
	results := map[string]IngestorRun{}

	// Concurrency gate.
	cap := r.Concurrency
	if cap <= 0 {
		cap = len(plan)
		if cap == 0 {
			cap = 1
		}
	}
	sem := make(chan struct{}, cap)

	// dispatch loop: repeatedly pick ready nodes (deps satisfied and not yet
	// started/skipped/done) and fire them off. Loop exits when every node has
	// a terminal status.
	pending := map[string]bool{}
	for _, ing := range plan {
		pending[ing.Identify().ID] = true
	}

	var wg sync.WaitGroup
	cond := sync.NewCond(&mu)

	// Watcher: when one ingestor finishes, broadcast so the dispatch loop wakes.
	dispatchDone := make(chan struct{})
	go func() {
		mu.Lock()
		for len(pending) > 0 {
			progress := false
			for _, ing := range plan {
				id := ing.Identify().ID
				if !pending[id] {
					continue
				}
				if !depsSatisfied(ing.Identify().Dependencies, done) {
					// If any dep is in a terminal failure, skip this one.
					if anyDepFailed(ing.Identify().Dependencies, done) {
						delete(pending, id)
						done[id] = ledger.StatusFailed
						results[id] = IngestorRun{
							IngestorID: id,
							SourceID:   ing.Identify().SourceType,
							Status:     ledger.StatusFailed,
							Error:      "skipped: upstream dependency failed",
						}
						progress = true
					}
					continue
				}
				// Dispatch.
				delete(pending, id)
				progress = true
				wg.Add(1)
				go func(ing ingestor.Ingestor) {
					defer wg.Done()
					sem <- struct{}{}
					defer func() { <-sem }()
					res := r.runOne(ctx, runID, triggerType, ing)
					mu.Lock()
					done[ing.Identify().ID] = res.Status
					results[ing.Identify().ID] = res
					mu.Unlock()
					cond.Broadcast()
				}(ing)
			}
			if !progress {
				// Wait for some in-flight ingestor to finish before re-scanning.
				cond.Wait()
			}
		}
		mu.Unlock()
		close(dispatchDone)
	}()

	<-dispatchDone
	wg.Wait()

	// Stable, deterministic result order.
	ids := make([]string, 0, len(results))
	for id := range results {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		summary.Results = append(summary.Results, results[id])
	}
	summary.EndedAt = time.Now().UTC()
	return summary, nil
}

// runOne is one ingestor's full pipeline. Errors are caught, recorded in the
// ledger, and (for record-level failures) pushed to the DLQ — but never
// propagated to the caller, so a peer ingestor isn't affected.
func (r *Runner) runOne(ctx context.Context, runID, triggerType string, ing ingestor.Ingestor) IngestorRun {
	md := ing.Identify()
	entry := ledger.Entry{
		RunID:       runID,
		IngestorID:  md.ID,
		SourceID:    md.SourceType + ":" + md.ID,
		TriggerType: triggerType,
		StartedAt:   time.Now().UTC(),
		Status:      ledger.StatusFailed, // pessimistic default
	}

	ckpt, err := r.Checkpoint.Load(md.ID)
	if err != nil {
		entry.Errors = append(entry.Errors, "load checkpoint: "+err.Error())
		return r.finishEntry(entry)
	}
	entry.CheckpointBefore = ckpt.Position

	batch, nextCheckpoint, ferr := ing.Fetch(ctx, runID, ckpt.Position)
	if ferr != nil {
		entry.Errors = append(entry.Errors, "fetch: "+ferr.Error())
		return r.finishEntry(entry)
	}
	if batch == nil {
		batch = &nif.Batch{}
	}
	entry.Fetched = batch.Len()

	// Per-record validation. Anything that fails goes to DLQ; the rest flows on.
	cleanEntities := batch.Entities[:0]
	for _, e := range batch.Entities {
		if vErr := nif.ValidateEntity(e); vErr != nil {
			r.pushDLQ(runID, dlq.StageNormalization, e, vErr.Error())
			entry.Failed++
			continue
		}
		cleanEntities = append(cleanEntities, e)
	}
	batch.Entities = cleanEntities

	cleanRels := batch.Relationships[:0]
	for _, rel := range batch.Relationships {
		if vErr := nif.ValidateRelationship(rel); vErr != nil {
			r.pushDLQ(runID, dlq.StageNormalization, rel, vErr.Error())
			entry.Failed++
			continue
		}
		cleanRels = append(cleanRels, rel)
	}
	batch.Relationships = cleanRels
	entry.Transformed = batch.Len()

	// Deliver. A delivery error is terminal for this ingestor's run.
	pubResult, perr := r.Sink.Publish(ctx, batch)
	if perr != nil {
		entry.Errors = append(entry.Errors, "publish: "+perr.Error())
		// Push the whole batch to DLQ so we can retry after fixing the sink.
		r.pushDLQ(runID, dlq.StageDelivery, batch, perr.Error())
		return r.finishEntry(entry)
	}
	entry.Emitted = pubResult.EntitiesEmitted + pubResult.RelationshipsEmitted
	entry.Failed += pubResult.Failed

	// Save checkpoint only after delivery succeeded — the spec is explicit
	// that a half-delivered batch must not advance the cursor.
	if nextCheckpoint != "" && nextCheckpoint != ckpt.Position {
		if cerr := r.Checkpoint.Save(md.ID, nextCheckpoint); cerr != nil {
			entry.Errors = append(entry.Errors, "save checkpoint: "+cerr.Error())
			// Delivery already happened — treat as partial.
			entry.Status = ledger.StatusPartial
			entry.CheckpointAfter = ckpt.Position
			return r.finishEntry(entry)
		}
	}
	entry.CheckpointAfter = nextCheckpoint

	switch {
	case entry.Failed > 0 && entry.Emitted > 0:
		entry.Status = ledger.StatusPartial
	case entry.Failed > 0 && entry.Emitted == 0:
		entry.Status = ledger.StatusFailed
	default:
		entry.Status = ledger.StatusSuccess
	}
	return r.finishEntry(entry)
}

// finishEntry stamps timing and writes the ledger row.
func (r *Runner) finishEntry(e ledger.Entry) IngestorRun {
	e.CompletedAt = time.Now().UTC()
	e.DurationMS = e.CompletedAt.Sub(e.StartedAt).Milliseconds()
	if err := r.Ledger.Append(e); err != nil {
		log.Printf("[orchestrator] ledger append failed for %s: %v", e.IngestorID, err)
	}
	errStr := ""
	if len(e.Errors) > 0 {
		errStr = e.Errors[0]
	}
	return IngestorRun{
		IngestorID: e.IngestorID,
		SourceID:   e.SourceID,
		Status:     e.Status,
		Emitted:    e.Emitted,
		Failed:     e.Failed,
		Skipped:    e.Skipped,
		Duration:   time.Duration(e.DurationMS) * time.Millisecond,
		Error:      errStr,
	}
}

func (r *Runner) pushDLQ(runID string, stage dlq.FailureStage, record any, reason string) {
	if r.DLQ == nil {
		return
	}
	raw, err := json.Marshal(record)
	if err != nil {
		raw = []byte(`"<unmarshalable>"`)
	}
	_ = r.DLQ.Push(dlq.Entry{
		OriginalRecord: raw,
		Reason:         reason,
		Stage:          stage,
		IngestionRun:   runID,
	})
}

// topoOrder verifies the dependency graph is a DAG and returns one valid
// linearization. The runner doesn't actually consume this order at runtime
// (it dispatches based on live readiness) — but a topological sort here
// catches cycles at plan time so we don't spin forever.
func topoOrder(ings []ingestor.Ingestor) ([]ingestor.Ingestor, error) {
	byID := map[string]ingestor.Ingestor{}
	for _, i := range ings {
		byID[i.Identify().ID] = i
	}
	color := map[string]int{} // 0 unseen, 1 in-progress, 2 done
	var order []ingestor.Ingestor
	var visit func(id string, stack []string) error
	visit = func(id string, stack []string) error {
		switch color[id] {
		case 1:
			return fmt.Errorf("dependency cycle: %v → %s", stack, id)
		case 2:
			return nil
		}
		ing, ok := byID[id]
		if !ok {
			return fmt.Errorf("dependency on unregistered ingestor %q (referenced by %v)", id, stack)
		}
		color[id] = 1
		for _, dep := range ing.Identify().Dependencies {
			if err := visit(dep, append(stack, id)); err != nil {
				return err
			}
		}
		color[id] = 2
		order = append(order, ing)
		return nil
	}
	ids := make([]string, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if err := visit(id, nil); err != nil {
			return nil, err
		}
	}
	return order, nil
}

func depsSatisfied(deps []string, done map[string]ledger.Status) bool {
	for _, d := range deps {
		s, ok := done[d]
		if !ok {
			return false
		}
		// Treat partial as satisfied — downstream still got some data.
		if s == ledger.StatusFailed {
			return false
		}
	}
	return true
}

func anyDepFailed(deps []string, done map[string]ledger.Status) bool {
	for _, d := range deps {
		if s, ok := done[d]; ok && s == ledger.StatusFailed {
			return true
		}
	}
	return false
}

func newRunID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return "run_" + hex.EncodeToString(b[:])
}
