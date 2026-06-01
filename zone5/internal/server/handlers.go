// Package server is Zone 5's REST surface.
//
// Wires the pieces together: HTTP request → planner → (analytical engine OR
// retriever+assembler+reasoner) → JSON response.
package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"archgraph/zone5/internal/analytics"
	"archgraph/zone5/internal/assembler"
	"archgraph/zone5/internal/intent"
	"archgraph/zone5/internal/planner"
	"archgraph/zone5/internal/reasoner"
	"archgraph/zone5/internal/retriever"
	"archgraph/zone5/internal/zone4client"
)

type Server struct {
	cl     *zone4client.Client
	reason *reasoner.Reasoner
}

func New(cl *zone4client.Client, reason *reasoner.Reasoner) *Server {
	return &Server{cl: cl, reason: reason}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/ask", s.handleAsk)
	mux.HandleFunc("POST /v1/blast-radius", s.handleBlastRadius)
	mux.HandleFunc("POST /v1/health-audit", s.handleHealthAudit)
	mux.HandleFunc("POST /v1/diff", s.handleDiff)
	mux.HandleFunc("GET /v1/entities/{id}", s.handleGetEntity)
	mux.HandleFunc("GET /v1/entities", s.handleListEntities)
	mux.HandleFunc("GET /v1/log", s.handleReadLog)
	mux.HandleFunc("GET /v1/health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS, PUT, DELETE")
		w.Header().Set("Access-Control-Allow-Headers", "Accept, Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization")
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		mux.ServeHTTP(w, r)
	})
}

// --- /v1/ask --- the natural-language query path.

type askReq struct {
	Question   string `json:"question"`
	Namespace  string `json:"namespace,omitempty"`
	EntityName string `json:"entity_name,omitempty"`
	Depth      int    `json:"depth,omitempty"`
}

type askResp struct {
	Plan           planner.Plan                     `json:"plan"`
	Classification intent.Classification            `json:"classification"`
	Answer         *reasoner.Answer                 `json:"answer,omitempty"`
	BlastRadius    *analytics.BlastRadius           `json:"blast_radius,omitempty"`
	HealthReport   *analytics.HealthReport          `json:"health_report,omitempty"`
	Evolution      *analytics.EvolutionReport       `json:"evolution,omitempty"`
	Stats          *retriever.RetrieveStats         `json:"retrieve_stats,omitempty"`
}

func (s *Server) handleAsk(w http.ResponseWriter, r *http.Request) {
	var req askReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if req.Question == "" {
		writeError(w, http.StatusBadRequest, "missing_question", "question is required")
		return
	}

	plan, cls, err := planner.Make(planner.Request{
		Question:   req.Question,
		Namespace:  req.Namespace,
		EntityName: req.EntityName,
		Depth:      req.Depth,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, "plan_failed", err.Error())
		return
	}

	resp := askResp{Plan: plan, Classification: cls}

	switch plan.Action {
	case planner.ActImpactAnalysis:
		entity, err := s.cl.GetEntityByName(r.Context(), plan.Namespace, plan.EntityName)
		if err != nil {
			writeClientErr(w, err)
			return
		}
		br, err := analytics.CalculateBlastRadius(r.Context(), s.cl, entity.ID, plan.Depth)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "blast_radius_failed", err.Error())
			return
		}
		resp.BlastRadius = br

	case planner.ActHealthAudit:
		rep, err := analytics.Audit(r.Context(), s.cl, plan.Namespace)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "audit_failed", err.Error())
			return
		}
		resp.HealthReport = rep

	case planner.ActReadLog:
		ev, err := analytics.ComputeEvolution(r.Context(), s.cl, plan.Namespace, time.Time{}, time.Time{})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "evolution_failed", err.Error())
			return
		}
		resp.Evolution = ev

	case planner.ActFetchNeighborhood:
		entity, err := s.cl.GetEntityByName(r.Context(), plan.Namespace, plan.EntityName)
		if err != nil {
			writeClientErr(w, err)
			return
		}
		sub, err := retriever.Retrieve(r.Context(), s.cl, entity.ID, plan.Depth, plan.Direction)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "retrieve_failed", err.Error())
			return
		}
		resp.Stats = &sub.Stats
		ctxOut := assembler.Assemble(sub)
		ans, err := s.reason.Answer(r.Context(), req.Question, ctxOut)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "reason_failed", err.Error())
			return
		}
		resp.Answer = ans

	default:
		writeError(w, http.StatusInternalServerError, "unhandled_action", string(plan.Action))
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

// --- /v1/blast-radius --- direct access without the natural-language layer.

type blastReq struct {
	EntityID   string `json:"entity_id,omitempty"`
	EntityName string `json:"entity_name,omitempty"`
	Namespace  string `json:"namespace,omitempty"`
	MaxDepth   int    `json:"max_depth,omitempty"`
}

func (s *Server) handleBlastRadius(w http.ResponseWriter, r *http.Request) {
	var req blastReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	id, err := s.resolveID(r, req.EntityID, req.Namespace, req.EntityName)
	if err != nil {
		writeClientErr(w, err)
		return
	}
	br, err := analytics.CalculateBlastRadius(r.Context(), s.cl, id, req.MaxDepth)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "blast_radius_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, br)
}

// --- /v1/health-audit --- whole-namespace smell scan.

type auditReq struct {
	Namespace string `json:"namespace"`
}

func (s *Server) handleHealthAudit(w http.ResponseWriter, r *http.Request) {
	var req auditReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if req.Namespace == "" {
		writeError(w, http.StatusBadRequest, "missing_namespace", "namespace required")
		return
	}
	rep, err := analytics.Audit(r.Context(), s.cl, req.Namespace)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "audit_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, rep)
}

// --- /v1/diff --- evolution between two timestamps.

type diffReq struct {
	Namespace string    `json:"namespace,omitempty"`
	From      time.Time `json:"from,omitempty"`
	To        time.Time `json:"to,omitempty"`
}

func (s *Server) handleDiff(w http.ResponseWriter, r *http.Request) {
	var req diffReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	rep, err := analytics.ComputeEvolution(r.Context(), s.cl, req.Namespace, req.From, req.To)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "diff_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, rep)
}

// --- /v1/entities/{id} --- thin passthrough so consumers don't need to hit Zone 4 directly.

func (s *Server) handleGetEntity(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	depthStr := r.URL.Query().Get("depth")
	if depthStr != "" {
		depth, err := strconv.Atoi(depthStr)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_depth", "depth must be int")
			return
		}
		nb, err := s.cl.Neighborhood(r.Context(), id, depth, zone4client.DirBoth)
		if err != nil {
			writeClientErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, nb)
		return
	}
	e, err := s.cl.GetEntity(r.Context(), id)
	if err != nil {
		writeClientErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, e)
}

func (s *Server) resolveID(r *http.Request, id, namespace, name string) (string, error) {
	if id != "" {
		return id, nil
	}
	if name == "" || namespace == "" {
		return "", errors.New("entity_id OR (namespace + entity_name) required")
	}
	e, err := s.cl.GetEntityByName(r.Context(), namespace, name)
	if err != nil {
		return "", err
	}
	return e.ID, nil
}

func writeClientErr(w http.ResponseWriter, err error) {
	if errors.Is(err, zone4client.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	writeError(w, http.StatusBadGateway, "zone4_error", err.Error())
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]string{"error": code, "message": msg})
}

func (s *Server) handleListEntities(w http.ResponseWriter, r *http.Request) {
	ns := r.URL.Query().Get("namespace")
	if ns == "" {
		writeError(w, http.StatusBadRequest, "missing_namespace", "namespace query parameter required")
		return
	}
	listing, err := s.cl.ListNamespace(r.Context(), ns)
	if err != nil {
		writeClientErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, listing)
}

func (s *Server) handleReadLog(w http.ResponseWriter, r *http.Request) {
	var opts zone4client.ReadLogOpts
	opts.EntityID = r.URL.Query().Get("entity_id")
	opts.RelationshipID = r.URL.Query().Get("relationship_id")
	opts.TransactionID = r.URL.Query().Get("transaction_id")
	if v := r.URL.Query().Get("from_entry_id"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err == nil {
			opts.FromEntryID = n
		}
	}
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err == nil {
			opts.Limit = n
		}
	}
	entries, err := s.cl.ReadLog(r.Context(), opts)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "log_read_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": entries})
}
