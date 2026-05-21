package vdom

import (
	"strings"
	"testing"
)

// findAttr returns the value of the first attr with the given name.
func findAttr(attrs []Attr, name string) (string, bool) {
	for _, a := range attrs {
		if a.Name == name {
			return a.Value, true
		}
	}
	return "", false
}

func TestCombineDataMsgWithComma(t *testing.T) {
	out := combinedAttrs([]Attr{
		{Name: "data-msg-click", Value: "h1"},
		{Name: "data-msg-click", Value: "h2"},
	})
	if len(out) != 1 {
		t.Fatalf("want 1 combined attr, got %d: %+v", len(out), out)
	}
	v, _ := findAttr(out, "data-msg-click")
	if v != "h1,h2" {
		t.Fatalf("want data-msg-click=\"h1,h2\", got %q", v)
	}
}

func TestCombineDistinctEventsKeepBoth(t *testing.T) {
	out := combinedAttrs([]Attr{
		{Name: "data-msg-click", Value: "h1"},
		{Name: "data-msg-submit", Value: "h2"},
	})
	if _, ok := findAttr(out, "data-msg-click"); !ok {
		t.Fatalf("missing data-msg-click; got %+v", out)
	}
	if _, ok := findAttr(out, "data-msg-submit"); !ok {
		t.Fatalf("missing data-msg-submit; got %+v", out)
	}
}

func TestCombineSingleDataMsgNoComma(t *testing.T) {
	out := combinedAttrs([]Attr{{Name: "data-msg-click", Value: "h1"}})
	v, _ := findAttr(out, "data-msg-click")
	if strings.Contains(v, ",") {
		t.Fatalf("want single value (no comma), got %q", v)
	}
}

func TestCombineClassWithSpace(t *testing.T) {
	out := combinedAttrs([]Attr{
		{Name: "class", Value: "a"},
		{Name: "class", Value: "b"},
	})
	if v, _ := findAttr(out, "class"); v != "a b" {
		t.Fatalf("want class=\"a b\", got %q", v)
	}
}

func TestCombineStyleWithSemicolon(t *testing.T) {
	out := combinedAttrs([]Attr{
		{Name: "style", Value: "color:red"},
		{Name: "style", Value: "font-weight:bold"},
	})
	if v, _ := findAttr(out, "style"); v != "color:red;font-weight:bold" {
		t.Fatalf("want combined style, got %q", v)
	}
}

func TestCombineOtherAttrFirstWins(t *testing.T) {
	out := combinedAttrs([]Attr{
		{Name: "id", Value: "first"},
		{Name: "id", Value: "second"},
	})
	if v, _ := findAttr(out, "id"); v != "first" {
		t.Fatalf("want id=\"first\" (first-wins), got %q", v)
	}
}

func TestCombineClassEmptyGuard(t *testing.T) {
	out := combinedAttrs([]Attr{
		{Name: "class", Value: ""},
		{Name: "class", Value: "b"},
	})
	if v, _ := findAttr(out, "class"); v != "b" {
		t.Fatalf("want class=\"b\" (no leading space), got %q", v)
	}
}

func TestCombineNoDuplicatesReturnsInput(t *testing.T) {
	in := []Attr{{Name: "id", Value: "x"}, {Name: "class", Value: "c"}}
	out := combinedAttrs(in)
	// We don't require identity, just equivalence — but the fast path is
	// designed to skip allocation when there are no duplicates.
	if len(out) != 2 {
		t.Fatalf("want 2 attrs, got %d", len(out))
	}
}
