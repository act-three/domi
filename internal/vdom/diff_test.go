package vdom

import (
	"iter"
	"slices"
	"testing"
)

// el builds an element with no attrs from a tag and child list.
func el(tag string, children ...Node) Element {
	return NewElement(tag, attrs(), children)
}

// tx builds a text node.
func tx(s string) Text { return Text(s) }

// at builds an attribute literal.
func at(name, value string) Attr { return Attr{Name: name, Value: value} }

// attrs wraps a slice as an iterator for NewElement.
func attrs(a ...Attr) iter.Seq[Attr] { return slices.Values(a) }

// keyedList builds a keyed <ul> whose children are <li>s named for each
// key, each keyed via WithKey — what domi's lowering produces.
func keyedList(keys ...string) Element {
	children := make([]Node, len(keys))
	for i, k := range keys {
		children[i] = NewElement("li", attrs(), []Node{Text(k)}).WithKey(k, false)
	}
	return NewElement("ul", attrs(), children)
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
		case "InsertChild":
			inserts++
		case "RemoveChild":
			removes++
		case "MoveChild":
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
	if len(got) != 1 || got[0].Op != "SetText" || got[0].Value != "bye" {
		t.Fatalf("unexpected: %+v", got)
	}
}

// A text change carries the new content unescaped: the client writes it
// straight to nodeValue, which does no entity decoding, so escaping it
// here would double-encode in the DOM.
func TestTextChangeValueIsUnescaped(t *testing.T) {
	a := el("div", tx("a < b"))
	b := el("div", tx("a > c & d"))
	got := diffOne(a, b)
	if len(got) != 1 || got[0].Op != "SetText" || got[0].Value != "a > c & d" {
		t.Fatalf("unexpected: %+v", got)
	}
}

// Adjacent text nodes coalesce into one Text node, so editing part of a
// run still rides a single SetText rather than degrading to a Replace.
func TestCoalescedTextChangeIsSetText(t *testing.T) {
	a := el("div", tx("Count: "), tx("5"))
	b := el("div", tx("Count: "), tx("6"))
	got := diffOne(a, b)
	if len(got) != 1 || got[0].Op != "SetText" || got[0].Value != "Count: 6" {
		t.Fatalf("unexpected: %+v", got)
	}
}

func TestEmptyTextChangeInsertsTextNode(t *testing.T) {
	a := el("div", tx(""))
	b := el("div", tx("hi"))
	got := diffOne(a, b)
	if len(got) != 1 || got[0].Op != "InsertChild" || got[0].Index == nil || *got[0].Index != 0 || got[0].HTML != "hi" {
		t.Fatalf("expected InsertChild text, got %+v", got)
	}
}

// Changing a node's kind (text → element) is structural: it replaces
// the subtree rather than editing text in place.
func TestTextToElementReplaces(t *testing.T) {
	a := el("div", tx("hi"))
	b := el("div", el("span"))
	got := diffOne(a, b)
	if len(got) != 1 || got[0].Op != "Replace" {
		t.Fatalf("expected replace, got %+v", got)
	}
}

func TestTagChangeReplaces(t *testing.T) {
	a := el("div")
	b := el("span")
	got := diffOne(a, b)
	if len(got) != 1 || got[0].Op != "Replace" {
		t.Fatalf("unexpected: %+v", got)
	}
}

func TestKeyedReorder(t *testing.T) {
	a := keyedList("a", "b", "c")
	b := keyedList("c", "a", "b")
	got := diffOne(a, b)
	for _, p := range got {
		if p.Op == "MoveChild" {
			return
		}
	}
	t.Fatalf("expected at least one MoveChild, got %+v", got)
}

func TestKeyedInsertMiddle(t *testing.T) {
	a := keyedList("a", "c")
	b := keyedList("a", "b", "c")
	got := diffOne(a, b)
	for _, p := range got {
		if p.Op == "InsertChild" && p.Key == "b" && p.Before == "c" {
			return
		}
	}
	t.Fatalf("expected InsertChild key=b before=c, got %+v", got)
}

func TestKeyedRemove(t *testing.T) {
	a := keyedList("a", "b", "c")
	b := keyedList("a", "c")
	got := diffOne(a, b)
	for _, p := range got {
		if p.Op == "RemoveChild" && p.Key == "b" {
			return
		}
	}
	t.Fatalf("expected RemoveChild key=b, got %+v", got)
}

func TestAttrAdded(t *testing.T) {
	a := el("div")
	b := NewElement("div", attrs(at("class", "x")), nil)
	got := diffOne(a, b)
	if len(got) != 1 || got[0].Op != "SetAttr" || got[0].Name != "class" || got[0].Value != "x" {
		t.Fatalf("expected single SetAttr, got %+v", got)
	}
}

func TestAttrChanged(t *testing.T) {
	a := NewElement("div", attrs(at("class", "x")), nil)
	b := NewElement("div", attrs(at("class", "y")), nil)
	got := diffOne(a, b)
	if len(got) != 1 || got[0].Op != "SetAttr" || got[0].Value != "y" {
		t.Fatalf("expected SetAttr to y, got %+v", got)
	}
}

func TestAttrRemoved(t *testing.T) {
	a := NewElement("div", attrs(at("class", "x")), nil)
	b := el("div")
	got := diffOne(a, b)
	if len(got) != 1 || got[0].Op != "RemoveAttr" || got[0].Name != "class" {
		t.Fatalf("expected RemoveAttr, got %+v", got)
	}
}

func TestPositionalReorderPatchesAttrs(t *testing.T) {
	row := func(href, title string, selected bool) Element {
		a := []Attr{at("href", href)}
		if selected {
			a = append(a, at("data-selected", ""))
		}
		return NewElement("a", attrs(a...), []Node{tx(title)})
	}
	a := el("div",
		row("/app/movies/mo123", "New Movie", true),
		row("/app/movies/star-wars", "Star Wars", false),
	)
	b := el("div",
		row("/app/movies/star-wars", "Star Wars", false),
		row("/app/movies/star-wars-mo123", "Star Wars", true),
	)
	got := diffOne(a, b)

	want := []patch{
		{Op: "RemoveAttr", Path: []int{0}, Name: "data-selected"},
		{Op: "SetAttr", Path: []int{0}, Name: "href", Value: "/app/movies/star-wars"},
		{Op: "SetText", Path: []int{0, 0}, Value: "Star Wars"},
		{Op: "SetAttr", Path: []int{1}, Name: "data-selected"},
		{Op: "SetAttr", Path: []int{1}, Name: "href", Value: "/app/movies/star-wars-mo123"},
	}
	if !slices.EqualFunc(got, want, func(a, b patch) bool {
		return a.Op == b.Op &&
			slices.Equal(a.Path, b.Path) &&
			a.Name == b.Name &&
			a.Value == b.Value
	}) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestReplacePatchCarriesHTML(t *testing.T) {
	a := el("div")
	b := el("span", tx("hi"))
	got := diffOne(a, b)
	if len(got) != 1 || got[0].Op != "Replace" {
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
	if len(got) != 1 || got[0].Op != "InsertChild" {
		t.Fatalf("expected single InsertChild, got %+v", got)
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
		if p.Op == "MoveChild" && p.From != nil {
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
		if p.Op == "MoveChild" {
			if p.Key != "d" || p.Before != "a" {
				t.Fatalf("want MoveChild key=d before=a, got %+v", p)
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
		if p.Op == "InsertChild" {
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
// (e.g. `RemoveChild index=2` when the DOM only has 2 childNodes).
//
// Setup: old = div containing [text "a", text "b", span] — 3 Go
// children, but 2 DOM childNodes after parsing (text "ab" + span).
// next = div containing just [span]. Coalesce should normalize old to
// 2 children, yielding RemoveChild index=1 + replace at [0].
func TestAdjacentTextCoalescesBeforePositionalDiff(t *testing.T) {
	old := el("div", tx("a"), tx("b"), el("span"))
	next := el("div", el("span"))
	got := diffOne(old, next)
	if len(got) != 2 {
		t.Fatalf("expected 2 patches (one remove + one replace), got %d: %+v", len(got), got)
	}
	if got[0].Op != "RemoveChild" || got[0].Index == nil || *got[0].Index != 1 {
		t.Fatalf("expected first patch RemoveChild index=1, got %+v", got[0])
	}
	if got[1].Op != "Replace" || len(got[1].Path) != 1 || got[1].Path[0] != 0 {
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
		return NewElement("div", attrs(), children)
	}
	leaf := func(class string) Node {
		return NewElement("span", attrs(Attr{Name: "class", Value: class}), nil)
	}
	old := plain(plain(plain(plain(leaf("old"), leaf("a"), leaf("b"), leaf("c")))))
	next := plain(plain(plain(plain(leaf("new"), leaf("a"), leaf("b"), leaf("c")))))
	got := diffOne(old, next)
	if len(got) != 1 {
		t.Fatalf("expected 1 SetAttr patch on first leaf, got %d: %+v", len(got), got)
	}
	if got[0].Op != "SetAttr" {
		t.Fatalf("expected SetAttr, got %q", got[0].Op)
	}
	if !slices.Equal(got[0].Path, []int{0, 0, 0, 0}) {
		t.Fatalf("SetAttr should target first leaf at [0,0,0,0], got %v", got[0].Path)
	}
}

// ---- mixed keyed/unkeyed children tests ----

// kid is one child of a [mixed] test element: its key ("" for an
// unkeyed child) and the node itself.
type kid struct {
	key string
	n   Node
}

// mixed builds an element from (key, node) pairs, keying the children
// with nonempty keys via WithKey.
func mixed(tag string, kids ...kid) Element {
	children := make([]Node, len(kids))
	for i, k := range kids {
		children[i] = k.n
		if k.key != "" {
			children[i] = k.n.(Element).WithKey(k.key, false)
		}
	}
	return NewElement(tag, attrs(), children)
}

// li builds a list item wrapping text s.
func li(s string) Element { return el("li", tx(s)) }

// The headline mixed case: unkeyed header, keyed run, unkeyed footer.
// Appending an item to the run emits exactly one insert — anchored
// after the last keyed child via the empty Before — and touches
// neither header nor footer, so footer DOM state (a text selection,
// say) survives the append.
func TestMixedFooterAppendSingleInsert(t *testing.T) {
	old := mixed("ul", kid{"", li("header")}, kid{"a", li("a")}, kid{"", li("footer")})
	new := mixed("ul", kid{"", li("header")}, kid{"a", li("a")}, kid{"b", li("b")}, kid{"", li("footer")})
	got := diffOne(old, new)
	if len(got) != 1 {
		t.Fatalf("want exactly 1 patch, got %d: %+v", len(got), got)
	}
	if p := got[0]; p.Op != "InsertChild" || p.Key != "b" || p.Before != "" {
		t.Fatalf(`want InsertChild key=b before="", got %+v`, p)
	}
}

// A keyed swap around unkeyed content — the case that forces gap
// pairing against the simulated post-keyed-phase list rather than the
// old tree. The move leaves u trailing ([u, b, a]); the gap diffs then
// rebuild the gaps to reach [b, u, a].
func TestMixedShuffleAroundUnkeyedRepairsGaps(t *testing.T) {
	old := mixed("ul", kid{"a", li("a")}, kid{"", li("u")}, kid{"b", li("b")})
	new := mixed("ul", kid{"b", li("b")}, kid{"", li("u")}, kid{"a", li("a")})
	got := diffOne(old, new)
	want := []patch{
		{Op: "MoveChild", Key: "a", Before: ""},
		{Op: "RemoveChild", Index: ptr(0)},
		{Op: "InsertChild", Index: ptr(1)},
	}
	if !slices.EqualFunc(got, want, func(a, b patch) bool {
		return a.Op == b.Op && a.Key == b.Key && a.Before == b.Before &&
			(a.Index == nil) == (b.Index == nil) &&
			(a.Index == nil || *a.Index == *b.Index)
	}) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func ptr(i int) *int { return &i }

// Removing the keyed separator between two text nodes leaves them
// adjacent in the live DOM while the new tree holds one coalesced
// Text; the gap diff converges them — surplus node removed, survivor
// SetText'd — because the simulated gap stays uncoalesced, mirroring
// real childNodes.
func TestMixedTextMergesWhenSeparatorRemoved(t *testing.T) {
	old := mixed("div", kid{"", tx("a")}, kid{"x", el("span")}, kid{"", tx("b")})
	new := el("div", tx("ab"))
	got := diffOne(old, new)
	if len(got) != 3 {
		t.Fatalf("want 3 patches, got %d: %+v", len(got), got)
	}
	if got[0].Op != "RemoveChild" || got[0].Key != "x" {
		t.Fatalf("want RemoveChild key=x first, got %+v", got[0])
	}
	if got[1].Op != "RemoveChild" || got[1].Index == nil || *got[1].Index != 1 {
		t.Fatalf("want RemoveChild index=1 second, got %+v", got[1])
	}
	if got[2].Op != "SetText" || !slices.Equal(got[2].Path, []int{0}) || got[2].Value != "ab" {
		t.Fatalf("want SetText [0] value=ab last, got %+v", got[2])
	}
}

// A key-matched pair's deferred content diff addresses the child by
// its childNodes index in the whole new list, not its index in the
// keyed subsequence: here key a is the second child but the first
// keyed one.
func TestMixedDeferredContentPathIsGlobal(t *testing.T) {
	old := mixed("div", kid{"", el("span", tx("hdr"))}, kid{"a", li("x")})
	new := mixed("div", kid{"", el("span", tx("hdr"))}, kid{"a", li("y")})
	got := diffOne(old, new)
	if len(got) != 1 || got[0].Op != "SetText" || !slices.Equal(got[0].Path, []int{1, 0}) || got[0].Value != "y" {
		t.Fatalf("want single SetText at [1 0], got %+v", got)
	}
}

// A child list whose key structure changes between renders reconciles
// like anything else — no wholesale Replace of the parent in either
// direction.
func TestKeyStructureChangeReconciles(t *testing.T) {
	keyed := mixed("ul", kid{"a", li("a")})
	plain := el("ul", li("z"))
	for name, pair := range map[string][2]Node{
		"keyed to plain": {keyed, plain},
		"plain to keyed": {plain, keyed},
	} {
		got := diffOne(pair[0], pair[1])
		ins, rm, mv := countOps(got)
		if ins != 1 || rm != 1 || mv != 0 {
			t.Fatalf("%s: want 1 insert + 1 remove, got ins=%d rm=%d mv=%d: %+v", name, ins, rm, mv, got)
		}
		for _, p := range got {
			if p.Op == "Replace" {
				t.Fatalf("%s: key structure change must reconcile, not Replace: %+v", name, got)
			}
		}
	}
}

// A Before:"" move whose subject is already the last keyed child stays
// put rather than leapfrogging the unkeyed content behind it. The
// client's insertAfterLastKeyed mirrors this.
func TestSimulateEmptyBeforeAlreadyLastKeyedHoldsStill(t *testing.T) {
	kids := []Node{li("a").WithKey("a", false), li("u0"), li("b").WithKey("b", false), li("u1")}
	sim := simulate(kids, []patch{{Op: "MoveChild", Key: "b"}})
	want := []string{"a", "", "b", ""}
	got := make([]string, len(sim))
	for i, n := range sim {
		got[i] = childKey(n)
	}
	if !slices.Equal(got, want) {
		t.Fatalf("sim keys = %v, want unchanged %v", got, want)
	}
}

// An opaque node keeps its freeze inside a mixed parent: unkeyed
// siblings around it diff positionally while the opaque child is
// matched by key and left alone.
func TestOpaqueSurvivesInMixedParent(t *testing.T) {
	build := func(body, footer string) Element {
		return NewElement("main", attrs(), []Node{
			el("p", tx("intro")),
			NewElement("div", attrs(), []Node{tx(body)}).WithKey("v", true),
			el("p", tx(footer)),
		})
	}
	got := diffOne(build("first", "foot"), build("second", "toes"))
	if len(got) != 1 || got[0].Op != "SetText" || !slices.Equal(got[0].Path, []int{2, 0}) {
		t.Fatalf("want only the footer SetText at [2 0], got %+v", got)
	}
}

// ---- node-kind change tests ----

// Changing a node's kind replaces the subtree: there is no in-place
// path between an element and a text node.
func TestElementToTextReplaces(t *testing.T) {
	a := el("div", el("span"))
	b := el("div", tx("hi"))
	got := diffOne(a, b)
	if len(got) != 1 || got[0].Op != "Replace" {
		t.Fatalf("expected replace, got %+v", got)
	}
}

// Two text nodes separated by an element are distinct DOM children, so
// they aren't coalesced across it: editing the element between them is
// an isolated patch addressed through its index.
func TestTextElementTextNotCoalesced(t *testing.T) {
	a := el("div", tx("a"), el("b", tx("x")), tx("c"))
	b := el("div", tx("a"), el("b", tx("y")), tx("c"))
	got := diffOne(a, b)
	if len(got) != 1 || got[0].Op != "SetText" {
		t.Fatalf("expected single SetText for the changed middle, got %+v", got)
	}
	if !slices.Equal(got[0].Path, []int{1, 0}) {
		t.Fatalf("SetText should target the b's text at [1,0], got path %v", got[0].Path)
	}
}

// ---- opaque (client-owned) subtree tests ----

// okEntry is one child of an [okeyed] parent: a key and the text its
// opaque child wraps.
type okEntry struct{ key, body string }

// okeyed builds a keyed <main> whose children are opaque <div>s, one per
// entry, each carrying its key and wrapping its body text. Marking the
// children opaque lets a test mutate their bodies (or attrs) and assert
// the differ leaves them alone.
func okeyed(entries ...okEntry) Element {
	children := make([]Node, len(entries))
	for i, e := range entries {
		children[i] = NewElement("div", attrs(), []Node{tx(e.body)}).WithKey(e.key, true)
	}
	return NewElement("main", attrs(), children)
}

// An opaque keyed child is frozen: its body changes but the differ emits
// nothing, because the client owns the subtree.
func TestOpaqueFreezesSubtree(t *testing.T) {
	a := okeyed(okEntry{"v", "first"})
	b := okeyed(okEntry{"v", "second"})
	if got := diffOne(a, b); len(got) != 0 {
		t.Fatalf("opaque subtree must be frozen, got %+v", got)
	}
}

// Freezing covers the element's own attributes too, not just its
// descendants.
func TestOpaqueFreezesOwnAttrs(t *testing.T) {
	child := func(class string) Node {
		return NewElement("div", attrs(at("class", class)), nil).WithKey("v", true)
	}
	a := NewElement("main", attrs(), []Node{child("x")})
	b := NewElement("main", attrs(), []Node{child("y")})
	if got := diffOne(a, b); len(got) != 0 {
		t.Fatalf("opaque element's own attrs must be frozen, got %+v", got)
	}
}

// Inserting a sibling ahead of an opaque node leaves the opaque node
// untouched: the keyed differ matches it by key regardless of its new
// position, so the fresh sibling can't clobber the client-owned DOM.
// This is the case positional diffing could not honor.
func TestOpaqueSurvivesSiblingInsert(t *testing.T) {
	a := okeyed(okEntry{"k", "body"})
	b := okeyed(okEntry{"n", "new"}, okEntry{"k", "body"})
	got := diffOne(a, b)
	ins, rm, mv := countOps(got)
	if ins != 1 || rm != 0 || mv != 0 {
		t.Fatalf("want a single insert, got ins=%d rm=%d mv=%d: %+v", ins, rm, mv, got)
	}
	for _, p := range got {
		if p.Op == "Replace" {
			t.Fatalf("opaque node must not be replaced by an inserted sibling: %+v", got)
		}
	}
}

// Reordering opaque siblings moves the live nodes (MoveChild) rather than
// rebuilding them, so their client-owned state survives the reshuffle.
func TestOpaqueReorderMovesNotRebuilds(t *testing.T) {
	a := okeyed(okEntry{"a", "A"}, okEntry{"b", "B"})
	b := okeyed(okEntry{"b", "B"}, okEntry{"a", "A"})
	got := diffOne(a, b)
	if _, _, mv := countOps(got); mv == 0 {
		t.Fatalf("want at least one move, got %+v", got)
	}
	for _, p := range got {
		if p.Op == "Replace" {
			t.Fatalf("reorder must move, not replace, opaque nodes: %+v", got)
		}
	}
}

// Changing an opaque node's key remounts it: the keyed differ removes the
// old node and inserts a freshly rendered one, so client code can
// re-initialize against the new server markup.
func TestOpaqueKeyChangeRemounts(t *testing.T) {
	a := okeyed(okEntry{"v1", "first"})
	b := okeyed(okEntry{"v2", "second"})
	got := diffOne(a, b)
	ins, rm, _ := countOps(got)
	if ins != 1 || rm != 1 {
		t.Fatalf("want remove+insert remount, got %+v", got)
	}
	var insKey, rmKey string
	for _, p := range got {
		switch p.Op {
		case "InsertChild":
			insKey = p.Key
		case "RemoveChild":
			rmKey = p.Key
		}
	}
	if rmKey != "v1" || insKey != "v2" {
		t.Fatalf("want remove v1 + insert v2, got remove %q insert %q", rmKey, insKey)
	}
}

// Toggling opacity off for the same key hands the subtree back to the
// framework with a clean Replace.
func TestOpaqueToggleReplaces(t *testing.T) {
	opaque := NewElement("main", attrs(), []Node{
		NewElement("div", attrs(), []Node{tx("x")}).WithKey("k", true),
	})
	plain := NewElement("main", attrs(), []Node{
		NewElement("div", attrs(), []Node{tx("x")}).WithKey("k", false),
	})
	got := diffOne(opaque, plain)
	if len(got) != 1 || got[0].Op != "Replace" || !slices.Equal(got[0].Path, []int{0}) {
		t.Fatalf("opacity toggle should Replace at [0], got %+v", got)
	}
}
