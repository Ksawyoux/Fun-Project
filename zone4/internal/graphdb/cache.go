package graphdb

import (
	"sync"
	"time"

	"archgraph/zone4/schema"
)

type cacheEntry struct {
	val       any
	expiresAt time.Time
}

type GraphCache struct {
	mu            sync.RWMutex
	entityCache   map[string]cacheEntry
	neighborCache map[string]cacheEntry
	subgraphCache map[string]cacheEntry
}

func NewCache() *GraphCache {
	return &GraphCache{
		entityCache:   make(map[string]cacheEntry),
		neighborCache: make(map[string]cacheEntry),
		subgraphCache: make(map[string]cacheEntry),
	}
}

// GetEntity gets an entity from Layer 1.
func (c *GraphCache) GetEntity(id string) (*schema.Entity, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	entry, ok := c.entityCache[id]
	if !ok || time.Now().After(entry.expiresAt) {
		return nil, false
	}
	return entry.val.(*schema.Entity), true
}

// SetEntity sets an entity in Layer 1 with a 5-minute TTL.
func (c *GraphCache) SetEntity(id string, e *schema.Entity) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entityCache[id] = cacheEntry{
		val:       e,
		expiresAt: time.Now().Add(5 * time.Minute),
	}
}

// GetNeighborhood gets a neighborhood from Layer 2.
func (c *GraphCache) GetNeighborhood(key string) (*Neighborhood, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	entry, ok := c.neighborCache[key]
	if !ok || time.Now().After(entry.expiresAt) {
		return nil, false
	}
	return entry.val.(*Neighborhood), true
}

// SetNeighborhood sets a neighborhood in Layer 2 with a 2-minute TTL.
func (c *GraphCache) SetNeighborhood(key string, n *Neighborhood) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.neighborCache[key] = cacheEntry{
		val:       n,
		expiresAt: time.Now().Add(2 * time.Minute),
	}
}

// GetSubgraph gets a cached subgraph or list query from Layer 3.
func (c *GraphCache) GetSubgraph(key string) (any, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	entry, ok := c.subgraphCache[key]
	if !ok || time.Now().After(entry.expiresAt) {
		return nil, false
	}
	return entry.val, true
}

// SetSubgraph sets a cached subgraph in Layer 3 with a 10-minute TTL.
func (c *GraphCache) SetSubgraph(key string, val any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.subgraphCache[key] = cacheEntry{
		val:       val,
		expiresAt: time.Now().Add(10 * time.Minute),
	}
}

// InvalidateEntity invalidates cached items associated with an entity.
func (c *GraphCache) InvalidateEntity(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Invalidate Layer 1
	delete(c.entityCache, id)

	// Invalidate Layer 2 entries that contain this entity as origin or neighbor
	for k, entry := range c.neighborCache {
		if n, ok := entry.val.(*Neighborhood); ok {
			if n.Origin.ID == id {
				delete(c.neighborCache, k)
				continue
			}
			for _, node := range n.Nodes {
				if node.ID == id {
					delete(c.neighborCache, k)
					break
				}
			}
		}
	}

	// Invalidate Layer 3 completely (or clear all to ensure safety)
	c.subgraphCache = make(map[string]cacheEntry)
}

// InvalidateRelationship invalidates neighborhood cache entries that contain the endpoints.
func (c *GraphCache) InvalidateRelationship(fromID, toID string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Invalidate Layer 2 entries involving fromID or toID
	for k, entry := range c.neighborCache {
		if n, ok := entry.val.(*Neighborhood); ok {
			if n.Origin.ID == fromID || n.Origin.ID == toID {
				delete(c.neighborCache, k)
				continue
			}
			for _, node := range n.Nodes {
				if node.ID == fromID || node.ID == toID {
					delete(c.neighborCache, k)
					break
				}
			}
		}
	}

	// Invalidate Layer 3 completely
	c.subgraphCache = make(map[string]cacheEntry)
}

// NeighborhoodKey helper to construct keys.
func NeighborhoodKey(originID string, maxDepth int, dir Direction) string {
	return originID + "|" + string(rune(maxDepth)) + "|" + string(rune(dir))
}
