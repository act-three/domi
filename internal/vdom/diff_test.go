package vdom

import (
	"slices"
	"testing"
)

// el builds an element with no attrs from a tag and child list.
func el(tag string, children ...Node) Element {
	return NewElement(tag, nil, children, nil)
}

// tx builds a text node.
func tx(s string) Text { return Text(s) }

// at builds an attribute literal.
func at(name, value string) Attr { return Attr{Name: name, Value: value} }

// keyedList builds a keyed <ul> whose children are <li>s named for each key.
// Each <li> carries a data-domi-key attribute matching its key — what the
// domi-side Keyed constructor injects at construction time.
func keyedList(keys ...string) Element {
	children := make([]Node, len(keys))
	for i, k := range keys {
		children[i] = NewElement("li",
			[]Attr{{Name: "data-domi-key", Value: k}},
			[]Node{Text(k)},
			nil)
	}
	return NewElement("ul", nil, children, slices.Clone(keys))
}

// diffOne returns the unwrapped patch slice for two single-root trees,
// so tests can read patch fields directly without going through the
// [Patch] wrapper that public [Diff] returns.
func diffOne(old, new Node) []patch {
	return diffNode(old, new, nil, nil)
}

// countOps returns counts of structural patch ops in `patches`.
func countOps(patches []patch) (inserts, removes, moves int) {
	for _, p := range patches {
		switch p.Op {
		case "insert_child":
			inserts++
		case "remove_child":
			removes++
		case "move_child":
			moves++
		}
	}
	return
}

func TestNoChange(t *testing.T) {
	a := el("div", tx("hi"))
	if got := diffOne(a, a); len(got) != 0 {
		t.Fatalf("want no patches, got %+v", got)
	}
}

func TestTextChange(t *testing.T) {
	a := el("div", tx("hi"))
	b := el("div", tx("bye"))
	got := diffOne(a, b)
	if len(got) != 1 || got[0].Op != "set_text" || got[0].Value != "bye" {
		t.Fatalf("unexpected: %+v", got)
	}
}

func TestTagChangeReplaces(t *testing.T) {
	a := el("div")
	b := el("span")
	got := diffOne(a, b)
	if len(got) != 1 || got[0].Op != "replace" {
		t.Fatalf("unexpected: %+v", got)
	}
}

func TestKeyedReorder(t *testing.T) {
	a := keyedList("a", "b", "c")
	b := keyedList("c", "a", "b")
	got := diffOne(a, b)
	for _, p := range got {
		if p.Op == "move_child" {
			return
		}
	}
	t.Fatalf("expected at least one move_child, got %+v", got)
}

func TestKeyedInsertMiddle(t *testing.T) {
	a := keyedList("a", "c")
	b := keyedList("a", "b", "c")
	got := diffOne(a, b)
	for _, p := range got {
		if p.Op == "insert_child" && p.Key == "b" && p.Before == "c" {
			return
		}
	}
	t.Fatalf("expected insert_child key=b before=c, got %+v", got)
}

func TestKeyedRemove(t *testing.T) {
	a := keyedList("a", "b", "c")
	b := keyedList("a", "c")
	got := diffOne(a, b)
	for _, p := range got {
		if p.Op == "remove_child" && p.Key == "b" {
			return
		}
	}
	t.Fatalf("expected remove_child key=b, got %+v", got)
}

func TestAttrAdded(t *testing.T) {
	a := el("div")
	b := NewElement("div", []Attr{at("class", "x")}, nil, nil)
	got := diffOne(a, b)
	if len(got) != 1 || got[0].Op != "set_attr" || got[0].Name != "class" || got[0].Value != "x" {
		t.Fatalf("expected single set_attr, got %+v", got)
	}
}

func TestAttrChanged(t *testing.T) {
	a := NewElement("div", []Attr{at("class", "x")}, nil, nil)
	b := NewElement("div", []Attr{at("class", "y")}, nil, nil)
	got := diffOne(a, b)
	if len(got) != 1 || got[0].Op != "set_attr" || got[0].Value != "y" {
		t.Fatalf("expected set_attr to y, got %+v", got)
	}
}

func TestAttrRemoved(t *testing.T) {
	a := NewElement("div", []Attr{at("class", "x")}, nil, nil)
	b := el("div")
	got := diffOne(a, b)
	if len(got) != 1 || got[0].Op != "remove_attr" || got[0].Name != "class" {
		t.Fatalf("expected remove_attr, got %+v", got)
	}
}

func TestReplacePatchCarriesHTML(t *testing.T) {
	a := el("div")
	b := el("span", tx("hi"))
	got := diffOne(a, b)
	if len(got) != 1 || got[0].Op != "replace" {
		t.Fatalf("expected single replace, got %+v", got)
	}
	if got[0].HTML != "<span>hi</span>" {
		t.Fatalf("expected rendered HTML, got %q", got[0].HTML)
	}
}

func TestInsertChildPatchCarriesHTML(t *testing.T) {
	a := el("ul")
	b := el("ul", el("li", tx("one")))
	got := diffOne(a, b)
	if len(got) != 1 || got[0].Op != "insert_child" {
		t.Fatalf("expected single insert_child, got %+v", got)
	}
	if got[0].HTML != "<li>one</li>" {
		t.Fatalf("expected rendered HTML, got %q", got[0].HTML)
	}
}

func TestLISBasic(t *testing.T) {
	// LIS of [3, 1, 4, 1, 5, 9, 2, 6, 5, 3, 5] is [1, 4, 5, 9] (positions 1,2,4,5)
	// or equivalent; check length only since multiple valid answers exist.
	got := longestIncreasingSubseq([]int{3, 1, 4, 1, 5, 9, 2, 6, 5, 3, 5})
	if len(got) != 4 {
		t.Fatalf("want LIS length 4, got %d: %v", len(got), got)
	}
	// Verify it's actually increasing.
	arr := []int{3, 1, 4, 1, 5, 9, 2, 6, 5, 3, 5}
	for i := 1; i < len(got); i++ {
		if arr[got[i]] <= arr[got[i-1]] {
			t.Fatalf("not strictly increasing: %v -> values %v", got, arr)
		}
	}
}

func TestLISSkipsZeros(t *testing.T) {
	got := longestIncreasingSubseq([]int{0, 2, 0, 3, 0, 1})
	// Zeros are skipped. Values 2, 3, 1 → LIS = [2, 3].
	if len(got) != 2 {
		t.Fatalf("want length 2, got %d: %v", len(got), got)
	}
}

func TestLISEmpty(t *testing.T) {
	if got := longestIncreasingSubseq(nil); got != nil {
		t.Fatalf("want nil, got %v", got)
	}
	if got := longestIncreasingSubseq([]int{0, 0, 0}); got != nil {
		t.Fatalf("want nil (all zeros), got %v", got)
	}
}

// ---- LIS-driven keyed-diff cases ----

// Append: head trim consumes all of old, leaving inserts at the end.
func TestKeyedAppendOnlyInserts(t *testing.T) {
	a := keyedList("a", "b", "c")
	b := keyedList("a", "b", "c", "d", "e")
	got := diffOne(a, b)
	ins, rm, mv := countOps(got)
	if ins != 2 || rm != 0 || mv != 0 {
		t.Fatalf("want 2 inserts, 0 removes, 0 moves; got ins=%d rm=%d mv=%d  patches=%+v", ins, rm, mv, got)
	}
}

// Prepend: tail trim consumes all of old, leaving inserts at the start.
func TestKeyedPrependOnlyInserts(t *testing.T) {
	a := keyedList("c", "d", "e")
	b := keyedList("a", "b", "c", "d", "e")
	got := diffOne(a, b)
	ins, rm, mv := countOps(got)
	if ins != 2 || rm != 0 || mv != 0 {
		t.Fatalf("want 2 inserts, 0 removes, 0 moves; got ins=%d rm=%d mv=%d  patches=%+v", ins, rm, mv, got)
	}
}

// Single-middle insert: head & tail both trim, leaving a single insert.
func TestKeyedMiddleInsertOnePatch(t *testing.T) {
	a := keyedList("a", "b", "d")
	b := keyedList("a", "b", "c", "d")
	got := diffOne(a, b)
	ins, rm, mv := countOps(got)
	if ins != 1 || rm != 0 || mv != 0 {
		t.Fatalf("want 1 insert only; got ins=%d rm=%d mv=%d  patches=%+v", ins, rm, mv, got)
	}
}

// Single-middle delete: head & tail both trim, leaving a single remove.
func TestKeyedMiddleDeleteOnePatch(t *testing.T) {
	a := keyedList("a", "b", "c", "d")
	b := keyedList("a", "b", "d")
	got := diffOne(a, b)
	ins, rm, mv := countOps(got)
	if ins != 0 || rm != 1 || mv != 0 {
		t.Fatalf("want 1 remove only; got ins=%d rm=%d mv=%d  patches=%+v", ins, rm, mv, got)
	}
}

// Swap two adjacent middle elements: LIS pins one, moves the other.
func TestKeyedAdjacentSwapOneMove(t *testing.T) {
	a := keyedList("a", "b", "c", "d")
	b := keyedList("a", "c", "b", "d")
	got := diffOne(a, b)
	ins, rm, mv := countOps(got)
	if ins != 0 || rm != 0 || mv != 1 {
		t.Fatalf("want exactly 1 move; got ins=%d rm=%d mv=%d  patches=%+v", ins, rm, mv, got)
	}
}

// Full reverse: Snabbdom rule 3 fires repeatedly, emitting n-1 keyed
// moves and never reaching the LIS branch.
func TestKeyedReverseMinimalMoves(t *testing.T) {
	a := keyedList("a", "b", "c", "d", "e")
	b := keyedList("e", "d", "c", "b", "a")
	got := diffOne(a, b)
	ins, rm, mv := countOps(got)
	if ins != 0 || rm != 0 || mv != 4 {
		t.Fatalf("want 4 moves; got ins=%d rm=%d mv=%d  patches=%+v", ins, rm, mv, got)
	}
	// All emitted moves should be keyed (no positional from/to fields used).
	for _, p := range got {
		if p.Op == "move_child" && p.From != nil {
			t.Fatalf("expected keyed move, got positional: %+v", p)
		}
	}
}

// Rule 4 specifically: move from the tail to the head.
func TestKeyedRule4TailToHead(t *testing.T) {
	a := keyedList("a", "b", "c", "d")
	b := keyedList("d", "a", "b", "c")
	got := diffOne(a, b)
	ins, rm, mv := countOps(got)
	if ins != 0 || rm != 0 || mv != 1 {
		t.Fatalf("want exactly 1 move; got ins=%d rm=%d mv=%d  patches=%+v", ins, rm, mv, got)
	}
	for _, p := range got {
		if p.Op == "move_child" {
			if p.Key != "d" || p.Before != "a" {
				t.Fatalf("want move_child key=d before=a, got %+v", p)
			}
		}
	}
}

// Identical: no patches at all.
func TestKeyedIdenticalNoOps(t *testing.T) {
	a := keyedList("a", "b", "c", "d")
	got := diffOne(a, a)
	if len(got) != 0 {
		t.Fatalf("want no patches, got %+v", got)
	}
}

// Mixed: remove one, insert one, move one — exercises pass-B remove,
// right-to-left insert, and LIS move all together.
func TestKeyedMixedRemoveInsertMove(t *testing.T) {
	a := keyedList("a", "b", "c", "d", "e") // remove c, insert x, swap d/b
	b := keyedList("a", "d", "x", "b", "e")
	got := diffOne(a, b)
	ins, rm, _ := countOps(got)
	if ins != 1 || rm != 1 {
		t.Fatalf("want 1 insert and 1 remove; got %+v", got)
	}
}

// Diff runs combinedAttrs on both sides, so duplicate-attr combining
// is part of what the diff sees: two `class` attrs collapse to one
// canonical "a b" value.
func TestAttrCombiningBeforeDiff(t *testing.T) {
	a := NewElement("div", []Attr{at("class", "a")}, nil, nil)
	b := NewElement("div", []Attr{at("class", "a"), at("class", "b")}, nil, nil)
	got := diffOne(a, b)
	if len(got) != 1 || got[0].Op != "set_attr" || got[0].Value != "a b" {
		t.Fatalf("expected class to combine to \"a b\", got %+v", got)
	}
}

// ---- regression cases pulled from property-test failures ----
//
// Each minimizes a failing seed from TestDiffApplyProperty into a
// hand-rolled fixture asserting on patch shape. Property tests sample
// a distribution; these pin the specific bug down so generator tweaks
// can never silently lose coverage.

// Regression: the keyed only-inserts branch must emit patches right-to-
// left so each insert's `before` anchor is in place when the patch is
// applied. Forward emission referenced a sibling that the next iteration
// would insert, which on the client resolved to "anchor not in childMap"
// and fell back to appending — producing the wrong child order.
//
// Setup: old = [a]; next = [b, c, a]. Snabbdom rule 2 matches `a` at
// the tail (deferred). The remaining region is only-inserts for b, c.
// Correct emission: insert c before a, then insert b before c.
func TestKeyedOnlyInsertsAnchorOrder(t *testing.T) {
	old := keyedList("a")
	next := keyedList("b", "c", "a")
	got := diffOne(old, next)
	var inserts []patch
	for _, p := range got {
		if p.Op == "insert_child" {
			inserts = append(inserts, p)
		}
	}
	if len(inserts) != 2 {
		t.Fatalf("expected 2 inserts, got %d: %+v", len(inserts), got)
	}
	if inserts[0].Key != "c" || inserts[0].Before != "a" {
		t.Fatalf("expected first insert key=c before=a, got %+v", inserts[0])
	}
	if inserts[1].Key != "b" || inserts[1].Before != "c" {
		t.Fatalf("expected second insert key=b before=c, got %+v", inserts[1])
	}
}

// Regression: adjacent text children on the Go side must be coalesced
// before the positional diff runs, because the HTML parser merges them
// into a single DOM Text node. Without coalescing, positional indices
// computed against the Go count walk off the end of the DOM's childNodes
// (e.g. `remove_child idx=2` when the DOM only has 2 childNodes).
//
// Setup: old = div containing [text "a", text "b", span] — 3 Go
// children, but 2 DOM childNodes after parsing (text "ab" + span).
// next = div containing just [span]. Coalesce should normalize old to
// 2 children, yielding remove_child idx=1 + replace at [0].
func TestAdjacentTextCoalescesBeforePositionalDiff(t *testing.T) {
	old := el("div", tx("a"), tx("b"), el("span"))
	next := el("div", el("span"))
	got := diffOne(old, next)
	if len(got) != 2 {
		t.Fatalf("expected 2 patches (one remove + one replace), got %d: %+v", len(got), got)
	}
	if got[0].Op != "remove_child" || got[0].Idx == nil || *got[0].Idx != 1 {
		t.Fatalf("expected first patch remove_child idx=1, got %+v", got[0])
	}
	if got[1].Op != "replace" || len(got[1].Path) != 1 || got[1].Path[0] != 0 {
		t.Fatalf("expected second patch replace at [0], got %+v", got[1])
	}
}

// Regression: diffPositional's `append(path, i)` reused the underlying
// array across sibling iterations when path had spare capacity, so a
// patch emitted on an early sibling had its tail index silently
// overwritten by a later sibling. Manifested in the wild as a span's
// style patch landing on the last button — same path prefix, last
// index clobbered to point at the last sibling.
//
// The construction below picks four nesting levels and four siblings:
// at depth 3 the path slice grows from cap 2 to cap 4 (Go append
// doubling), so depth-4 iterations write into the spare slot 3,
// overwriting whatever the first sibling's stored patch put there.
func TestDiffPathNotAliasedAcrossSiblings(t *testing.T) {
	plain := func(children ...Node) Node {
		return NewElement("div", nil, children, nil)
	}
	leaf := func(class string) Node {
		return NewElement("span", []Attr{{Name: "class", Value: class}}, nil, nil)
	}
	old := plain(plain(plain(plain(leaf("old"), leaf("a"), leaf("b"), leaf("c")))))
	next := plain(plain(plain(plain(leaf("new"), leaf("a"), leaf("b"), leaf("c")))))
	got := diffOne(old, next)
	if len(got) != 1 {
		t.Fatalf("expected 1 set_attr patch on first leaf, got %d: %+v", len(got), got)
	}
	if got[0].Op != "set_attr" {
		t.Fatalf("expected set_attr, got %q", got[0].Op)
	}
	if !slices.Equal(got[0].Path, []int{0, 0, 0, 0}) {
		t.Fatalf("set_attr should target first leaf at [0,0,0,0], got %v", got[0].Path)
	}
}
