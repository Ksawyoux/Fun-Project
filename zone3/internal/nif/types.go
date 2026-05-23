package nif

import "time"

type EntityType string

const (
	EntityService       EntityType = "SERVICE"
	EntityModule        EntityType = "MODULE"
	EntityFunction      EntityType = "FUNCTION"
	EntityAPIEndpoint   EntityType = "API_ENDPOINT"
	EntityDatabaseTable EntityType = "DATABASE_TABLE"
	EntityTeam          EntityType = "TEAM"
	EntitySchema        EntityType = "SCHEMA"
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
	RelAuthoredBy        RelationshipType = "AUTHORED_BY"
	RelDeployedTo        RelationshipType = "DEPLOYED_TO"
)

type SourceInfo struct {
	SourceType string    `json:"source_type"`  // git | ast | api | runtime | …
	SourceID   string    `json:"source_id"`    // which specific configured source
	SourceRef  string    `json:"source_ref"`   // original identifier in that source (repo URL, file path, …)
	ObservedAt time.Time `json:"observed_at"`
}

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

type Batch struct {
	Entities      []*Entity       `json:"entities"`
	Relationships []*Relationship `json:"relationships"`
}

func (b *Batch) Len() int {
	if b == nil {
		return 0
	}
	return len(b.Entities) + len(b.Relationships)
}
