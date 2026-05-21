package domi

import (
	"testing"

	"ily.dev/domi/internal/vdom"
)

type tMsg struct {
	Tag string `json:"t"`
}

// Two On() calls with the same Msg value share a registry slot because
// the hash is content-addressable.
func TestHandlerHashIsContentAddressable(t *testing.T) {
	a := On("click", tMsg{"x"}).(attr)
	b := On("click", tMsg{"x"}).(attr)
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
	a := lowerOne(Tag("div")()(Fragment(Text("a"), Text("b"))))
	b := lowerOne(Tag("div")()(Text("a"), Text("b")))
	if got := vdom.Diff(a, b); len(got) != 0 {
		t.Fatalf("Fragment-wrapped should diff identically: got %+v", got)
	}
}

func TestFragmentAtRootPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("expected panic for Fragment at root, got none")
		}
	}()
	_ = lowerOne(Fragment(Tag("div")()()))
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
	a := lowerOne(Tag("div")(Group(Group(Attribute("class", "a"), Attribute("id", "x")), Attribute("data-x", "1")))())
	b := lowerOne(Tag("div")(Attribute("class", "a"), Attribute("id", "x"), Attribute("data-x", "1"))())
	if vdom.Render(a) != vdom.Render(b) {
		t.Fatalf("nested Group should flatten: %q vs %q", vdom.Render(a), vdom.Render(b))
	}
}

func TestGroupEmptyContributesNothing(t *testing.T) {
	a := lowerOne(Tag("div")(Group(), Attribute("id", "x"))())
	b := lowerOne(Tag("div")(Attribute("id", "x"))())
	if vdom.Render(a) != vdom.Render(b) {
		t.Fatalf("empty Group should contribute nothing: %q vs %q", vdom.Render(a), vdom.Render(b))
	}
}

func TestGroupPreservesAttrOrder(t *testing.T) {
	a := lowerOne(Tag("div")(
		Attribute("id", "x"),
		Group(Attribute("class", "a"), Attribute("data-y", "1")),
		Attribute("data-z", "2"),
	)())
	b := lowerOne(Tag("div")(
		Attribute("id", "x"),
		Attribute("class", "a"),
		Attribute("data-y", "1"),
		Attribute("data-z", "2"),
	)())
	if vdom.Render(a) != vdom.Render(b) {
		t.Fatalf("Group attrs should appear in position: %q vs %q", vdom.Render(a), vdom.Render(b))
	}
}

// Combining (e.g. class joining) is a property of the lowered list, so
// a Group of duplicate classes should combine with a sibling Class just
// like inline duplicates do.
func TestGroupClassCombinesAcrossBoundary(t *testing.T) {
	a := lowerOne(Tag("div")(Group(Attribute("class", "a"), Attribute("class", "b")), Attribute("class", "c"))())
	b := lowerOne(Tag("div")(Attribute("class", "a"), Attribute("class", "b"), Attribute("class", "c"))())
	if vdom.Render(a) != vdom.Render(b) {
		t.Fatalf("Group-of-classes should combine like inline: %q vs %q", vdom.Render(a), vdom.Render(b))
	}
}

// Group works in Keyed's attrs slot for the same reason it works in
// Tag's — both lower attrs at construction via the same path.
func TestGroupInKeyedAttrs(t *testing.T) {
	a := lowerOne(Keyed("ul")(Group(Attribute("class", "a"), Attribute("id", "x")))(func(yield func(string, Node) bool) {}))
	b := lowerOne(Keyed("ul")(Attribute("class", "a"), Attribute("id", "x"))(func(yield func(string, Node) bool) {}))
	if vdom.Render(a) != vdom.Render(b) {
		t.Fatalf("Group in Keyed attrs should flatten: %q vs %q", vdom.Render(a), vdom.Render(b))
	}
}
