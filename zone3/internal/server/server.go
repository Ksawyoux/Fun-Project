package server

import (
	"encoding/json"
	"net/http"

	"archgraph/nif"
	"archgraph/zone3/internal/pipeline"
	"archgraph/zone3/internal/z4client"
)

type Server struct {
	pl *pipeline.Pipeline
	z4 *z4client.Client
}

func New(pl *pipeline.Pipeline, z4 *z4client.Client) *Server {
	return &Server{
		pl: pl,
		z4: z4,
	}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/ingest", s.handleBatches)
	mux.HandleFunc("POST /v1/batches", s.handleBatches)
	mux.HandleFunc("GET /v1/health", s.handleHealth)
	return mux
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleBatches(w http.ResponseWriter, r *http.Request) {
	var batch nif.Batch
	if err := json.NewDecoder(r.Body).Decode(&batch); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}

	mutations, err := s.pl.Process(r.Context(), &batch)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "pipeline_error", err.Error())
		return
	}

	if len(mutations) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{
			"status":            "success",
			"mutations_applied": 0,
			"message":           "no changes detected, graph is up to date",
		})
		return
	}

	// Ship delta mutations to Zone 4 Graph Storage Mutation API
	if err := s.z4.ApplyBatch(r.Context(), mutations); err != nil {
		writeError(w, http.StatusInternalServerError, "storage_error", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":            "success",
		"mutations_applied": len(mutations),
	})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]string{"error": code, "message": msg})
}
