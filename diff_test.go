package domi

import "testing"

// keyedText constructs a keyed text node for tests.
func keyedText(k, s string) Node { return Text(s).WithKey(k) }

// keyedItem constructs a keyed <li> wrapping the given text.
func keyedItem(k string) Node {
	return Tag("li")()(Text(k)).WithKey(k)
}

// keyedList builds a <ul> whose children are keyed <li> elements (one per key).
// Keyed children must be elements so they can carry data-domi-key in the
// rendered HTML (text nodes can't).
func keyedList(keys ...string) Node {
	kids := make([]Node, len(keys))
	for i, k := range keys {
		kids[i] = keyedItem(k)
	}
	return Tag("ul")()(kids...)
}

// countOps returns counts of structural patch ops in `patches`.
func countOps(patches []patch) (inserts, removes, moves int) {
	for _, p := range patches {
		switch p.op {
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
	a := Tag("div")()(Text("hi"))
	if got := diff(a, a); len(got) != 0 {
		t.Fatalf("want no patches, got %+v", got)
	}
}

func TestTextChange(t *testing.T) {
	a := Tag("div")()(Text("hi"))
	b := Tag("div")()(Text("bye"))
	got := diff(a, b)
	if len(got) != 1 || got[0].op != "set_text" || got[0].value != "bye" {
		t.Fatalf("unexpected: %+v", got)
	}
}

func TestTagChangeReplaces(t *testing.T) {
	a := Tag("div")()()
	b := Tag("span")()()
	got := diff(a, b)
	if len(got) != 1 || got[0].op != "replace" {
		t.Fatalf("unexpected: %+v", got)
	}
}

func TestKeyedReorder(t *testing.T) {
	a := keyedList("a", "b", "c")
	b := keyedList("c", "a", "b")
	got := diff(a, b)
	for _, p := range got {
		if p.op == "move_child" {
			return
		}
	}
	t.Fatalf("expected at least one move_child, got %+v", got)
}

func TestKeyedInsertMiddle(t *testing.T) {
	a := keyedList("a", "c")
	b := keyedList("a", "b", "c")
	got := diff(a, b)
	for _, p := range got {
		if p.op == "insert_child" && p.key == "b" && p.before == "c" {
			return
		}
	}
	t.Fatalf("expected insert_child key=b before=c, got %+v", got)
}

func TestKeyedRemove(t *testing.T) {
	a := keyedList("a", "b", "c")
	b := keyedList("a", "c")
	got := diff(a, b)
	for _, p := range got {
		if p.op == "remove_child" && p.key == "b" {
			return
		}
	}
	t.Fatalf("expected remove_child key=b, got %+v", got)
}

func TestAttrAdded(t *testing.T) {
	a := Tag("div")()()
	b := Tag("div")(Attribute("class", "x"))()
	got := diff(a, b)
	if len(got) != 1 || got[0].op != "set_attr" || got[0].name != "class" || got[0].value != "x" {
		t.Fatalf("expected single set_attr, got %+v", got)
	}
}

func TestAttrChanged(t *testing.T) {
	a := Tag("div")(Attribute("class", "x"))()
	b := Tag("div")(Attribute("class", "y"))()
	got := diff(a, b)
	if len(got) != 1 || got[0].op != "set_attr" || got[0].value != "y" {
		t.Fatalf("expected set_attr to y, got %+v", got)
	}
}

func TestAttrRemoved(t *testing.T) {
	a := Tag("div")(Attribute("class", "x"))()
	b := Tag("div")()()
	got := diff(a, b)
	if len(got) != 1 || got[0].op != "remove_attr" || got[0].name != "class" {
		t.Fatalf("expected remove_attr, got %+v", got)
	}
}

func TestReplacePatchCarriesHTML(t *testing.T) {
	a := Tag("div")()()
	b := Tag("span")()(Text("hi"))
	got := diff(a, b)
	if len(got) != 1 || got[0].op != "replace" {
		t.Fatalf("expected single replace, got %+v", got)
	}
	if got[0].html != "<span>hi</span>" {
		t.Fatalf("expected rendered HTML, got %q", got[0].html)
	}
}

func TestInsertChildPatchCarriesHTML(t *testing.T) {
	a := Tag("ul")()()
	b := Tag("ul")()(Tag("li")()(Text("one")))
	got := diff(a, b)
	if len(got) != 1 || got[0].op != "insert_child" {
		t.Fatalf("expected single insert_child, got %+v", got)
	}
	if got[0].html != "<li>one</li>" {
		t.Fatalf("expected rendered HTML, got %q", got[0].html)
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
	got := diff(a, b)
	ins, rm, mv := countOps(got)
	if ins != 2 || rm != 0 || mv != 0 {
		t.Fatalf("want 2 inserts, 0 removes, 0 moves; got ins=%d rm=%d mv=%d  patches=%+v", ins, rm, mv, got)
	}
}

// Prepend: tail trim consumes all of old, leaving inserts at the start.
func TestKeyedPrependOnlyInserts(t *testing.T) {
	a := keyedList("c", "d", "e")
	b := keyedList("a", "b", "c", "d", "e")
	got := diff(a, b)
	ins, rm, mv := countOps(got)
	if ins != 2 || rm != 0 || mv != 0 {
		t.Fatalf("want 2 inserts, 0 removes, 0 moves; got ins=%d rm=%d mv=%d  patches=%+v", ins, rm, mv, got)
	}
}

// Single-middle insert: head & tail both trim, leaving a single insert.
func TestKeyedMiddleInsertOnePatch(t *testing.T) {
	a := keyedList("a", "b", "d")
	b := keyedList("a", "b", "c", "d")
	got := diff(a, b)
	ins, rm, mv := countOps(got)
	if ins != 1 || rm != 0 || mv != 0 {
		t.Fatalf("want 1 insert only; got ins=%d rm=%d mv=%d  patches=%+v", ins, rm, mv, got)
	}
}

// Single-middle delete: head & tail both trim, leaving a single remove.
func TestKeyedMiddleDeleteOnePatch(t *testing.T) {
	a := keyedList("a", "b", "c", "d")
	b := keyedList("a", "b", "d")
	got := diff(a, b)
	ins, rm, mv := countOps(got)
	if ins != 0 || rm != 1 || mv != 0 {
		t.Fatalf("want 1 remove only; got ins=%d rm=%d mv=%d  patches=%+v", ins, rm, mv, got)
	}
}

// Swap two adjacent middle elements: LIS pins one, moves the other.
func TestKeyedAdjacentSwapOneMove(t *testing.T) {
	a := keyedList("a", "b", "c", "d")
	b := keyedList("a", "c", "b", "d")
	got := diff(a, b)
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
	got := diff(a, b)
	ins, rm, mv := countOps(got)
	if ins != 0 || rm != 0 || mv != 4 {
		t.Fatalf("want 4 moves; got ins=%d rm=%d mv=%d  patches=%+v", ins, rm, mv, got)
	}
	// All emitted moves should be keyed (no positional from/to fields used).
	for _, p := range got {
		if p.op == "move_child" && !p.keyed {
			t.Fatalf("expected keyed move, got positional: %+v", p)
		}
	}
}

// Rule 4 specifically: move from the tail to the head.
func TestKeyedRule4TailToHead(t *testing.T) {
	a := keyedList("a", "b", "c", "d")
	b := keyedList("d", "a", "b", "c")
	got := diff(a, b)
	ins, rm, mv := countOps(got)
	if ins != 0 || rm != 0 || mv != 1 {
		t.Fatalf("want exactly 1 move; got ins=%d rm=%d mv=%d  patches=%+v", ins, rm, mv, got)
	}
	for _, p := range got {
		if p.op == "move_child" {
			if p.key != "d" || p.before != "a" {
				t.Fatalf("want move_child key=d before=a, got %+v", p)
			}
		}
	}
}

// Identical: no patches at all.
func TestKeyedIdenticalNoOps(t *testing.T) {
	a := keyedList("a", "b", "c", "d")
	got := diff(a, a)
	if len(got) != 0 {
		t.Fatalf("want no patches, got %+v", got)
	}
}

// Mixed: remove one, insert one, move one — exercises pass-B remove,
// right-to-left insert, and LIS move all together.
func TestKeyedMixedRemoveInsertMove(t *testing.T) {
	a := keyedList("a", "b", "c", "d", "e") // remove c, insert x, swap d/b
	b := keyedList("a", "d", "x", "b", "e")
	got := diff(a, b)
	ins, rm, _ := countOps(got)
	if ins != 1 || rm != 1 {
		t.Fatalf("want 1 insert and 1 remove; got %+v", got)
	}
}

// Diff also runs combining: two `class` attrs at construction produce a single
// canonical "a b" value that the diff sees.
func TestAttrCombiningBeforeDiff(t *testing.T) {
	a := Tag("div")(Attribute("class", "a"))()
	b := Tag("div")(Attribute("class", "a"), Attribute("class", "b"))()
	got := diff(a, b)
	if len(got) != 1 || got[0].op != "set_attr" || got[0].value != "a b" {
		t.Fatalf("expected class to combine to \"a b\", got %+v", got)
	}
}

// ---- regression cases pulled from property-test failures ----
//
// Each minimizes a failing seed from TestDiffApplyProperty into a
// hand-rolled fixture asserting on patch shape. Property tests sample
// a distribution; these pin the specific bug down so generator tweaks
// can never silently lose coverage.

// Regression: same tag + different key must trigger a replace. The key
// is rendered as data-domi-key on the element, but lives in Node.key
// (not attrs), so an attr-level diff misses it. Without the replace,
// the client's data-domi-key drifts out of sync with the server's view.
func TestKeyChangeForcesReplace(t *testing.T) {
	old := Tag("ul")()(Tag("li")()())
	next := Tag("ul")()(Tag("li")()().WithKey("a"))
	got := diff(old, next)
	if len(got) != 1 || got[0].op != "replace" || len(got[0].path) != 1 || got[0].path[0] != 0 {
		t.Fatalf("expected single replace at [0], got %+v", got)
	}
}

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
	old := Tag("ul")()(Tag("li")()().WithKey("a"))
	next := Tag("ul")()(
		Tag("li")()().WithKey("b"),
		Tag("li")()().WithKey("c"),
		Tag("li")()().WithKey("a"),
	)
	got := diff(old, next)
	var inserts []patch
	for _, p := range got {
		if p.op == "insert_child" {
			inserts = append(inserts, p)
		}
	}
	if len(inserts) != 2 {
		t.Fatalf("expected 2 inserts, got %d: %+v", len(inserts), got)
	}
	if inserts[0].key != "c" || inserts[0].before != "a" {
		t.Fatalf("expected first insert key=c before=a, got %+v", inserts[0])
	}
	if inserts[1].key != "b" || inserts[1].before != "c" {
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
	old := Tag("div")()(Text("a"), Text("b"), Tag("span")()())
	next := Tag("div")()(Tag("span")()())
	got := diff(old, next)
	if len(got) != 2 {
		t.Fatalf("expected 2 patches (one remove + one replace), got %d: %+v", len(got), got)
	}
	if got[0].op != "remove_child" || got[0].idx != 1 {
		t.Fatalf("expected first patch remove_child idx=1, got %+v", got[0])
	}
	if got[1].op != "replace" || len(got[1].path) != 1 || got[1].path[0] != 0 {
		t.Fatalf("expected second patch replace at [0], got %+v", got[1])
	}
}
