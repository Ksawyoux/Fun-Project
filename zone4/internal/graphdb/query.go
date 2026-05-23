package graphdb

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"archgraph/zone4/internal/schema"
)

// GetEntity returns the entity by canonical ID. Soft-deleted entities are
// returned (with is_active=false) so historical lookups still work.
func (s *Store) GetEntity(ctx context.Context, id string) (*schema.Entity, error) {
	row := s.db.QueryRowContext(ctx, entitySelectByID, id)
	return scanEntity(row)
}

// GetEntityByName looks up the most recently updated active entity by
// canonical name within a namespace. Returns ErrNotFound if no active row
// matches.
func (s *Store) GetEntityByName(ctx context.Context, namespace, name string) (*schema.Entity, error) {
	row := s.db.QueryRowContext(ctx, entitySelectByName, namespace, name)
	return scanEntity(row)
}

func (s *Store) GetRelationship(ctx context.Context, id string) (*schema.Relationship, error) {
	row := s.db.QueryRowContext(ctx, relSelectByID, id)
	return scanRelationship(row)
}

// ListEntitiesByNamespace returns all active entities in a namespace, ordered
// by canonical_name. Used by Zone 5's Health Auditor which needs to scan a
// workspace to find cycles, supernodes, etc. Hard-capped at 5000 to keep the
// payload bounded — callers needing more should paginate (deferred).
func (s *Store) ListEntitiesByNamespace(ctx context.Context, namespace string) ([]*schema.Entity, error) {
	rows, err := s.db.QueryContext(ctx, `
        SELECT `+entityColumns+`
          FROM entities
         WHERE namespace = ? AND is_active = 1
         ORDER BY canonical_name ASC
         LIMIT 5000
    `, namespace)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*schema.Entity
	for rows.Next() {
		e, err := scanEntity(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ListRelationshipsByNamespace returns all active relationships where both
// endpoints share the namespace. Needed for cycle/supernode detection.
func (s *Store) ListRelationshipsByNamespace(ctx context.Context, namespace string) ([]*schema.Relationship, error) {
	rows, err := s.db.QueryContext(ctx, `
        SELECT `+relColumns+`
          FROM relationships r
         WHERE r.is_active = 1
           AND EXISTS (SELECT 1 FROM entities e WHERE e.id = r.from_id AND e.namespace = ?)
           AND EXISTS (SELECT 1 FROM entities e WHERE e.id = r.to_id   AND e.namespace = ?)
         LIMIT 20000
    `, namespace, namespace)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*schema.Relationship
	for rows.Next() {
		r, err := scanRelationship(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Neighborhood is the result of an N-hop traversal starting from an origin.
type Neighborhood struct {
	Origin   *schema.Entity         `json:"origin"`
	Nodes    []*schema.Entity       `json:"nodes"`
	Edges    []*schema.Relationship `json:"edges"`
	MaxDepth int                    `json:"max_depth"`
}

// Direction controls which edges count during traversal.
type Direction int

const (
	DirOutbound Direction = iota // from_id -> to_id
	DirInbound                   // to_id -> from_id (callers/dependents)
	DirBoth
)

// Neighborhood does an iterative BFS up to depth `maxDepth`. Only active
// edges are followed. Capped at 5 hops to mirror the spec's recommended bound
// on transitive inference depth.
//
// Plain Go BFS rather than a recursive CTE: the CTE form makes it awkward to
// bound by depth AND return full row data for both nodes and edges.
func (s *Store) Neighborhood(ctx context.Context, originID string, maxDepth int, dir Direction) (*Neighborhood, error) {
	if maxDepth < 0 {
		maxDepth = 0
	}
	if maxDepth > 5 {
		maxDepth = 5
	}

	origin, err := s.GetEntity(ctx, originID)
	if err != nil {
		return nil, err
	}

	result := &Neighborhood{Origin: origin, MaxDepth: maxDepth}
	seenNodes := map[string]*schema.Entity{origin.ID: origin}
	seenEdges := map[string]struct{}{}
	frontier := []string{origin.ID}

	for depth := 0; depth < maxDepth && len(frontier) > 0; depth++ {
		var next []string
		for _, nid := range frontier {
			edges, err := s.edgesForNode(ctx, nid, dir)
			if err != nil {
				return nil, fmt.Errorf("expand %s: %w", nid, err)
			}
			for _, e := range edges {
				if _, ok := seenEdges[e.ID]; ok {
					continue
				}
				seenEdges[e.ID] = struct{}{}
				result.Edges = append(result.Edges, e)

				other := e.ToID
				if e.ToID == nid {
					other = e.FromID
				}
				if _, ok := seenNodes[other]; ok {
					continue
				}
				node, err := s.GetEntity(ctx, other)
				if err != nil {
					if errors.Is(err, ErrNotFound) {
						continue
					}
					return nil, fmt.Errorf("load neighbor %s: %w", other, err)
				}
				seenNodes[other] = node
				next = append(next, other)
			}
		}
		frontier = next
	}

	for id, n := range seenNodes {
		if id == origin.ID {
			continue
		}
		result.Nodes = append(result.Nodes, n)
	}
	return result, nil
}

func (s *Store) edgesForNode(ctx context.Context, id string, dir Direction) ([]*schema.Relationship, error) {
	var rows *sql.Rows
	var err error
	switch dir {
	case DirOutbound:
		rows, err = s.db.QueryContext(ctx, relSelectOutbound, id)
	case DirInbound:
		rows, err = s.db.QueryContext(ctx, relSelectInbound, id)
	default:
		rows, err = s.db.QueryContext(ctx, relSelectEitherDir, id, id)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*schema.Relationship
	for rows.Next() {
		r, err := scanRelationship(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

type rowScanner interface {
	Scan(dest ...any) error
}

// ScanEntityRow is exported so the mutation package can scan an entity row
// from inside an open transaction without taking a second pool connection.
func ScanEntityRow(row rowScanner) (*schema.Entity, error) {
	return scanEntity(row)
}

// ScanRelationshipRow is exported for the same reason as ScanEntityRow.
func ScanRelationshipRow(row rowScanner) (*schema.Relationship, error) {
	return scanRelationship(row)
}

func scanEntity(row rowScanner) (*schema.Entity, error) {
	var (
		e         schema.Entity
		isActive  int
		props     string
		validFrom string
		validTo   sql.NullString
		lifecycle string
		createdAt string
		updatedAt string
	)
	err := row.Scan(
		&e.ID, &e.Type, &e.SubType, &e.CanonicalName, &e.Namespace,
		&e.Confidence, &isActive, &lifecycle, &validFrom, &validTo,
		&e.Version, &props, &createdAt, &updatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan entity: %w", err)
	}
	_ = createdAt
	_ = updatedAt
	e.IsActive = isActive == 1
	e.LifecycleStage = schema.LifecycleStage(lifecycle)
	if e.ValidFrom, err = time.Parse(time.RFC3339Nano, validFrom); err != nil {
		return nil, fmt.Errorf("parse valid_from: %w", err)
	}
	if validTo.Valid {
		t, err := time.Parse(time.RFC3339Nano, validTo.String)
		if err != nil {
			return nil, fmt.Errorf("parse valid_to: %w", err)
		}
		e.ValidTo = &t
	}
	if props != "" {
		if err := json.Unmarshal([]byte(props), &e.Properties); err != nil {
			return nil, fmt.Errorf("unmarshal properties: %w", err)
		}
	}
	return &e, nil
}

func scanRelationship(row rowScanner) (*schema.Relationship, error) {
	var (
		r         schema.Relationship
		isActive  int
		validFrom string
		validTo   sql.NullString
		props     string
		createdAt string
		updatedAt string
	)
	err := row.Scan(
		&r.ID, &r.Type, &r.FromID, &r.ToID, &r.Confidence,
		&isActive, &validFrom, &validTo, &r.Version, &props,
		&createdAt, &updatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan relationship: %w", err)
	}
	_ = createdAt
	_ = updatedAt
	r.IsActive = isActive == 1
	if r.ValidFrom, err = time.Parse(time.RFC3339Nano, validFrom); err != nil {
		return nil, fmt.Errorf("parse valid_from: %w", err)
	}
	if validTo.Valid {
		t, err := time.Parse(time.RFC3339Nano, validTo.String)
		if err != nil {
			return nil, fmt.Errorf("parse valid_to: %w", err)
		}
		r.ValidTo = &t
	}
	if props != "" {
		if err := json.Unmarshal([]byte(props), &r.Properties); err != nil {
			return nil, fmt.Errorf("unmarshal properties: %w", err)
		}
	}
	return &r, nil
}

const entityColumns = `
    id, type, sub_type, canonical_name, namespace, confidence,
    is_active, lifecycle_stage, valid_from, valid_to, version,
    properties, created_at, updated_at
`

const relColumns = `
    id, type, from_id, to_id, confidence, is_active,
    valid_from, valid_to, version, properties, created_at, updated_at
`

var (
	entitySelectByID = `SELECT ` + entityColumns + ` FROM entities WHERE id = ?`

	entitySelectByName = `
        SELECT ` + entityColumns + `
          FROM entities
         WHERE namespace = ? AND canonical_name = ? AND is_active = 1
         ORDER BY updated_at DESC
         LIMIT 1
    `

	relSelectByID = `SELECT ` + relColumns + ` FROM relationships WHERE id = ?`

	relSelectOutbound = `
        SELECT ` + relColumns + `
          FROM relationships
         WHERE from_id = ? AND is_active = 1
    `

	relSelectInbound = `
        SELECT ` + relColumns + `
          FROM relationships
         WHERE to_id = ? AND is_active = 1
    `

	relSelectEitherDir = `
        SELECT ` + relColumns + `
          FROM relationships
         WHERE (from_id = ? OR to_id = ?) AND is_active = 1
    `
)
