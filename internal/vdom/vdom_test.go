package vdom

import "testing"

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
