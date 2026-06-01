package schema

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"
)

// RelationshipType enumerates the canonical edge types in the graph.
type RelationshipType string

const (
	// Code-level
	RelImports    RelationshipType = "IMPORTS"
	RelCalls      RelationshipType = "CALLS"
	RelExtends    RelationshipType = "EXTENDS"
	RelImplements RelationshipType = "IMPLEMENTS"
	RelExposes    RelationshipType = "EXPOSES"

	// Runtime
	RelRuntimeCalls RelationshipType = "RUNTIME_CALLS"
	RelProduces     RelationshipType = "PRODUCES"
	RelConsumes     RelationshipType = "CONSUMES"
	RelReadsFrom    RelationshipType = "READS_FROM"
	RelWritesTo     RelationshipType = "WRITES_TO"

	// Dependency
	RelDependsOn             RelationshipType = "DEPENDS_ON"
	RelTransitivelyDependsOn RelationshipType = "TRANSITIVELY_DEPENDS_ON"
	RelUsesLibrary           RelationshipType = "USES_LIBRARY"
	RelCallsExternal         RelationshipType = "CALLS_EXTERNAL"

	// Organizational
	RelOwns          RelationshipType = "OWNS"
	RelContributedTo RelationshipType = "CONTRIBUTED_TO"
	RelReviews       RelationshipType = "REVIEWS"
	RelOnCallFor     RelationshipType = "ON_CALL_FOR"

	// Deployment
	RelDeployedOn RelationshipType = "DEPLOYED_ON"
	RelDeployedIn RelationshipType = "DEPLOYED_IN"
	RelRunsIn     RelationshipType = "RUNS_IN"

	// Coupling
	RelChangeCoupledWith     RelationshipType = "CHANGE_COUPLED_WITH"
	RelDeploymentCoupledWith RelationshipType = "DEPLOYMENT_COUPLED_WITH"
	RelDataCoupledWith       RelationshipType = "DATA_COUPLED_WITH"
	RelFailureCorrelatedWith RelationshipType = "FAILURE_CORRELATED_WITH"

	// Lifecycle
	RelIntroducedIn RelationshipType = "INTRODUCED_IN"
	RelModifiedIn   RelationshipType = "MODIFIED_IN"
	RelDeprecatedBy RelationshipType = "DEPRECATED_BY"
	RelReplacedBy   RelationshipType = "REPLACED_BY"
)

var validRelTypes = map[RelationshipType]struct{}{
	RelImports: {}, RelCalls: {}, RelExtends: {}, RelImplements: {}, RelExposes: {},
	RelRuntimeCalls: {}, RelProduces: {}, RelConsumes: {}, RelReadsFrom: {}, RelWritesTo: {},
	RelDependsOn: {}, RelTransitivelyDependsOn: {}, RelUsesLibrary: {}, RelCallsExternal: {},
	RelOwns: {}, RelContributedTo: {}, RelReviews: {}, RelOnCallFor: {},
	RelDeployedOn: {}, RelDeployedIn: {}, RelRunsIn: {},
	RelChangeCoupledWith: {}, RelDeploymentCoupledWith: {}, RelDataCoupledWith: {}, RelFailureCorrelatedWith: {},
	RelIntroducedIn: {}, RelModifiedIn: {}, RelDeprecatedBy: {}, RelReplacedBy: {},
}

func (t RelationshipType) Valid() bool {
	_, ok := validRelTypes[t]
	return ok
}

// Relationship is the canonical graph edge.
type Relationship struct {
	ID         string           `json:"id"`
	Type       RelationshipType `json:"type"`
	FromID     string           `json:"from_id"`
	ToID       string           `json:"to_id"`
	Confidence float64          `json:"confidence"`
	IsActive   bool             `json:"is_active"`
	ValidFrom  time.Time        `json:"valid_from"`
	ValidTo    *time.Time       `json:"valid_to,omitempty"`
	Version    int              `json:"version"`
	Properties map[string]any   `json:"properties,omitempty"`
}

// DeterministicID hashes the edge's identity tuple.
// Per spec: hash(relationship_type + from_entity_id + to_entity_id + source_type).
// MVP omits source_type (no Zone 2 yet) so two sources reporting the same edge
// collide on one ID — which is what we want for deduplication.
func (r *Relationship) DeterministicID() string {
	h := sha256.New()
	fmt.Fprintf(h, "%s|%s|%s", r.Type, r.FromID, r.ToID)
	return "rel_" + hex.EncodeToString(h.Sum(nil))[:24]
}
