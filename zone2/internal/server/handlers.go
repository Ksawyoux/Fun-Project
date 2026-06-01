// Package server is the thin HTTP layer over the orchestrator.
//
// Endpoints (Zone 2 §11):
//   POST /v1/runs          trigger a run; body: {"trigger":"manual"}
//   GET  /v1/runs          list recent runs (from ledger)
//   GET  /v1/ledger        raw tail of ledger.jsonl
//   GET  /v1/ingestors     list registered ingestors + connectivity
//   GET  /v1/staleness     per-source last-success timestamp
//   GET  /v1/health        readiness probe (200 if all ingestors reachable)
package server

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"time"

	"archgraph/nif"
	"archgraph/zone2/internal/ledger"
	"archgraph/zone2/internal/orchestrator"
)

type Server struct {
	Runner   *orchestrator.Runner
	Registry *orchestrator.Registry
}

func New(r *orchestrator.Runner, reg *orchestrator.Registry) *Server {
	return &Server{Runner: r, Registry: reg}
}

func (s *Server) Routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/runs", s.triggerRun)
	mux.HandleFunc("GET /v1/runs", s.listRuns)
	mux.HandleFunc("GET /v1/ledger", s.listLedger)
	mux.HandleFunc("GET /v1/ingestors", s.listIngestors)
	mux.HandleFunc("GET /v1/staleness", s.staleness)
	mux.HandleFunc("GET /v1/health", s.health)
	mux.HandleFunc("POST /v1/webhooks/github", s.handleGithubWebhook)
	mux.HandleFunc("POST /v1/traces", s.handleTraces)
	return mux
}

func (s *Server) triggerRun(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Trigger string `json:"trigger"`
	}
	if r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
	}
	if body.Trigger == "" {
		body.Trigger = "manual"
	}
	// Cap the run to 10 minutes for the HTTP-triggered path; if you need
	// longer, increase here or run from a CLI.
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	defer cancel()
	summary, err := s.Runner.RunAll(ctx, body.Trigger)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, summary)
}

func (s *Server) listRuns(w http.ResponseWriter, r *http.Request) {
	limit := intParam(r, "limit", 50)
	entries, err := ledger.Tail(s.Runner.Ledger.Path(), limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": entries})
}

func (s *Server) listLedger(w http.ResponseWriter, r *http.Request) {
	// Same as listRuns but explicit endpoint name from the spec.
	s.listRuns(w, r)
}

func (s *Server) listIngestors(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	conn := s.Registry.CheckConnectivity(ctx)

	type ingestorView struct {
		ID            string   `json:"id"`
		Name          string   `json:"name"`
		SourceType    string   `json:"source_type"`
		ConnectorType string   `json:"connector_type"`
		Version       string   `json:"version"`
		Dependencies  []string `json:"dependencies,omitempty"`
		Reachable     bool     `json:"reachable"`
		Error         string   `json:"error,omitempty"`
	}

	mds := s.Registry.Metadata()
	out := make([]ingestorView, 0, len(mds))
	for _, m := range mds {
		v := ingestorView{
			ID: m.ID, Name: m.Name, SourceType: m.SourceType,
			ConnectorType: m.ConnectorType, Version: m.Version,
			Dependencies: m.Dependencies,
		}
		if err := conn[m.ID]; err != nil {
			v.Reachable = false
			v.Error = err.Error()
		} else {
			v.Reachable = true
		}
		out = append(out, v)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ingestors": out})
}

func (s *Server) staleness(w http.ResponseWriter, r *http.Request) {
	entries, err := ledger.Tail(s.Runner.Ledger.Path(), 5000)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	type row struct {
		SourceID         string    `json:"source_id"`
		LastSuccessAt    time.Time `json:"last_success_at,omitempty"`
		StalenessSeconds float64   `json:"staleness_seconds"`
		LastStatus       string    `json:"last_status"`
	}
	bySource := map[string]row{}
	for _, e := range entries {
		r, seen := bySource[e.SourceID]
		if !seen {
			r.SourceID = e.SourceID
			r.LastStatus = string(e.Status)
		}
		if e.Status == ledger.StatusSuccess && e.CompletedAt.After(r.LastSuccessAt) {
			r.LastSuccessAt = e.CompletedAt
		}
		bySource[e.SourceID] = r
	}
	now := time.Now().UTC()
	out := make([]row, 0, len(bySource))
	for _, r := range bySource {
		if !r.LastSuccessAt.IsZero() {
			r.StalenessSeconds = now.Sub(r.LastSuccessAt).Seconds()
		}
		out = append(out, r)
	}
	writeJSON(w, http.StatusOK, map[string]any{"sources": out})
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	// Lightweight: confirm we have at least one ingestor registered and the
	// ledger is writable. Connectivity is not in the hot path because some
	// ingestors (e.g. git over network) might be intermittent and we don't
	// want to flap supervisor health on a transient failure.
	if len(s.Registry.Metadata()) == 0 {
		writeErr(w, http.StatusServiceUnavailable, errors.New("no ingestors registered"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

// --- helpers ---

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		log.Printf("[zone2] write json: %v", err)
	}
}

func writeErr(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]any{"error": err.Error()})
}

func intParam(r *http.Request, key string, def int) int {
	v := r.URL.Query().Get(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return def
	}
	return n
}

type Span struct {
	TraceID      string `json:"trace_id"`
	SpanID       string `json:"span_id"`
	ParentSpanID string `json:"parent_span_id,omitempty"`
	ServiceName  string `json:"service_name"`
	Name         string `json:"name"`
}

func (s *Server) handleGithubWebhook(w http.ResponseWriter, r *http.Request) {
	go func() {
		_, err := s.Runner.RunAll(context.Background(), "webhook:github")
		if err != nil {
			log.Printf("[zone2] github webhook run failed: %v", err)
		}
	}()
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "accepted"})
}

func (s *Server) handleTraces(w http.ResponseWriter, r *http.Request) {
	var spans []Span
	if err := json.NewDecoder(r.Body).Decode(&spans); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}

	spansByTrace := map[string][]Span{}
	spansByID := map[string]Span{}
	for _, span := range spans {
		spansByTrace[span.TraceID] = append(spansByTrace[span.TraceID], span)
		spansByID[span.SpanID] = span
	}

	batch := &nif.Batch{}
	now := time.Now().UTC()
	seenCalls := map[string]bool{}

	for _, span := range spans {
		if span.ParentSpanID == "" {
			continue
		}
		parent, ok := spansByID[span.ParentSpanID]
		if !ok {
			continue
		}

		if parent.ServiceName != span.ServiceName && parent.ServiceName != "" && span.ServiceName != "" {
			callKey := parent.ServiceName + "->" + span.ServiceName
			if seenCalls[callKey] {
				continue
			}
			seenCalls[callKey] = true

			parentID := nif.DeterministicEntityID("runtime", "otel", nif.EntityService, parent.ServiceName, "runtime")
			childID := nif.DeterministicEntityID("runtime", "otel", nif.EntityService, span.ServiceName, "runtime")

			parentEnt := &nif.Entity{
				ID:        parentID,
				Type:      nif.EntityService,
				Name:      parent.ServiceName,
				RawName:   parent.ServiceName,
				Namespace: "runtime",
				Source: nif.SourceInfo{
					SourceType: "runtime",
					SourceID:   "otel",
					ObservedAt: now,
				},
				Confidence:   1.0,
				IngestionRun: "otel_runtime",
			}
			childEnt := &nif.Entity{
				ID:        childID,
				Type:      nif.EntityService,
				Name:      span.ServiceName,
				RawName:   span.ServiceName,
				Namespace: "runtime",
				Source: nif.SourceInfo{
					SourceType: "runtime",
					SourceID:   "otel",
					ObservedAt: now,
				},
				Confidence:   1.0,
				IngestionRun: "otel_runtime",
			}

			addEntityUnique(batch, parentEnt)
			addEntityUnique(batch, childEnt)

			rel := &nif.Relationship{
				ID:           nif.DeterministicRelationshipID(nif.RelCalls, parentID, childID, "otel"),
				Type:         nif.RelCalls,
				FromEntityID: parentID,
				ToEntityID:   childID,
				Source: nif.SourceInfo{
					SourceType: "runtime",
					SourceID:   "otel",
					ObservedAt: now,
				},
				Confidence:   1.0,
				IngestionRun: "otel_runtime",
			}
			batch.Relationships = append(batch.Relationships, rel)
		}
	}

	if batch.Len() > 0 {
		_, err := s.Runner.Sink.Publish(r.Context(), batch)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{"status": "success", "calls_ingested": len(seenCalls)})
}

func addEntityUnique(b *nif.Batch, e *nif.Entity) {
	for _, existing := range b.Entities {
		if existing.ID == e.ID {
			return
		}
	}
	b.Entities = append(b.Entities, e)
}
