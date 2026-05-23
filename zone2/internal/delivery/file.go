package delivery

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"archgraph/zone2/internal/nif"
)

// FileSink dumps records to JSONL on disk. One file per run for clarity —
// timestamped name keeps multiple runs distinct. Useful when zone4d isn't
// running and you want to inspect what the ingestors produced.
type FileSink struct {
	dir string
	mu  sync.Mutex
}

func NewFileSink(dir string) (*FileSink, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("file sink dir: %w", err)
	}
	return &FileSink{dir: dir}, nil
}

func (s *FileSink) Publish(_ context.Context, batch *nif.Batch) (PublishResult, error) {
	if batch == nil || batch.Len() == 0 {
		return PublishResult{}, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	name := time.Now().UTC().Format("20060102T150405.000000000") + ".jsonl"
	path := filepath.Join(s.dir, name)
	f, err := os.Create(path)
	if err != nil {
		return PublishResult{}, fmt.Errorf("create %s: %w", path, err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)

	res := PublishResult{}
	for _, e := range batch.Entities {
		if err := enc.Encode(map[string]any{"kind": "entity", "record": e}); err != nil {
			res.Failed++
			continue
		}
		res.EntitiesEmitted++
	}
	for _, r := range batch.Relationships {
		if err := enc.Encode(map[string]any{"kind": "relationship", "record": r}); err != nil {
			res.Failed++
			continue
		}
		res.RelationshipsEmitted++
	}
	return res, nil
}
