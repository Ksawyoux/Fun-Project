package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"archgraph/nif"
	"archgraph/zone2/internal/checkpoint"
	"archgraph/zone2/internal/delivery"
	"archgraph/zone2/internal/dlq"
	"archgraph/zone2/internal/ledger"
	"archgraph/zone2/internal/orchestrator"
	"archgraph/zone2/internal/server"
)

type mockSink struct {
	published []*nif.Batch
}

func (m *mockSink) Publish(ctx context.Context, b *nif.Batch) (delivery.PublishResult, error) {
	m.published = append(m.published, b)
	return delivery.PublishResult{
		EntitiesEmitted:      len(b.Entities),
		RelationshipsEmitted: len(b.Relationships),
	}, nil
}

func TestServer_GithubWebhook(t *testing.T) {
	dir := t.TempDir()
	ckpt, _ := checkpoint.New(dir + "/c")
	led, _ := ledger.Open(dir + "/l")
	defer led.Close()
	dq, _ := dlq.Open(dir + "/d")
	defer dq.Close()

	reg := orchestrator.NewRegistry()
	runner := &orchestrator.Runner{
		Registry:    reg,
		Checkpoint:  ckpt,
		Ledger:      led,
		DLQ:         dq,
		Sink:        &mockSink{},
		Concurrency: 1,
	}

	srv := server.New(runner, reg)
	mux := srv.Routes()

	req := httptest.NewRequest("POST", "/v1/webhooks/github", bytes.NewReader([]byte(`{"ref":"refs/heads/main"}`)))
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Errorf("expected 202, got %d", w.Code)
	}

	var res map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if res["status"] != "accepted" {
		t.Errorf("expected accepted status, got %q", res["status"])
	}
}

func TestServer_TraceIngestion(t *testing.T) {
	dir := t.TempDir()
	ckpt, _ := checkpoint.New(dir + "/c")
	led, _ := ledger.Open(dir + "/l")
	defer led.Close()
	dq, _ := dlq.Open(dir + "/d")
	defer dq.Close()

	reg := orchestrator.NewRegistry()
	sink := &mockSink{}
	runner := &orchestrator.Runner{
		Registry:    reg,
		Checkpoint:  ckpt,
		Ledger:      led,
		DLQ:         dq,
		Sink:        sink,
		Concurrency: 1,
	}

	srv := server.New(runner, reg)
	mux := srv.Routes()

	payload := `[
		{"trace_id": "t1", "span_id": "s1", "service_name": "checkout-service", "name": "GET /checkout"},
		{"trace_id": "t1", "span_id": "s2", "parent_span_id": "s1", "service_name": "payment-service", "name": "POST /charge"}
	]`

	req := httptest.NewRequest("POST", "/v1/traces", bytes.NewReader([]byte(payload)))
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	// Verify mock sink received the CALLS relationship
	if len(sink.published) != 1 {
		t.Fatalf("expected 1 published batch, got %d", len(sink.published))
	}

	batch := sink.published[0]
	if len(batch.Entities) != 2 {
		t.Errorf("expected 2 entities (services), got %d", len(batch.Entities))
	}
	if len(batch.Relationships) != 1 {
		t.Errorf("expected 1 relationship, got %d", len(batch.Relationships))
	}

	rel := batch.Relationships[0]
	if rel.Type != nif.RelCalls {
		t.Errorf("expected CALLS relationship type, got %s", rel.Type)
	}
}
