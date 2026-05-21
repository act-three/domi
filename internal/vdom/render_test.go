package vdom

import "testing"

// TestRenderEmptyAttrEmitsNameOnly: an empty-valued attribute renders
// as just the name. The HTML5 parser maps `<input disabled>` and
// `<input disabled="">` to the same DOM state, so this is purely a
// serialization preference — the boolean-attribute form is the
// idiomatic one.
func TestRenderEmptyAttrEmitsNameOnly(t *testing.T) {
	in := NewElement("input", []Attr{{Name: "disabled", Value: ""}}, nil, nil)
	got := Render(in)
	want := `<input disabled/>`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestRenderMixedAttrs: name-only and name=value attrs coexist on the
// same element in source order.
func TestRenderMixedAttrs(t *testing.T) {
	in := NewElement("input", []Attr{
		{Name: "type", Value: "checkbox"},
		{Name: "checked", Value: ""},
		{Name: "name", Value: "agree"},
	}, nil, nil)
	got := Render(in)
	want := `<input type="checkbox" checked name="agree"/>`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
