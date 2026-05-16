// Package html provides convenience wrappers around domi.E for common
// HTML elements. App authors using a UI component library typically reach
// for those components and the domi.Node type directly; this package is
// a fallback for ad-hoc markup.
package html

import "ily.dev/domi"

func Div(attrs []domi.Attr, children []domi.Node) domi.Node {
	return domi.E("div", attrs, children)
}
func Span(attrs []domi.Attr, children []domi.Node) domi.Node {
	return domi.E("span", attrs, children)
}
func P(attrs []domi.Attr, children []domi.Node) domi.Node {
	return domi.E("p", attrs, children)
}
func H1(attrs []domi.Attr, children []domi.Node) domi.Node {
	return domi.E("h1", attrs, children)
}
func H2(attrs []domi.Attr, children []domi.Node) domi.Node {
	return domi.E("h2", attrs, children)
}
func H3(attrs []domi.Attr, children []domi.Node) domi.Node {
	return domi.E("h3", attrs, children)
}
func Ul(attrs []domi.Attr, children []domi.Node) domi.Node {
	return domi.E("ul", attrs, children)
}
func Ol(attrs []domi.Attr, children []domi.Node) domi.Node {
	return domi.E("ol", attrs, children)
}
func Li(attrs []domi.Attr, children []domi.Node) domi.Node {
	return domi.E("li", attrs, children)
}
func Button(attrs []domi.Attr, children []domi.Node) domi.Node {
	return domi.E("button", attrs, children)
}
func Form(attrs []domi.Attr, children []domi.Node) domi.Node {
	return domi.E("form", attrs, children)
}
func Input(attrs []domi.Attr) domi.Node {
	return domi.E("input", attrs, nil)
}
func A(attrs []domi.Attr, children []domi.Node) domi.Node {
	return domi.E("a", attrs, children)
}
