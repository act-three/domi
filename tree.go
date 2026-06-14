package domi

import "ily.dev/domi/internal/vdom"

// A tree captures the client-visible state at a tree version: its
// view and title, and the path sets delivered to the client as of that
// version. Restoring one (see restoreSnapshot) re-roots the lineage and
// resets path-set delivery to the captured set, so the next render
// re-sends any path set that reached the client only in a frame later
// dropped at the rebase.
type tree struct {
	view     []vdom.Node
	title    string
	pathSets map[string]pathSet
}

// A treeRing is a fixed-capacity, recency-evicting store of trees
// keyed by tree version.
// Putting a version a second time refreshes its recency
// rather than consuming a second slot.
// The zero value is unusable; use newTreeRing.
type treeRing struct {
	cap  int
	m    map[string]tree
	age  []string // ring of versions in insertion order, for eviction
	next int      // next write position in age
}

func newTreeRing(cap int) treeRing {
	return treeRing{
		cap: cap,
		m:   map[string]tree{},
		age: make([]string, cap),
	}
}

// put stores t under ver, evicting the oldest entry when at capacity.
func (tr *treeRing) put(ver string, t tree) {
	if _, ok := tr.m[ver]; ok {
		// Clear the old ring slot — a later write recycles the hole — and
		// fall through to re-append at the young end.
		for i, old := range tr.age {
			if old == ver {
				tr.age[i] = ""
				break
			}
		}
	}
	if old := tr.age[tr.next]; old != "" {
		delete(tr.m, old)
	}
	tr.age[tr.next] = ver
	tr.next = (tr.next + 1) % tr.cap
	tr.m[ver] = t
}

func (tr *treeRing) get(ver string) (tree, bool) {
	t, ok := tr.m[ver]
	return t, ok
}

func (tr *treeRing) len() int { return len(tr.m) }
