package domi

import (
	"fmt"
	"testing"

	"ily.dev/domi/internal/vdom"
)

// lowerOne narrows a single Node to its lowered vdom.Node form, asserting
// the Node materializes to exactly one entry. Test helper for assertions
// on single-node trees.
func lowerOne(n Node) vdom.Node {
	out := lower(n)
	if len(out) != 1 {
		panic(fmt.Sprintf("lowerOne: expected 1 node, got %d", len(out)))
	}
	return out[0]
}

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
	a := lowerOne(Tag("div")(Group(Group(Name("class", "a"), Name("id", "x")), Name("data-x", "1")))())
	b := lowerOne(Tag("div")(Name("class", "a"), Name("id", "x"), Name("data-x", "1"))())
	if vdom.Render(a) != vdom.Render(b) {
		t.Fatalf("nested Group should flatten: %q vs %q", vdom.Render(a), vdom.Render(b))
	}
}

func TestGroupEmptyContributesNothing(t *testing.T) {
	a := lowerOne(Tag("div")(Group(), Name("id", "x"))())
	b := lowerOne(Tag("div")(Name("id", "x"))())
	if vdom.Render(a) != vdom.Render(b) {
		t.Fatalf("empty Group should contribute nothing: %q vs %q", vdom.Render(a), vdom.Render(b))
	}
}

func TestGroupPreservesAttrOrder(t *testing.T) {
	a := lowerOne(Tag("div")(
		Name("id", "x"),
		Group(Name("class", "a"), Name("data-y", "1")),
		Name("data-z", "2"),
	)())
	b := lowerOne(Tag("div")(
		Name("id", "x"),
		Name("class", "a"),
		Name("data-y", "1"),
		Name("data-z", "2"),
	)())
	if vdom.Render(a) != vdom.Render(b) {
		t.Fatalf("Group attrs should appear in position: %q vs %q", vdom.Render(a), vdom.Render(b))
	}
}

// Combining (e.g. class joining) is a property of the lowered list, so
// a Group of duplicate classes should combine with a sibling Class just
// like inline duplicates do.
func TestGroupClassCombinesAcrossBoundary(t *testing.T) {
	a := lowerOne(Tag("div")(Group(Name("class", "a"), Name("class", "b")), Name("class", "c"))())
	b := lowerOne(Tag("div")(Name("class", "a"), Name("class", "b"), Name("class", "c"))())
	if vdom.Render(a) != vdom.Render(b) {
		t.Fatalf("Group-of-classes should combine like inline: %q vs %q", vdom.Render(a), vdom.Render(b))
	}
}

// Group works in Keyed's attrs slot for the same reason it works in
// Tag's — both lower attrs at construction via the same path.
func TestGroupInKeyedAttrs(t *testing.T) {
	a := lowerOne(Keyed("ul")(Group(Name("class", "a"), Name("id", "x")))(func(yield func(string, Node) bool) {}))
	b := lowerOne(Keyed("ul")(Name("class", "a"), Name("id", "x"))(func(yield func(string, Node) bool) {}))
	if vdom.Render(a) != vdom.Render(b) {
		t.Fatalf("Group in Keyed attrs should flatten: %q vs %q", vdom.Render(a), vdom.Render(b))
	}
}
