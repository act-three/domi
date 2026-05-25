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
		for _, a := range combinedAttrs(v.attrs) {
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
	last := 0
	for i := range len(s) {
		var esc []byte
		switch s[i] {
		case '&':
			esc = escAmp
		case '<':
			esc = escLt
		case '>':
			esc = escGt
		default:
			continue
		}
		if _, err := io.WriteString(w, s[last:i]); err != nil {
			return err
		}
		if _, err := w.Write(esc); err != nil {
			return err
		}
		last = i + 1
	}
	_, err := io.WriteString(w, s[last:])
	return err
}

func writeEscapedAttr(w io.Writer, s string) error {
	last := 0
	for i := range len(s) {
		var esc []byte
		switch s[i] {
		case '&':
			esc = escAmp
		case '<':
			esc = escLt
		case '>':
			esc = escGt
		case '"':
			esc = escQuot
		case '\'':
			esc = escApos
		default:
			continue
		}
		if _, err := io.WriteString(w, s[last:i]); err != nil {
			return err
		}
		if _, err := w.Write(esc); err != nil {
			return err
		}
		last = i + 1
	}
	_, err := io.WriteString(w, s[last:])
	return err
}

var (
	lt      = []byte("<")
	gt      = []byte(">")
	ltSlash = []byte("</")
	sp      = []byte(" ")
	eqQuote = []byte(`="`)
	dquote  = []byte(`"`)
	escAmp  = []byte("&amp;")
	escLt   = []byte("&lt;")
	escGt   = []byte("&gt;")
	escQuot = []byte("&quot;")
	escApos = []byte("&#39;")
)
