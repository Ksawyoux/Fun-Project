// Package server exposes Zone 4 over HTTP. It is a thin translation layer
// from JSON requests to mutation.API + graphdb.Store calls — no business
// logic lives here.
package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"archgraph/zone4/internal/deltalog"
	"archgraph/zone4/internal/graphdb"
	"archgraph/zone4/internal/mutation"
	"archgraph/zone4/internal/schema"
)

type Server struct {
	store *graphdb.Store
	api   *mutation.API
}

func New(store *graphdb.Store, api *mutation.API) *Server {
	return &Server{store: store, api: api}
}

// Routes returns an http.Handler with all v1 routes registered.
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/mutations", s.handleMutations)
	mux.HandleFunc("GET /v1/entities/{id}", s.handleGetEntity)
	mux.HandleFunc("GET /v1/entities", s.handleGetEntityByName)
	mux.HandleFunc("GET /v1/entities/{id}/neighborhood", s.handleNeighborhood)
	mux.HandleFunc("GET /v1/log", s.handleReadLog)
	mux.HandleFunc("GET /v1/health", s.handleHealth)
	return mux
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleMutations(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Mutations []mutation.Mutation `json:"mutations"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if len(body.Mutations) == 0 {
		writeError(w, http.StatusBadRequest, "empty_batch", "mutations array is required and must be non-empty")
		return
	}
	result, err := s.api.ApplyBatch(r.Context(), body.Mutations)
	if err != nil {
		status := http.StatusInternalServerError
		code := "internal_error"
		var verr *schema.ValidationError
		if errors.As(err, &verr) {
			status, code = http.StatusBadRequest, "validation_error"
		} else if errors.Is(err, schema.ErrUnknownEntity) {
			status, code = http.StatusUnprocessableEntity, "unknown_entity"
		} else if errors.Is(err, graphdb.ErrVersionConflict) {
			status, code = http.StatusConflict, "version_conflict"
		} else if errors.Is(err, graphdb.ErrNotFound) {
			status, code = http.StatusNotFound, "not_found"
		}
		writeJSON(w, status, map[string]any{
			"error":   code,
			"message": err.Error(),
			"result":  result,
		})
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleGetEntity(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing_id", "id path parameter required")
		return
	}
	e, err := s.store.GetEntity(r.Context(), id)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, e)
}

func (s *Server) handleGetEntityByName(w http.ResponseWriter, r *http.Request) {
	ns := r.URL.Query().Get("namespace")
	name := r.URL.Query().Get("name")
	if ns == "" || name == "" {
		writeError(w, http.StatusBadRequest, "missing_params", "namespace and name query params required")
		return
	}
	e, err := s.store.GetEntityByName(r.Context(), ns, name)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, e)
}

func (s *Server) handleNeighborhood(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	depth := 1
	if v := r.URL.Query().Get("depth"); v != "" {
		d, err := strconv.Atoi(v)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_depth", "depth must be an integer")
			return
		}
		depth = d
	}
	dir := graphdb.DirBoth
	switch strings.ToLower(r.URL.Query().Get("direction")) {
	case "out", "outbound":
		dir = graphdb.DirOutbound
	case "in", "inbound":
		dir = graphdb.DirInbound
	case "", "both":
		dir = graphdb.DirBoth
	default:
		writeError(w, http.StatusBadRequest, "invalid_direction", "direction must be in|out|both")
		return
	}
	n, err := s.store.Neighborhood(r.Context(), id, depth, dir)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, n)
}

func (s *Server) handleReadLog(w http.ResponseWriter, r *http.Request) {
	opts := deltalog.ReadOpts{
		EntityID:       r.URL.Query().Get("entity_id"),
		RelationshipID: r.URL.Query().Get("relationship_id"),
		TransactionID:  r.URL.Query().Get("transaction_id"),
	}
	if v := r.URL.Query().Get("from_entry_id"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_from_entry_id", "must be integer")
			return
		}
		opts.FromEntryID = n
	}
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_limit", "must be integer")
			return
		}
		opts.Limit = n
	}
	entries, err := deltalog.Read(r.Context(), s.store.DB(), opts)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "log_read_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": entries})
}

func writeStoreErr(w http.ResponseWriter, err error) {
	if errors.Is(err, graphdb.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]string{"error": code, "message": msg})
}
