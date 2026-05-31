// Package vdom holds the lowered (canonical) form of the domi virtual
// DOM, plus the renderer and differ that walk it. The public-facing
// constructors live in [ily.dev/domi]; vdom is the internal data layer
// they lower into.
//
// The exported types here are deliberately minimal: an HTML element
// has three kinds of children (element, raw, and text) so a sum of
// three concrete types covers the tree shape exactly. Renderers and
// differs switch exhaustively on those cases.
package vdom

import (
	"iter"
	"slices"
	"strings"
)

// Node is anything in a lowered VDOM tree: an [Element], a [Raw], or a
// [Text].
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
// len(keys) equals len(children), and each child is an Element (raw
// nodes can't carry a data-domi-key attribute).
type Element struct {
	tag      string
	attrs    []Attr
	children []Node
	keys     []string

	// opaque marks the element as ignored by the differ.
	// It is rendered once and thereafter no patches are emitted
	// (until its eventual removal, if any).
	opaque bool
}

func (Element) vdomNode() {}

// attrOpaque marks an element as client-owned; its presence sets
// [Element.opaque]. See [ily.dev/domi.Opaque] for the contract.
const attrOpaque = "data-domi-opaque"

// NewElement constructs an [Element]. Pass nil for keys to build a
// positional element; pass a slice (even empty) parallel to children
// to build a keyed element.
//
// Attrs are sorted by name and deduplicated according to the combining rules.
func NewElement(tag string, attrs iter.Seq[Attr], children []Node, keys []string) Element {
	a := slices.Collect(attrs)
	slices.SortStableFunc(a, cmpAttrName)
	a = combineAttrs(a)
	_, opaque := slices.BinarySearchFunc(a, Attr{Name: attrOpaque}, cmpAttrName)
	if keys == nil {
		opaqueMustBeKeyed(children)
		children = coalesceText(children)
	}
	return Element{tag: tag, attrs: a, children: children, keys: keys, opaque: opaque}
}

// opaqueMustBeKeyed panics if nodes holds an opaque element.
// The differ needs a stable identifier for each opaque node,
// so they must be keyed children.
func opaqueMustBeKeyed(nodes []Node) {
	for _, n := range nodes {
		if e, ok := n.(Element); ok && e.opaque {
			panic("domi: an opaque node must be a keyed child")
		}
	}
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
	return Element{tag: e.tag, attrs: out, children: e.children, keys: e.keys, opaque: e.opaque}
}

// Raw is a pre-rendered element node: its content is written verbatim
// by the renderer with no escaping. Inline SVG, sanitized markdown, and
// fragments from third-party HTML generators all pass through as Raw by
// the time they reach the tree.
//
// Raw must parse as exactly one HTML element (not text). The differ
// relies on this: it reconciles every Raw as a single element node and
// never coalesces it with adjacent text. The domi-side constructor
// enforces the invariant; callers lowering their own trees must uphold
// it. Use [Text] for text content.
type Raw string

func (Raw) vdomNode() {}

// Text is a text leaf node holding plain, unescaped text. The renderer
// escapes the content for safe embedding in HTML.
//
// When only a Text node's content changes between renders, the differ
// updates the existing DOM text node in place instead of replacing it,
// so a text selection anchored in that node survives the update.
type Text string

func (Text) vdomNode() {}

// Attr is a flat name/value attribute pair.
type Attr struct {
	Name  string
	Value string
}

// combineAttrs resolves duplicate attribute names.
// attrs must be sorted.
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

var combining = map[string]string{
	"class": " ",
	"style": ";",
}

// RegisterCombining registers name as a combining attribute with the
// given separator. When a combining attribute appears more than once
// on an element, the values are joined with sep into a single
// attribute.
//
// RegisterCombining must be called before any call to [NewElement],
// typically from a package init function.
func RegisterCombining(name, sep string) {
	combining[name] = sep
}

// combineSep returns the separator for attributes whose duplicate
// occurrences should be combined.
func combineSep(name string) (string, bool) {
	if strings.HasPrefix(name, "data-msg-") {
		return ",", true
	}
	sep, ok := combining[name]
	return sep, ok
}

// coalesceText concatenates adjacent [Text] children into a single Text
// node, matching the shape an HTML parser produces: it merges adjacent
// text nodes when it reparses the rendered output, so the vdom child
// list must merge them too or positional indices drift off the live DOM.
//
// Merging also keeps later edits cheap — one combined Text node lets a
// content change ride a single in-place update. A [Raw] is always a
// single element, so it never participates here.
//
// Returns the input slice unchanged when no coalescing is needed.
func coalesceText(children []Node) []Node {
	merged := false
	for i := 1; i < len(children); i++ {
		if isText(children[i-1]) && isText(children[i]) {
			merged = true
			break
		}
	}
	if !merged {
		return children
	}
	out := make([]Node, 0, len(children))
	var buf string
	flush := func() {
		if buf != "" {
			out = append(out, Text(buf))
			buf = ""
		}
	}
	for _, c := range children {
		if t, ok := c.(Text); ok {
			buf += string(t)
			continue
		}
		flush()
		out = append(out, c)
	}
	flush()
	return out
}

// isText reports whether n is a [Text] node.
func isText(n Node) bool {
	_, ok := n.(Text)
	return ok
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
