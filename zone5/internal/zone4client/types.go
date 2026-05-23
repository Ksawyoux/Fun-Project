// Package zone4client is the HTTP boundary between Zone 5 and Zone 4.
//
// Why DTOs instead of importing zone4's schema: zone4 is a separate Go
// module, and its types live under `internal/`. So Zone 5 mirrors the wire
// shape here. Mild duplication, sharp boundary — same contract zone4d
// publishes over JSON.
package zone4client

import "time"

type Entity struct {
	ID             string         `json:"id"`
	Type           string         `json:"type"`
	SubType        string         `json:"sub_type,omitempty"`
	CanonicalName  string         `json:"canonical_name"`
	Namespace      string         `json:"namespace"`
	Confidence     float64        `json:"confidence"`
	IsActive       bool           `json:"is_active"`
	LifecycleStage string         `json:"lifecycle_stage"`
	ValidFrom      time.Time      `json:"valid_from"`
	ValidTo        *time.Time     `json:"valid_to,omitempty"`
	Version        int64          `json:"version"`
	Properties     map[string]any `json:"properties,omitempty"`
}

type Relationship struct {
	ID         string         `json:"id"`
	Type       string         `json:"type"`
	FromID     string         `json:"from_id"`
	ToID       string         `json:"to_id"`
	Confidence float64        `json:"confidence"`
	IsActive   bool           `json:"is_active"`
	ValidFrom  time.Time      `json:"valid_from"`
	ValidTo    *time.Time     `json:"valid_to,omitempty"`
	Version    int64          `json:"version"`
	Properties map[string]any `json:"properties,omitempty"`
}

type Neighborhood struct {
	Origin   *Entity        `json:"origin"`
	Nodes    []*Entity      `json:"nodes"`
	Edges    []*Relationship `json:"edges"`
	MaxDepth int            `json:"max_depth"`
}

type NamespaceListing struct {
	Namespace     string          `json:"namespace"`
	Entities      []*Entity       `json:"entities"`
	Relationships []*Relationship `json:"relationships"`
}

type LogEntry struct {
	EntryID        int64           `json:"entry_id"`
	TransactionID  string          `json:"transaction_id"`
	MutationType   string          `json:"mutation_type"`
	EntityID       string          `json:"entity_id,omitempty"`
	RelationshipID string          `json:"relationship_id,omitempty"`
	BeforeState    map[string]any  `json:"before_state,omitempty"`
	AfterState     map[string]any  `json:"after_state,omitempty"`
	OccurredAt     time.Time       `json:"occurred_at"`
	RecordedAt     time.Time       `json:"recorded_at"`
}

type LogListing struct {
	Entries []LogEntry `json:"entries"`
}

// Direction mirrors zone4's enum on the wire.
type Direction string

const (
	DirOut  Direction = "out"
	DirIn   Direction = "in"
	DirBoth Direction = "both"
)
