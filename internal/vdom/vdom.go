// Package vdom holds the lowered (canonical) form of the domi virtual
// DOM, plus the renderer and differ that walk it. The public-facing
// constructors live in [ily.dev/domi]; vdom is the internal data layer
// they lower into.
//
// The exported types here are deliberately minimal: an HTML element
// has only two kinds of children (element and text), so a sum of two
// concrete structs covers the tree shape exactly. Renderers and
// differs switch exhaustively on those two cases.
package vdom

import "strings"

// Node is anything in a lowered VDOM tree: an [Element] or a [Text].
type Node interface {
	vdomNode()
}

// Element is a fully-realized element node. Construct via [NewElement];
// fields are read-only outside this package so a constructed Element
// can't be mutated post-hoc (the renderer and differ rely on the tree
// being stable for the lifetime of a diff/render call).
//
// keys discriminates positional from keyed elements: nil means
// positional; non-nil (even empty) means children are paired with
// these keys for identity-based reconciliation. When non-nil,
// len(keys) equals len(children), and each child is an Element (text
// can't carry a data-domi-key attribute).
type Element struct {
	tag      string
	attrs    []Attr
	children []Node
	keys     []string
}

func (Element) vdomNode() {}

// NewElement constructs an [Element]. Pass nil for keys to build a
// positional element; pass a slice (even empty) parallel to children
// to build a keyed element.
func NewElement(tag string, attrs []Attr, children []Node, keys []string) Element {
	return Element{tag: tag, attrs: attrs, children: children, keys: keys}
}

// WithAttr returns a copy of e with a single additional attribute
// appended. Used by [ily.dev/domi.Keyed] to inject the data-domi-key
// attribute onto child elements without mutating the original.
func (e Element) WithAttr(a Attr) Element {
	out := make([]Attr, len(e.attrs)+1)
	copy(out, e.attrs)
	out[len(e.attrs)] = a
	return Element{tag: e.tag, attrs: out, children: e.children, keys: e.keys}
}

// Text is a text node.
type Text string

func (Text) vdomNode() {}

// Attr is a flat name/value attribute pair.
type Attr struct {
	Name  string
	Value string
}

// combineSep returns the separator for attributes whose duplicate
// occurrences should be combined. Non-combining attributes are
// first-wins.
//
//   - class:      single space
//   - style:      semicolon
//   - data-msg-*: comma (the server splits on commas to recover the
//     individual handler hashes)
func combineSep(name string) (sep string, ok bool) {
	switch name {
	case "class":
		return " ", true
	case "style":
		return ";", true
	}
	if strings.HasPrefix(name, "data-msg-") {
		return ",", true
	}
	return "", false
}

// combinedAttrs returns attrs with duplicates resolved per the rules
// in combineSep. First-occurrence order is preserved. The walker is a
// single pass; each combining attribute accumulates into its own
// strings.Builder (amortized O(N) per name, replacing the previous
// quadratic string concat).
func combinedAttrs(attrs []Attr) []Attr {
	if len(attrs) < 2 {
		return attrs
	}
	out := make([]Attr, 0, len(attrs))
	idx := make(map[string]int, len(attrs))
	var bufs map[string]*strings.Builder // lazy; allocated on first duplicate
	for _, a := range attrs {
		i, dup := idx[a.Name]
		if !dup {
			idx[a.Name] = len(out)
			out = append(out, a)
			continue
		}
		sep, isComb := combineSep(a.Name)
		if !isComb {
			continue // first-wins
		}
		if bufs == nil {
			bufs = map[string]*strings.Builder{}
		}
		buf, ok := bufs[a.Name]
		if !ok {
			buf = &strings.Builder{}
			buf.WriteString(out[i].Value)
			bufs[a.Name] = buf
		}
		if a.Value != "" {
			if buf.Len() > 0 {
				buf.WriteString(sep)
			}
			buf.WriteString(a.Value)
		}
	}
	for name, buf := range bufs {
		out[idx[name]].Value = buf.String()
	}
	return out
}

// isVoid reports whether tag is a void HTML element (one that must not
// have a closing tag, per the HTML spec).
func isVoid(tag string) bool {
	switch tag {
	case "area", "base", "br", "col", "embed", "hr", "img", "input",
		"link", "meta", "source", "track", "wbr":
		return true
	}
	return false
}
