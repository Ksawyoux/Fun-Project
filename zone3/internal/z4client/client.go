package z4client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var ErrNotFound = errors.New("zone4: not found")

type Z4Entity struct {
	ID             string         `json:"id"`
	Type           string         `json:"type"`
	SubType        string         `json:"sub_type,omitempty"`
	CanonicalName  string         `json:"canonical_name"`
	Namespace      string         `json:"namespace"`
	Confidence     float64        `json:"confidence"`
	IsActive       bool           `json:"is_active"`
	LifecycleStage string         `json:"lifecycle_stage,omitempty"`
	ValidFrom      time.Time      `json:"valid_from"`
	ValidTo        *time.Time     `json:"valid_to,omitempty"`
	Version        int            `json:"version"`
	Properties     map[string]any `json:"properties,omitempty"`
}

type Z4Relationship struct {
	ID         string         `json:"id"`
	Type       string         `json:"type"`
	FromID     string         `json:"from_id"`
	ToID       string         `json:"to_id"`
	Confidence float64        `json:"confidence"`
	IsActive   bool           `json:"is_active"`
	ValidFrom  time.Time      `json:"valid_from"`
	ValidTo    *time.Time     `json:"valid_to,omitempty"`
	Version    int            `json:"version"`
	Properties map[string]any `json:"properties,omitempty"`
}

type MutationKind string

const (
	KindUpsertEntity       MutationKind = "UPSERT_ENTITY"
	KindUpsertRelationship MutationKind = "UPSERT_RELATIONSHIP"
	KindSoftDeleteEntity   MutationKind = "SOFT_DELETE_ENTITY"
	KindDeleteRelationship MutationKind = "DELETE_RELATIONSHIP"
)

type Mutation struct {
	Kind         MutationKind    `json:"kind"`
	Entity       *Z4Entity       `json:"entity,omitempty"`
	Relationship *Z4Relationship `json:"relationship,omitempty"`
	TargetID     string          `json:"target_id,omitempty"`
	Reason       string          `json:"reason,omitempty"`
}

type z4Request struct {
	Mutations []Mutation `json:"mutations"`
}

type z4Response struct {
	TransactionID string `json:"transaction_id"`
	Status        string `json:"status"`
	Results       []struct {
		Index          int    `json:"index"`
		Kind           string `json:"kind"`
		Operation      string `json:"operation"`
		EntityID       string `json:"entity_id,omitempty"`
		RelationshipID string `json:"relationship_id,omitempty"`
		Error          string `json:"error,omitempty"`
	} `json:"results"`
}

type NamespaceListing struct {
	Namespace     string            `json:"namespace"`
	Entities      []*Z4Entity       `json:"entities"`
	Relationships []*Z4Relationship `json:"relationships"`
}

type Client struct {
	base string
	http *http.Client
}

func New(baseURL string) *Client {
	return &Client{
		base: strings.TrimRight(baseURL, "/"),
		http: &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *Client) ListNamespace(ctx context.Context, namespace string) (*NamespaceListing, error) {
	v := url.Values{"namespace": {namespace}}
	var out NamespaceListing
	if err := c.get(ctx, "/v1/entities?"+v.Encode(), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GetEntity(ctx context.Context, id string) (*Z4Entity, error) {
	var out Z4Entity
	if err := c.get(ctx, "/v1/entities/"+url.PathEscape(id), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GetEntityByName(ctx context.Context, namespace, name string) (*Z4Entity, error) {
	v := url.Values{"namespace": {namespace}, "name": {name}}
	var out Z4Entity
	if err := c.get(ctx, "/v1/entities?"+v.Encode(), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) ApplyBatch(ctx context.Context, mutations []Mutation) error {
	if len(mutations) == 0 {
		return nil
	}

	body, err := json.Marshal(z4Request{Mutations: mutations})
	if err != nil {
		return fmt.Errorf("marshal mutations: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/v1/mutations", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("send mutations request: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return fmt.Errorf("zone4 %s: %s", resp.Status, truncate(respBody, 512))
	}

	var parsed z4Response
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}

	if parsed.Status == "FAILED" {
		var errs []string
		for _, r := range parsed.Results {
			if r.Error != "" {
				errs = append(errs, fmt.Sprintf("[mutation %d] %s", r.Index, r.Error))
			}
		}
		return fmt.Errorf("mutation batch failed: %s", strings.Join(errs, "; "))
	}

	return nil
}

func (c *Client) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("zone4 request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("zone4 read: %w", err)
	}
	if resp.StatusCode == http.StatusNotFound {
		return ErrNotFound
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("zone4 %s: %s: %s", path, resp.Status, truncate(body, 256))
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("zone4 decode: %w", err)
	}
	return nil
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "…"
}
