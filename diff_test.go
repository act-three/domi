package domi

import "testing"

// keyedText constructs a keyed text node for tests.
func keyedText(k, s string) Node { return Text(s).WithKey(k) }

// keyedItem constructs a keyed <li> wrapping the given text.
func keyedItem(k string) Node {
	return E("li", nil, []Node{Text(k)}).WithKey(k)
}

// keyedList builds a <ul> whose children are keyed <li> elements (one per key).
// Keyed children must be elements so they can carry data-domi-key in the
// rendered HTML (text nodes can't).
func keyedList(keys ...string) Node {
	kids := make([]Node, len(keys))
	for i, k := range keys {
		kids[i] = keyedItem(k)
	}
	return E("ul", nil, kids)
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
	a := E("div", nil, []Node{Text("hi")})
	if got := diff(a, a); len(got) != 0 {
		t.Fatalf("want no patches, got %+v", got)
	}
}

func TestTextChange(t *testing.T) {
	a := E("div", nil, []Node{Text("hi")})
	b := E("div", nil, []Node{Text("bye")})
	got := diff(a, b)
	if len(got) != 1 || got[0].op != "set_text" || got[0].value != "bye" {
		t.Fatalf("unexpected: %+v", got)
	}
}

func TestTagChangeReplaces(t *testing.T) {
	a := E("div", nil, nil)
	b := E("span", nil, nil)
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
	a := E("div", nil, nil)
	b := E("div", []Attr{Attribute("class", "x")}, nil)
	got := diff(a, b)
	if len(got) != 1 || got[0].op != "set_attr" || got[0].name != "class" || got[0].value != "x" {
		t.Fatalf("expected single set_attr, got %+v", got)
	}
}

func TestAttrChanged(t *testing.T) {
	a := E("div", []Attr{Attribute("class", "x")}, nil)
	b := E("div", []Attr{Attribute("class", "y")}, nil)
	got := diff(a, b)
	if len(got) != 1 || got[0].op != "set_attr" || got[0].value != "y" {
		t.Fatalf("expected set_attr to y, got %+v", got)
	}
}

func TestAttrRemoved(t *testing.T) {
	a := E("div", []Attr{Attribute("class", "x")}, nil)
	b := E("div", nil, nil)
	got := diff(a, b)
	if len(got) != 1 || got[0].op != "remove_attr" || got[0].name != "class" {
		t.Fatalf("expected remove_attr, got %+v", got)
	}
}

func TestReplacePatchCarriesHTML(t *testing.T) {
	a := E("div", nil, nil)
	b := E("span", nil, []Node{Text("hi")})
	got := diff(a, b)
	if len(got) != 1 || got[0].op != "replace" {
		t.Fatalf("expected single replace, got %+v", got)
	}
	if got[0].html != "<span>hi</span>" {
		t.Fatalf("expected rendered HTML, got %q", got[0].html)
	}
}

func TestInsertChildPatchCarriesHTML(t *testing.T) {
	a := E("ul", nil, nil)
	b := E("ul", nil, []Node{E("li", nil, []Node{Text("one")})})
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
	a := E("div", []Attr{Attribute("class", "a")}, nil)
	b := E("div", []Attr{Attribute("class", "a"), Attribute("class", "b")}, nil)
	got := diff(a, b)
	if len(got) != 1 || got[0].op != "set_attr" || got[0].value != "a b" {
		t.Fatalf("expected class to combine to \"a b\", got %+v", got)
	}
}
