// Package ledger is the append-only audit log of every ingestion run.
// JSONL on disk — one line per run. Cheap to tail, cheap to grep.
//
// We don't truncate. If size becomes a problem in production, rotate at the
// OS level (logrotate) or swap the file backend for SQLite.
package ledger

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Status mirrors Zone 2 §6.1.
type Status string

const (
	StatusSuccess Status = "success"
	StatusPartial Status = "partial"
	StatusFailed  Status = "failed"
)

// Entry is one run's record. Fields are the spec's verbatim with one
// shortcut: errors collapses to a slice of strings (not structured) for MVP.
type Entry struct {
	RunID        string    `json:"run_id"`
	IngestorID   string    `json:"ingestor_id"`
	SourceID     string    `json:"source_id"`
	TriggerType  string    `json:"trigger_type"`
	StartedAt    time.Time `json:"started_at"`
	CompletedAt  time.Time `json:"completed_at"`
	DurationMS   int64     `json:"duration_ms"`
	Fetched      int       `json:"records_fetched"`
	Transformed  int       `json:"records_transformed"`
	Emitted      int       `json:"records_emitted"`
	Failed       int       `json:"records_failed"`
	Skipped      int       `json:"records_skipped"`
	CheckpointBefore string `json:"checkpoint_before"`
	CheckpointAfter  string `json:"checkpoint_after"`
	Errors       []string  `json:"errors,omitempty"`
	Status       Status    `json:"status"`
}

// Ledger is the writer-side handle. Concurrent Append calls are safe; reads
// are best-effort consistent (a tail in flight may not show the entry yet).
type Ledger struct {
	path string
	mu   sync.Mutex
	f    *os.File
}

func Open(dir string) (*Ledger, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("ledger dir: %w", err)
	}
	path := filepath.Join(dir, "ledger.jsonl")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open ledger: %w", err)
	}
	return &Ledger{path: path, f: f}, nil
}

func (l *Ledger) Close() error {
	if l == nil || l.f == nil {
		return nil
	}
	return l.f.Close()
}

// Append writes one Entry as a JSON line and fsyncs. fsync is overkill on
// modern filesystems but the spec is firm on "no silent data loss".
func (l *Ledger) Append(e Entry) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f == nil {
		return errors.New("ledger closed")
	}
	b, err := json.Marshal(&e)
	if err != nil {
		return fmt.Errorf("encode ledger entry: %w", err)
	}
	b = append(b, '\n')
	if _, err := l.f.Write(b); err != nil {
		return fmt.Errorf("write ledger: %w", err)
	}
	return l.f.Sync()
}

// Tail returns up to `limit` most-recent entries by reading the file end-to-start.
// Simple linear scan — the ledger isn't expected to grow beyond tens of
// thousands of lines in MVP, where this is still milliseconds.
func Tail(path string, limit int) ([]Entry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	var all []Entry
	for scanner.Scan() {
		var e Entry
		if err := json.Unmarshal(scanner.Bytes(), &e); err != nil {
			continue // skip malformed lines rather than failing the whole read
		}
		all = append(all, e)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if limit > 0 && len(all) > limit {
		all = all[len(all)-limit:]
	}
	// Reverse so newest first.
	for i, j := 0, len(all)-1; i < j; i, j = i+1, j-1 {
		all[i], all[j] = all[j], all[i]
	}
	return all, nil
}

// LedgerPath exposes the on-disk path so HTTP handlers can read it.
func (l *Ledger) Path() string { return l.path }
