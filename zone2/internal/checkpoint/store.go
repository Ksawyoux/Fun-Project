// Package checkpoint persists "where each ingestor left off" so reruns are
// incremental.
//
// File-backed JSON, one file per source. Two-file write (tmp + rename) so a
// crash mid-write can't corrupt the checkpoint — the spec calls this out as
// load-bearing (§1.1 Checkpoint Manager).
package checkpoint

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Record struct {
	SourceID   string    `json:"source_id"`
	Position   string    `json:"position"`   // SHA, hash, timestamp window — meaning is per-ingestor
	UpdatedAt  time.Time `json:"updated_at"`
	Generation int       `json:"generation"` // bumped each save; helps rollback
}

// Store persists one Record per source under a directory.
type Store struct {
	dir string
	mu  sync.Mutex // serializes writes; reads can race but are cheap
}

func New(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("checkpoint dir: %w", err)
	}
	return &Store{dir: dir}, nil
}

// Load returns the empty Record if nothing has been saved yet — a
// fresh-source first-run convention.
func (s *Store) Load(sourceID string) (Record, error) {
	path := s.pathFor(sourceID)
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return Record{SourceID: sourceID}, nil
	}
	if err != nil {
		return Record{}, fmt.Errorf("read checkpoint %s: %w", sourceID, err)
	}
	var r Record
	if err := json.Unmarshal(b, &r); err != nil {
		return Record{}, fmt.Errorf("decode checkpoint %s: %w", sourceID, err)
	}
	return r, nil
}

// Save writes atomically via tmp + rename.
func (s *Store) Save(sourceID, position string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	prev, err := s.Load(sourceID)
	if err != nil {
		return err
	}
	next := Record{
		SourceID:   sourceID,
		Position:   position,
		UpdatedAt:  time.Now().UTC(),
		Generation: prev.Generation + 1,
	}
	b, err := json.MarshalIndent(&next, "", "  ")
	if err != nil {
		return fmt.Errorf("encode checkpoint: %w", err)
	}
	path := s.pathFor(sourceID)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return fmt.Errorf("write checkpoint tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename checkpoint: %w", err)
	}
	return nil
}

func (s *Store) pathFor(sourceID string) string {
	return filepath.Join(s.dir, sanitize(sourceID)+".json")
}

// sanitize keeps filenames portable — we don't want a source_id with "/" in
// it to land us a level deep.
func sanitize(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '_', c == '.':
			out = append(out, c)
		default:
			out = append(out, '_')
		}
	}
	if len(out) == 0 {
		return "default"
	}
	return string(out)
}
