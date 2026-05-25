package vdom

import (
	"io"
	"strconv"
	"testing"
)

func benchTree() Node {
	items := make([]Node, 100)
	for i := range items {
		items[i] = NewElement("li", []Attr{
			{Name: "class", Value: "item"},
			{Name: "data-idx", Value: strconv.Itoa(i)},
		}, []Node{
			NewElement("span", []Attr{{Name: "class", Value: "label"}},
				[]Node{Text("Item " + strconv.Itoa(i))}, nil),
			NewElement("button", []Attr{
				{Name: "class", Value: "btn delete"},
				{Name: "data-msg-click", Value: "abc123"},
			}, []Node{Text("×")}, nil),
		}, nil)
	}
	return NewElement("div", []Attr{{Name: "id", Value: "app"}}, []Node{
		NewElement("nav", []Attr{{Name: "class", Value: "navbar"}}, []Node{
			NewElement("a", []Attr{
				{Name: "href", Value: "/"},
				{Name: "class", Value: "brand"},
			}, []Node{Text("My App")}, nil),
		}, nil),
		NewElement("h1", nil, []Node{Text("Todo List")}, nil),
		NewElement("ul", []Attr{{Name: "class", Value: "list"}}, items, nil),
	}, nil)
}

func BenchmarkRender(b *testing.B) {
	tree := benchTree()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		Render(tree)
	}
}

func BenchmarkRenderTo(b *testing.B) {
	tree := benchTree()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		RenderTo(io.Discard, tree)
	}
}
