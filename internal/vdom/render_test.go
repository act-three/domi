package vdom

import "testing"

// TestRenderEmptyAttrEmitsNameOnly: an empty-valued attribute renders
// as just the name. The HTML5 parser maps `<input disabled>` and
// `<input disabled="">` to the same DOM state, so this is purely a
// serialization preference — the boolean-attribute form is the
// idiomatic one.
func TestRenderEmptyAttrEmitsNameOnly(t *testing.T) {
	in := NewElement("input", attrs(Attr{Name: "disabled", Value: ""}), nil)
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
	), nil)
	got := Render(in)
	want := `<input checked name="agree" type="checkbox">`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// Inside a raw-text element (script, style) text is emitted verbatim:
// the HTML parser does not entity-decode such content, so escaping it
// would corrupt the script or stylesheet.
func TestRenderScriptVerbatim(t *testing.T) {
	in := NewElement("script", attrs(), []Node{Text("a && b < c")})
	got := Render(in)
	want := "<script>a && b < c</script>"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestRenderStyleVerbatim(t *testing.T) {
	in := NewElement("style", attrs(), []Node{Text(".a > .b { color: red }")})
	got := Render(in)
	want := "<style>.a > .b { color: red }</style>"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// A raw-text element with no children (e.g. an external script) renders
// as an empty element.
func TestRenderEmptyScript(t *testing.T) {
	in := NewElement("script", attrs(Attr{Name: "src", Value: "/x.js"}), nil)
	got := Render(in)
	want := `<script src="/x.js"></script>`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// A raw-text element can't hold an element child — there's no faithful
// way to serialize one inside script or style. NewElement rejects the
// shape at construction; the write site panics on a hand-built tree
// rather than emitting escaped or malformed output.
func TestRenderRawTextRejectsElementChild(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for element child in a raw-text element")
		}
	}()
	in := Element{tag: "script", children: []Node{Element{tag: "b"}}}
	Render(in)
}

func TestRenderTextEscapes(t *testing.T) {
	got := Render(Text("a < b & c"))
	want := "a &lt; b &amp; c"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// Ordinary elements still escape their text children.
func TestRenderNormalElementEscapes(t *testing.T) {
	in := NewElement("div", attrs(), []Node{Text("a < b & c")})
	got := Render(in)
	want := "<div>a &lt; b &amp; c</div>"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
