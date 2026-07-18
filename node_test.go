package domi

import (
	"strings"
	"testing"

	"ily.dev/domi/internal/vdom"
)

// lowerOneNode and lowerNodes lower test nodes from the root address,
// dropping the harvested handlers that these structural tests don't
// exercise. They keep the call sites reading the way they did before
// lowering grew an address parameter and a second return value.
func lowerOneNode(n Node) vdom.Node {
	v, _ := lowerOne(0, n)
	return v
}

func lowerNodes(nodes ...Node) []vdom.Node {
	v, _ := lower(0, nodes...)
	return v
}

// Fragment is supposed to be indistinguishable from writing its children
// inline at the use site. The tests below pin that property through the
// observable contract — rendered HTML and emitted diffs against the
// inline-equivalent tree.

func TestFragmentNestedFlattens(t *testing.T) {
	a := lowerOneNode(Tag("div")()(Fragment(Fragment(Text("a"), Text("b")), Text("c"))))
	b := lowerOneNode(Tag("div")()(Text("a"), Text("b"), Text("c")))
	if vdom.Render(a) != vdom.Render(b) {
		t.Fatalf("nested Fragment should flatten: %q vs %q", vdom.Render(a), vdom.Render(b))
	}
}

func TestFragmentEmptyContributesNothing(t *testing.T) {
	a := lowerOneNode(Tag("div")()(Fragment(), Text("x")))
	b := lowerOneNode(Tag("div")()(Text("x")))
	if vdom.Render(a) != vdom.Render(b) {
		t.Fatalf("empty Fragment should contribute nothing: %q vs %q", vdom.Render(a), vdom.Render(b))
	}
}

func TestFragmentPreservesSiblingOrder(t *testing.T) {
	a := lowerOneNode(Tag("div")()(
		Text("a"),
		Fragment(Text("b"), Text("c")),
		Text("d"),
	))
	b := lowerOneNode(Tag("div")()(Text("a"), Text("b"), Text("c"), Text("d")))
	if vdom.Render(a) != vdom.Render(b) {
		t.Fatalf("Fragment children should appear in position: %q vs %q", vdom.Render(a), vdom.Render(b))
	}
}

func TestFragmentIsTransparentToDiff(t *testing.T) {
	a := lowerNodes(Tag("div")()(Fragment(Text("a"), Text("b"))))
	b := lowerNodes(Tag("div")()(Text("a"), Text("b")))
	if got := vdom.Diff(a, b); len(got) != 0 {
		t.Fatalf("Fragment-wrapped should diff identically: got %+v", got)
	}
}

func TestFragmentAtRootLowers(t *testing.T) {
	// A Fragment returned from App.View becomes the mount's children.
	got := lowerNodes(Fragment(Tag("div")()(Text("a")), Tag("span")()(Text("b"))))
	if len(got) != 2 {
		t.Fatalf("expected 2 lowered nodes from Fragment root, got %d: %+v", len(got), got)
	}
	if vdom.Render(got[0]) != "<div>a</div>" || vdom.Render(got[1]) != "<span>b</span>" {
		t.Fatalf("Fragment children should lower in order: %q, %q",
			vdom.Render(got[0]), vdom.Render(got[1]))
	}
}

// ---- WithKey tests ----

// keyedLis builds a Fragment of keyed <li> items, one per key, each
// wrapping its key as text.
func keyedLis(keys ...string) Node {
	rows := make([]Node, len(keys))
	for i, k := range keys {
		rows[i] = WithKey(k, Tag("li")()(Text(k)))
	}
	return Fragment(rows...)
}

// Keyed children compose with unkeyed siblings in one parent: they
// render in place, carrying their keys, between the unkeyed header
// and footer.
func TestWithKeyMixesWithUnkeyedSiblings(t *testing.T) {
	got := vdom.Render(lowerOneNode(Tag("ul")()(
		Tag("li")()(Text("header")),
		keyedLis("a", "b"),
		Tag("li")()(Text("footer")),
	)))
	want := `<ul><li>header</li><li data-domi-key="a">a</li><li data-domi-key="b">b</li><li>footer</li></ul>`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// WithKey accepts a childless [Element] builder just as a child list
// does, applying it to a finished element.
func TestWithKeyAppliesElementBuilder(t *testing.T) {
	a := vdom.Render(lowerOneNode(Tag("ul")()(WithKey("a", Tag("li")(Name("class")("x"))))))
	b := vdom.Render(lowerOneNode(Tag("ul")()(WithKey("a", Tag("li")(Name("class")("x"))()))))
	if a != b {
		t.Fatalf("builder and element should key identically: %q vs %q", a, b)
	}
}

// Appending to the keyed run of a mixed list is a single patch: the
// unkeyed header and footer are matched in place, not rebuilt.
func TestWithKeyAppendIsSinglePatch(t *testing.T) {
	view := func(keys ...string) []vdom.Node {
		return lowerNodes(Tag("ul")()(
			Tag("li")()(Text("header")),
			keyedLis(keys...),
			Tag("li")()(Text("footer")),
		))
	}
	if ps := vdom.Diff(view("a"), view("a", "b")); len(ps) != 1 {
		t.Fatalf("append into a mixed list should be a single patch, got %d", len(ps))
	}
}

// The view's roots are the children of the domi-root mount, so keyed
// children work there like anywhere else: a root-level reorder is a
// move, not a rebuild.
func TestWithKeyAtRoot(t *testing.T) {
	got := lowerNodes(keyedLis("a", "b"))
	if len(got) != 2 {
		t.Fatalf("expected 2 lowered roots, got %d", len(got))
	}
	if html := vdom.Render(got[0]); html != `<li data-domi-key="a">a</li>` {
		t.Fatalf("keyed root should carry its key: %q", html)
	}
	ps := vdom.Diff(lowerNodes(keyedLis("a", "b")), lowerNodes(keyedLis("b", "a")))
	if len(ps) != 1 {
		t.Fatalf("root-level keyed reorder should be a single move, got %d patches", len(ps))
	}
}

// The empty string marks an unkeyed child in the lowered form, so it
// cannot be a key; WithKey panics at construction.
func TestWithKeyEmptyKeyPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for an empty key")
		}
	}()
	_ = keyedLis("")
}

// A Fragment has no single identity to key; WithKey panics at
// construction.
func TestWithKeyFragmentPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for a keyed Fragment")
		}
	}()
	_ = WithKey("a", Fragment(Tag("li")()(Text("x"))))
}

// Re-keying a keyed node is a construction error, not a silent
// override.
func TestWithKeyTwicePanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for a doubly keyed child")
		}
	}()
	_ = WithKey("b", WithKey("a", Tag("li")()))
}

// ---- nil Node tests ----
//
// A nil Node is the empty Fragment's degenerate twin: it lowers to
// nothing wherever a Node is accepted, so conditional content can be a
// node-or-nil with no guard at the use site.

func TestNilNodeContributesNothing(t *testing.T) {
	a := lowerOneNode(Tag("div")()(Text("a"), nil, Text("b")))
	b := lowerOneNode(Tag("div")()(Text("a"), Text("b")))
	if vdom.Render(a) != vdom.Render(b) {
		t.Fatalf("nil child should contribute nothing: %q vs %q", vdom.Render(a), vdom.Render(b))
	}
}

func TestNilNodeAtRootLowersToNothing(t *testing.T) {
	var n Node // nil
	if got := lowerNodes(n); len(got) != 0 {
		t.Fatalf("nil root should lower to nothing, got %d nodes", len(got))
	}
}

// A keyed child must be a real element with an identity; a nil child has
// none, so WithKey panics rather than silently dropping the slot.
func TestNilKeyedChildPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("expected panic for a nil keyed child, got none")
		}
	}()
	_ = WithKey("a", nil)
}

// lowerOne needs exactly one node; a nil Node lowers to zero, so it
// panics rather than inventing one.
func TestLowerOneNilPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("expected panic for lowerOneNode(nil), got none")
		}
	}()
	var n Node // nil
	_ = lowerOneNode(n)
}

// Group is the attr-side mirror of Fragment. The tests below pin the
// same property through the observable contract: a Group should be
// indistinguishable from writing its attrs inline at the use site.

func TestGroupNestedFlattens(t *testing.T) {
	a := lowerOneNode(Tag("div")(Group(Group(Name("class")("a"), Name("id")("x")), Name("data-x")("1")))())
	b := lowerOneNode(Tag("div")(Name("class")("a"), Name("id")("x"), Name("data-x")("1"))())
	if vdom.Render(a) != vdom.Render(b) {
		t.Fatalf("nested Group should flatten: %q vs %q", vdom.Render(a), vdom.Render(b))
	}
}

func TestGroupEmptyContributesNothing(t *testing.T) {
	a := lowerOneNode(Tag("div")(Group(), Name("id")("x"))())
	b := lowerOneNode(Tag("div")(Name("id")("x"))())
	if vdom.Render(a) != vdom.Render(b) {
		t.Fatalf("empty Group should contribute nothing: %q vs %q", vdom.Render(a), vdom.Render(b))
	}
}

// A nil Attr is the empty Group's degenerate twin: it lowers to nothing
// wherever an Attr is accepted, so conditional attributes can be an
// attr-or-nil with no guard at the use site.
func TestNilAttrContributesNothing(t *testing.T) {
	a := lowerOneNode(Tag("div")(Name("class")("a"), nil, Name("id")("x"))())
	b := lowerOneNode(Tag("div")(Name("class")("a"), Name("id")("x"))())
	if vdom.Render(a) != vdom.Render(b) {
		t.Fatalf("nil attr should contribute nothing: %q vs %q", vdom.Render(a), vdom.Render(b))
	}
}

func TestGroupPreservesAttrOrder(t *testing.T) {
	a := lowerOneNode(Tag("div")(
		Name("id")("x"),
		Group(Name("class")("a"), Name("data-y")("1")),
		Name("data-z")("2"),
	)())
	b := lowerOneNode(Tag("div")(
		Name("id")("x"),
		Name("class")("a"),
		Name("data-y")("1"),
		Name("data-z")("2"),
	)())
	if vdom.Render(a) != vdom.Render(b) {
		t.Fatalf("Group attrs should appear in position: %q vs %q", vdom.Render(a), vdom.Render(b))
	}
}

// Combining (e.g. class joining) is a property of the lowered list, so
// a Group of duplicate classes should combine with a sibling Class just
// like inline duplicates do.
func TestGroupClassCombinesAcrossBoundary(t *testing.T) {
	a := lowerOneNode(Tag("div")(Group(Name("class")("a"), Name("class")("b")), Name("class")("c"))())
	b := lowerOneNode(Tag("div")(Name("class")("a"), Name("class")("b"), Name("class")("c"))())
	if vdom.Render(a) != vdom.Render(b) {
		t.Fatalf("Group-of-classes should combine like inline: %q vs %q", vdom.Render(a), vdom.Render(b))
	}
}

// ---- attribute combining tests ----
//
// NewElement normalizes attrs at construction, so duplicate names are
// resolved before the renderer or differ ever sees them. These tests
// exercise the observable contract through Tag → Render.

func TestCombineClassWithSpace(t *testing.T) {
	got := vdom.Render(lowerOneNode(Tag("div")(Name("class")("a"), Name("class")("b"))()))
	want := `<div class="a b"></div>`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestCombineStyleWithSemicolon(t *testing.T) {
	got := vdom.Render(lowerOneNode(Tag("div")(Name("style")("color:red"), Name("style")("font-weight:bold"))()))
	want := `<div style="color:red;font-weight:bold"></div>`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestCombineDataMsgWithComma(t *testing.T) {
	got := vdom.Render(lowerOneNode(Tag("div")(Name("data-msg-click")("h1"), Name("data-msg-click")("h2"))()))
	want := `<div data-msg-click="h1,h2"></div>`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestCombineDistinctEventsKeepBoth(t *testing.T) {
	rendered := vdom.Render(lowerOneNode(Tag("div")(Name("data-msg-click")("h1"), Name("data-msg-submit")("h2"))()))
	if !strings.Contains(rendered, "data-msg-click") || !strings.Contains(rendered, "data-msg-submit") {
		t.Fatalf("distinct event attrs should both appear: %q", rendered)
	}
}

func TestCombineSingleDataMsgNoComma(t *testing.T) {
	rendered := vdom.Render(lowerOneNode(Tag("div")(Name("data-msg-click")("h1"))()))
	if strings.Contains(rendered, ",") {
		t.Fatalf("single data-msg should have no comma: %q", rendered)
	}
}

func TestCombineOtherAttrFirstWins(t *testing.T) {
	got := vdom.Render(lowerOneNode(Tag("div")(Name("id")("first"), Name("id")("second"))()))
	want := `<div id="first"></div>`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestCombineClassEmptyGuard(t *testing.T) {
	got := vdom.Render(lowerOneNode(Tag("div")(Name("class")(""), Name("class")("b"))()))
	want := `<div class="b"></div>`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestRegisterCombining(t *testing.T) {
	RegisterCombining("data-x", ":")
	got := vdom.Render(lowerOneNode(Tag("div")(Name("data-x")("a"), Name("data-x")("b"), Name("data-x")("c"))()))
	want := `<div data-x="a:b:c"></div>`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// ---- variadic Name tests ----
//
// A single Name(...) call with multiple values lowers to one attr per
// value, so it resolves through the same combining rules as repeated
// calls. These pin that equivalence at the builder's own signature.

func TestNameVariadicClass(t *testing.T) {
	got := vdom.Render(lowerOneNode(Tag("div")(Name("class")("a", "b"))()))
	want := `<div class="a b"></div>`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestNameVariadicStyle(t *testing.T) {
	got := vdom.Render(lowerOneNode(Tag("div")(Name("style")("color:red", "font-weight:bold"))()))
	want := `<div style="color:red;font-weight:bold"></div>`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestNameZeroArgBare(t *testing.T) {
	got := vdom.Render(lowerOneNode(Tag("div")(Name("disabled")())()))
	want := `<div disabled></div>`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestNameVariadicFirstWins(t *testing.T) {
	got := vdom.Render(lowerOneNode(Tag("div")(Name("id")("first", "second"))()))
	want := `<div id="first"></div>`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// ---- UnsafeParseRaw tests ----

// renderTree lowers a node and renders it (and any fragment siblings)
// to a single HTML string.
func renderTree(t *testing.T, n Node) string {
	t.Helper()
	var b strings.Builder
	for _, ln := range lowerNodes(n) {
		b.WriteString(vdom.Render(ln))
	}
	return b.String()
}

// renderParsed parses HTML and renders the resulting fragment back to a
// string — the round-trip that checks faithful adoption of prerendered
// HTML.
func renderParsed(t *testing.T, s string) string {
	t.Helper()
	n, err := UnsafeParseRaw(s)
	if err != nil {
		t.Fatalf("UnsafeParseRaw(%q): %v", s, err)
	}
	return renderTree(t, n)
}

func TestUnsafeParseRawElement(t *testing.T) {
	if got := renderParsed(t, "<div><span>hi</span></div>"); got != "<div><span>hi</span></div>" {
		t.Fatalf("got %q", got)
	}
}

func TestUnsafeParseRawVoidElement(t *testing.T) {
	if got := renderParsed(t, "<br>"); got != "<br>" {
		t.Fatalf("got %q", got)
	}
}

// Text content is escaped on the way back out: a literal '<' or '&' in
// the source becomes an entity in the rendered output.
func TestUnsafeParseRawEscapesText(t *testing.T) {
	if got := renderParsed(t, "<div>5 < 10 & rising</div>"); got != "<div>5 &lt; 10 &amp; rising</div>" {
		t.Fatalf("got %q", got)
	}
}

// The fragment can hold several top-level nodes — a mix of text and
// elements — not just a single element.
func TestUnsafeParseRawMultipleTopLevel(t *testing.T) {
	n, err := UnsafeParseRaw("hello <b>world</b> and more")
	if err != nil {
		t.Fatal(err)
	}
	if got := len(lowerNodes(n)); got != 3 {
		t.Fatalf("expected 3 top-level nodes, got %d", got)
	}
	if got := renderParsed(t, "hello <b>world</b> and more"); got != "hello <b>world</b> and more" {
		t.Fatalf("got %q", got)
	}
}

// Empty input yields an empty fragment with no error.
func TestUnsafeParseRawEmpty(t *testing.T) {
	n, err := UnsafeParseRaw("")
	if err != nil {
		t.Fatal(err)
	}
	if got := len(lowerNodes(n)); got != 0 {
		t.Fatalf("expected empty fragment, got %d nodes", got)
	}
}

// Comments are dropped; surrounding content survives and the text on
// either side of a removed comment coalesces.
func TestUnsafeParseRawDropsComments(t *testing.T) {
	if got := renderParsed(t, "<div>a<!-- note -->b</div>"); got != "<div>ab</div>" {
		t.Fatalf("got %q", got)
	}
	n, err := UnsafeParseRaw("<!-- just a comment -->")
	if err != nil {
		t.Fatal(err)
	}
	if got := len(lowerNodes(n)); got != 0 {
		t.Fatalf("comment-only input should be empty, got %d nodes", got)
	}
}

// Script and style content is preserved verbatim — not entity-escaped —
// since the parser treats it as raw text.
func TestUnsafeParseRawScriptVerbatim(t *testing.T) {
	src := "<script>if (a && b) c < d;</script>"
	if got := renderParsed(t, src); got != src {
		t.Fatalf("got %q, want %q", got, src)
	}
}

// SVG survives the round-trip: camelCase tag and attribute names are
// preserved, and a namespaced attribute keeps its prefix.
func TestUnsafeParseRawSVG(t *testing.T) {
	src := `<svg viewBox="0 0 10 10"><clipPath id="c"></clipPath><use xlink:href="#c"></use></svg>`
	got := renderParsed(t, src)
	for _, want := range []string{"viewBox=", "<clipPath", `xlink:href="#c"`} {
		if !strings.Contains(got, want) {
			t.Fatalf("rendered SVG missing %q:\n%s", want, got)
		}
	}
}

// Context-sensitive fragments parse as a browser's <template> would: a
// bare <tr> keeps its structure instead of being stripped.
func TestUnsafeParseRawTableFragment(t *testing.T) {
	if got := renderParsed(t, "<tr><td>x</td></tr>"); got != "<tr><td>x</td></tr>" {
		t.Fatalf("got %q", got)
	}
}

// Parsing renders faithfully on nested flow content: markup already in
// the parser's canonical form — entities escaped, void elements bare —
// comes back byte for byte. This pins parser fidelity for the adoption
// use case (inlining trusted static markup); it makes no promise about
// re-ingesting domi's own rendered output, which is not a supported
// flow.
func TestUnsafeParseRawCanonicalMarkupIsStable(t *testing.T) {
	const src = `<div class="card"><h1>Title &amp; co</h1><p>a &lt; b<br>done</p><ul><li>one</li><li>two</li></ul></div>`
	if got := renderParsed(t, src); got != src {
		t.Fatalf("canonical markup changed in the round trip:\n src: %s\n got: %s", src, got)
	}
}

// ---- Bool tests ----

func TestBoolTrueEmitsNameOnly(t *testing.T) {
	got := vdom.Render(lowerOneNode(Tag("input")(Bool("disabled")(true))()))
	want := `<input disabled>`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestBoolFalseEmitsNothing(t *testing.T) {
	got := vdom.Render(lowerOneNode(Tag("input")(Bool("disabled")(false))()))
	want := `<input>`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestBoolTrueWithOtherAttrs(t *testing.T) {
	got := vdom.Render(lowerOneNode(Tag("input")(
		Name("type")("checkbox"),
		Bool("checked")(true),
	)()))
	want := `<input checked type="checkbox">`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestBoolFalseWithOtherAttrs(t *testing.T) {
	got := vdom.Render(lowerOneNode(Tag("input")(
		Name("type")("checkbox"),
		Bool("checked")(false),
	)()))
	want := `<input type="checkbox">`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestBoolToggleDiffProducesSetAndRemove(t *testing.T) {
	a := lowerNodes(Tag("input")(Bool("disabled")(false))())
	b := lowerNodes(Tag("input")(Bool("disabled")(true))())

	// false → true should produce a set_attr
	ps := vdom.Diff(a, b)
	if len(ps) != 1 {
		t.Fatalf("false→true: expected 1 patch, got %d", len(ps))
	}

	// true → false should produce a remove_attr
	ps = vdom.Diff(b, a)
	if len(ps) != 1 {
		t.Fatalf("true→false: expected 1 patch, got %d", len(ps))
	}
}

func TestBoolSameValueNoDiff(t *testing.T) {
	a := lowerNodes(Tag("input")(Bool("disabled")(true))())
	b := lowerNodes(Tag("input")(Bool("disabled")(true))())
	if ps := vdom.Diff(a, b); len(ps) != 0 {
		t.Fatalf("same value should produce no patches, got %d", len(ps))
	}
}

func TestBoolInGroup(t *testing.T) {
	a := lowerOneNode(Tag("input")(Group(
		Name("type")("text"),
		Bool("readonly")(true),
	))())
	b := lowerOneNode(Tag("input")(
		Name("type")("text"),
		Bool("readonly")(true),
	)())
	if vdom.Render(a) != vdom.Render(b) {
		t.Fatalf("Bool in Group should flatten: %q vs %q", vdom.Render(a), vdom.Render(b))
	}
}

// ---- Enumerated boolean tests ----
//
// contenteditable, draggable, spellcheck, and translate take the
// string values "true" and "false" rather than using presence/absence.

func TestEnumBoolTrueEmitsValueTrue(t *testing.T) {
	got := vdom.Render(lowerOneNode(Tag("div")(Bool("contenteditable")(true))()))
	want := `<div contenteditable="true"></div>`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestEnumBoolFalseEmitsValueFalse(t *testing.T) {
	got := vdom.Render(lowerOneNode(Tag("div")(Bool("contenteditable")(false))()))
	want := `<div contenteditable="false"></div>`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestEnumBoolDiffProducesSetAttr(t *testing.T) {
	a := lowerNodes(Tag("div")(Bool("spellcheck")(true))())
	b := lowerNodes(Tag("div")(Bool("spellcheck")(false))())
	ps := vdom.Diff(a, b)
	if len(ps) != 1 {
		t.Fatalf("true→false: expected 1 patch, got %d", len(ps))
	}
}

func TestEnumBoolSameValueNoDiff(t *testing.T) {
	a := lowerNodes(Tag("div")(Bool("draggable")(true))())
	b := lowerNodes(Tag("div")(Bool("draggable")(true))())
	if ps := vdom.Diff(a, b); len(ps) != 0 {
		t.Fatalf("same value should produce no patches, got %d", len(ps))
	}
}

func TestEnumBoolAllFour(t *testing.T) {
	for _, name := range []string{"contenteditable", "draggable", "spellcheck", "translate"} {
		got := vdom.Render(lowerOneNode(Tag("div")(Bool(name)(true))()))
		want := `<div ` + name + `="true"></div>`
		if got != want {
			t.Fatalf("Bool(%q)(true): got %q, want %q", name, got, want)
		}
	}
}

func TestRegularBoolStillUsesPresenceAbsence(t *testing.T) {
	got := vdom.Render(lowerOneNode(Tag("input")(Bool("disabled")(true))()))
	if strings.Contains(got, `="true"`) {
		t.Fatalf("regular bool should use presence, not value: %q", got)
	}
}

// ---- Opaque tests ----

// Opaque marks a keyed child client-owned: the framework freezes it and
// its subtree, emitting no patches even as the contents change. This pins
// the public Opaque attribute to the differ's freeze behavior end to end.
func TestOpaqueKeyedChildFreezes(t *testing.T) {
	build := func(body string) []vdom.Node {
		return lowerNodes(Tag("main")()(
			WithKey("player", Tag("div")(Opaque, Name("data-controller")("player"))(Text(body))),
		))
	}
	if got := vdom.Diff(build("first"), build("second")); len(got) != 0 {
		t.Fatalf("opaque keyed child must freeze, got %+v", got)
	}
}

// Opaque is an internal construction directive, not an HTML attribute, so
// it never reaches the rendered output — unlike data-domi-key, which the
// client reads and which stays in the markup.
func TestOpaqueNotRendered(t *testing.T) {
	html := vdom.Render(lowerOneNode(Tag("ul")()(
		WithKey("a", Tag("li")(Opaque, Name("class")("widget"))(Text("x"))),
	)))
	if strings.Contains(html, "opaque") {
		t.Fatalf("internal opaque marker leaked into HTML: %q", html)
	}
	if !strings.Contains(html, `data-domi-key="a"`) {
		t.Fatalf("keyed child should still render its key: %q", html)
	}
}

// An opaque node placed positionally rather than as a keyed child panics,
// so the safety property can't be requested where the differ can't honor
// it. The panic surfaces here at the Tag construction that introduced
// the violation.
func TestOpaquePositionalPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for opaque non-keyed node")
		}
	}()
	_ = lowerOneNode(Tag("section")()(Tag("div")(Opaque)(Text("x"))))
}

// The opaque check runs at construction, not lowering, so the panic's
// stack trace points at the Tag call holding the misplaced child —
// whether the child arrives as a finished element, a childless Element
// builder, inside a Fragment, or with Opaque tucked into a Group.
func TestOpaquePositionalPanicsAtConstruction(t *testing.T) {
	for name, build := range map[string]func(){
		"element":  func() { Tag("section")()(Tag("div")(Opaque)(Text("x"))) },
		"builder":  func() { Tag("section")()(Tag("div")(Opaque)) },
		"fragment": func() { Tag("section")()(Fragment(Tag("div")(Opaque)(Text("x")))) },
		"grouped":  func() { Tag("section")()(Tag("div")(Group(Opaque))()) },
	} {
		func() {
			defer func() {
				if r := recover(); r == nil {
					t.Fatalf("%s: expected construction-time panic for opaque positional child", name)
				}
			}()
			build()
		}()
	}
}
