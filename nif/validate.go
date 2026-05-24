package nif

import (
	"errors"
	"fmt"
)

// ValidationError is a structured rejection — the DLQ stores the record
// along with one of these so investigators can filter by Reason later.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("validate %s: %s", e.Field, e.Message)
}

func vErr(field, message string) error {
	return &ValidationError{Field: field, Message: message}
}

// ValidateEntity is the schema gate from the spec §4. Any record failing
// this never reaches the sink — it goes to the DLQ instead.
func ValidateEntity(e *Entity) error {
	if e == nil {
		return errors.New("nil entity")
	}
	if e.ID == "" {
		return vErr("id", "required (Zone 2 must generate deterministic IDs)")
	}
	if !knownEntityType(e.Type) {
		return vErr("type", fmt.Sprintf("unknown type %q", e.Type))
	}
	if e.Name == "" {
		return vErr("name", "canonical name required")
	}
	if e.Namespace == "" {
		return vErr("namespace", "required (scopes identity)")
	}
	if e.Confidence < 0 || e.Confidence > 1 {
		return vErr("confidence", "must be in [0, 1]")
	}
	if e.Source.SourceType == "" || e.Source.SourceID == "" {
		return vErr("source", "source_type and source_id required")
	}
	if e.Source.ObservedAt.IsZero() {
		return vErr("source.observed_at", "required")
	}
	if e.IngestionRun == "" {
		return vErr("ingestion_run_id", "required (every record must be traceable to a run)")
	}
	return nil
}

func ValidateRelationship(r *Relationship) error {
	if r == nil {
		return errors.New("nil relationship")
	}
	if r.ID == "" {
		return vErr("id", "required")
	}
	if !knownRelType(r.Type) {
		return vErr("type", fmt.Sprintf("unknown type %q", r.Type))
	}
	if r.FromEntityID == "" || r.ToEntityID == "" {
		return vErr("endpoints", "from_entity_id and to_entity_id required")
	}
	if r.FromEntityID == r.ToEntityID {
		return vErr("endpoints", "self-loop not allowed (REL-001 in Zone 4)")
	}
	if r.Confidence < 0 || r.Confidence > 1 {
		return vErr("confidence", "must be in [0, 1]")
	}
	if r.Source.SourceType == "" || r.Source.SourceID == "" {
		return vErr("source", "source_type and source_id required")
	}
	if r.Source.ObservedAt.IsZero() {
		return vErr("source.observed_at", "required")
	}
	if r.IngestionRun == "" {
		return vErr("ingestion_run_id", "required")
	}
	return nil
}

var entitySet = map[EntityType]struct{}{
	EntityService:        {},
	EntityModule:         {},
	EntityFunction:       {},
	EntityAPIEndpoint:    {},
	EntityDatabaseTable:  {},
	EntityTeam:           {},
	EntityDatabaseSchema: {},
}

func knownEntityType(t EntityType) bool {
	_, ok := entitySet[t]
	return ok
}

var relSet = map[RelationshipType]struct{}{
	RelDependsOn: {}, RelCalls: {}, RelImports: {}, RelOwns: {},
	RelExposes: {}, RelRuntimeCalls: {}, RelReadsFrom: {},
	RelWritesTo: {}, RelChangeCoupledWith: {}, RelContributedTo: {}, RelDeployedOn: {},
}

func knownRelType(t RelationshipType) bool {
	_, ok := relSet[t]
	return ok
}
