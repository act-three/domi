package domi

import (
	"strings"
	"testing"
)

type tMsg struct {
	Tag string `json:"t"`
}

// resolveHandler splits a comma-joined handler attribute value and looks
// each hash up in the registry, returning the stored Msg JSON for each.
func resolveHandler(value string) []string {
	var out []string
	for _, h := range strings.Split(value, ",") {
		raw, ok := lookupHandler(h)
		if !ok {
			out = append(out, "<missing:"+h+">")
			continue
		}
		out = append(out, string(raw))
	}
	return out
}

// findAttr returns the value of the first Attr with the given name.
func findAttr(attrs []Attr, name string) (string, bool) {
	for _, a := range attrs {
		if a.name == name {
			return a.value, true
		}
	}
	return "", false
}

func TestCombineHandlersSameEvent(t *testing.T) {
	out := combinedAttrs([]Attr{
		On("click", tMsg{"a"}),
		On("click", tMsg{"b"}),
	})
	if len(out) != 1 {
		t.Fatalf("want 1 combined attr, got %d: %+v", len(out), out)
	}
	v, _ := findAttr(out, "data-msg-click")
	got := resolveHandler(v)
	want := []string{`{"t":"a"}`, `{"t":"b"}`}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("want %v through registry, got %v (raw value %q)", want, got, v)
	}
}

func TestCombineHandlersDistinctEvents(t *testing.T) {
	out := combinedAttrs([]Attr{
		On("click", tMsg{"a"}),
		On("submit", tMsg{"b"}),
	})
	if _, ok := findAttr(out, "data-msg-click"); !ok {
		t.Fatalf("missing data-msg-click; got %+v", out)
	}
	if _, ok := findAttr(out, "data-msg-submit"); !ok {
		t.Fatalf("missing data-msg-submit; got %+v", out)
	}
}

func TestCombineSingleHandlerNoComma(t *testing.T) {
	out := combinedAttrs([]Attr{On("click", tMsg{"a"})})
	v, _ := findAttr(out, "data-msg-click")
	if strings.Contains(v, ",") {
		t.Fatalf("want single value (no comma), got %q", v)
	}
	got := resolveHandler(v)
	if len(got) != 1 || got[0] != `{"t":"a"}` {
		t.Fatalf("want single msg resolved, got %v (raw %q)", got, v)
	}
}

// Two On() calls with the same Msg value share a registry slot because
// the hash is content-addressable.
func TestHandlerHashIsContentAddressable(t *testing.T) {
	a := On("click", tMsg{"x"})
	b := On("click", tMsg{"x"})
	if a.value != b.value {
		t.Fatalf("identical Msgs should produce identical attr values; got %q vs %q", a.value, b.value)
	}
}

func TestCombineClassWithSpace(t *testing.T) {
	out := combinedAttrs([]Attr{
		Attribute("class", "a"),
		Attribute("class", "b"),
	})
	if v, _ := findAttr(out, "class"); v != "a b" {
		t.Fatalf("want class=\"a b\", got %q", v)
	}
}

func TestCombineStyleWithSemicolon(t *testing.T) {
	out := combinedAttrs([]Attr{
		Attribute("style", "color:red"),
		Attribute("style", "font-weight:bold"),
	})
	if v, _ := findAttr(out, "style"); v != "color:red;font-weight:bold" {
		t.Fatalf("want combined style, got %q", v)
	}
}

func TestCombineOtherAttrFirstWins(t *testing.T) {
	out := combinedAttrs([]Attr{
		Attribute("id", "first"),
		Attribute("id", "second"),
	})
	if v, _ := findAttr(out, "id"); v != "first" {
		t.Fatalf("want id=\"first\" (first-wins), got %q", v)
	}
}

func TestCombineClassEmptyGuard(t *testing.T) {
	out := combinedAttrs([]Attr{
		Attribute("class", ""),
		Attribute("class", "b"),
	})
	if v, _ := findAttr(out, "class"); v != "b" {
		t.Fatalf("want class=\"b\" (no leading space), got %q", v)
	}
}

func TestCombineNoDuplicatesReturnsInput(t *testing.T) {
	in := []Attr{Attribute("id", "x"), Attribute("class", "c")}
	out := combinedAttrs(in)
	// We don't require identity, just equivalence — but the fast path is
	// designed to skip allocation when there are no duplicates.
	if len(out) != 2 {
		t.Fatalf("want 2 attrs, got %d", len(out))
	}
}

// Fragment is supposed to be indistinguishable from writing its children
// inline at the use site. The tests below pin that property at the
// places it could leak: the lowered children slice, the rendered HTML,
// and a diff against the inline-equivalent tree.

func TestFragmentLowersToInlineChildren(t *testing.T) {
	e := Tag("div")()(Fragment(Text("a"), Text("b")))
	if len(e.children) != 2 {
		t.Fatalf("want 2 children after flatten, got %d", len(e.children))
	}
	for i, c := range e.children {
		if _, ok := c.(text); !ok {
			t.Fatalf("child[%d]: want text, got %T", i, c)
		}
	}
}

func TestFragmentNestedFlattens(t *testing.T) {
	a := Tag("div")()(Fragment(Fragment(Text("a"), Text("b")), Text("c")))
	b := Tag("div")()(Text("a"), Text("b"), Text("c"))
	if render(a) != render(b) {
		t.Fatalf("nested Fragment should flatten: %q vs %q", render(a), render(b))
	}
}

func TestFragmentEmptyContributesNothing(t *testing.T) {
	a := Tag("div")()(Fragment(), Text("x"))
	b := Tag("div")()(Text("x"))
	if render(a) != render(b) {
		t.Fatalf("empty Fragment should contribute nothing: %q vs %q", render(a), render(b))
	}
}

func TestFragmentPreservesSiblingOrder(t *testing.T) {
	a := Tag("div")()(
		Text("a"),
		Fragment(Text("b"), Text("c")),
		Text("d"),
	)
	b := Tag("div")()(Text("a"), Text("b"), Text("c"), Text("d"))
	if render(a) != render(b) {
		t.Fatalf("Fragment children should appear in position: %q vs %q", render(a), render(b))
	}
}

func TestFragmentIsTransparentToDiff(t *testing.T) {
	a := Tag("div")()(Fragment(Text("a"), Text("b")))
	b := Tag("div")()(Text("a"), Text("b"))
	if got := diff(a, b); len(got) != 0 {
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
