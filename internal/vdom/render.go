package vdom

import (
	"fmt"
	"io"
	"strings"
)

// Render produces the static HTML for a Node tree.
func Render(n Node) string {
	var b strings.Builder
	// strings.Builder.Write never returns a non-nil error.
	_ = RenderTo(&b, n)
	return b.String()
}

// RenderTo writes the HTML for a Node tree to w.
// The only errors returned are from w.
//
// Keyed and unkeyed children render identically — for keyed children,
// data-domi-key is already in Attrs (injected by domi's lowering).
func RenderTo(w io.Writer, n Node) error {
	switch v := n.(type) {
	case Text:
		_, err := textEscaper.WriteString(w, string(v))
		return err
	case Element:
		w.Write(lt)
		io.WriteString(w, v.tag)
		for _, a := range v.attrs {
			if err := writeAttr(w, a); err != nil {
				return err
			}
		}
		if !isVoid(v.tag) {
			w.Write(gt)
			if err := renderChildren(w, v); err != nil {
				return err
			}
			w.Write(ltSlash)
			io.WriteString(w, v.tag)
		}
		_, err := w.Write(gt)
		return err
	}
	return nil
}

// renderChildren writes element e's children to w. Every element other
// than a raw-text one renders its children normally, escaping text.
//
// A raw-text element (script, style) is empty or holds exactly one text
// child, written verbatim: the parser does not entity-decode raw text,
// so escaping would corrupt the script or stylesheet. The parser yields
// a single raw-text token and the constructors coalesce adjacent text,
// so any other shape is a hand-built tree that has no faithful
// serialization here — it panics rather than emit escaped or malformed
// output.
func renderChildren(w io.Writer, e Element) error {
	if isRawTextElement(e.tag) {
		if len(e.children) == 0 {
			return nil
		}
		t, ok := e.children[0].(Text)
		if len(e.children) != 1 || !ok {
			panic(fmt.Sprintf("domi: <%s> must hold a single text child", e.tag))
		}
		_, err := io.WriteString(w, string(t))
		return err
	}
	for _, c := range e.children {
		if err := RenderTo(w, c); err != nil {
			return err
		}
	}
	return nil
}

// isRawTextElement reports whether tag holds CDATA-style raw text whose
// content an HTML parser does not entity-decode. Only script and style
// qualify; textarea and title are escapable raw text, so ordinary text
// escaping already renders them correctly.
func isRawTextElement(tag string) bool {
	return tag == "script" || tag == "style"
}

// writeAttr writes a single attribute. An empty value renders as
// name-only (disabled instead of disabled=""), matching the
// idiomatic HTML form for boolean attributes. The two forms are
// indistinguishable in the DOM — the HTML5 parser maps both to an
// attribute node with empty value — so this is purely a serialization
// choice.
func writeAttr(w io.Writer, a Attr) error {
	w.Write(sp)
	io.WriteString(w, a.Name)
	if a.Value == "" {
		return nil
	}
	w.Write(eqQuote)
	if err := writeEscapedAttr(w, a.Value); err != nil {
		return err
	}
	_, err := w.Write(dquote)
	return err
}

// writeEscapedAttr only escapes & and " — the two characters that
// are special inside double-quoted attribute values per the HTML5
// spec. <, >, and ' are literal inside double quotes.
func writeEscapedAttr(w io.Writer, s string) error {
	_, err := attrEscaper.WriteString(w, s)
	return err
}

var (
	textEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	attrEscaper = strings.NewReplacer("&", "&amp;", `"`, "&quot;")
)

var (
	lt      = []byte("<")
	gt      = []byte(">")
	ltSlash = []byte("</")
	sp      = []byte(" ")
	eqQuote = []byte(`="`)
	dquote  = []byte(`"`)
)
