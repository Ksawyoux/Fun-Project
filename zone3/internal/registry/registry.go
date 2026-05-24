package registry

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

var ErrNotFound = errors.New("entity not found in registry")

type AliasInfo struct {
	Name       string    `json:"name"`
	SourceType string    `json:"source_type"`
	SourceID   string    `json:"source_id"`
	Confidence float64   `json:"confidence"`
	AddedAt    time.Time `json:"added_at"`
}

type SourceContribution struct {
	SourceType string    `json:"source_type"`
	SourceID   string    `json:"source_id"`
	SourceRef  string    `json:"source_ref"`
	LastSeen   time.Time `json:"last_seen"`
}

type RegistryEntity struct {
	CanonicalID         string               `json:"canonical_id"`
	EntityType          string               `json:"entity_type"`
	SubType             string               `json:"sub_type"`
	CanonicalName       string               `json:"canonical_name"`
	Namespace           string               `json:"namespace"`
	Aliases             []AliasInfo          `json:"aliases"`
	SourceContributions []SourceContribution `json:"source_contributions"`
	Confidence          float64              `json:"confidence"`
	FirstSeen           time.Time            `json:"first_seen"`
	LastConfirmed       time.Time            `json:"last_confirmed"`
}

type Registry struct {
	db *sql.DB
}

func Open(dbPath string) (*Registry, error) {
	if dbPath != ":memory:" {
		dir := filepath.Dir(dbPath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("create registry db directory: %w", err)
		}
	}

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite db: %w", err)
	}

	// Initialize tables
	schema := `
	CREATE TABLE IF NOT EXISTS registry_entities (
		canonical_id TEXT PRIMARY KEY,
		entity_type TEXT NOT NULL,
		sub_type TEXT,
		canonical_name TEXT NOT NULL,
		namespace TEXT NOT NULL,
		aliases TEXT NOT NULL DEFAULT '[]',
		source_contributions TEXT NOT NULL DEFAULT '[]',
		confidence REAL NOT NULL DEFAULT 1.0,
		first_seen DATETIME NOT NULL,
		last_confirmed DATETIME NOT NULL
	);

	CREATE UNIQUE INDEX IF NOT EXISTS idx_registry_entities_name_ns ON registry_entities(canonical_name, namespace);
	`
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("init registry schema: %w", err)
	}

	return &Registry{db: db}, nil
}

func (r *Registry) Close() error {
	return r.db.Close()
}

func (r *Registry) GetByID(ctx context.Context, id string) (*RegistryEntity, error) {
	query := `
		SELECT canonical_id, entity_type, sub_type, canonical_name, namespace, aliases, source_contributions, confidence, first_seen, last_confirmed
		FROM registry_entities
		WHERE canonical_id = ?
	`
	return r.scanOne(ctx, r.db.QueryRowContext(ctx, query, id))
}

func (r *Registry) GetByCanonicalName(ctx context.Context, name, namespace string) (*RegistryEntity, error) {
	query := `
		SELECT canonical_id, entity_type, sub_type, canonical_name, namespace, aliases, source_contributions, confidence, first_seen, last_confirmed
		FROM registry_entities
		WHERE canonical_name = ? AND namespace = ?
	`
	return r.scanOne(ctx, r.db.QueryRowContext(ctx, query, name, namespace))
}

func (r *Registry) GetByAlias(ctx context.Context, aliasName, namespace string) (*RegistryEntity, error) {
	// Look up by finding an entity that has aliasName in its aliases JSON array of objects
	query := `
		SELECT DISTINCT e.canonical_id, e.entity_type, e.sub_type, e.canonical_name, e.namespace, e.aliases, e.source_contributions, e.confidence, e.first_seen, e.last_confirmed
		FROM registry_entities e, json_each(e.aliases)
		WHERE e.namespace = ? AND json_extract(json_each.value, '$.name') = ?
	`
	return r.scanOne(ctx, r.db.QueryRowContext(ctx, query, namespace, aliasName))
}

func (r *Registry) Upsert(ctx context.Context, ent *RegistryEntity) error {
	aliasesJSON, err := json.Marshal(ent.Aliases)
	if err != nil {
		return fmt.Errorf("marshal aliases: %w", err)
	}
	contribJSON, err := json.Marshal(ent.SourceContributions)
	if err != nil {
		return fmt.Errorf("marshal contributions: %w", err)
	}

	query := `
		INSERT INTO registry_entities (canonical_id, entity_type, sub_type, canonical_name, namespace, aliases, source_contributions, confidence, first_seen, last_confirmed)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(canonical_id) DO UPDATE SET
			entity_type = excluded.entity_type,
			sub_type = excluded.sub_type,
			canonical_name = excluded.canonical_name,
			namespace = excluded.namespace,
			aliases = excluded.aliases,
			source_contributions = excluded.source_contributions,
			confidence = excluded.confidence,
			last_confirmed = excluded.last_confirmed
	`

	_, err = r.db.ExecContext(ctx, query,
		ent.CanonicalID,
		ent.EntityType,
		ent.SubType,
		ent.CanonicalName,
		ent.Namespace,
		string(aliasesJSON),
		string(contribJSON),
		ent.Confidence,
		ent.FirstSeen,
		ent.LastConfirmed,
	)
	if err != nil {
		return fmt.Errorf("upsert registry entity: %w", err)
	}
	return nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func (r *Registry) scanOne(ctx context.Context, scanner rowScanner) (*RegistryEntity, error) {
	var (
		ent         RegistryEntity
		aliasesStr  string
		contribsStr string
	)

	err := scanner.Scan(
		&ent.CanonicalID,
		&ent.EntityType,
		&ent.SubType,
		&ent.CanonicalName,
		&ent.Namespace,
		&aliasesStr,
		&contribsStr,
		&ent.Confidence,
		&ent.FirstSeen,
		&ent.LastConfirmed,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("scan registry entity: %w", err)
	}

	if err := json.Unmarshal([]byte(aliasesStr), &ent.Aliases); err != nil {
		return nil, fmt.Errorf("unmarshal aliases: %w", err)
	}
	if err := json.Unmarshal([]byte(contribsStr), &ent.SourceContributions); err != nil {
		return nil, fmt.Errorf("unmarshal contributions: %w", err)
	}

	return &ent, nil
}
