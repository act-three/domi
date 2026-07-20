package vdom

import (
	"slices"
	"testing"
)

func TestRegisterCombining(t *testing.T) {
	RegisterCombining("data-test", ",")
	defer delete(combining, "data-test")

	attrs := []Attr{
		{Name: "data-test", Value: "x"},
		{Name: "data-test", Value: "y"},
		{Name: "data-test", Value: "z"},
	}
	got := combineAttrs(attrs)
	if len(got) != 1 {
		t.Fatalf("want 1 attr, got %d", len(got))
	}
	if got[0].Value != "x,y,z" {
		t.Fatalf("got %q, want %q", got[0].Value, "x,y,z")
	}
}

// nodeSummary renders a child list into a compact, comparable form so a
// table test can assert the post-coalesce shape: a text node shows as
// "t:" + its content, an element as "e:" + its tag.
func nodeSummary(nodes []Node) []string {
	out := make([]string, len(nodes))
	for i, n := range nodes {
		switch x := n.(type) {
		case Text:
			out[i] = "t:" + string(x)
		case Element:
			out[i] = "e:" + x.tag
		}
	}
	return out
}

// coalesceText canonicalizes a child list to match the shape an HTML
// parser yields when it reparses the rendered output: adjacent text
// merges into one node and empty text — which renders to nothing and so
// parses to no node — is dropped. Whitespace-only text is real text the
// parser keeps, so it survives.
func TestCoalesceText(t *testing.T) {
	tests := []struct {
		name string
		in   []Node
		want []string
	}{
		{"lone empty text dropped", []Node{tx("")}, nil},
		{"empty between elements dropped", []Node{el("a"), tx(""), el("b")}, []string{"e:a", "e:b"}},
		{"mixed run keeps only non-empty", []Node{tx(""), tx("hi"), tx("")}, []string{"t:hi"}},
		{"adjacent text merges", []Node{tx("Count: "), tx("5")}, []string{"t:Count: 5"}},
		{"whitespace-only text survives", []Node{tx(" ")}, []string{"t: "}},
		{"whitespace merges with text", []Node{tx("a"), tx(" "), tx("b")}, []string{"t:a b"}},
		{"text is not merged across an element", []Node{tx("a"), el("x"), tx("b")}, []string{"t:a", "e:x", "t:b"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := nodeSummary(coalesceText(tt.in))
			if !slices.Equal(got, tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

// A keyed element parts adjacent text like any element — its DOM node
// sits between them — and keeps its key through the rewrite.
func TestCoalesceTextAroundKeyedElement(t *testing.T) {
	in := []Node{tx("a"), tx("b"), el("x").WithKey("kx", false), tx("")}
	got := coalesceText(in)
	if want := []string{"t:ab", "e:x"}; !slices.Equal(nodeSummary(got), want) {
		t.Fatalf("got %v, want %v", nodeSummary(got), want)
	}
	if childKey(got[1]) != "kx" {
		t.Fatalf("keyed element lost its key: %q", childKey(got[1]))
	}
}

// Sibling keys must be unique: identity-based reconciliation resolves
// siblings by key on both sides of the wire, so a collision would
// silently corrupt the client. NewElement rejects it at construction,
// where the panic points at the render that introduced it.
func TestDuplicateSiblingKeysPanic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for duplicate sibling keys")
		}
	}()
	_ = NewElement("ul", attrs(), []Node{
		el("li").WithKey("a", false),
		el("li"),
		el("li").WithKey("a", false),
	})
}

// A void element serializes without children, so a child kept in the
// tree would exist only in the server's shadow tree and any later
// patch addressed into it would fail on the client. NewElement rejects
// it at construction, where the panic points at the render that
// introduced it.
func TestVoidElementChildrenPanic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for children of a void element")
		}
	}()
	_ = NewElement("input", attrs(), []Node{tx("boom")})
}

// Providing any child at all is the error: even empty text, inert
// everywhere else, panics on a void element.
func TestVoidElementEmptyTextPanic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for empty text child of a void element")
		}
	}()
	_ = NewElement("input", attrs(), []Node{tx("")})
}

// The same holds for the root list, validated by Diff.
func TestDuplicateRootKeysPanic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for duplicate root keys")
		}
	}()
	_ = Diff(nil, []Node{el("li").WithKey("a", false), el("li").WithKey("a", false)})
}

// coalesceText returns the input slice untouched when nothing needs
// merging or dropping, so an unchanged child list costs no allocation.
func TestCoalesceTextNoChangeReturnsInput(t *testing.T) {
	in := []Node{el("a"), tx("hi"), el("b")}
	out := coalesceText(in)
	if &out[0] != &in[0] {
		t.Fatal("no-op path should return input slice unchanged")
	}
}

func TestCombineNoDuplicatesReturnsInput(t *testing.T) {
	in := []Attr{{Name: "id", Value: "x"}, {Name: "class", Value: "c"}}
	out := combineAttrs(in)
	if len(out) != 2 {
		t.Fatalf("want 2 attrs, got %d", len(out))
	}
	// Fast path should return the input slice unchanged.
	if &out[0] != &in[0] {
		t.Fatal("no-duplicate fast path should return input slice")
	}
}
