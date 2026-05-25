package vdom

import (
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
// Keyed and unkeyed elements render identically — for keyed children,
// data-domi-key is already in Attrs (injected by domi.Keyed at
// construction).
func RenderTo(w io.Writer, n Node) error {
	switch v := n.(type) {
	case Text:
		return writeEscapedText(w, string(v))
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
			for _, c := range v.children {
				if err := RenderTo(w, c); err != nil {
					return err
				}
			}
			w.Write(ltSlash)
			io.WriteString(w, v.tag)
		}
		_, err := w.Write(gt)
		return err
	}
	return nil
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

func writeEscapedText(w io.Writer, s string) error {
	_, err := textEscaper.WriteString(w, s)
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
