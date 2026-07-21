package vdom

import (
	"encoding/json/v2"
	"fmt"
	"reflect"
	"slices"
	"testing"
)

// childKeys lists e's children's keys, "" for an unkeyed child.
func childKeys(e Element) []string {
	out := make([]string, len(e.children))
	for i, c := range e.children {
		out[i] = childKey(c)
	}
	return out
}

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
		return []Node{NewElement("main", attrs(), []Node{
			keyedList("a", "b").WithKey("s1", false),
			keyedList("x", "y").WithKey("s2", false),
		})}
	}
	got, err := Apply(main(), move([]any{0, "s1"}, "a", []any{0, "s2"}, "x"))
	if err != nil {
		t.Fatal(err)
	}
	want := []Node{NewElement("main", attrs(), []Node{
		keyedList("b").WithKey("s1", false),
		keyedList("a", "x", "y").WithKey("s2", false),
	})}
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
	roots := []Node{el("div", keyedList("a", "b"), el("ul"))}
	got, err := Apply(roots, move([]any{0, 0}, "a", []any{0, 1}, ""))
	if err != nil {
		t.Fatal(err)
	}
	want := []Node{el("div", keyedList("b"), keyedList("a"))}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// Root-level children are their own container: a single-step path
// names a child of the domi-root mount, and a move with no container
// steps reorders the root list directly.
func TestApplyMoveAtRoot(t *testing.T) {
	li := func(k string) Node { return el("li", tx(k)).WithKey(k, false) }
	roots := []Node{li("a"), li("b")}
	got, err := Apply(roots, move([]any{}, "a", []any{}, ""))
	if err != nil {
		t.Fatal(err)
	}
	want := []Node{li("b"), li("a")}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// A move can cross between the root list and a nested container; the
// destination path addresses the tree as it stands after the removal,
// the way the client reports it.
func TestApplyMoveFromRootIntoContainer(t *testing.T) {
	roots := []Node{el("li", tx("a")).WithKey("a", false), keyedList("x")}
	got, err := Apply(roots, []ClientMutation{{Op: "move", From: steps("a"), To: steps(0, "a"), Before: "x"}})
	if err != nil {
		t.Fatal(err)
	}
	want := []Node{keyedList("a", "x")}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// A move whose key already names a child in the destination is
// rejected: the client re-keys to dodge collisions before reporting,
// so a colliding move is forged or stale, and accepting it would
// plant duplicate sibling keys in the server's tree.
func TestApplyMoveKeyCollisionErrors(t *testing.T) {
	roots := []Node{el("div", keyedList("a", "b"), keyedList("a", "c"))}
	if _, err := Apply(roots, move([]any{0, 0}, "a", []any{0, 1}, "")); err == nil {
		t.Fatal("expected an error for a key collision in the destination")
	}
}

// A destination that cannot hold element children — a raw-text or
// void element — is rejected rather than corrupting the server's tree
// into something the renderer must refuse.
func TestApplyMoveIntoChildlessElementErrors(t *testing.T) {
	for name, dst := range map[string]Node{
		"raw text": NewElement("script", attrs(), []Node{tx("x")}),
		"void":     el("br"),
	} {
		roots := []Node{el("div", keyedList("a"), dst)}
		if _, err := Apply(roots, move([]any{0, 0}, "a", []any{0, 1}, "")); err == nil {
			t.Fatalf("%s: expected an error for a childless destination", name)
		}
	}
}

// A move into a container with no keyed children makes it mixed: the
// moved child keeps its key among its unkeyed siblings.
func TestApplyMoveIntoUnkeyedContainer(t *testing.T) {
	roots := []Node{el("div", keyedList("a", "b"), el("p", tx("plain")))}
	got, err := Apply(roots, move([]any{0, 0}, "b", []any{0, 1}, ""))
	if err != nil {
		t.Fatal(err)
	}
	if src := nodeAt(t, got, 0, 0); !slices.Equal(childKeys(src), []string{"a"}) {
		t.Fatalf("source keys = %v, want [a]", childKeys(src))
	}
	if dst := nodeAt(t, got, 0, 1); !slices.Equal(childKeys(dst), []string{"", "b"}) {
		t.Fatalf(`destination keys = %v, want ["" b]`, childKeys(dst))
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
	if src := nodeAt(t, got, 0, 0); !slices.Equal(childKeys(src), []string{"b"}) {
		t.Fatalf("source keys = %v, want [b]", childKeys(src))
	}
	if dst := nodeAt(t, got, 0, 1); !slices.Equal(childKeys(dst), []string{"a", "a#dup", "c"}) {
		t.Fatalf("destination keys = %v, want [a a#dup c]", childKeys(dst))
	}
}

// The rewrite is functional: the caller's tree is never mutated, so it can
// still be diffed against as the pre-move tree.
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
		{"key absent from unkeyed source", move([]any{0}, "a", []any{0, 0}, "")},
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
	roots := []Node{NewElement("main", attrs(), []Node{
		keyedList("a", "b").WithKey("s1", false),
		keyedList("x", "y").WithKey("s2", false),
	})}
	got, err := Apply(roots, muts)
	if err != nil {
		t.Fatal(err)
	}
	want := []Node{NewElement("main", attrs(), []Node{
		keyedList("b").WithKey("s1", false),
		keyedList("a", "x", "y").WithKey("s2", false),
	})}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// ---- control-state mutation tests ----

// input builds an <input> carrying the given attrs.
func input(a ...Attr) Element { return NewElement("input", attrs(a...), nil) }

// A setvalue replay records the committed value as the input's value
// attribute — replacing a rendered default or adding the attribute to
// an input that had none — and leaves the caller's tree unchanged.
func TestApplySetValue(t *testing.T) {
	base := func() []Node {
		return []Node{el("div",
			input(at("type", "text"), at("value", "old")),
			input(),
		)}
	}
	roots := base()
	got, err := Apply(roots, []ClientMutation{
		{Op: "setvalue", Path: steps(0, 0), Value: "typed"},
		{Op: "setvalue", Path: steps(0, 1), Value: "fresh"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []Node{el("div",
		input(at("type", "text"), at("value", "typed")),
		input(at("value", "fresh")),
	)}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if !reflect.DeepEqual(roots, base()) {
		t.Fatal("Apply mutated its input")
	}
}

// A settext replay records the committed value as the textarea's text
// content; the empty value leaves no text child — the shape
// canonicalization gives an empty textarea.
func TestApplySetText(t *testing.T) {
	roots := []Node{el("textarea", tx("old"))}
	got, err := Apply(roots, []ClientMutation{{Op: "settext", Path: steps(0), Value: "new"}})
	if err != nil {
		t.Fatal(err)
	}
	if want := []Node{el("textarea", tx("new"))}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	got, err = Apply(got, []ClientMutation{{Op: "settext", Path: steps(0)}})
	if err != nil {
		t.Fatal(err)
	}
	if want := []Node{el("textarea")}; !reflect.DeepEqual(got, want) {
		t.Fatalf("blanked: got %v, want %v", got, want)
	}
}

// A setchecked replay records checkedness as the presence of the
// checked attribute, in both directions.
func TestApplySetChecked(t *testing.T) {
	box := func(extra ...Attr) Element {
		return input(append([]Attr{at("type", "checkbox")}, extra...)...)
	}
	roots := []Node{el("div", box(at("checked", "")), box())}
	got, err := Apply(roots, []ClientMutation{
		{Op: "setchecked", Path: steps(0, 0), Checked: false},
		{Op: "setchecked", Path: steps(0, 1), Checked: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []Node{el("div", box(), box(at("checked", "")))}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// A setselected replay records an option's selectedness as the
// presence of the selected attribute, in both directions — the shape
// of a single-select whose choice moved from a to b.
func TestApplySetSelected(t *testing.T) {
	opt := func(label string, a ...Attr) Element {
		return NewElement("option", attrs(a...), []Node{tx(label)})
	}
	roots := []Node{el("select", opt("a", at("selected", "")), opt("b"))}
	got, err := Apply(roots, []ClientMutation{
		{Op: "setselected", Path: steps(0, 0), Selected: false},
		{Op: "setselected", Path: steps(0, 1), Selected: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []Node{el("select", opt("a"), opt("b", at("selected", "")))}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// Control paths resolve keyed steps the way move paths do, and on the
// wire the committed facts ride the op directly.
func TestApplyControlJSONKeyedPath(t *testing.T) {
	var muts []ClientMutation
	const wire = `[{"Op":"setvalue","Path":[0,"row",0],"Value":"typed"}]`
	if err := json.Unmarshal([]byte(wire), &muts); err != nil {
		t.Fatal(err)
	}
	row := func(v string) Node {
		return NewElement("li", attrs(), []Node{input(at("value", v))}).WithKey("row", false)
	}
	roots := []Node{el("ul", row("old"))}
	got, err := Apply(roots, muts)
	if err != nil {
		t.Fatal(err)
	}
	want := []Node{el("ul", row("typed"))}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// A control op that doesn't fit — the wrong element kind, an input
// mode whose committed state is not a value fact, or a target on or
// inside an app-owned opaque subtree — is reported, never applied
// silently.
func TestApplyControlErrors(t *testing.T) {
	base := []Node{el("main",
		input(),                       // 0
		input(at("type", "file")),     // 1
		input(at("type", "checkbox")), // 2
		el("textarea", tx("x")),       // 3
		el("p", tx("plain")),          // 4
		NewElement("div", attrs(), []Node{input()}).WithKey("w", true), // 5: opaque widget
		input().WithKey("oi", true),                                    // 6: opaque input
	)}
	tests := []struct {
		name string
		muts []ClientMutation
	}{
		{"setvalue on a non-input", []ClientMutation{{Op: "setvalue", Path: steps(0, 4)}}},
		{"setvalue on a file input", []ClientMutation{{Op: "setvalue", Path: steps(0, 1)}}},
		{"setvalue on a checkbox", []ClientMutation{{Op: "setvalue", Path: steps(0, 2)}}},
		{"settext on a non-textarea", []ClientMutation{{Op: "settext", Path: steps(0, 0)}}},
		{"setchecked on a text input", []ClientMutation{{Op: "setchecked", Path: steps(0, 0), Checked: true}}},
		{"setchecked on a non-input", []ClientMutation{{Op: "setchecked", Path: steps(0, 3), Checked: true}}},
		{"setselected on a non-option", []ClientMutation{{Op: "setselected", Path: steps(0, 0), Selected: true}}},
		{"target inside an opaque subtree", []ClientMutation{{Op: "setvalue", Path: steps(0, "w", 0)}}},
		{"target itself opaque", []ClientMutation{{Op: "setvalue", Path: steps(0, "oi")}}},
		{"path out of range", []ClientMutation{{Op: "setvalue", Path: steps(0, 9)}}},
		{"empty path", []ClientMutation{{Op: "setvalue"}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Apply(base, tc.muts); err == nil {
				t.Fatal("expected an error, got nil")
			}
		})
	}
}

// Input types match the way a browser matches attribute keywords:
// ASCII case-insensitively, and only ASCII. CHECKBOX is a checkbox;
// checKbox (the Kelvin sign, which Unicode folding would turn
// into checkbox) is no keyword at all, so the browser falls back to a
// text-mode input and the vet must agree with what the client saw.
func TestApplyInputTypeASCIIFolding(t *testing.T) {
	base := []Node{el("div",
		input(at("type", "CHECKBOX")),
		input(at("type", "chec\u212abox")),
	)}
	if _, err := Apply(base, []ClientMutation{{Op: "setchecked", Path: steps(0, 0), Checked: true}}); err != nil {
		t.Fatalf("setchecked on an ASCII-uppercase checkbox: %v", err)
	}
	if _, err := Apply(base, []ClientMutation{{Op: "setvalue", Path: steps(0, 0)}}); err == nil {
		t.Fatal("setvalue on an ASCII-uppercase checkbox should be rejected")
	}
	if _, err := Apply(base, []ClientMutation{{Op: "setvalue", Path: steps(0, 1)}}); err != nil {
		t.Fatalf("setvalue on a Kelvin-sign type, text mode to a browser: %v", err)
	}
	if _, err := Apply(base, []ClientMutation{{Op: "setchecked", Path: steps(0, 1), Checked: true}}); err == nil {
		t.Fatal("setchecked on a Kelvin-sign type should be rejected; the browser sees text mode")
	}
}
