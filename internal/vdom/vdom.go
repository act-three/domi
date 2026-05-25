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

import (
	"iter"
	"slices"
	"strings"
)

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
//
// Attrs are sorted by name and deduplicated according to the combining rules.
func NewElement(tag string, attrs iter.Seq[Attr], children []Node, keys []string) Element {
	a := slices.Collect(attrs)
	slices.SortStableFunc(a, cmpAttrName)
	a = combineAttrs(a)
	return Element{tag: tag, attrs: a, children: children, keys: keys}
}

func cmpAttrName(a, b Attr) int {
	return strings.Compare(a.Name, b.Name)
}

// WithAttr returns a copy of e with a single additional attribute
// inserted in sorted position. Used by [ily.dev/domi.Keyed] to
// inject the data-domi-key attribute onto child elements without
// mutating the original.
func (e Element) WithAttr(a Attr) Element {
	i, _ := slices.BinarySearchFunc(e.attrs, a, cmpAttrName)
	out := make([]Attr, len(e.attrs)+1)
	copy(out, e.attrs[:i])
	out[i] = a
	copy(out[i+1:], e.attrs[i:])
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

// combineAttrs resolves duplicate attribute names in a sorted attr
// list: class joins with " ", style with ";", data-msg-* with ",",
// and everything else is first-wins. Returns the input slice
// unchanged when no duplicates are present.
func combineAttrs(attrs []Attr) []Attr {
	if len(attrs) < 2 {
		return attrs
	}
	// Fast path: with sorted input, duplicates are adjacent.
	hasDup := false
	for i := 1; i < len(attrs); i++ {
		if attrs[i].Name == attrs[i-1].Name {
			hasDup = true
			break
		}
	}
	if !hasDup {
		return attrs
	}
	// Slow path: linear scan merging adjacent duplicates.
	out := make([]Attr, 0, len(attrs))
	out = append(out, attrs[0])
	for _, a := range attrs[1:] {
		prev := &out[len(out)-1]
		if a.Name != prev.Name {
			out = append(out, a)
			continue
		}
		sep, isComb := combineSep(a.Name)
		if !isComb {
			continue // first-wins
		}
		if a.Value != "" {
			if prev.Value != "" {
				prev.Value += sep + a.Value
			} else {
				prev.Value = a.Value
			}
		}
	}
	return out
}

// combineSep returns the separator for attributes whose duplicate
// occurrences should be combined.
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
