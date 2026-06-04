package delivery

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"archgraph/nif"
)

// Zone3Sink ships NIF records directly to zone3d's POST /v1/ingest endpoint.
type Zone3Sink struct {
	base string
	http *http.Client
}

func NewZone3Sink(baseURL string) *Zone3Sink {
	return &Zone3Sink{
		base: strings.TrimRight(baseURL, "/"),
		http: &http.Client{Timeout: 30 * time.Second},
	}
}

func (s *Zone3Sink) Publish(ctx context.Context, batch *nif.Batch) (PublishResult, error) {
	if batch == nil || batch.Len() == 0 {
		return PublishResult{}, nil
	}

	body, err := json.Marshal(batch)
	if err != nil {
		return PublishResult{}, fmt.Errorf("encode batch: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.base+"/v1/ingest", bytes.NewReader(body))
	if err != nil {
		return PublishResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.http.Do(req)
	if err != nil {
		return PublishResult{}, fmt.Errorf("post batch: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return PublishResult{}, fmt.Errorf("zone3 %s: %s", resp.Status, truncate(respBody, 512))
	}

	var parsed struct {
		Status           string `json:"status"`
		MutationsApplied int    `json:"mutations_applied"`
		Message          string `json:"message,omitempty"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return PublishResult{}, fmt.Errorf("decode zone3 response: %w", err)
	}

	return PublishResult{
		EntitiesEmitted:      len(batch.Entities),
		RelationshipsEmitted: len(batch.Relationships),
	}, nil
}
