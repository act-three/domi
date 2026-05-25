package vdom

import "testing"

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
