package delivery

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"archgraph/nif"
)

// Zone4Sink ships NIF records to zone4d's POST /v1/mutations endpoint.
//
// Two collapse rules applied on the way out:
//   1. We translate NIF Entity → Zone 4 Entity shape (different field names).
//   2. We DROP source_type from the deterministic ID re-derivation when
//      possible — Zone 4 already has its own DeterministicID fallback;
//      passing our Zone-2-side IDs preserves idempotency *within a source*
//      but two different sources observing the same logical thing will
//      currently land as two Zone 4 entities. That cross-source collapse
//      is Zone 3's job (when Zone 3 exists). Documented in the design doc.
type Zone4Sink struct {
	base string
	http *http.Client
}

func NewZone4Sink(baseURL string) *Zone4Sink {
	return &Zone4Sink{
		base: strings.TrimRight(baseURL, "/"),
		http: &http.Client{Timeout: 30 * time.Second},
	}
}

// Publish converts the batch into a Zone 4 mutations request and POSTs it.
// Entities go first so subsequent relationships in the same batch find
// their endpoints — see Zone 4 mutation handler ordering.
func (s *Zone4Sink) Publish(ctx context.Context, batch *nif.Batch) (PublishResult, error) {
	if batch == nil || batch.Len() == 0 {
		return PublishResult{}, nil
	}

	muts := make([]z4Mutation, 0, batch.Len())
	for _, e := range batch.Entities {
		muts = append(muts, z4Mutation{
			Kind:   "UPSERT_ENTITY",
			Entity: entityToZ4(e),
		})
	}
	for _, r := range batch.Relationships {
		muts = append(muts, z4Mutation{
			Kind:         "UPSERT_RELATIONSHIP",
			Relationship: relationshipToZ4(r),
		})
	}

	body, err := json.Marshal(z4Request{Mutations: muts})
	if err != nil {
		return PublishResult{}, fmt.Errorf("encode mutations: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.base+"/v1/mutations", bytes.NewReader(body))
	if err != nil {
		return PublishResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.http.Do(req)
	if err != nil {
		return PublishResult{}, fmt.Errorf("post mutations: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return PublishResult{}, fmt.Errorf("zone4 %s: %s", resp.Status, truncate(respBody, 512))
	}

	var parsed z4Response
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return PublishResult{}, fmt.Errorf("decode mutations response: %w", err)
	}

	res := PublishResult{}
	for _, r := range parsed.Results {
		switch {
		case r.Operation == "FAILED":
			res.Failed++
		case r.RelationshipID != "":
			res.RelationshipsEmitted++
		case r.EntityID != "":
			res.EntitiesEmitted++
		}
	}
	return res, nil
}

// --- Wire types: matches zone4/internal/server expectations ---

type z4Request struct {
	Mutations []z4Mutation `json:"mutations"`
}

type z4Mutation struct {
	Kind         string                 `json:"kind"`
	Entity       map[string]any         `json:"entity,omitempty"`
	Relationship map[string]any         `json:"relationship,omitempty"`
	TargetID     string                 `json:"target_id,omitempty"`
}

type z4Response struct {
	TransactionID string `json:"transaction_id"`
	Status        string `json:"status"`
	Results       []struct {
		Index          int    `json:"index"`
		Kind           string `json:"kind"`
		Operation      string `json:"operation"`
		EntityID       string `json:"entity_id,omitempty"`
		RelationshipID string `json:"relationship_id,omitempty"`
		LogEntryID     int64  `json:"log_entry_id,omitempty"`
		Error          string `json:"error,omitempty"`
	} `json:"results"`
}

// entityToZ4 maps NIF Entity → Zone 4 entity wire shape. The two differ in
// the name of the canonical-name field (`name` vs `canonical_name`) and in
// what provenance Zone 4 stores (essentially nothing; provenance lives on
// the delta-log entry, not the entity row).
func entityToZ4(e *nif.Entity) map[string]any {
	props := map[string]any{}
	for k, v := range e.Properties {
		props[k] = v
	}
	// Always carry the source provenance into the entity properties so it
	// isn't lost — Zone 4 doesn't have first-class source_info fields yet.
	props["source_type"] = e.Source.SourceType
	props["source_id"] = e.Source.SourceID
	props["source_ref"] = e.Source.SourceRef
	props["observed_at"] = e.Source.ObservedAt
	props["ingestion_run_id"] = e.IngestionRun
	if e.RawName != "" && e.RawName != e.Name {
		props["raw_name"] = e.RawName
	}
	if e.IsPartial {
		props["is_partial"] = true
	}
	return map[string]any{
		"id":              e.ID,
		"type":            string(e.Type),
		"sub_type":        e.SubType,
		"canonical_name":  e.Name,
		"namespace":       e.Namespace,
		"confidence":      e.Confidence,
		"is_active":       true,
		"valid_from":      e.Source.ObservedAt,
		"lifecycle_stage": "ACTIVE",
		"properties":      props,
	}
}

func relationshipToZ4(r *nif.Relationship) map[string]any {
	props := map[string]any{}
	for k, v := range r.Properties {
		props[k] = v
	}
	props["source_type"] = r.Source.SourceType
	props["source_id"] = r.Source.SourceID
	props["source_ref"] = r.Source.SourceRef
	props["observed_at"] = r.Source.ObservedAt
	props["ingestion_run_id"] = r.IngestionRun
	if r.IsInferred {
		props["is_inferred"] = true
	}
	return map[string]any{
		"id":         r.ID,
		"type":       string(r.Type),
		"from_id":    r.FromEntityID,
		"to_id":      r.ToEntityID,
		"confidence": r.Confidence,
		"is_active":  true,
		"valid_from": r.Source.ObservedAt,
		"properties": props,
	}
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "…"
}
