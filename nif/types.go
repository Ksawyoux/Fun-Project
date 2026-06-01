// Package nif defines the Normalized Ingestion Format — the one shape every
// record exits Zone 2 in. Downstream zones speak NIF and nothing else; this
// is the contract that lets us swap or add sources without touching Zone 3+.
package nif

import "time"

// EntityType is intentionally a string (not enum) so Zone 2 can emit types
// Zone 4 doesn't yet recognize without a code change here — the receiving
// side rejects unknowns. The set we emit today is documented in
// docs/superpowers/specs/.
type EntityType string

const (
	EntityService        EntityType = "SERVICE"
	EntityModule         EntityType = "MODULE"
	EntityFunction       EntityType = "FUNCTION"
	EntityAPIEndpoint    EntityType = "API_ENDPOINT"
	EntityDatabaseTable  EntityType = "DATABASE_TABLE"
	EntityTeam           EntityType = "TEAM"
	EntityDatabaseSchema EntityType = "DATABASE_SCHEMA"
	EntityClass          EntityType = "CLASS"
)

type RelationshipType string

const (
	RelDependsOn         RelationshipType = "DEPENDS_ON"
	RelCalls             RelationshipType = "CALLS"
	RelImports           RelationshipType = "IMPORTS"
	RelOwns              RelationshipType = "OWNS"
	RelExposes           RelationshipType = "EXPOSES"
	RelRuntimeCalls      RelationshipType = "RUNTIME_CALLS"
	RelReadsFrom         RelationshipType = "READS_FROM"
	RelWritesTo          RelationshipType = "WRITES_TO"
	RelChangeCoupledWith RelationshipType = "CHANGE_COUPLED_WITH"
	RelContributedTo     RelationshipType = "CONTRIBUTED_TO"
	RelDeployedOn        RelationshipType = "DEPLOYED_ON"
)

// SourceInfo is the per-record provenance the spec mandates. Every NIF
// record carries it — that's how Zone 3 deduplicates across sources and how
// staleness is computed downstream.
type SourceInfo struct {
	SourceType string    `json:"source_type"`  // git | ast | api | runtime | …
	SourceID   string    `json:"source_id"`    // which specific configured source
	SourceRef  string    `json:"source_ref"`   // original identifier in that source (repo URL, file path, …)
	ObservedAt time.Time `json:"observed_at"`
}

// Entity is the normalized representation of one thing.
//
// Fields beyond Zone 4's schema:
//   - RawName: the original name we saw, before canonicalization. Useful
//     when the same logical entity surfaces under different aliases across
//     sources (e.g. "payment-service" vs "payments-svc").
//   - IsPartial / IngestionRunID / SourceInfo: provenance.
type Entity struct {
	ID            string         `json:"id"`
	Type          EntityType     `json:"type"`
	SubType       string         `json:"sub_type,omitempty"`
	Name          string         `json:"name"`
	RawName       string         `json:"raw_name,omitempty"`
	Namespace     string         `json:"namespace"`
	Source        SourceInfo     `json:"source"`
	Properties    map[string]any `json:"properties,omitempty"`
	Confidence    float64        `json:"confidence"`
	IsPartial     bool           `json:"is_partial"`
	IngestionRun  string         `json:"ingestion_run_id"`
}

// Relationship is the normalized edge between two entities. Endpoints are
// referenced by NIF ID, NOT by raw name — because IDs are deterministic, two
// ingestors observing the same underlying entity converge on the same ID.
type Relationship struct {
	ID            string           `json:"id"`
	Type          RelationshipType `json:"type"`
	FromEntityID  string           `json:"from_entity_id"`
	ToEntityID    string           `json:"to_entity_id"`
	Source        SourceInfo       `json:"source"`
	Properties    map[string]any   `json:"properties,omitempty"`
	Confidence    float64          `json:"confidence"`
	IsInferred    bool             `json:"is_inferred"`
	IngestionRun  string           `json:"ingestion_run_id"`
}

// Batch is one ingestor's output for one run. Entities come first because
// the receiving Zone 4 mutation API processes them in order and rejects
// relationships whose endpoints don't exist yet (REL-001).
type Batch struct {
	Entities      []*Entity       `json:"entities"`
	Relationships []*Relationship `json:"relationships"`
}

// Len reports the total record count — useful for ledger entries and the
// backpressure-aware logging in the orchestrator.
func (b *Batch) Len() int {
	if b == nil {
		return 0
	}
	return len(b.Entities) + len(b.Relationships)
}
