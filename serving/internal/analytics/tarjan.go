package analytics

// tarjanSCC returns the strongly connected components of a directed graph
// expressed as an adjacency list. Standard iterative-ish implementation of
// Tarjan's algorithm — chosen because it's O(V + E), gives SCCs directly
// (unlike Kosaraju which requires a transpose), and is the algorithm the
// spec names explicitly.
//
// We use recursion bounded by the workspace size (the Audit caller caps
// ListNamespace at 5000 entities so recursion depth is safe).
func tarjanSCC(adj map[string][]string) [][]string {
	// Collect node set: include any id appearing as a source OR sink so we
	// don't drop nodes that only have inbound edges.
	nodes := map[string]struct{}{}
	for from, outs := range adj {
		nodes[from] = struct{}{}
		for _, to := range outs {
			nodes[to] = struct{}{}
		}
	}

	type meta struct {
		index, lowlink int
		onStack        bool
	}
	const unset = -1

	state := make(map[string]*meta, len(nodes))
	for id := range nodes {
		state[id] = &meta{index: unset, lowlink: unset}
	}

	var (
		index int
		stack []string
		sccs  [][]string
	)

	var strongconnect func(v string)
	strongconnect = func(v string) {
		sv := state[v]
		sv.index = index
		sv.lowlink = index
		index++
		stack = append(stack, v)
		sv.onStack = true

		for _, w := range adj[v] {
			sw, ok := state[w]
			if !ok {
				continue
			}
			if sw.index == unset {
				strongconnect(w)
				if sw.lowlink < sv.lowlink {
					sv.lowlink = sw.lowlink
				}
			} else if sw.onStack {
				if sw.index < sv.lowlink {
					sv.lowlink = sw.index
				}
			}
		}

		if sv.lowlink == sv.index {
			var component []string
			for {
				top := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				state[top].onStack = false
				component = append(component, top)
				if top == v {
					break
				}
			}
			sccs = append(sccs, component)
		}
	}

	for id := range nodes {
		if state[id].index == unset {
			strongconnect(id)
		}
	}
	return sccs
}
