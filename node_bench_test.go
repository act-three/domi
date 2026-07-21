package domi

import "testing"

// BenchmarkLower builds and lowers a handler-free tree — the common case,
// where lowering should not allocate a handler map per node.
func BenchmarkLower(b *testing.B) {
	b.ReportAllocs()
	for range b.N {
		rows := make([]Node, 64)
		for i := range rows {
			rows[i] = Tag("li", Name("class", "row"))(
				Tag("span", Name("class", "label"))(Text("item")),
				Tag("span", Name("class", "value"))(Text("42")),
			)
		}
		_, _ = lower(0, Tag("ul", Name("class", "list"))(rows...))
	}
}
