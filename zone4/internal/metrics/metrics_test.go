package metrics_test

import (
	"context"
	"testing"
	"time"

	"archgraph/zone4/internal/graphdb"
	"archgraph/zone4/internal/metrics"
)

func TestMetrics_IngestAndQuery(t *testing.T) {
	ctx := context.Background()
	store, err := graphdb.Open(":memory:")
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer store.Close()

	metricsStore := metrics.New(store.DB())

	t1 := time.Now().Add(-10 * time.Minute)
	t2 := time.Now().Add(-5 * time.Minute)

	batch := metrics.MetricsBatch{
		Entities: []metrics.EntityMetrics{
			{
				EntityID:          "svc_1",
				Timestamp:         t1,
				RequestRate:       100.0,
				ErrorRate:         0.02,
				P99LatencyMs:      120.0,
				CPUUtilization:    0.35,
				MemoryUtilization: 0.50,
			},
			{
				EntityID:          "svc_1",
				Timestamp:         t2,
				RequestRate:       150.0,
				ErrorRate:         0.01,
				P99LatencyMs:      95.0,
				CPUUtilization:    0.45,
				MemoryUtilization: 0.55,
			},
		},
		Relationships: []metrics.RelationshipMetrics{
			{
				RelationshipID:  "rel_1",
				Timestamp:       t1,
				CallRate:        50.0,
				ErrorRate:       0.0,
				P99LatencyMs:    15.0,
				DataVolumeBytes: 10240,
			},
		},
	}

	if err := metricsStore.Ingest(ctx, batch); err != nil {
		t.Fatalf("ingest failed: %v", err)
	}

	// 1. Query entity metrics range
	results, err := metricsStore.QueryEntityMetrics(ctx, "svc_1", t1.Add(-1*time.Minute), t2.Add(1*time.Minute))
	if err != nil {
		t.Fatalf("query entity metrics failed: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 metrics points, got %d", len(results))
	}
	if results[0].RequestRate != 100.0 || results[1].RequestRate != 150.0 {
		t.Errorf("unexpected entity query results: %+v", results)
	}

	// 2. Query relationship metrics range
	relResults, err := metricsStore.QueryRelationshipMetrics(ctx, "rel_1", t1.Add(-1*time.Minute), t2)
	if err != nil {
		t.Fatalf("query relationship metrics failed: %v", err)
	}
	if len(relResults) != 1 {
		t.Errorf("expected 1 metrics point, got %d", len(relResults))
	}
	if relResults[0].CallRate != 50.0 {
		t.Errorf("unexpected relationship query results: %+v", relResults)
	}

	// 3. Get latest entity metrics
	latest, err := metricsStore.GetLatestEntityMetrics(ctx, "svc_1")
	if err != nil {
		t.Fatalf("get latest entity metrics failed: %v", err)
	}
	if latest == nil || latest.RequestRate != 150.0 {
		t.Errorf("expected latest request rate 150.0, got %+v", latest)
	}
}
