package vdom

import "testing"

// TestRenderEmptyAttrEmitsNameOnly: an empty-valued attribute renders
// as just the name. The HTML5 parser maps `<input disabled>` and
// `<input disabled="">` to the same DOM state, so this is purely a
// serialization preference — the boolean-attribute form is the
// idiomatic one.
func TestRenderEmptyAttrEmitsNameOnly(t *testing.T) {
	in := NewElement("input", attrs(Attr{Name: "disabled", Value: ""}), nil, nil)
	got := Render(in)
	want := `<input disabled>`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestRenderMixedAttrs: name-only and name=value attrs coexist on the
// same element. NewElement sorts attrs by name, so the output is in
// alphabetical order regardless of input order.
func TestRenderMixedAttrs(t *testing.T) {
	in := NewElement("input", attrs(
		Attr{Name: "type", Value: "checkbox"},
		Attr{Name: "checked", Value: ""},
		Attr{Name: "name", Value: "agree"},
	), nil, nil)
	got := Render(in)
	want := `<input checked name="agree" type="checkbox">`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestRenderRawVerbatim(t *testing.T) {
	got := Render(Raw("<b>hi</b>"))
	want := "<b>hi</b>"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestRenderRawNoEscaping(t *testing.T) {
	got := Render(Raw("a &amp; b"))
	want := "a &amp; b"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestRenderTextEscapes(t *testing.T) {
	got := Render(Text("a < b & c"))
	want := "a &lt; b &amp; c"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestRenderRawInsideElement(t *testing.T) {
	in := NewElement("div", attrs(), []Node{Raw("<b>hi</b>")}, nil)
	got := Render(in)
	want := "<div><b>hi</b></div>"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
