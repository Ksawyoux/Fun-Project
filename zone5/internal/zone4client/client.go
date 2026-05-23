package zone4client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// ErrNotFound mirrors zone4's not-found error so callers can branch on it.
var ErrNotFound = errors.New("zone4: not found")

// Client is a thin wrapper over zone4d's REST surface. Stateless aside from
// the http.Client and base URL.
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

func (c *Client) GetEntity(ctx context.Context, id string) (*Entity, error) {
	var out Entity
	if err := c.get(ctx, "/v1/entities/"+url.PathEscape(id), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GetEntityByName(ctx context.Context, namespace, name string) (*Entity, error) {
	v := url.Values{"namespace": {namespace}, "name": {name}}
	var out Entity
	if err := c.get(ctx, "/v1/entities?"+v.Encode(), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListNamespace returns everything active in a namespace. This is the
// payload backing the Health Auditor — it pays the cost of the full pull
// up front in exchange for in-process Tarjan / supernode detection.
func (c *Client) ListNamespace(ctx context.Context, namespace string) (*NamespaceListing, error) {
	v := url.Values{"namespace": {namespace}}
	var out NamespaceListing
	if err := c.get(ctx, "/v1/entities?"+v.Encode(), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) Neighborhood(ctx context.Context, id string, depth int, dir Direction) (*Neighborhood, error) {
	v := url.Values{
		"depth":     {strconv.Itoa(depth)},
		"direction": {string(dir)},
	}
	var out Neighborhood
	if err := c.get(ctx, "/v1/entities/"+url.PathEscape(id)+"/neighborhood?"+v.Encode(), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ReadLogOpts mirrors deltalog.ReadOpts on the wire.
type ReadLogOpts struct {
	FromEntryID    int64
	EntityID       string
	RelationshipID string
	TransactionID  string
	Limit          int
}

func (c *Client) ReadLog(ctx context.Context, opts ReadLogOpts) ([]LogEntry, error) {
	v := url.Values{}
	if opts.FromEntryID > 0 {
		v.Set("from_entry_id", strconv.FormatInt(opts.FromEntryID, 10))
	}
	if opts.EntityID != "" {
		v.Set("entity_id", opts.EntityID)
	}
	if opts.RelationshipID != "" {
		v.Set("relationship_id", opts.RelationshipID)
	}
	if opts.TransactionID != "" {
		v.Set("transaction_id", opts.TransactionID)
	}
	if opts.Limit > 0 {
		v.Set("limit", strconv.Itoa(opts.Limit))
	}
	var out LogListing
	if err := c.get(ctx, "/v1/log?"+v.Encode(), &out); err != nil {
		return nil, err
	}
	return out.Entries, nil
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
		return fmt.Errorf("zone4 decode: %w (body=%s)", err, truncate(body, 256))
	}
	return nil
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "…"
}
