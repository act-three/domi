package vdom

import "strings"

// Render produces the static HTML for a Node tree.
func Render(n Node) string {
	var b strings.Builder
	writeNode(n, &b)
	return b.String()
}

// writeNode walks a Node and writes its HTML to b. Keyed and unkeyed
// elements render identically — for keyed children, data-domi-key is
// already in Attrs (injected by domi.Keyed at construction).
func writeNode(n Node, b *strings.Builder) {
	switch v := n.(type) {
	case Text:
		writeEscapedText(string(v), b)
	case Element:
		b.WriteByte('<')
		b.WriteString(v.tag)
		for _, a := range combinedAttrs(v.attrs) {
			writeAttr(a, b)
		}
		if isVoid(v.tag) {
			b.WriteString("/>")
			return
		}
		b.WriteByte('>')
		for _, c := range v.children {
			writeNode(c, b)
		}
		b.WriteString("</")
		b.WriteString(v.tag)
		b.WriteByte('>')
	}
}

// writeAttr writes a single attribute. An empty value renders as
// name-only (disabled instead of disabled=""), matching the
// idiomatic HTML form for boolean attributes. The two forms are
// indistinguishable in the DOM — the HTML5 parser maps both to an
// attribute node with empty value — so this is purely a serialization
// choice.
func writeAttr(a Attr, b *strings.Builder) {
	b.WriteByte(' ')
	b.WriteString(a.Name)
	if a.Value == "" {
		return
	}
	b.WriteString(`="`)
	writeEscapedAttr(a.Value, b)
	b.WriteByte('"')
}

// EscapeText returns s with the characters that have special meaning
// in HTML text content (&, <, >) replaced by their entity references.
func EscapeText(s string) string {
	var b strings.Builder
	writeEscapedText(s, &b)
	return b.String()
}

// EscapeAttr returns s with the characters that have special meaning
// in an HTML attribute value (&, <, >, ", ') replaced by their entity
// references.
func EscapeAttr(s string) string {
	var b strings.Builder
	writeEscapedAttr(s, &b)
	return b.String()
}

func writeEscapedText(s string, b *strings.Builder) {
	for _, r := range s {
		switch r {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		default:
			b.WriteRune(r)
		}
	}
}

func writeEscapedAttr(s string, b *strings.Builder) {
	for _, r := range s {
		switch r {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '"':
			b.WriteString("&quot;")
		case '\'':
			b.WriteString("&#39;")
		default:
			b.WriteRune(r)
		}
	}
}
