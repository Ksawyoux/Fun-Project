// Package wiki turns the live knowledge graph into a Code-Wiki-style set of
// narrative, code-linked pages organized by subsystem.
//
// The hard part is going from a flat graph to *human-meaningful topics*. We use
// a directory + graph hybrid: the repository's directory structure is the
// topic skeleton (developers already think in directories), refined by graph
// edge density — small, weakly-standing directories are folded into whichever
// subsystem actually depends on them. Segmentation here is fully deterministic
// and unit-tested; the LLM only authors prose later, in generate.go.
package wiki

import (
	"sort"
	"strings"

	"archgraph/serving/internal/storageclient"
)

// Topic is one node in the wiki's table of contents. EntityIDs are the graph
// entities that belong to this subsystem; Children is reserved for hierarchical
// splits (currently always empty — flat topics ship first, nesting is a
// follow-up once thresholds are tuned against real repos).
type Topic struct {
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	Summary   string   `json:"summary"`
	Dir       string   `json:"dir"`
	EntityIDs []string `json:"entity_ids"`
	Children  []Topic  `json:"children,omitempty"`
}

// SegmentOptions tunes the merge/split heuristics. Defaults are applied by
// Segment when zero — see defaultSegmentOptions.
type SegmentOptions struct {
	// MinTopicSize: directories with fewer entities than this are folded into
	// the subsystem they are most graph-connected to (or "misc").
	MinTopicSize int
	// MaxTopics caps the table of contents; the smallest overflow topics are
	// merged into "misc".
	MaxTopics int
}

func (o SegmentOptions) withDefaults() SegmentOptions {
	if o.MinTopicSize <= 0 {
		o.MinTopicSize = 3
	}
	if o.MaxTopics <= 0 {
		o.MaxTopics = 25
	}
	return o
}

const miscDir = "misc"

// Segment groups a namespace listing into an ordered slice of Topics.
// Deterministic: same listing in → same topics out (titles, ordering, IDs).
func Segment(listing *storageclient.NamespaceListing, opts SegmentOptions) []Topic {
	opts = opts.withDefaults()
	if listing == nil || len(listing.Entities) == 0 {
		return nil
	}

	// 1. Skeleton: bucket every entity by its top-level source directory.
	entityDir := make(map[string]string, len(listing.Entities))
	buckets := map[string][]string{}
	for _, e := range listing.Entities {
		if e == nil {
			continue
		}
		d := dirKeyOf(e)
		entityDir[e.ID] = d
		buckets[d] = append(buckets[d], e.ID)
	}

	// 2. Graph signal: cross-directory edge counts, used to decide where a
	//    small directory belongs.
	cross := map[string]map[string]int{}
	for _, r := range listing.Relationships {
		if r == nil {
			continue
		}
		a, aok := entityDir[r.FromID]
		b, bok := entityDir[r.ToID]
		if !aok || !bok || a == b {
			continue
		}
		addCross(cross, a, b)
		addCross(cross, b, a)
	}

	// 3. Fold small directories into their most-connected large neighbor.
	isLarge := func(d string) bool { return len(buckets[d]) >= opts.MinTopicSize }
	for _, d := range sortedKeysBySize(buckets) {
		if d == miscDir || isLarge(d) {
			continue
		}
		target := bestLargeNeighbor(cross[d], buckets, opts.MinTopicSize, d)
		if target == "" {
			target = miscDir
		}
		mergeBucket(buckets, entityDir, d, target)
	}

	// 4. Cap topic count: merge smallest overflow into misc.
	if len(buckets) > opts.MaxTopics {
		keep := sortedKeysBySize(buckets) // largest first
		for _, d := range keep[opts.MaxTopics:] {
			if d == miscDir {
				continue
			}
			mergeBucket(buckets, entityDir, d, miscDir)
		}
	}

	// 5. Materialize ordered topics: largest subsystem first, "misc" last.
	topics := make([]Topic, 0, len(buckets))
	for d, ids := range buckets {
		sort.Strings(ids)
		topics = append(topics, Topic{
			ID:        slug(d),
			Title:     humanize(d),
			Dir:       d,
			EntityIDs: ids,
		})
	}
	sort.SliceStable(topics, func(i, j int) bool {
		mi, mj := topics[i].Dir == miscDir, topics[j].Dir == miscDir
		if mi != mj {
			return mj // misc sinks to the bottom
		}
		if len(topics[i].EntityIDs) != len(topics[j].EntityIDs) {
			return len(topics[i].EntityIDs) > len(topics[j].EntityIDs)
		}
		return topics[i].Dir < topics[j].Dir
	})
	return topics
}

func addCross(m map[string]map[string]int, a, b string) {
	if m[a] == nil {
		m[a] = map[string]int{}
	}
	m[a][b]++
}

// bestLargeNeighbor returns the large directory `small` is most connected to.
// Ties broken by directory name for determinism. Returns "" if none qualify.
func bestLargeNeighbor(edges map[string]int, buckets map[string][]string, minSize int, self string) string {
	best, bestN := "", 0
	for d, n := range edges {
		if d == self || d == miscDir || len(buckets[d]) < minSize {
			continue
		}
		if n > bestN || (n == bestN && (best == "" || d < best)) {
			best, bestN = d, n
		}
	}
	return best
}

func mergeBucket(buckets map[string][]string, entityDir map[string]string, from, to string) {
	if from == to {
		return
	}
	buckets[to] = append(buckets[to], buckets[from]...)
	for _, id := range buckets[from] {
		entityDir[id] = to
	}
	delete(buckets, from)
}

// sortedKeysBySize returns bucket keys, smallest bucket first then by name —
// stable iteration order so merges are deterministic.
func sortedKeysBySize(buckets map[string][]string) []string {
	keys := make([]string, 0, len(buckets))
	for k := range buckets {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if len(buckets[keys[i]]) != len(buckets[keys[j]]) {
			return len(buckets[keys[i]]) > len(buckets[keys[j]]) // largest first
		}
		return keys[i] < keys[j]
	})
	return keys
}

// dirKeyOf derives the top-level subsystem directory for an entity from its
// provenance properties (set by the AST/git/openapi ingestors).
func dirKeyOf(e *storageclient.Entity) string {
	if d := strProp(e.Properties, "dir"); d != "" {
		return topSegment(d)
	}
	if f := strProp(e.Properties, "file", "path"); f != "" {
		if dir := pathDir(f); dir != "" {
			return topSegment(dir)
		}
		return "root"
	}
	if pkg := strProp(e.Properties, "package"); pkg != "" {
		return topSegment(pkg)
	}
	// Git-sourced FILE/MODULE entities often carry the path as their canonical
	// name with no extra properties — use it when it looks like a path so they
	// land in their real subsystem instead of "misc".
	if strings.Contains(e.CanonicalName, "/") {
		if dir := pathDir(e.CanonicalName); dir != "" {
			return topSegment(dir)
		}
	}
	return miscDir
}

func strProp(p map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := p[k]; ok {
			if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
				return strings.TrimSpace(s)
			}
		}
	}
	return ""
}

// topSegment returns the first path component of a slash/backslash path.
func topSegment(p string) string {
	p = strings.TrimLeft(strings.ReplaceAll(p, "\\", "/"), "./")
	if i := strings.IndexByte(p, '/'); i >= 0 {
		p = p[:i]
	}
	if p == "" {
		return "root"
	}
	return p
}

// pathDir returns the directory portion of a file path ("" if the file is at
// the root).
func pathDir(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[:i]
	}
	return ""
}

// slug makes a URL/path-safe topic ID from a directory name.
func slug(d string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(d) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_' || r == ' ' || r == '/' || r == '.':
			b.WriteByte('-')
		}
	}
	s := strings.Trim(b.String(), "-")
	if s == "" {
		return miscDir
	}
	return s
}

// humanize turns a directory name into a readable title
// ("ingestion" → "Ingestion", "archgraph-cli" → "Archgraph Cli").
func humanize(d string) string {
	if d == miscDir {
		return "Miscellaneous"
	}
	d = strings.NewReplacer("-", " ", "_", " ", "/", " ").Replace(d)
	fields := strings.Fields(d)
	for i, f := range fields {
		fields[i] = strings.ToUpper(f[:1]) + f[1:]
	}
	if len(fields) == 0 {
		return "Miscellaneous"
	}
	return strings.Join(fields, " ")
}
