package delivery

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"archgraph/nif"
)

func TestZone3SinkPublishesNIFBatchToIngestEndpoint(t *testing.T) {
	var gotPath string
	var gotBatch nif.Batch
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBatch); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "mutations_applied": 1})
	}))
	defer ts.Close()

	sink := NewZone3Sink(ts.URL)
	res, err := sink.Publish(context.Background(), &nif.Batch{Entities: []*nif.Entity{{
		ID: "ent_test", Type: nif.EntityModule, Name: "pkg", Namespace: "test",
		Confidence: 1, IngestionRun: "run-1",
		Source: nif.SourceInfo{SourceType: "ast", SourceID: "src", ObservedAt: time.Now().UTC()},
	}}})
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if gotPath != "/v1/ingest" {
		t.Fatalf("path = %s, want /v1/ingest", gotPath)
	}
	if len(gotBatch.Entities) != 1 {
		t.Fatalf("entities sent = %d, want 1", len(gotBatch.Entities))
	}
	if res.EntitiesEmitted != 1 || res.RelationshipsEmitted != 0 {
		t.Fatalf("result = %+v, want one entity emitted", res)
	}
}
