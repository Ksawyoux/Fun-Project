package orchestrator

import (
	"context"
	"errors"
	"testing"
	"time"

	"archgraph/zone2/internal/checkpoint"
	"archgraph/zone2/internal/delivery"
	"archgraph/zone2/internal/dlq"
	"archgraph/zone2/internal/ingestor"
	"archgraph/zone2/internal/ledger"
	"archgraph/zone2/internal/nif"
)

// fakeIngestor lets us script outcomes without touching git or the filesystem.
type fakeIngestor struct {
	id   string
	deps []string
	fail bool
	hits *[]string
}

func (f *fakeIngestor) Identify() ingestor.Metadata {
	return ingestor.Metadata{
		ID:           f.id,
		Name:         f.id,
		SourceType:   "fake",
		Dependencies: f.deps,
	}
}
func (f *fakeIngestor) ValidateConfig() error                       { return nil }
func (f *fakeIngestor) CheckConnectivity(_ context.Context) error   { return nil }
func (f *fakeIngestor) Fetch(_ context.Context, runID, _ string) (*nif.Batch, string, error) {
	*f.hits = append(*f.hits, f.id)
	if f.fail {
		return nil, "", errors.New("boom")
	}
	return &nif.Batch{Entities: []*nif.Entity{{
		ID: "ent_" + f.id, Type: nif.EntityModule, Name: f.id, Namespace: "test",
		Confidence: 1, IngestionRun: runID,
		Source: nif.SourceInfo{SourceType: "fake", SourceID: f.id, ObservedAt: time.Now().UTC()},
	}}}, "v1", nil
}

// nilSink discards everything successfully — keeps the test off disk.
type nilSink struct{}

func (nilSink) Publish(_ context.Context, b *nif.Batch) (delivery.PublishResult, error) {
	if b == nil {
		return delivery.PublishResult{}, nil
	}
	return delivery.PublishResult{EntitiesEmitted: len(b.Entities), RelationshipsEmitted: len(b.Relationships)}, nil
}

func TestRunner_PartialFailureIsolation(t *testing.T) {
	// a → c (a fails, c must be skipped)   b is independent and must succeed
	hits := []string{}
	reg := NewRegistry()
	mustReg(t, reg, &fakeIngestor{id: "a", fail: true, hits: &hits})
	mustReg(t, reg, &fakeIngestor{id: "b", hits: &hits})
	mustReg(t, reg, &fakeIngestor{id: "c", deps: []string{"a"}, hits: &hits})

	dir := t.TempDir()
	ckpt, _ := checkpoint.New(dir + "/c")
	led, _ := ledger.Open(dir + "/l")
	defer led.Close()
	dq, _ := dlq.Open(dir + "/d")
	defer dq.Close()
	r := &Runner{Registry: reg, Checkpoint: ckpt, Ledger: led, DLQ: dq, Sink: nilSink{}}

	summary, err := r.RunAll(context.Background(), "test")
	if err != nil {
		t.Fatalf("RunAll: %v", err)
	}
	if len(summary.Results) != 3 {
		t.Fatalf("want 3 results, got %d", len(summary.Results))
	}
	got := map[string]ledger.Status{}
	for _, res := range summary.Results {
		got[res.IngestorID] = res.Status
	}
	if got["a"] != ledger.StatusFailed {
		t.Errorf("a should be failed, got %s", got["a"])
	}
	if got["b"] != ledger.StatusSuccess {
		t.Errorf("b should be success (independent of a), got %s", got["b"])
	}
	if got["c"] != ledger.StatusFailed {
		t.Errorf("c should be skipped/failed due to a, got %s", got["c"])
	}
	// c must NOT have been invoked
	for _, h := range hits {
		if h == "c" {
			t.Error("c was invoked despite upstream failure")
		}
	}
}

func TestTopoOrder_DetectsCycle(t *testing.T) {
	a := &fakeIngestor{id: "a", deps: []string{"b"}}
	b := &fakeIngestor{id: "b", deps: []string{"a"}}
	_, err := topoOrder([]ingestor.Ingestor{a, b})
	if err == nil {
		t.Fatal("expected cycle error")
	}
}

func TestTopoOrder_MissingDependency(t *testing.T) {
	a := &fakeIngestor{id: "a", deps: []string{"ghost"}}
	_, err := topoOrder([]ingestor.Ingestor{a})
	if err == nil {
		t.Fatal("expected missing-dep error")
	}
}

func mustReg(t *testing.T, r *Registry, i ingestor.Ingestor) {
	t.Helper()
	if err := r.Register(i); err != nil {
		t.Fatalf("register %s: %v", i.Identify().ID, err)
	}
}
