package wiki

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"archgraph/serving/internal/storageclient"
)

// entityHash is a stable fingerprint of a topic's entity set and their
// versions. When it's unchanged between runs the cached page is reused, so a
// regeneration only pays the LLM cost for topics whose graph actually moved.
func entityHash(ids []string, index map[string]*storageclient.Entity) string {
	pairs := make([]string, 0, len(ids))
	for _, id := range ids {
		v := int64(0)
		if e := index[id]; e != nil {
			v = e.Version
		}
		pairs = append(pairs, fmt.Sprintf("%s@%d", id, v))
	}
	sort.Strings(pairs)
	h := sha256.New()
	for _, p := range pairs {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func (g *Generator) pagePath(namespace, topicID string) string {
	if g.cacheDir == "" {
		return ""
	}
	return filepath.Join(g.cacheDir, safe(namespace), safe(topicID)+".json")
}

func (g *Generator) loadPage(namespace, topicID string) (*Page, bool) {
	p := g.pagePath(namespace, topicID)
	if p == "" {
		return nil, false
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, false
	}
	var page Page
	if err := json.Unmarshal(data, &page); err != nil {
		return nil, false
	}
	return &page, true
}

func (g *Generator) savePage(namespace string, page Page) {
	p := g.pagePath(namespace, page.TopicID)
	if p == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return
	}
	data, err := json.MarshalIndent(page, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(p, data, 0o644)
}

// safe sanitizes a path segment so namespaces/topic IDs can't escape cacheDir.
func safe(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			out = append(out, r)
		default:
			out = append(out, '_')
		}
	}
	if len(out) == 0 {
		return "_"
	}
	return string(out)
}
