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

// ---- Raw tests ----

func TestRawRendersVerbatim(t *testing.T) {
	got := vdom.Render(lowerOne(Raw("<b>hi</b>")))
	want := "<b>hi</b>"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestRawInElement(t *testing.T) {
	got := vdom.Render(lowerOne(Tag("div")()(Raw("<b>hi</b>"))))
	want := "<div><b>hi</b></div>"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestRawInFragment(t *testing.T) {
	got := lower(Fragment(Text("a"), Raw("<b>hi</b>"), Text("c")))
	if len(got) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(got))
	}
	if vdom.Render(got[1]) != "<b>hi</b>" {
		t.Fatalf("Raw in Fragment should render verbatim: got %q", vdom.Render(got[1]))
	}
}

func TestRawTransparentToDiff(t *testing.T) {
	a := lower(Tag("div")()(Raw("<b>hi</b>")))
	b := lower(Tag("div")()(Raw("<b>hi</b>")))
	if got := vdom.Diff(a, b); len(got) != 0 {
		t.Fatalf("identical Raw should produce no patches: got %+v", got)
	}
}

func TestRawDiffProducesReplace(t *testing.T) {
	a := lower(Tag("div")()(Raw("<b>hi</b>")))
	b := lower(Tag("div")()(Raw("<i>bye</i>")))
	got := vdom.Diff(a, b)
	if len(got) != 1 {
		t.Fatalf("expected 1 patch, got %d: %+v", len(got), got)
	}
}

func TestRawSVG(t *testing.T) {
	svg := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path d="M12 2L2 22h20z"></path></svg>`
	got := vdom.Render(lowerOne(Raw(svg)))
	if got != svg {
		t.Fatalf("SVG should render verbatim:\n got: %q\nwant: %q", got, svg)
	}
}

func TestRawPureText(t *testing.T) {
	// Raw with no markup is valid — it's a single text node.
	got := vdom.Render(lowerOne(Raw("hello world")))
	if got != "hello world" {
		t.Fatalf("got %q, want %q", got, "hello world")
	}
}

func TestRawVoidElement(t *testing.T) {
	got := vdom.Render(lowerOne(Raw("<br>")))
	if got != "<br>" {
		t.Fatalf("got %q, want %q", got, "<br>")
	}
}

// ---- Raw validation tests ----

func TestRawPanicsOnEmpty(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for empty Raw")
		}
	}()
	Raw("")
}

func TestRawPanicsOnMultipleElements(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for multiple elements")
		}
	}()
	Raw("<b>a</b><i>b</i>")
}

func TestRawPanicsOnLeadingText(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for leading text")
		}
	}()
	Raw("hello<b>world</b>")
}

func TestRawPanicsOnTrailingText(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for trailing text")
		}
	}()
	Raw("<b>hello</b>world")
}

func TestRawAcceptsAutoClosedTag(t *testing.T) {
	// The HTML5 parser auto-closes unclosed tags, producing one node.
	Raw("<div>hello")
}

func TestRawPanicsOnVoidWithTrailing(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for void element with trailing content")
		}
	}()
	Raw("<br><br>")
}

func TestRawAcceptsNestedSameTag(t *testing.T) {
	// Should not panic — validator tracks depth for the root tag.
	Raw("<div><div>inner</div></div>")
}

func TestRawAcceptsBareAngleBracketInContent(t *testing.T) {
	// Bare '<' inside an element is valid (the HTML parser handles it).
	Raw("<div>5 < 10</div>")
}

func TestRawAcceptsAttributeWithAngleBracket(t *testing.T) {
	Raw(`<div data-x="a>b">content</div>`)
}

func TestRawAcceptsComment(t *testing.T) {
	Raw("<div><!-- comment --></div>")
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
