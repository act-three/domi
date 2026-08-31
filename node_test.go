package domi

import (
	"fmt"
	"strings"
	"testing"

	"ily.dev/domi/internal/vdom"
)

// lowerOneNode and lowerNodes lower test nodes from the root address,
// dropping the harvested handlers that these structural tests don't
// exercise. They keep the call sites reading the way they did before
// lowering grew an address parameter and a second return value.
func lowerOneNode(n Node) vdom.Node {
	return lowerNodes(n)[0]
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
	a := lowerOneNode(Tag("div")(Fragment(Fragment(Text("a"), Text("b")), Text("c"))))
	b := lowerOneNode(Tag("div")(Text("a"), Text("b"), Text("c")))
	if vdom.Render(a) != vdom.Render(b) {
		t.Fatalf("nested Fragment should flatten: %q vs %q", vdom.Render(a), vdom.Render(b))
	}
}

func TestFragmentEmptyContributesNothing(t *testing.T) {
	a := lowerOneNode(Tag("div")(Fragment(), Text("x")))
	b := lowerOneNode(Tag("div")(Text("x")))
	if vdom.Render(a) != vdom.Render(b) {
		t.Fatalf("empty Fragment should contribute nothing: %q vs %q", vdom.Render(a), vdom.Render(b))
	}
}

func TestFragmentPreservesSiblingOrder(t *testing.T) {
	a := lowerOneNode(Tag("div")(
		Text("a"),
		Fragment(Text("b"), Text("c")),
		Text("d"),
	))
	b := lowerOneNode(Tag("div")(Text("a"), Text("b"), Text("c"), Text("d")))
	if vdom.Render(a) != vdom.Render(b) {
		t.Fatalf("Fragment children should appear in position: %q vs %q", vdom.Render(a), vdom.Render(b))
	}
}

func TestFragmentIsTransparentToDiff(t *testing.T) {
	a := lowerNodes(Tag("div")(Fragment(Text("a"), Text("b"))))
	b := lowerNodes(Tag("div")(Text("a"), Text("b")))
	if got := vdom.Diff(a, b); len(got) != 0 {
		t.Fatalf("Fragment-wrapped should diff identically: got %+v", got)
	}
}

func TestFragmentAtRootLowers(t *testing.T) {
	// A Fragment returned from App.View becomes the mount's children.
	got := lowerNodes(Fragment(Tag("div")(Text("a")), Tag("span")(Text("b"))))
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
		rows[i] = WithKey(k, Tag("li")(Text(k)))
	}
	return Fragment(rows...)
}

// Keyed children compose with unkeyed siblings in one parent: they
// render in place, carrying their keys, between the unkeyed header
// and footer.
func TestWithKeyMixesWithUnkeyedSiblings(t *testing.T) {
	got := vdom.Render(lowerOneNode(Tag("ul")(
		Tag("li")(Text("header")),
		keyedLis("a", "b"),
		Tag("li")(Text("footer")),
	)))
	want := `<ul><li>header</li><li domi-key="a">a</li><li domi-key="b">b</li><li>footer</li></ul>`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// WithKey accepts a childless [Element] builder just as a child list
// does, applying it to a finished element.
func TestWithKeyAppliesElementBuilder(t *testing.T) {
	a := vdom.Render(lowerOneNode(Tag("ul")(WithKey("a", Tag("li", Name("class", "x"))))))
	b := vdom.Render(lowerOneNode(Tag("ul")(WithKey("a", Tag("li", Name("class", "x"))()))))
	if a != b {
		t.Fatalf("builder and element should key identically: %q vs %q", a, b)
	}
}

// Appending to the keyed run of a mixed list is a single patch: the
// unkeyed header and footer are matched in place, not rebuilt.
func TestWithKeyAppendIsSinglePatch(t *testing.T) {
	view := func(keys ...string) []vdom.Node {
		return lowerNodes(Tag("ul")(
			Tag("li")(Text("header")),
			keyedLis(keys...),
			Tag("li")(Text("footer")),
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
	if html := vdom.Render(got[0]); html != `<li domi-key="a">a</li>` {
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

// A Fragment consisting of one element is that element for keying
// purposes, so a keyed view assembled through Fragment (or MapNode) keys
// identically to the bare element.
func TestWithKeySingleElementFragment(t *testing.T) {
	a := vdom.Render(lowerOneNode(Tag("ul")(WithKey("a", Fragment(nil, Tag("li")(Text("x")), Fragment())))))
	b := vdom.Render(lowerOneNode(Tag("ul")(WithKey("a", Tag("li")(Text("x"))))))
	if a != b {
		t.Fatalf("fragment and element should key identically: %q vs %q", a, b)
	}
}

// Keying a MapNode keeps its handler rewriting: the key names the element
// and the mapper still applies to its harvest.
func TestWithKeyMapNode(t *testing.T) {
	li := MapNode(func(s string) int { return len(s) }, Tag("li", On("click", msgFn("go"))))
	_, h := lower(0, Tag("ul")(WithKey("a", li)))
	for _, fn := range typed[int](h) {
		got, err := fn(nil)
		if err != nil {
			t.Fatal(err)
		}
		if got != 2 {
			t.Fatalf("got %d, want 2", got)
		}
	}
}

// A Fragment of several nodes has no single identity to key; WithKey
// panics at construction.
func TestWithKeySeveralNodesPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for a keyed Fragment of several nodes")
		}
	}()
	_ = WithKey("a", Fragment(Tag("li")(Text("x")), Tag("li")(Text("y"))))
}

// Only elements carry keys; a text node panics at construction.
func TestWithKeyTextPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for a keyed text node")
		}
	}()
	_ = WithKey("a", Text("x"))
}

// Re-keying a keyed node is a construction error, not a silent
// override.
func TestWithKeyTwicePanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for a doubly keyed child")
		}
	}()
	_ = WithKey("b", WithKey("a", Tag("li")))
}

// A void element serializes without children, so children provided to
// one would live only in the server's vdom tree and desync the
// instance once they change. The Element panics at construction, where
// the stack points at the offending call site, rather than letting
// the divergence go latent until a render.
func TestVoidElementWithChildrenPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for children of a void element")
		}
	}()
	_ = Tag("input")(Text("boom"))
}

// The void-element check counts contributed nodes, not arguments:
// nils and empty Fragments give the element nothing, so passing them
// is not the mistake the panic guards against.
func TestVoidElementWithNoRealChildren(t *testing.T) {
	for _, tt := range []struct {
		name     string
		children []Node
	}{
		{"none", nil},
		{"nil", []Node{nil}},
		{"empty fragment", []Node{Fragment()}},
		{"nested nothing", []Node{Fragment(nil, Fragment(Fragment(), nil))}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := vdom.Render(lowerOneNode(Tag("input")(tt.children...)))
			if want := vdom.Render(lowerOneNode(Tag("input"))); got != want {
				t.Fatalf("got %q, want %q", got, want)
			}
		})
	}
}

// An empty text node is still a child: it lives in the vdom tree even
// though it renders as nothing, so a void element rejects it.
func TestVoidElementWithEmptyTextPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for an empty text child of a void element")
		}
	}()
	_ = Tag("input")(Text(""))
}

// ---- nil Node tests ----
//
// A nil Node is the empty Fragment's degenerate twin: it lowers to
// nothing wherever a Node is accepted, so conditional content can be a
// node-or-nil with no guard at the use site.

func TestNilNodeContributesNothing(t *testing.T) {
	a := lowerOneNode(Tag("div")(Text("a"), nil, Text("b")))
	b := lowerOneNode(Tag("div")(Text("a"), Text("b")))
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

// Group is the attr-side mirror of Fragment. The tests below pin the
// same property through the observable contract: a Group should be
// indistinguishable from writing its attrs inline at the use site.

func TestGroupNestedFlattens(t *testing.T) {
	a := lowerOneNode(Tag("div", Group(Group(Name("class", "a"), Name("id", "x")), Name("data-x", "1")))())
	b := lowerOneNode(Tag("div", Name("class", "a"), Name("id", "x"), Name("data-x", "1"))())
	if vdom.Render(a) != vdom.Render(b) {
		t.Fatalf("nested Group should flatten: %q vs %q", vdom.Render(a), vdom.Render(b))
	}
}

func TestGroupEmptyContributesNothing(t *testing.T) {
	a := lowerOneNode(Tag("div", Group(), Name("id", "x"))())
	b := lowerOneNode(Tag("div", Name("id", "x"))())
	if vdom.Render(a) != vdom.Render(b) {
		t.Fatalf("empty Group should contribute nothing: %q vs %q", vdom.Render(a), vdom.Render(b))
	}
}

// A nil Attr is the empty Group's degenerate twin: it lowers to nothing
// wherever an Attr is accepted, so conditional attributes can be an
// attr-or-nil with no guard at the use site.
func TestNilAttrContributesNothing(t *testing.T) {
	a := lowerOneNode(Tag("div", Name("class", "a"), nil, Name("id", "x"))())
	b := lowerOneNode(Tag("div", Name("class", "a"), Name("id", "x"))())
	if vdom.Render(a) != vdom.Render(b) {
		t.Fatalf("nil attr should contribute nothing: %q vs %q", vdom.Render(a), vdom.Render(b))
	}
}

func TestGroupPreservesAttrOrder(t *testing.T) {
	a := lowerOneNode(Tag("div",
		Name("id", "x"),
		Group(Name("class", "a"), Name("data-y", "1")),
		Name("data-z", "2"))())
	b := lowerOneNode(Tag("div",
		Name("id", "x"),
		Name("class", "a"),
		Name("data-y", "1"),
		Name("data-z", "2"))())
	if vdom.Render(a) != vdom.Render(b) {
		t.Fatalf("Group attrs should appear in position: %q vs %q", vdom.Render(a), vdom.Render(b))
	}
}

// Combining (e.g. class joining) is a property of the lowered list, so
// a Group of duplicate classes should combine with a sibling Class just
// like inline duplicates do.
func TestGroupClassCombinesAcrossBoundary(t *testing.T) {
	a := lowerOneNode(Tag("div", Group(Name("class", "a"), Name("class", "b")), Name("class", "c"))())
	b := lowerOneNode(Tag("div", Name("class", "a"), Name("class", "b"), Name("class", "c"))())
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
	got := vdom.Render(lowerOneNode(Tag("div", Name("class", "a"), Name("class", "b"))()))
	want := `<div class="a b"></div>`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestCombineStyleWithSemicolon(t *testing.T) {
	got := vdom.Render(lowerOneNode(Tag("div", Name("style", "color:red"), Name("style", "font-weight:bold"))()))
	want := `<div style="color:red;font-weight:bold"></div>`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// Handler attrs combine with a comma: two handlers for one event on
// one element join into a single domi-msg attribute. The attrs come
// from On — domi-msg names are reserved, so apps cannot spell them.
func TestCombineMsgWithComma(t *testing.T) {
	got := vdom.Render(lowerOneNode(Tag("div", On("click", msgFn("h1")), On("click", msgFn("h2")))()))
	const marker = `domi-msg-click="`
	i := strings.Index(got, marker)
	if i < 0 {
		t.Fatalf("no handler attr in render %q", got)
	}
	value := got[i+len(marker):]
	value = value[:strings.IndexByte(value, '"')]
	if strings.Count(value, ",") != 1 {
		t.Fatalf("two handlers should join with one comma: %q", value)
	}
}

func TestCombineDistinctEventsKeepBoth(t *testing.T) {
	rendered := vdom.Render(lowerOneNode(Tag("div", On("click", msgFn("h1")), On("submit", msgFn("h2")))()))
	if !strings.Contains(rendered, "domi-msg-click") || !strings.Contains(rendered, "domi-msg-submit") {
		t.Fatalf("distinct event attrs should both appear: %q", rendered)
	}
}

func TestCombineSingleMsgNoComma(t *testing.T) {
	rendered := vdom.Render(lowerOneNode(Tag("div", On("click", msgFn("h1")))()))
	if strings.Contains(rendered, ",") {
		t.Fatalf("single handler should have no comma: %q", rendered)
	}
}

func TestCombineOtherAttrFirstWins(t *testing.T) {
	got := vdom.Render(lowerOneNode(Tag("div", Name("id", "first"), Name("id", "second"))()))
	want := `<div id="first"></div>`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestCombineClassEmptyGuard(t *testing.T) {
	got := vdom.Render(lowerOneNode(Tag("div", Name("class", ""), Name("class", "b"))()))
	want := `<div class="b"></div>`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestCombineClassAllEmptyOmitted(t *testing.T) {
	got := vdom.Render(lowerOneNode(Tag("div", Name("class", "", ""))()))
	want := `<div></div>`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestCombineClassLoneEmptyOmitted(t *testing.T) {
	got := vdom.Render(lowerOneNode(Tag("div", Name("class"))()))
	want := `<div></div>`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestRegisterCombining(t *testing.T) {
	RegisterCombining("data-x", ":")
	got := vdom.Render(lowerOneNode(Tag("div", Name("data-x", "a"), Name("data-x", "b"), Name("data-x", "c"))()))
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
	got := vdom.Render(lowerOneNode(Tag("div", Name("class", "a", "b"))()))
	want := `<div class="a b"></div>`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestNameVariadicStyle(t *testing.T) {
	got := vdom.Render(lowerOneNode(Tag("div", Name("style", "color:red", "font-weight:bold"))()))
	want := `<div style="color:red;font-weight:bold"></div>`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// Name is fully general.
// It can represent a boolean attribute in its name-only form.
func TestNameZeroArgBare(t *testing.T) {
	got := vdom.Render(lowerOneNode(Tag("div", Name("disabled"))()))
	want := `<div disabled></div>`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestNameVariadicFirstWins(t *testing.T) {
	got := vdom.Render(lowerOneNode(Tag("div", Name("id", "first", "second"))()))
	want := `<div id="first"></div>`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// ---- name validity tests ----
//
// A name is valid only as the browser's parser would produce it.
// See comments in names.go.

func TestTagInvalidNamePanics(t *testing.T) {
	// "1x" and "-x" have valid characters but the tokenizer only
	// recognizes a start tag when the character after < is a letter,
	// so they would render as text, not an element.
	for _, name := range []string{"DIV", "BR", "", "a b", "a>b", `a"b`, "1x", "-x", "a?b", "a\xff"} {
		func() {
			defer func() {
				if recover() == nil {
					t.Fatalf("expected panic for invalid tag name %q", name)
				}
			}()
			Tag(name)
		}()
	}
	for _, name := range []string{"div", "clipPath", "foreignObject", "my-element"} {
		Tag(name) // valid; must not panic
	}
}

func TestNameInvalidNamePanics(t *testing.T) {
	for _, name := range []string{"CLASS", "DOMI-KEY", "", "a b", "a=b", `a"b`, "a/b", "1x", "-x", "a?b", "a`b", "a\xff"} {
		func() {
			defer func() {
				if recover() == nil {
					t.Fatalf("expected panic for invalid attribute name %q", name)
				}
			}()
			Name(name)
		}()
	}
	for _, name := range []string{"class", "viewBox", "xlink:href", "definitionURL", "data-x", "_x"} {
		Name(name) // valid; must not panic
	}
}

// The vendored foreign-content tables must match the parser: for
// every canonical spelling domi accepts, parsing its lowercase form
// in foreign content restores exactly that spelling. This pins
// domi's copy of the case-adjustment tables to the x/net/html parser
// UnsafeParseRaw uses — and, both implementing the HTML standard's
// tables, to the browser's.
func TestForeignNamesMatchParser(t *testing.T) {
	for name := range foreignAttrNames {
		container := "svg"
		if name == "definitionURL" {
			container = "math"
		}
		src := fmt.Sprintf(`<%s %s="x"></%s>`, container, strings.ToLower(name), container)
		if got := renderParsed(t, src); !strings.Contains(got, name+`="x"`) {
			t.Errorf("parser does not restore attribute %s: %q", name, got)
		}
	}
	for name := range foreignTagNames {
		src := fmt.Sprintf(`<svg><%s></%s></svg>`, strings.ToLower(name), strings.ToLower(name))
		if got := renderParsed(t, src); !strings.Contains(got, "<"+name) {
			t.Errorf("parser does not restore tag <%s>: %q", name, got)
		}
	}
}

// The domi- tag namespace is reserved for domi's own elements, like
// the domi-root mount. Tag panics at construction, where the panic
// points at the offending call.
func TestTagReservedPanics(t *testing.T) {
	for _, name := range []string{"domi-root", "domi-anything"} {
		func() {
			defer func() {
				if recover() == nil {
					t.Fatalf("expected panic for the reserved tag %s", name)
				}
			}()
			Tag(name)
		}()
	}
}

// The domi- attribute namespace is owned by domi: apart from the
// attributes domi defines for app use, like domi-handle, every domi-
// name is reserved — the client trusts them as framework state, and
// an app-supplied domi-key, for one, would let an unkeyed
// element masquerade as keyed. Name panics at construction, where the
// panic points at the offending call.
func TestNameReservedAttrPanics(t *testing.T) {
	for _, name := range []string{"domi-key", "domi-anything"} {
		func() {
			defer func() {
				if recover() == nil {
					t.Fatalf("expected panic for the reserved attribute %s", name)
				}
			}()
			Name(name)
		}()
	}
	Name("domi-handle", "yes") // defined for app use, not reserved; must not panic
}

func TestHandleLink(t *testing.T) {
	for _, policy := range []string{"yes", "same-origin", "no"} {
		got := renderTree(t, Tag("a", HandleLink(policy))(Text("go")))
		want := `<a domi-handle="` + policy + `">go</a>`
		if got != want {
			t.Errorf("HandleLink(%q) = %q, want %q", policy, got, want)
		}
	}
	defer func() {
		if recover() == nil {
			t.Fatal("HandleLink accepted an unsupported policy")
		}
	}()
	HandleLink("sometimes")
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

// Raw text under script renders verbatim, so an end-tag sequence in
// it would terminate the element during HTML parsing and let the rest
// of the string parse as markup the virtual DOM does not contain.
// Lowering panics rather than building a tree that cannot serialize
// faithfully.
func TestScriptTextBreakoutPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for an end-tag sequence in script text")
		}
	}()
	_ = lowerOneNode(Tag("script")(Text(`</script><img src=x onerror=alert(1)>`)))
}

// Splitting the payload across text children doesn't dodge the check:
// adjacent text coalesces before validation, exactly as the browser's
// parser would merge it.
func TestScriptSplitTextBreakoutPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for a split end-tag sequence in script text")
		}
	}()
	_ = lowerOneNode(Tag("script")(Text("var x=1;"), Text("</script><img src=x onerror=alert(1)>")))
}

// Script text that opens a comment around "<script" can parse to
// text containing an end-tag sequence, or — truncated — to text
// whose serialized closing tag would not end the element.
// CheckRawText rejects both conservatively, so UnsafeParseRaw returns
// an error rather than panicking at lowering.
func TestUnsafeParseRawRejectsCommentedScript(t *testing.T) {
	for _, src := range []string{
		"<script><!--<script>var a = '</script>';//--></script>",
		"<script><!--<script>",
	} {
		if _, err := UnsafeParseRaw(src); err == nil {
			t.Fatalf("UnsafeParseRaw(%q): expected error for commented script text", src)
		}
	}
}

// In foreign content script is an ordinary element, so the parser can
// hand back shapes HTML content never produces: element children, or
// "&lt;/script&gt;" entity-decoded into text that would end the
// element when written back verbatim. Both are rejected — including
// an end-tag sequence assembled across a comment, which contributes
// nothing to the rendered output and so no longer separates the text
// around it.
func TestUnsafeParseRawRejectsForeignScriptShapes(t *testing.T) {
	for _, src := range []string{
		`<svg><script>a<rect></rect>b</script></svg>`,
		`<svg><script>&lt;/script&gt;</script></svg>`,
		`<svg><script>&lt;/scr<!--c-->ipt&gt;</script></svg>`,
	} {
		if _, err := UnsafeParseRaw(src); err == nil {
			t.Fatalf("UnsafeParseRaw(%q): expected error for foreign-content script", src)
		}
	}
}

// A comment inside foreign-content script is dropped like comments
// everywhere else, and the raw-text validation sees the text as
// lowering will: nils gone and the surrounding text joined — the
// same coalescing NewElement applies.
func TestUnsafeParseRawForeignScriptDropsComments(t *testing.T) {
	if got := renderParsed(t, `<svg><script>a<!--c-->b</script></svg>`); got != `<svg><script>ab</script></svg>` {
		t.Fatalf("got %q", got)
	}
	if got := renderParsed(t, `<svg><script><!--c--></script></svg>`); got != `<svg><script></script></svg>` {
		t.Fatalf("got %q", got)
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

// A reserved domi- attribute is rejected wherever it appears in the
// input: parsed markup cannot make an element masquerade as keyed, or
// carry any other forged framework state. The empty-valued spelling
// is rejected too — the client tests keyedness by the attribute's
// presence. The attributes domi defines for app use, like
// domi-handle, parse as an ordinary attribute.
func TestUnsafeParseRawRejectsReservedAttr(t *testing.T) {
	for _, src := range []string{
		`<li domi-key="a">a</li>`,
		`<div><span domi-key="">x</span></div>`,
		`<div domi-anything="x">x</div>`,
	} {
		if _, err := UnsafeParseRaw(src); err == nil {
			t.Fatalf("UnsafeParseRaw(%q): expected error for a reserved attribute", src)
		}
	}
	if got := renderParsed(t, `<a domi-handle="yes" href="/x">out</a>`); got != `<a domi-handle="yes" href="/x">out</a>` {
		t.Fatalf("domi-handle should parse: got %q", got)
	}
}

// The HTML parser lowercases and case-adjusts names, so a case slip
// never reaches parse validation — but it emits attribute and tag
// names containing quotes and the like verbatim from hostile markup,
// and those are rejected rather than re-serialized.
func TestUnsafeParseRawRejectsInvalidNames(t *testing.T) {
	for _, src := range []string{
		`<div a"b="x">y</div>`,
		`<a"b>y</a"b>`,
		`<div><span c'd="e">y</span></div>`,
	} {
		if _, err := UnsafeParseRaw(src); err == nil {
			t.Fatalf("UnsafeParseRaw(%q): expected error for an invalid name", src)
		}
	}
}

// A reserved domi- tag is rejected wherever it appears in the input,
// matching Tag's construction-time panic.
func TestUnsafeParseRawRejectsReservedTag(t *testing.T) {
	for _, src := range []string{
		`<domi-root>x</domi-root>`,
		`<div><domi-anything>x</domi-anything></div>`,
	} {
		if _, err := UnsafeParseRaw(src); err == nil {
			t.Fatalf("UnsafeParseRaw(%q): expected error for a reserved tag", src)
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

// ---- Opaque tests ----

// WithKeyOpaque marks a keyed child client-owned: the framework freezes it and
// its subtree, emitting no patches even as the contents change. This pins
// the public WithKeyOpaque constructor to the differ's freeze behavior end to end.
func TestOpaqueKeyedChildFreezes(t *testing.T) {
	build := func(body string) []vdom.Node {
		return lowerNodes(Tag("main")(
			WithKeyOpaque("player", Tag("div", Name("data-controller", "player"))(Text(body))),
		))
	}
	if got := vdom.Diff(build("first"), build("second")); len(got) != 0 {
		t.Fatalf("opaque keyed child must freeze, got %+v", got)
	}
}

// Opacity is mirrored into the domi-opaque marker attribute the way the
// key is mirrored into domi-key: the client reads it to recognize
// app-owned subtrees in the live DOM and keep its hands off the state
// inside them. A merely keyed child carries no such marker.
func TestOpaqueRendered(t *testing.T) {
	html := vdom.Render(lowerOneNode(Tag("ul")(
		WithKeyOpaque("a", Tag("li", Name("class", "widget"))(Text("x"))),
		WithKey("b", Tag("li")(Text("y"))),
	)))
	if !strings.Contains(html, "domi-opaque") {
		t.Fatalf("opaque child should render its marker: %q", html)
	}
	if !strings.Contains(html, `domi-key="a"`) {
		t.Fatalf("keyed child should still render its key: %q", html)
	}
	if strings.Contains(html, `<li domi-key="b" domi-opaque`) {
		t.Fatalf("keyed-but-not-opaque child must not carry the marker: %q", html)
	}
}

// WithKeyOpaque shares WithKey's construction rules, including the
// re-keying panic — in either order of the two constructors.
func TestWithKeyOpaqueTwicePanics(t *testing.T) {
	for name, build := range map[string]func(){
		"opaque then keyed": func() { WithKey("b", WithKeyOpaque("a", Tag("li"))) },
		"keyed then opaque": func() { WithKeyOpaque("b", WithKey("a", Tag("li"))) },
	} {
		func() {
			defer func() {
				if r := recover(); r == nil {
					t.Fatalf("%s: expected panic for a doubly keyed child", name)
				}
			}()
			build()
		}()
	}
}
