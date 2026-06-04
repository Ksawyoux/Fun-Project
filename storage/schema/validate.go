package schema

import (
	"errors"
	"fmt"
	"strings"
)

const (
	MaxCanonicalNameLen = 256
	MaxNamespaceLen     = 256
	MaxEntityProperties = 500
	MaxRelProperties    = 200
)

// ValidationError carries the offending field plus a human-readable reason.
// The HTTP layer translates this to a 400 with structured detail.
type ValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("validation: %s: %s", e.Field, e.Message)
}

func vfail(field, msg string) error {
	return &ValidationError{Field: field, Message: msg}
}

// ValidateEntity enforces the schema rules from Zone4.md's Schema Enforcer section.
func ValidateEntity(e *Entity) error {
	if e == nil {
		return vfail("entity", "nil entity")
	}
	if !e.Type.Valid() {
		return vfail("type", fmt.Sprintf("unknown EntityType %q", e.Type))
	}
	if strings.TrimSpace(e.CanonicalName) == "" {
		return vfail("canonical_name", "required")
	}
	if len(e.CanonicalName) > MaxCanonicalNameLen {
		return vfail("canonical_name", fmt.Sprintf("exceeds max length %d", MaxCanonicalNameLen))
	}
	if strings.TrimSpace(e.Namespace) == "" {
		return vfail("namespace", "required")
	}
	if len(e.Namespace) > MaxNamespaceLen {
		return vfail("namespace", fmt.Sprintf("exceeds max length %d", MaxNamespaceLen))
	}
	if e.Confidence < 0.0 || e.Confidence > 1.0 {
		return vfail("confidence", "must be in [0.0, 1.0]")
	}
	if e.ValidFrom.IsZero() {
		return vfail("valid_from", "required")
	}
	if e.ValidTo != nil && e.ValidTo.Before(e.ValidFrom) {
		return vfail("valid_to", "must be >= valid_from")
	}
	if len(e.Properties) > MaxEntityProperties {
		return vfail("properties", fmt.Sprintf("exceeds max %d keys", MaxEntityProperties))
	}
	return nil
}

// ValidateRelationship enforces edge-level rules including REL-002 (no self-loops).
func ValidateRelationship(r *Relationship) error {
	if r == nil {
		return vfail("relationship", "nil relationship")
	}
	if !r.Type.Valid() {
		return vfail("type", fmt.Sprintf("unknown RelationshipType %q", r.Type))
	}
	if strings.TrimSpace(r.FromID) == "" {
		return vfail("from_id", "required")
	}
	if strings.TrimSpace(r.ToID) == "" {
		return vfail("to_id", "required")
	}
	if r.FromID == r.ToID {
		return vfail("from_id/to_id", "self-referential relationship not allowed")
	}
	if r.Confidence < 0.0 || r.Confidence > 1.0 {
		return vfail("confidence", "must be in [0.0, 1.0]")
	}
	if r.ValidFrom.IsZero() {
		return vfail("valid_from", "required")
	}
	if r.ValidTo != nil && r.ValidTo.Before(r.ValidFrom) {
		return vfail("valid_to", "must be >= valid_from")
	}
	if len(r.Properties) > MaxRelProperties {
		return vfail("properties", fmt.Sprintf("exceeds max %d keys", MaxRelProperties))
	}
	return nil
}

// ErrUnknownEntity is returned when a relationship references an entity that
// doesn't exist in the graph. Surfaced to the caller so they can decide
// whether to hold-and-retry or fail the batch.
var ErrUnknownEntity = errors.New("relationship references unknown entity")
