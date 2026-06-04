package wiki

import (
	"testing"

	"archgraph/serving/internal/storageclient"
)

func ent(id, dir string) *storageclient.Entity {
	return &storageclient.Entity{
		ID:            id,
		Type:          "FUNCTION",
		CanonicalName: id,
		Namespace:     "test",
		Properties:    map[string]any{"dir": dir, "file": dir + "/" + id + ".go"},
	}
}

func rel(from, to string) *storageclient.Relationship {
	return &storageclient.Relationship{ID: from + "->" + to, Type: "CALLS", FromID: from, ToID: to}
}

func TestSegment_GroupsByTopLevelDir(t *testing.T) {
	listing := &storageclient.NamespaceListing{
		Namespace: "test",
		Entities: []*storageclient.Entity{
			ent("a1", "serving/internal/reasoner"),
			ent("a2", "serving/internal/assembler"),
			ent("a3", "serving/cmd"),
			ent("b1", "storage/internal/graphdb"),
			ent("b2", "storage/internal/deltalog"),
			ent("b3", "storage/schema"),
		},
	}
	topics := Segment(listing, SegmentOptions{})
	if len(topics) != 2 {
		t.Fatalf("expected 2 top-level topics (serving, storage), got %d: %+v", len(topics), topics)
	}
	byDir := map[string]Topic{}
	for _, tp := range topics {
		byDir[tp.Dir] = tp
	}
	if _, ok := byDir["serving"]; !ok {
		t.Errorf("expected a 'serving' topic, got %+v", topics)
	}
	if _, ok := byDir["storage"]; !ok {
		t.Errorf("expected a 'storage' topic, got %+v", topics)
	}
	if got := byDir["serving"].Title; got != "Serving" {
		t.Errorf("title humanize: got %q want %q", got, "Serving")
	}
}

func TestSegment_SmallDirFoldsIntoConnectedNeighbor(t *testing.T) {
	// "serving" is large (4); "util" is small (1) and only CALLS into serving.
	// It should be folded into serving, not survive as its own topic.
	listing := &storageclient.NamespaceListing{
		Namespace: "test",
		Entities: []*storageclient.Entity{
			ent("s1", "serving"), ent("s2", "serving"), ent("s3", "serving"), ent("s4", "serving"),
			ent("u1", "util"),
		},
		Relationships: []*storageclient.Relationship{
			rel("u1", "s1"), rel("u1", "s2"),
		},
	}
	topics := Segment(listing, SegmentOptions{MinTopicSize: 3})
	if len(topics) != 1 {
		t.Fatalf("expected util folded into serving (1 topic), got %d: %+v", len(topics), topics)
	}
	if topics[0].Dir != "serving" {
		t.Fatalf("expected surviving topic 'serving', got %q", topics[0].Dir)
	}
	if len(topics[0].EntityIDs) != 5 {
		t.Errorf("expected 5 entities after fold, got %d", len(topics[0].EntityIDs))
	}
}

func TestSegment_SmallDirNoEdgesGoesToMisc(t *testing.T) {
	listing := &storageclient.NamespaceListing{
		Namespace: "test",
		Entities: []*storageclient.Entity{
			ent("s1", "serving"), ent("s2", "serving"), ent("s3", "serving"),
			ent("orphan", "weird"),
		},
	}
	topics := Segment(listing, SegmentOptions{MinTopicSize: 3})
	var misc *Topic
	for i := range topics {
		if topics[i].Dir == miscDir {
			misc = &topics[i]
		}
	}
	if misc == nil {
		t.Fatalf("expected a misc topic for the orphan, got %+v", topics)
	}
	// misc must sort last.
	if topics[len(topics)-1].Dir != miscDir {
		t.Errorf("misc should be ordered last, got %+v", topics)
	}
}

func TestSegment_Deterministic(t *testing.T) {
	mk := func() *storageclient.NamespaceListing {
		return &storageclient.NamespaceListing{
			Namespace: "test",
			Entities: []*storageclient.Entity{
				ent("a", "alpha"), ent("b", "alpha"), ent("c", "alpha"),
				ent("d", "beta"), ent("e", "beta"), ent("f", "beta"),
			},
		}
	}
	t1 := Segment(mk(), SegmentOptions{})
	t2 := Segment(mk(), SegmentOptions{})
	if len(t1) != len(t2) {
		t.Fatalf("non-deterministic length")
	}
	for i := range t1 {
		if t1[i].ID != t2[i].ID || t1[i].Dir != t2[i].Dir {
			t.Fatalf("non-deterministic order at %d: %q vs %q", i, t1[i].Dir, t2[i].Dir)
		}
	}
}
