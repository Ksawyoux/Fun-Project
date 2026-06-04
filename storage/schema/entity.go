package schema

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"
)

// EntityType enumerates the canonical node types in the graph.
// Mirrors the catalog in Zone4.md.
type EntityType string

const (
	EntityService                EntityType = "SERVICE"
	EntityDeployment             EntityType = "DEPLOYMENT"
	EntityInfrastructureResource EntityType = "INFRASTRUCTURE_RESOURCE"
	EntityContainer              EntityType = "CONTAINER"

	EntityRepository EntityType = "REPOSITORY"
	EntityModule     EntityType = "MODULE"
	EntityFile       EntityType = "FILE"
	EntityFunction   EntityType = "FUNCTION"
	EntityClass      EntityType = "CLASS"

	EntityAPIEndpoint    EntityType = "API_ENDPOINT"
	EntityEventTopic     EntityType = "EVENT_TOPIC"
	EntityDatabaseTable  EntityType = "DATABASE_TABLE"
	EntityDatabaseSchema EntityType = "DATABASE_SCHEMA"

	EntityTeam         EntityType = "TEAM"
	EntityDeveloper    EntityType = "DEVELOPER"
	EntityOrganization EntityType = "ORGANIZATION"

	EntityCommit          EntityType = "COMMIT"
	EntityPullRequest     EntityType = "PULL_REQUEST"
	EntityDeploymentEvent EntityType = "DEPLOYMENT_EVENT"
	EntityIncident        EntityType = "INCIDENT"

	EntityExternalLibrary EntityType = "EXTERNAL_LIBRARY"
	EntityExternalService EntityType = "EXTERNAL_SERVICE"
)

var validEntityTypes = map[EntityType]struct{}{
	EntityService: {}, EntityDeployment: {}, EntityInfrastructureResource: {},
	EntityContainer: {}, EntityRepository: {}, EntityModule: {},
	EntityFile: {}, EntityFunction: {}, EntityClass: {},
	EntityAPIEndpoint: {}, EntityEventTopic: {}, EntityDatabaseTable: {},
	EntityDatabaseSchema: {}, EntityTeam: {}, EntityDeveloper: {},
	EntityOrganization: {}, EntityCommit: {}, EntityPullRequest: {},
	EntityDeploymentEvent: {}, EntityIncident: {},
	EntityExternalLibrary: {}, EntityExternalService: {},
}

func (t EntityType) Valid() bool {
	_, ok := validEntityTypes[t]
	return ok
}

// LifecycleStage mirrors the spec's lifecycle marker.
type LifecycleStage string

const (
	LifecycleActive     LifecycleStage = "ACTIVE"
	LifecycleDeprecated LifecycleStage = "DEPRECATED"
	LifecycleSunset     LifecycleStage = "SUNSET"
	LifecycleDeleted    LifecycleStage = "DELETED"
)

// Entity is the canonical graph node.
//
// MVP scope: omits enrichment fields (criticality, maturity, velocity, etc.)
// and the runtime metrics summary. Those are deferred to later iterations.
type Entity struct {
	ID             string         `json:"id"`
	Type           EntityType     `json:"type"`
	SubType        string         `json:"sub_type,omitempty"`
	CanonicalName  string         `json:"canonical_name"`
	Namespace      string         `json:"namespace"`
	Confidence     float64        `json:"confidence"`
	IsActive       bool           `json:"is_active"`
	LifecycleStage LifecycleStage `json:"lifecycle_stage,omitempty"`
	ValidFrom      time.Time      `json:"valid_from"`
	ValidTo        *time.Time     `json:"valid_to,omitempty"`
	Version        int            `json:"version"`
	Properties     map[string]any `json:"properties,omitempty"`
}

// DeterministicID produces a stable identifier from the entity's identity
// fields. Zone 2's NIF transformer normally produces this; the MVP recomputes
// it here so callers can omit `ID` and get a consistent hash.
//
// Spec recipe: hash(source_type + source_id + entity_type + canonical_name + namespace).
// The MVP currently lacks source_type/source_id (no Zone 2), so we hash the
// available identity fields only. Two writes for the same logical entity from
// different sources will collide on the same ID — which is the intended
// outcome.
func (e *Entity) DeterministicID() string {
	h := sha256.New()
	fmt.Fprintf(h, "%s|%s|%s", e.Type, e.CanonicalName, e.Namespace)
	return "ent_" + hex.EncodeToString(h.Sum(nil))[:24]
}
