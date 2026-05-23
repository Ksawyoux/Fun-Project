package nif

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// DeterministicEntityID is the Zone 2 spec formula:
//   hash(source_type + source_id + entity_type + canonical_name + namespace)
//
// Different from Zone 4's formula (which omits source_type and source_id).
// That's intentional: Zone 2 IDs are *per-source*, so two ingestors looking
// at the same logical service produce different IDs — and Zone 3's job
// (when it exists) is the cross-source resolution. Until Zone 3 lands, the
// Zone 4 sink in this module is responsible for collapsing same-name
// entities across sources by canonicalizing source_type to "nif" on the way
// out. See zone2/internal/delivery/zone4.go.
//
// The hash is SHA-256 truncated to 16 bytes (32 hex chars). 16 bytes give
// 2^128 distinct IDs — collision-resistant for any realistic workload.
func DeterministicEntityID(sourceType, sourceID string, t EntityType, canonicalName, namespace string) string {
	return "ent_" + hashFields(sourceType, sourceID, string(t), canonicalName, namespace)
}

// DeterministicRelationshipID — per spec, the relationship ID does NOT
// include source_id, because we want two ingestors observing the same
// (from, to, type) edge to converge. source_type IS included so different
// kinds of observation (e.g. static IMPORTS vs runtime CALLS) stay separate
// even at the same vertex pair.
func DeterministicRelationshipID(t RelationshipType, fromID, toID, sourceType string) string {
	return "rel_" + hashFields(string(t), fromID, toID, sourceType)
}

func hashFields(fields ...string) string {
	// Joining with a delimiter that can't appear in any of the inputs keeps
	// hash(a, b) distinct from hash(ab, ""). \x1f is the ASCII Unit
	// Separator — never appears in identifiers or paths.
	joined := strings.Join(fields, "\x1f")
	sum := sha256.Sum256([]byte(joined))
	return hex.EncodeToString(sum[:16])
}
