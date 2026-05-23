package analytics

import (
	"sort"
	"testing"
)

func TestTarjan_DetectsCycle(t *testing.T) {
	adj := map[string][]string{
		"a": {"b"},
		"b": {"c"},
		"c": {"a"}, // 3-cycle a → b → c → a
		"d": {"e"}, // unrelated edge
	}
	sccs := tarjanSCC(adj)
	var cycle []string
	for _, s := range sccs {
		if len(s) >= 2 {
			cycle = s
		}
	}
	if cycle == nil {
		t.Fatalf("expected one cycle of size ≥ 2, got %+v", sccs)
	}
	sort.Strings(cycle)
	want := []string{"a", "b", "c"}
	for i, n := range want {
		if cycle[i] != n {
			t.Errorf("cycle node %d: got %s, want %s", i, cycle[i], n)
		}
	}
}

func TestTarjan_SelfLoop(t *testing.T) {
	adj := map[string][]string{"x": {"x"}}
	sccs := tarjanSCC(adj)
	if len(sccs) != 1 || sccs[0][0] != "x" {
		t.Fatalf("expected single-node SCC for self-loop, got %+v", sccs)
	}
	if !containsSelfLoop(adj, "x") {
		t.Error("containsSelfLoop should detect x → x")
	}
}

func TestTarjan_NoCycle(t *testing.T) {
	adj := map[string][]string{
		"a": {"b", "c"},
		"b": {"c"},
		"c": {"d"},
	}
	sccs := tarjanSCC(adj)
	for _, s := range sccs {
		if len(s) >= 2 {
			t.Errorf("DAG produced multi-node SCC %v", s)
		}
	}
}
