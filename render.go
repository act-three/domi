package domi

import (
	"fmt"
	"strings"
)

// render produces the static HTML for a Node tree (used for first page load).
func render(n Node) string {
	var b strings.Builder
	writeNode(n, &b)
	return b.String()
}

func writeNode(n Node, b *strings.Builder) {
	switch n.kind {
	case nodeText:
		writeEscapedText(n.text, b)
	case nodeElement:
		b.WriteByte('<')
		b.WriteString(n.tag)
		for _, a := range combinedAttrs(n.attrs) {
			writeAttr(a, b)
		}
		if n.key != "" {
			// Emit alongside user attrs so the client can resolve this
			// element by key when applying keyed structural patches.
			b.WriteString(` data-domi-key="`)
			writeEscapedAttr(n.key, b)
			b.WriteByte('"')
		}
		if isVoid(n.tag) {
			b.WriteString("/>")
			return
		}
		b.WriteByte('>')
		for _, c := range n.children {
			writeNode(c, b)
		}
		b.WriteString("</")
		b.WriteString(n.tag)
		b.WriteByte('>')
	}
}

func writeAttr(a Attr, b *strings.Builder) {
	b.WriteByte(' ')
	b.WriteString(a.name)
	b.WriteString(`="`)
	writeEscapedAttr(a.value, b)
	b.WriteByte('"')
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

func isVoid(tag string) bool {
	switch tag {
	case "area", "base", "br", "col", "embed", "hr", "img", "input",
		"link", "meta", "source", "track", "wbr":
		return true
	}
	return false
}

// page wraps body content in a minimal HTML document and embeds the session ID.
func page(title, bodyHTML, sessionID string) string {
	var t, s strings.Builder
	writeEscapedText(title, &t)
	writeEscapedAttr(sessionID, &s)
	return fmt.Sprintf(
		`<!DOCTYPE html>
<html><head>
<meta charset="utf-8">
<title>%s</title>
</head><body>
<div id="domi-root" data-domi-session="%s">%s</div>
<script type="module" src="%s"></script>
</body></html>`,
		t.String(), s.String(), bodyHTML, clientJSPath,
	)
}
