package attr_test

import (
	"strings"
	"testing"

	"ily.dev/domi"
	"ily.dev/domi/attr"
)

// render renders a single div carrying a, so each test reads as the
// attribute's rendered output.
func render(t *testing.T, a attr.Attr) string {
	t.Helper()
	var sb strings.Builder
	if err := domi.RenderTo(&sb, domi.Tag("div", a)); err != nil {
		t.Fatal(err)
	}
	return sb.String()
}

// Boolean attribute constructors are present as a name-only attribute
// when b is true and absent otherwise.
func TestBoolAttrs(t *testing.T) {
	for name, f := range map[string]func(bool) attr.Attr{
		"autofocus":      attr.Autofocus,
		"autoplay":       attr.Autoplay,
		"checked":        attr.Checked,
		"controls":       attr.Controls,
		"default":        attr.Default,
		"disabled":       attr.Disabled,
		"formnovalidate": attr.FormNoValidate,
		"inert":          attr.Inert,
		"loop":           attr.Loop,
		"multiple":       attr.Multiple,
		"muted":          attr.Muted,
		"novalidate":     attr.NoValidate,
		"open":           attr.Open,
		"playsinline":    attr.PlaysInline,
		"readonly":       attr.ReadOnly,
		"required":       attr.Required,
		"reversed":       attr.Reversed,
		"selected":       attr.Selected,
	} {
		if got, want := render(t, f(true)), `<div `+name+`></div>`; got != want {
			t.Errorf("%s(true): got %q, want %q", name, got, want)
		}
		if got, want := render(t, f(false)), `<div></div>`; got != want {
			t.Errorf("%s(false): got %q, want %q", name, got, want)
		}
	}
}

// Enumerated boolean attribute constructors have the keyword value for
// the given state.
func TestEnumBoolAttrs(t *testing.T) {
	for _, tt := range []struct {
		name                string
		f                   func(bool) attr.Attr
		wantTrue, wantFalse string
	}{
		{"draggable", attr.Draggable, "true", "false"},
		{"spellcheck", attr.Spellcheck, "true", "false"},
		{"translate", attr.Translate, "yes", "no"},
	} {
		if got, want := render(t, tt.f(true)), `<div `+tt.name+`="`+tt.wantTrue+`"></div>`; got != want {
			t.Errorf("%s(true): got %q, want %q", tt.name, got, want)
		}
		if got, want := render(t, tt.f(false)), `<div `+tt.name+`="`+tt.wantFalse+`"></div>`; got != want {
			t.Errorf("%s(false): got %q, want %q", tt.name, got, want)
		}
	}
}
