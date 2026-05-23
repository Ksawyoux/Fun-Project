// Package dlq is the dead-letter queue. Anything that fails validation or
// delivery lands here with enough context to reprocess after a fix.
//
// JSONL on disk — same shape and reasoning as the ledger.
package dlq

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// FailureStage tells investigators where the record died.
type FailureStage string

const (
	StageConnector     FailureStage = "connector"
	StageParsing       FailureStage = "parsing"
	StageNormalization FailureStage = "normalization"
	StageDelivery      FailureStage = "delivery"
)

// Entry carries the failed record (as raw JSON, since shape varies) plus
// the metadata Zone 2 §5.2 calls out.
type Entry struct {
	OriginalRecord json.RawMessage `json:"original_record"`
	Reason         string          `json:"reason"`
	Stage          FailureStage    `json:"stage"`
	FailedAt       time.Time       `json:"failed_at"`
	RetryCount     int             `json:"retry_count"`
	IngestionRun   string          `json:"ingestion_run_id"`
}

type Queue struct {
	path string
	mu   sync.Mutex
	f    *os.File
}

func Open(dir string) (*Queue, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("dlq dir: %w", err)
	}
	path := filepath.Join(dir, "dlq.jsonl")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open dlq: %w", err)
	}
	return &Queue{path: path, f: f}, nil
}

func (q *Queue) Close() error {
	if q == nil || q.f == nil {
		return nil
	}
	return q.f.Close()
}

// Push writes one failure entry. Marshalling the original record happens
// outside this call — callers usually have the raw bytes already (from the
// ingestor) or marshal whatever object they couldn't deliver.
func (q *Queue) Push(e Entry) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.f == nil {
		return fmt.Errorf("dlq closed")
	}
	if e.FailedAt.IsZero() {
		e.FailedAt = time.Now().UTC()
	}
	b, err := json.Marshal(&e)
	if err != nil {
		return fmt.Errorf("encode dlq entry: %w", err)
	}
	b = append(b, '\n')
	if _, err := q.f.Write(b); err != nil {
		return fmt.Errorf("write dlq: %w", err)
	}
	return q.f.Sync()
}

func (q *Queue) Path() string { return q.path }
