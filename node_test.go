package domi

import (
	"strings"
	"testing"

	"ily.dev/domi/internal/vdom"
)

type tMsg struct {
	Tag string `json:"t"`
}

// Two On() calls with the same Msg value share a registry slot because
// the hash is content-addressable.
func TestHandlerHashIsContentAddressable(t *testing.T) {
	a := On("click")(tMsg{"x"}).(attr)
	b := On("click")(tMsg{"x"}).(attr)
	if a.Value != b.Value {
		t.Fatalf("identical Msgs should produce identical attr values; got %q vs %q", a.Value, b.Value)
	}
}

// Fragment is supposed to be indistinguishable from writing its children
// inline at the use site. The tests below pin that property through the
// observable contract — rendered HTML and emitted diffs against the
// inline-equivalent tree.

func TestFragmentNestedFlattens(t *testing.T) {
	a := lowerOne(Tag("div")()(Fragment(Fragment(Text("a"), Text("b")), Text("c"))))
	b := lowerOne(Tag("div")()(Text("a"), Text("b"), Text("c")))
	if vdom.Render(a) != vdom.Render(b) {
		t.Fatalf("nested Fragment should flatten: %q vs %q", vdom.Render(a), vdom.Render(b))
	}
}

func TestFragmentEmptyContributesNothing(t *testing.T) {
	a := lowerOne(Tag("div")()(Fragment(), Text("x")))
	b := lowerOne(Tag("div")()(Text("x")))
	if vdom.Render(a) != vdom.Render(b) {
		t.Fatalf("empty Fragment should contribute nothing: %q vs %q", vdom.Render(a), vdom.Render(b))
	}
}

func TestFragmentPreservesSiblingOrder(t *testing.T) {
	a := lowerOne(Tag("div")()(
		Text("a"),
		Fragment(Text("b"), Text("c")),
		Text("d"),
	))
	b := lowerOne(Tag("div")()(Text("a"), Text("b"), Text("c"), Text("d")))
	if vdom.Render(a) != vdom.Render(b) {
		t.Fatalf("Fragment children should appear in position: %q vs %q", vdom.Render(a), vdom.Render(b))
	}
}

func TestFragmentIsTransparentToDiff(t *testing.T) {
	a := lower(Tag("div")()(Fragment(Text("a"), Text("b"))))
	b := lower(Tag("div")()(Text("a"), Text("b")))
	if got := vdom.Diff(a, b); len(got) != 0 {
		t.Fatalf("Fragment-wrapped should diff identically: got %+v", got)
	}
}

func TestFragmentAtRootLowers(t *testing.T) {
	// A Fragment returned from App.View becomes the mount's children.
	got := lower(Fragment(Tag("div")()(Text("a")), Tag("span")()(Text("b"))))
	if len(got) != 2 {
		t.Fatalf("expected 2 lowered nodes from Fragment root, got %d: %+v", len(got), got)
	}
	if vdom.Render(got[0]) != "<div>a</div>" || vdom.Render(got[1]) != "<span>b</span>" {
		t.Fatalf("Fragment children should lower in order: %q, %q",
			vdom.Render(got[0]), vdom.Render(got[1]))
	}
}

func TestFragmentInKeyedPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("expected panic for Fragment yielded into Keyed, got none")
		}
	}()
	_ = Keyed("ul")()(func(yield func(string, Node) bool) {
		yield("a", Fragment(Tag("li")()(Text("x"))))
	})
}

// Group is the attr-side mirror of Fragment. The tests below pin the
// same property through the observable contract: a Group should be
// indistinguishable from writing its attrs inline at the use site.

func TestGroupNestedFlattens(t *testing.T) {
	a := lowerOne(Tag("div")(Group(Group(Name("class")("a"), Name("id")("x")), Name("data-x")("1")))())
	b := lowerOne(Tag("div")(Name("class")("a"), Name("id")("x"), Name("data-x")("1"))())
	if vdom.Render(a) != vdom.Render(b) {
		t.Fatalf("nested Group should flatten: %q vs %q", vdom.Render(a), vdom.Render(b))
	}
}

func TestGroupEmptyContributesNothing(t *testing.T) {
	a := lowerOne(Tag("div")(Group(), Name("id")("x"))())
	b := lowerOne(Tag("div")(Name("id")("x"))())
	if vdom.Render(a) != vdom.Render(b) {
		t.Fatalf("empty Group should contribute nothing: %q vs %q", vdom.Render(a), vdom.Render(b))
	}
}

func TestGroupPreservesAttrOrder(t *testing.T) {
	a := lowerOne(Tag("div")(
		Name("id")("x"),
		Group(Name("class")("a"), Name("data-y")("1")),
		Name("data-z")("2"),
	)())
	b := lowerOne(Tag("div")(
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
	a := lowerOne(Tag("div")(Group(Name("class")("a"), Name("class")("b")), Name("class")("c"))())
	b := lowerOne(Tag("div")(Name("class")("a"), Name("class")("b"), Name("class")("c"))())
	if vdom.Render(a) != vdom.Render(b) {
		t.Fatalf("Group-of-classes should combine like inline: %q vs %q", vdom.Render(a), vdom.Render(b))
	}
}

// Group works in Keyed's attrs slot for the same reason it works in
// Tag's — both lower attrs at construction via the same path.
func TestGroupInKeyedAttrs(t *testing.T) {
	a := lowerOne(Keyed("ul")(Group(Name("class")("a"), Name("id")("x")))(func(yield func(string, Node) bool) {}))
	b := lowerOne(Keyed("ul")(Name("class")("a"), Name("id")("x"))(func(yield func(string, Node) bool) {}))
	if vdom.Render(a) != vdom.Render(b) {
		t.Fatalf("Group in Keyed attrs should flatten: %q vs %q", vdom.Render(a), vdom.Render(b))
	}
}

// ---- attribute combining tests ----
//
// NewElement normalizes attrs at construction, so duplicate names are
// resolved before the renderer or differ ever sees them. These tests
// exercise the observable contract through Tag → Render.

func TestCombineClassWithSpace(t *testing.T) {
	got := vdom.Render(lowerOne(Tag("div")(Name("class")("a"), Name("class")("b"))()))
	want := `<div class="a b"></div>`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestCombineStyleWithSemicolon(t *testing.T) {
	got := vdom.Render(lowerOne(Tag("div")(Name("style")("color:red"), Name("style")("font-weight:bold"))()))
	want := `<div style="color:red;font-weight:bold"></div>`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestCombineDataMsgWithComma(t *testing.T) {
	got := vdom.Render(lowerOne(Tag("div")(Name("data-msg-click")("h1"), Name("data-msg-click")("h2"))()))
	want := `<div data-msg-click="h1,h2"></div>`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestCombineDistinctEventsKeepBoth(t *testing.T) {
	rendered := vdom.Render(lowerOne(Tag("div")(Name("data-msg-click")("h1"), Name("data-msg-submit")("h2"))()))
	if !strings.Contains(rendered, "data-msg-click") || !strings.Contains(rendered, "data-msg-submit") {
		t.Fatalf("distinct event attrs should both appear: %q", rendered)
	}
}

func TestCombineSingleDataMsgNoComma(t *testing.T) {
	rendered := vdom.Render(lowerOne(Tag("div")(Name("data-msg-click")("h1"))()))
	if strings.Contains(rendered, ",") {
		t.Fatalf("single data-msg should have no comma: %q", rendered)
	}
}

func TestCombineOtherAttrFirstWins(t *testing.T) {
	got := vdom.Render(lowerOne(Tag("div")(Name("id")("first"), Name("id")("second"))()))
	want := `<div id="first"></div>`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestCombineClassEmptyGuard(t *testing.T) {
	got := vdom.Render(lowerOne(Tag("div")(Name("class")(""), Name("class")("b"))()))
	want := `<div class="b"></div>`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestRegisterCombining(t *testing.T) {
	RegisterCombining("data-x", ":")
	got := vdom.Render(lowerOne(Tag("div")(Name("data-x")("a"), Name("data-x")("b"), Name("data-x")("c"))()))
	want := `<div data-x="a:b:c"></div>`
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
	for _, ln := range lower(n) {
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
	if got := len(lower(n)); got != 3 {
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
	if got := len(lower(n)); got != 0 {
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
	if got := len(lower(n)); got != 0 {
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

// A tree built with domi's own constructors, rendered to HTML and then
// re-adopted via UnsafeParseRaw, renders identically: parsing faithfully
// round-trips the framework's own output, so a prerendered subtree and
// its live-built equivalent converge.
func TestUnsafeParseRawRoundTripsRenderedTree(t *testing.T) {
	tree := Tag("div")(Name("class")("card"))(
		Tag("h1")()(Text("Title & co")),
		Tag("p")()(Text("a < b"), Tag("br")(), Text("done")),
		Tag("ul")()(
			Tag("li")()(Text("one")),
			Tag("li")()(Text("two")),
		),
	)
	first := renderTree(t, tree)
	if again := renderParsed(t, first); again != first {
		t.Fatalf("round-trip changed output:\n first: %s\n again: %s", first, again)
	}
}

// ---- Bool tests ----

func TestBoolTrueEmitsNameOnly(t *testing.T) {
	got := vdom.Render(lowerOne(Tag("input")(Bool("disabled")(true))()))
	want := `<input disabled>`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestBoolFalseEmitsNothing(t *testing.T) {
	got := vdom.Render(lowerOne(Tag("input")(Bool("disabled")(false))()))
	want := `<input>`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestBoolTrueWithOtherAttrs(t *testing.T) {
	got := vdom.Render(lowerOne(Tag("input")(
		Name("type")("checkbox"),
		Bool("checked")(true),
	)()))
	want := `<input checked type="checkbox">`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestBoolFalseWithOtherAttrs(t *testing.T) {
	got := vdom.Render(lowerOne(Tag("input")(
		Name("type")("checkbox"),
		Bool("checked")(false),
	)()))
	want := `<input type="checkbox">`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestBoolToggleDiffProducesSetAndRemove(t *testing.T) {
	a := lower(Tag("input")(Bool("disabled")(false))())
	b := lower(Tag("input")(Bool("disabled")(true))())

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
	a := lower(Tag("input")(Bool("disabled")(true))())
	b := lower(Tag("input")(Bool("disabled")(true))())
	if ps := vdom.Diff(a, b); len(ps) != 0 {
		t.Fatalf("same value should produce no patches, got %d", len(ps))
	}
}

func TestBoolInGroup(t *testing.T) {
	a := lowerOne(Tag("input")(Group(
		Name("type")("text"),
		Bool("readonly")(true),
	))())
	b := lowerOne(Tag("input")(
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
	got := vdom.Render(lowerOne(Tag("div")(Bool("contenteditable")(true))()))
	want := `<div contenteditable="true"></div>`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestEnumBoolFalseEmitsValueFalse(t *testing.T) {
	got := vdom.Render(lowerOne(Tag("div")(Bool("contenteditable")(false))()))
	want := `<div contenteditable="false"></div>`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestEnumBoolDiffProducesSetAttr(t *testing.T) {
	a := lower(Tag("div")(Bool("spellcheck")(true))())
	b := lower(Tag("div")(Bool("spellcheck")(false))())
	ps := vdom.Diff(a, b)
	if len(ps) != 1 {
		t.Fatalf("true→false: expected 1 patch, got %d", len(ps))
	}
}

func TestEnumBoolSameValueNoDiff(t *testing.T) {
	a := lower(Tag("div")(Bool("draggable")(true))())
	b := lower(Tag("div")(Bool("draggable")(true))())
	if ps := vdom.Diff(a, b); len(ps) != 0 {
		t.Fatalf("same value should produce no patches, got %d", len(ps))
	}
}

func TestEnumBoolAllFour(t *testing.T) {
	for _, name := range []string{"contenteditable", "draggable", "spellcheck", "translate"} {
		got := vdom.Render(lowerOne(Tag("div")(Bool(name)(true))()))
		want := `<div ` + name + `="true"></div>`
		if got != want {
			t.Fatalf("Bool(%q)(true): got %q, want %q", name, got, want)
		}
	}
}

func TestRegularBoolStillUsesPresenceAbsence(t *testing.T) {
	got := vdom.Render(lowerOne(Tag("input")(Bool("disabled")(true))()))
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
		return lower(Keyed("main")()(func(yield func(string, Node) bool) {
			yield("player", Tag("div")(Opaque, Name("data-controller")("player"))(Text(body)))
		}))
	}
	if got := vdom.Diff(build("first"), build("second")); len(got) != 0 {
		t.Fatalf("opaque keyed child must freeze, got %+v", got)
	}
}

// An opaque node placed positionally rather than as a keyed child panics,
// so the safety property can't be requested where the differ can't honor
// it. The panic originates in the vdom layer but surfaces here at the
// Tag construction that introduced the violation.
func TestOpaquePositionalPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for opaque non-keyed node")
		}
	}()
	_ = lowerOne(Tag("section")()(Tag("div")(Opaque)(Text("x"))))
}
