package vdom

import (
	"encoding/json/v2"
	"fmt"
	"reflect"
	"slices"
	"testing"
)

// nodeAt descends nodes by child index, returning the element at path.
func nodeAt(t *testing.T, nodes []Node, path ...int) Element {
	t.Helper()
	var e Element
	for _, i := range path {
		got, ok := nodes[i].(Element)
		if !ok {
			t.Fatalf("path %v hits a non-element at index %d", path, i)
		}
		e = got
		nodes = e.children
	}
	return e
}

// steps builds a step path from ints (positional) and strings (keyed).
func steps(path ...any) []Step {
	out := make([]Step, len(path))
	for i, s := range path {
		switch s := s.(type) {
		case int:
			out[i] = Index(s)
		case string:
			out[i] = Key(s)
		default:
			panic(fmt.Sprintf("steps: %v is not an int or string", s))
		}
	}
	return out
}

// move is a one-element move mutation set: the child named key travels from
// the from container to the to container, before the anchor (or to the end
// when before is empty), keeping its key.
func move(from []any, key string, to []any, before string) []ClientMutation {
	return []ClientMutation{{Op: "move", From: childPath(from, key), To: childPath(to, key), Before: before}}
}

// childPath builds a move path: the container steps followed by the child's key.
func childPath(container []any, key string) []Step {
	return steps(append(append([]any{}, container...), key)...)
}

// A reorder within one keyed container relocates the child and leaves the
// rest in order.
func TestApplyReorderWithinContainer(t *testing.T) {
	roots := []Node{keyedList("a", "b", "c")}
	got, err := Apply(roots, move([]any{0}, "c", []any{0}, "a"))
	if err != nil {
		t.Fatal(err)
	}
	want := []Node{keyedList("c", "a", "b")}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// An empty anchor appends to the destination — here, sending a head to the
// tail of its own container.
func TestApplyMoveToEnd(t *testing.T) {
	roots := []Node{keyedList("a", "b", "c")}
	got, err := Apply(roots, move([]any{0}, "a", []any{0}, ""))
	if err != nil {
		t.Fatal(err)
	}
	want := []Node{keyedList("b", "c", "a")}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// A move between two keyed containers under a common ancestor — the
// episode-between-seasons case — removes from the source and inserts at
// the destination, and the moved element keeps its identity (it is the
// same subtree, not a fresh render).
func TestApplyMoveAcrossContainers(t *testing.T) {
	roots := []Node{el("div", keyedList("a", "b", "c"), keyedList("x", "y"))}
	got, err := Apply(roots, move([]any{0, 0}, "b", []any{0, 1}, "y"))
	if err != nil {
		t.Fatal(err)
	}
	want := []Node{el("div", keyedList("a", "c"), keyedList("x", "b", "y"))}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// A container nested under a keyed ancestor is addressed by that ancestor's
// key, not its position — so the move lands on the right subtree however its
// siblings are ordered. Here two keyed sections each hold a keyed list, and
// an item moves between them addressed as [0, "s1"] → [0, "s2"].
func TestApplyMoveThroughKeyedAncestor(t *testing.T) {
	main := func() []Node {
		return []Node{NewElement("main", attrs(),
			[]Node{keyedList("a", "b"), keyedList("x", "y")},
			[]string{"s1", "s2"})}
	}
	got, err := Apply(main(), move([]any{0, "s1"}, "a", []any{0, "s2"}, "x"))
	if err != nil {
		t.Fatal(err)
	}
	want := []Node{NewElement("main", attrs(),
		[]Node{keyedList("b"), keyedList("a", "x", "y")},
		[]string{"s1", "s2"})}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}

	// A keyed step that names no child fails rather than silently landing
	// on the wrong sibling.
	if _, err := Apply(main(), move([]any{0, "s9"}, "a", []any{0, "s2"}, "x")); err == nil {
		t.Fatal("expected an error for an absent ancestor key")
	}
}

// Appending into an empty destination container, where no anchor can name
// the spot.
func TestApplyMoveIntoEmptyContainer(t *testing.T) {
	emptyKeyed := NewElement("ul", attrs(), nil, []string{})
	roots := []Node{el("div", keyedList("a", "b"), emptyKeyed)}
	got, err := Apply(roots, move([]any{0, 0}, "a", []any{0, 1}, ""))
	if err != nil {
		t.Fatal(err)
	}
	want := []Node{el("div", keyedList("b"), keyedList("a"))}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// A batch applies its mutations in order, each onto the result of the last.
func TestApplyBatch(t *testing.T) {
	roots := []Node{keyedList("a", "b", "c")}
	got, err := Apply(roots, []ClientMutation{
		{Op: "move", From: steps(0, "c"), To: steps(0, "c"), Before: "a"}, // c a b
		{Op: "move", From: steps(0, "b"), To: steps(0, "b"), Before: "c"}, // b c a
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []Node{keyedList("b", "c", "a")}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// When the moved key already names a child in the destination, the client
// re-keys its incoming node; the replay records that key so the next diff
// removes it (the server's render owns the real key). The source loses the
// original key, the destination gains the new one in anchor position.
func TestApplyMoveRekeys(t *testing.T) {
	roots := []Node{el("div", keyedList("a", "b"), keyedList("a", "c"))}
	got, err := Apply(roots, []ClientMutation{{Op: "move", From: steps(0, 0, "a"), To: steps(0, 1, "a#dup"), Before: "c"}})
	if err != nil {
		t.Fatal(err)
	}
	if src := nodeAt(t, got, 0, 0); !slices.Equal(src.keys, []string{"b"}) {
		t.Fatalf("source keys = %v, want [b]", src.keys)
	}
	if dst := nodeAt(t, got, 0, 1); !slices.Equal(dst.keys, []string{"a", "a#dup", "c"}) {
		t.Fatalf("destination keys = %v, want [a a#dup c]", dst.keys)
	}
}

// The rewrite is functional: the caller's tree is never mutated, so it can
// still be diffed against as the pre-move shadow.
func TestApplyLeavesInputUnchanged(t *testing.T) {
	roots := []Node{el("div", keyedList("a", "b", "c"), keyedList("x", "y"))}
	before := []Node{el("div", keyedList("a", "b", "c"), keyedList("x", "y"))}
	if _, err := Apply(roots, move([]any{0, 0}, "a", []any{0, 1}, "x")); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(roots, before) {
		t.Fatal("Apply mutated its input")
	}
}

// A mutation that doesn't fit the tree is reported, never applied silently —
// the caller resyncs instead of trusting a bad reconstruction.
func TestApplyErrors(t *testing.T) {
	// div > ul(keyed a,b) , p("plain")
	base := func() []Node {
		return []Node{el("div", keyedList("a", "b"), el("p", tx("plain")))}
	}
	tests := []struct {
		name string
		muts []ClientMutation
	}{
		{"key absent from source", move([]any{0, 0}, "z", []any{0, 0}, "a")},
		{"source not keyed", move([]any{0}, "a", []any{0, 0}, "")},
		{"destination not keyed", move([]any{0, 0}, "a", []any{0}, "")},
		{"anchor absent from destination", move([]any{0, 0}, "a", []any{0, 0}, "z")},
		{"path index out of range", move([]any{0, 9}, "a", []any{0, 0}, "")},
		{"path descends into text", move([]any{0, 1, 0}, "a", []any{0, 0}, "")},
		{"empty path", []ClientMutation{{Op: "move", From: nil, To: steps(0, "a")}}},
		{"path does not end in a key", []ClientMutation{{Op: "move", From: steps(0, 0), To: steps(0, "a")}}},
		{"unknown op", []ClientMutation{{Op: "teleport"}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Apply(base(), tc.muts); err == nil {
				t.Fatal("expected an error, got nil")
			}
		})
	}
}

// On the wire a path's steps arrive as JSON strings (keys) and numbers
// (indices); UnmarshalJSONFrom decodes each, and Apply resolves them.
func TestApplyJSONPath(t *testing.T) {
	var muts []ClientMutation
	const wire = `[{"Op":"move","From":[0,"s1","a"],"To":[0,"s2","a"],"Before":"x"}]`
	if err := json.Unmarshal([]byte(wire), &muts); err != nil {
		t.Fatal(err)
	}
	roots := []Node{NewElement("main", attrs(),
		[]Node{keyedList("a", "b"), keyedList("x", "y")}, []string{"s1", "s2"})}
	got, err := Apply(roots, muts)
	if err != nil {
		t.Fatal(err)
	}
	want := []Node{NewElement("main", attrs(),
		[]Node{keyedList("b"), keyedList("a", "x", "y")}, []string{"s1", "s2"})}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}
