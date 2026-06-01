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
	"time"

	"archgraph/zone4/internal/deltalog"
	"archgraph/zone4/internal/graphdb"
	"archgraph/zone4/internal/metrics"
	"archgraph/zone4/internal/mutation"
	"archgraph/zone4/internal/search"
	"archgraph/zone4/internal/snapshot"
	"archgraph/zone4/schema"
)

type Server struct {
	store         *graphdb.Store
	api           *mutation.API
	indexer       *search.Indexer
	snapshotStore *snapshot.SnapshotStore
}

func New(store *graphdb.Store, api *mutation.API, indexer *search.Indexer, snap *snapshot.SnapshotStore) *Server {
	return &Server{
		store:         store,
		api:           api,
		indexer:       indexer,
		snapshotStore: snap,
	}
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

	// New endpoints
	mux.HandleFunc("GET /v1/search", s.handleSearch)
	mux.HandleFunc("POST /v1/snapshots", s.handleCreateSnapshot)
	mux.HandleFunc("GET /v1/graph", s.handleGetGraph)
	mux.HandleFunc("POST /v1/metrics", s.handlePostMetrics)
	mux.HandleFunc("GET /v1/entities/{id}/metrics", s.handleGetEntityMetrics)
	mux.HandleFunc("GET /v1/relationships/{id}/metrics", s.handleGetRelationshipMetrics)

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
	if ns == "" {
		writeError(w, http.StatusBadRequest, "missing_params", "namespace query param required")
		return
	}
	if name != "" {
		e, err := s.store.GetEntityByName(r.Context(), ns, name)
		if err != nil {
			writeStoreErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, e)
		return
	}
	// No name → list active entities in the namespace.
	entities, err := s.store.ListEntitiesByNamespace(r.Context(), ns)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list_failed", err.Error())
		return
	}
	rels, err := s.store.ListRelationshipsByNamespace(r.Context(), ns)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"namespace":     ns,
		"entities":      entities,
		"relationships": rels,
	})
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

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	if s.indexer == nil {
		writeError(w, http.StatusNotImplemented, "search_disabled", "search indexer is not configured")
		return
	}
	opts := search.SearchOptions{
		Query:       r.URL.Query().Get("q"),
		Namespace:   r.URL.Query().Get("namespace"),
		EntityType:  r.URL.Query().Get("entity_type"),
		SubType:     r.URL.Query().Get("sub_type"),
		OwnerTeam:   r.URL.Query().Get("owner_team"),
		Criticality: r.URL.Query().Get("criticality"),
		Maturity:    r.URL.Query().Get("maturity"),
		Velocity:    r.URL.Query().Get("velocity"),
	}
	if opts.EntityType == "" {
		opts.EntityType = r.URL.Query().Get("type")
	}
	if val := r.URL.Query().Get("is_active"); val != "" {
		isActive, err := strconv.ParseBool(val)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_is_active", "must be true or false")
			return
		}
		opts.IsActive = &isActive
	}

	results, err := s.indexer.Search(r.Context(), opts)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "search_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": results})
}

func (s *Server) handleCreateSnapshot(w http.ResponseWriter, r *http.Request) {
	if s.snapshotStore == nil {
		writeError(w, http.StatusNotImplemented, "snapshots_disabled", "snapshot store is not configured")
		return
	}
	var body struct {
		SnapshotID string `json:"snapshot_id"`
		SnapshotAt string `json:"snapshot_at"`
	}
	if r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
			return
		}
	}

	var at time.Time
	if body.SnapshotAt != "" {
		parsed, err := time.Parse(time.RFC3339, body.SnapshotAt)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_snapshot_at", "must be RFC3339 format")
			return
		}
		at = parsed
	}

	meta, err := s.snapshotStore.CreateSnapshot(r.Context(), body.SnapshotID, at)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "snapshot_creation_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, meta)
}

func (s *Server) handleGetGraph(w http.ResponseWriter, r *http.Request) {
	if s.snapshotStore == nil {
		writeError(w, http.StatusNotImplemented, "snapshots_disabled", "snapshot store is not configured")
		return
	}
	var asOf time.Time
	if val := r.URL.Query().Get("as_of"); val != "" {
		parsed, err := time.Parse(time.RFC3339, val)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_as_of", "must be RFC3339 format")
			return
		}
		asOf = parsed
	}

	state, err := s.snapshotStore.RestoreGraph(r.Context(), asOf)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "graph_restore_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, state)
}

func (s *Server) handlePostMetrics(w http.ResponseWriter, r *http.Request) {
	var batch metrics.MetricsBatch
	if err := json.NewDecoder(r.Body).Decode(&batch); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}

	metricsStore := s.store.Metrics()
	if metricsStore == nil {
		writeError(w, http.StatusNotImplemented, "metrics_disabled", "metrics store is not configured")
		return
	}

	if err := metricsStore.Ingest(r.Context(), batch); err != nil {
		writeError(w, http.StatusInternalServerError, "metrics_ingestion_failed", err.Error())
		return
	}

	// Invalidate cache for entities and relationships with updated metrics
	for _, em := range batch.Entities {
		s.store.InvalidateCache(em.EntityID)
	}
	for _, rm := range batch.Relationships {
		var fromID, toID string
		err := s.store.DB().QueryRowContext(r.Context(), "SELECT from_id, to_id FROM relationships WHERE id = ?", rm.RelationshipID).Scan(&fromID, &toID)
		if err == nil {
			s.store.InvalidateRelCache(fromID, toID)
		}
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "success"})
}

func (s *Server) handleGetEntityMetrics(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing_id", "id path parameter required")
		return
	}
	var start, end time.Time
	if val := r.URL.Query().Get("start"); val != "" {
		parsed, err := time.Parse(time.RFC3339, val)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_start", "must be RFC3339 format")
			return
		}
		start = parsed
	}
	if val := r.URL.Query().Get("end"); val != "" {
		parsed, err := time.Parse(time.RFC3339, val)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_end", "must be RFC3339 format")
			return
		}
		end = parsed
	}

	metricsStore := s.store.Metrics()
	if metricsStore == nil {
		writeError(w, http.StatusNotImplemented, "metrics_disabled", "metrics store is not configured")
		return
	}

	res, err := metricsStore.QueryEntityMetrics(r.Context(), id, start, end)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleGetRelationshipMetrics(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing_id", "id path parameter required")
		return
	}
	var start, end time.Time
	if val := r.URL.Query().Get("start"); val != "" {
		parsed, err := time.Parse(time.RFC3339, val)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_start", "must be RFC3339 format")
			return
		}
		start = parsed
	}
	if val := r.URL.Query().Get("end"); val != "" {
		parsed, err := time.Parse(time.RFC3339, val)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_end", "must be RFC3339 format")
			return
		}
		end = parsed
	}

	metricsStore := s.store.Metrics()
	if metricsStore == nil {
		writeError(w, http.StatusNotImplemented, "metrics_disabled", "metrics store is not configured")
		return
	}

	res, err := metricsStore.QueryRelationshipMetrics(r.Context(), id, start, end)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}
