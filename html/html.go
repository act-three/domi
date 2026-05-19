// Package html provides prebound builders for common HTML elements. App
// authors using a UI component library typically reach for those components
// and the domi.Node type directly; this package is a fallback for ad-hoc
// markup.
//
// Each builder is the result of [domi.Tag], so usage is two curried calls
// — first attributes, then children:
//
//	Div(attr.Class("x"))(Text("hi"), Button(event.Click(msg))(Text("ok")))
//
// Void elements (Input, etc.) still need the trailing empty children call:
// Input(attr.Type("text"))().
package html

import "ily.dev/domi"

var (
	A      = domi.Tag("a")
	Button = domi.Tag("button")
	Div    = domi.Tag("div")
	Form   = domi.Tag("form")
	H1     = domi.Tag("h1")
	H2     = domi.Tag("h2")
	H3     = domi.Tag("h3")
	Input  = domi.Tag("input")
	Li     = domi.Tag("li")
	Ol     = domi.Tag("ol")
	P      = domi.Tag("p")
	Span   = domi.Tag("span")
	Ul     = domi.Tag("ul")
)
