package search

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"strings"

	"archgraph/zone4/internal/graphdb"
	"archgraph/zone4/schema"
)

type SearchOptions struct {
	Query       string
	Namespace   string
	EntityType  string
	SubType     string
	OwnerTeam   string
	Criticality string
	Maturity    string
	Velocity    string
	IsActive    *bool
}

type Indexer struct {
	db    *sql.DB
	queue chan string
	stop  chan struct{}
}

func New(db *sql.DB) *Indexer {
	return &Indexer{
		db:    db,
		queue: make(chan string, 1000),
		stop:  make(chan struct{}),
	}
}

// Start starts the background indexing worker.
func (idx *Indexer) Start(ctx context.Context) {
	go func() {
		for {
			select {
			case id := <-idx.queue:
				if err := idx.indexEntity(id); err != nil {
					log.Printf("failed to index entity %q: %v", id, err)
				}
			case <-idx.stop:
				// Drain queue
				for len(idx.queue) > 0 {
					id := <-idx.queue
					_ = idx.indexEntity(id)
				}
				return
			case <-ctx.Done():
				return
			}
		}
	}()
}

// Stop stops the indexer.
func (idx *Indexer) Stop() {
	close(idx.stop)
}

// Enqueue adds an entity ID to the indexing queue.
func (idx *Indexer) Enqueue(entityID string) {
	select {
	case idx.queue <- entityID:
	default:
		log.Printf("warning: search indexer queue full, dropping entity %q", entityID)
	}
}

func (idx *Indexer) indexEntity(id string) error {
	row := idx.db.QueryRow(`
        SELECT id, type, sub_type, canonical_name, namespace, confidence,
               is_active, lifecycle_stage, valid_from, valid_to, version,
               properties, created_at, updated_at
          FROM entities
         WHERE id = ?
    `, id)
	e, err := graphdb.ScanEntityRow(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, graphdb.ErrNotFound) {
			_, err = idx.db.Exec(`DELETE FROM entity_search WHERE entity_id = ?`, id)
			return err
		}
		return err
	}

	aliases := extractStringSlice(e.Properties, "aliases")
	tags := extractStringSlice(e.Properties, "tags")
	smells := extractStringSlice(e.Properties, "architectural_smells")

	ownerTeam := extractString(e.Properties, "owner_team")
	if ownerTeam == "" {
		ownerTeam = extractString(e.Properties, "owner")
	}
	criticality := extractString(e.Properties, "criticality")
	maturity := extractString(e.Properties, "maturity")
	velocity := extractString(e.Properties, "velocity")

	isActiveVal := 0
	if e.IsActive {
		isActiveVal = 1
	}

	_, err = idx.db.Exec(`
        INSERT OR REPLACE INTO entity_search (
            entity_id, canonical_name, aliases, entity_type, sub_type, namespace,
            owner_team, criticality, maturity, velocity, is_active, architectural_smells, tags
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
    `,
		e.ID, e.CanonicalName, strings.Join(aliases, " "), string(e.Type), e.SubType, e.Namespace,
		ownerTeam, criticality, maturity, velocity, isActiveVal, strings.Join(smells, " "), strings.Join(tags, " "),
	)
	return err
}

func (idx *Indexer) Search(ctx context.Context, opts SearchOptions) ([]*schema.Entity, error) {
	var clauses []string
	var args []any

	if opts.Namespace != "" {
		clauses = append(clauses, "s.namespace = ?")
		args = append(args, opts.Namespace)
	}
	if opts.EntityType != "" {
		clauses = append(clauses, "s.entity_type = ?")
		args = append(args, opts.EntityType)
	}
	if opts.SubType != "" {
		clauses = append(clauses, "s.sub_type = ?")
		args = append(args, opts.SubType)
	}
	if opts.OwnerTeam != "" {
		clauses = append(clauses, "s.owner_team = ?")
		args = append(args, opts.OwnerTeam)
	}
	if opts.Criticality != "" {
		clauses = append(clauses, "s.criticality = ?")
		args = append(args, opts.Criticality)
	}
	if opts.Maturity != "" {
		clauses = append(clauses, "s.maturity = ?")
		args = append(args, opts.Maturity)
	}
	if opts.Velocity != "" {
		clauses = append(clauses, "s.velocity = ?")
		args = append(args, opts.Velocity)
	}
	if opts.IsActive != nil {
		activeVal := 0
		if *opts.IsActive {
			activeVal = 1
		}
		clauses = append(clauses, "s.is_active = ?")
		args = append(args, activeVal)
	}
	if opts.Query != "" {
		clauses = append(clauses, "entity_search MATCH ?")
		args = append(args, opts.Query)
	}

	where := ""
	if len(clauses) > 0 {
		where = " WHERE " + strings.Join(clauses, " AND ")
	}

	q := `
        SELECT e.id, e.type, e.sub_type, e.canonical_name, e.namespace, e.confidence,
               e.is_active, e.lifecycle_stage, e.valid_from, e.valid_to, e.version,
               e.properties, e.created_at, e.updated_at
          FROM entities e
          JOIN entity_search s ON e.id = s.entity_id` + where + `
         ORDER BY e.canonical_name ASC
         LIMIT 200
    `

	rows, err := idx.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("search entities: %w", err)
	}
	defer rows.Close()

	var out []*schema.Entity
	for rows.Next() {
		e, err := graphdb.ScanEntityRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func extractString(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	val, ok := m[key]
	if !ok {
		return ""
	}
	if s, ok := val.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", val)
}

func extractStringSlice(m map[string]any, key string) []string {
	if m == nil {
		return nil
	}
	val, ok := m[key]
	if !ok {
		return nil
	}
	if slice, ok := val.([]any); ok {
		var res []string
		for _, item := range slice {
			if s, ok := item.(string); ok {
				res = append(res, s)
			}
		}
		return res
	}
	if s, ok := val.(string); ok {
		return []string{s}
	}
	return nil
}
