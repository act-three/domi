package domi

import (
	"fmt"
	"iter"
	"slices"
	"strings"

	"ily.dev/domi/internal/vdom"
)

// A Node is text, an HTML element, or a fragment (a sequence of nodes).
//
// A nil Node is an empty fragment.
type Node interface {
	isNode()
}

// node is the normalized form of a Node, satisfied only by element and
// text. Public constructors normalize their inputs at construction time;
// the vdom form is materialized later, by [lower], which passes each
// node its address.
type node interface {
	Node
	lowered(addr) (vdom.Node, handlers)
}

// text is the domi-side wrapper around [vdom.Text].
type text vdom.Text

func (text) isNode()                              {}
func (t text) lowered(addr) (vdom.Node, handlers) { return vdom.Text(t), nil }

// Text returns a text node containing s.
//
// The contents of s are preserved exactly
// in the browser's DOM text node,
// so Text cannot be used to construct HTML elements from a string.
// Use [HTML] for HTML markup.
func Text(s string) Node {
	return text(s)
}

// Textf returns a text node formatted with [fmt.Sprintf].
func Textf(format string, a ...any) Node {
	return Text(fmt.Sprintf(format, a...))
}

// An Element is an HTML element containing a tag name and attributes.
//
// An Element is a function.
// Calling it with child values produces a Node
// that renders the HTML element including its children.
//
//	Tag("div")(Text("hello")) // <div>hello</div>
//
// Element itself also satisfies [Node],
// and renders without children.
//
//	Tag("div")()                  // <div></div>
//	Tag("div")                    // <div></div>
//	Tag("input", attr.Value("x")) // <input value="x">
//
// If a [void element] is called with children, it panics.
// Arguments that contribute no nodes,
// such as nil or an empty [Fragment],
// are permitted.
//
//	Tag("input")(Text("not allowed")) // panic
//	Tag("input")(nil, Fragment())     // ok
//
// [void element]: https://html.spec.whatwg.org/multipage/syntax.html#void-elements
type Element func(child ...Node) node

func (Element) isNode() {}

// element is the normalized form of an element [Node]: its parts as
// given at construction, not yet materialized. The vdom form is built
// during the top-down walk in [lower], so tree-position information is
// in hand as each element materializes.
//
// key is the element's reconciliation key in its parent's child list,
// set by [WithKey]; empty means unkeyed, reconciled by position.
//
// opaque marks the element as ignored by the differ.
type element struct {
	tag      string
	key      string
	attrs    []Attr
	children []Node
	opaque   bool

	// mapper rewrites each handler harvested from this tree.
	// See MapNode. Can be nil.
	mapper func(handler) handler
}

func (element) isNode() {}

// lowered materializes e and its entire subtree into vdom form, element
// before children, and merges the handlers harvested along the way.
func (e element) lowered(a addr) (vdom.Node, handlers) {
	var attrs []vdom.Attr
	var h handlers
	slot := 0
	for at := range Group(e.attrs...).(group) {
		va := at.attr
		if at.handler != nil {
			key := a.handlerKey(va.Name, slot)
			slot++
			va.Value = key + ":" + at.handler.ps.key()
			h = h.merge(handlers{key: *at.handler})
		}
		attrs = append(attrs, va)
	}
	children, ch := lower(a, e.children...)
	h = h.merge(ch)
	if e.mapper != nil {
		for key, hd := range h {
			h[key] = e.mapper(hd)
		}
	}
	return vdom.NewElement(e.tag, slices.Values(attrs), children), h
}

// isReservedTag returns whether name is reserved for internal use only.
func isReservedTag(name string) bool {
	return strings.HasPrefix(name, "domi-")
}

// Tag returns an HTML element with the given name and attributes.
// Helpers for common tags can be found in [ily.dev/domi/html].
//
// Tag names must be lowercase,
// except for foreign-content (SVG and MathML) mixed-case names
// like clipPath.
// Tag names beginning with "domi-"
// are reserved for use by domi.
// If name is invalid or reserved, Tag panics.
func Tag(name string, attr ...Attr) Element {
	mustValidTagName(name)
	if isReservedTag(name) {
		panic(fmt.Sprintf("domi: tag %s is reserved", name))
	}
	return func(children ...Node) node {
		if vdom.IsVoid(name) && hasNode(children) {
			panic(fmt.Sprintf("domi: void element <%s> cannot have children", name))
		}
		return element{tag: name, attrs: attr, children: children}
	}
}

// hasNode returns whether nodes contains
// at least one element or text node.
func hasNode(nodes []Node) bool {
	for range Fragment(nodes...).(fragment) {
		return true
	}
	return false
}

// WithKey returns a copy of element n assigned to key.
// Keyed nodes are diffed by identity rather than position:
// inserting, removing, or reordering elements
// in the middle of a keyed sequence
// moves the surviving elements intact to their new positions
// instead of replacing their contents.
//
//	var rows []Node
//	for _, it := range items {
//	    rows = append(rows, WithKey(itemKey(it), itemRow(it)))
//	}
//	list := Tag("ul")(header, Fragment(rows...), footer)
//
// The value of key must be nonempty,
// stable (any given element should be assigned the same key every time),
// and unique within the enclosing element.
//
// If n already has a key or is not a single element, WithKey panics.
func WithKey(key string, n Node) Node {
	if key == "" {
		panic("domi: key must be nonempty")
	}
	var only node
	for c := range Fragment(n).(fragment) {
		if only != nil {
			panic(fmt.Sprintf("domi: keyed node %q must be a single element", key))
		}
		only = c
	}
	e, ok := only.(element)
	if !ok {
		panic(fmt.Sprintf("domi: keyed node %q must be an element, got %T", key, only))
	}
	if e.key != "" {
		panic(fmt.Sprintf("domi: keyed node %q already has key %q", key, e.key))
	}
	e.key = key
	return e
}

// WithKeyOpaque returns a copy of element n assigned to key,
// as [WithKey] does,
// and additionally marks n as opaque.
//
// An opaque element is inserted
// and never modified until it is removed.
// Any changes the app makes
// to the element's contents
// during its existence
// are ignored.
// This allows client-side browser code to take ownership of the node
// without worrying about patches modifying it underfoot.
//
// When an element is opaque,
// its contents are opaque.
// This includes attributes, text nodes, and child elements, recursively,
// covering the entire subtree.
//
// The value of key must be nonempty,
// stable (any given element should be assigned the same key every time),
// and unique within the enclosing element.
//
// If n already has a key or is not a single element, WithKeyOpaque panics.
func WithKeyOpaque(key string, n Node) Node {
	e := WithKey(key, n).(element)
	e.opaque = true
	return e
}

// fragment is the normalized form of a [Fragment]: a lazy sequence of
// normalized nodes, with nil entries dropped, [Element] builders
// applied, and nested fragments expanded inline.
type fragment iter.Seq[node]

func (fragment) isNode() {}

// A Fragment is a sequence of HTML nodes.
//
// Fragment(n) is equivalent to n.
func Fragment(n ...Node) Node {
	return fragment(func(yield func(node) bool) {
		for _, c := range n {
			switch v := c.(type) {
			case nil:
				// A nil Node contributes nothing, like an empty Fragment.
			case Element:
				if !yield(v()) {
					return
				}
			case fragment:
				for inner := range v {
					if !yield(inner) {
						return
					}
				}
			case node:
				if !yield(v) {
					return
				}
			default:
				panic(fmt.Sprintf("domi: cannot lower %T", c))
			}
		}
	})
}

// lower materializes nodes — the children of the element at address
// a, or a view's roots (the children of the domi-root mount, address
// 0) — into their lowered vdom form in a single top-down walk,
// expanding any [Fragment] entries inline so the result is a flat
// slice of element and text nodes ready for vdom rendering or
// diffing, along with the handlers harvested from every subtree. A
// keyed node carries its key into its lowered form (see
// [vdom.Element.WithKey]).
//
// Each child's address extends a following the differ's matching
// rules: a keyed child by its key, an unkeyed child by its gap — the
// run of unkeyed children between keyed siblings, numbered by the
// count of preceding keyed children — and its index within that gap.
// Gap 0 is addressed as a itself, so a list with no keyed children is
// addressed by plain index, exactly as before keys existed.
func lower(a addr, nodes ...Node) (n []vdom.Node, h handlers) {
	gap, gapOrd, gapIdx := a, 0, 0 // the current gap's address, ordinal, and next index
	for c := range Fragment(nodes...).(fragment) {
		var v vdom.Node
		var hh handlers
		if e, ok := c.(element); ok && e.key != "" {
			ve, eh := e.lowered(a.key(e.key))
			v, hh = ve.(vdom.Element).WithKey(e.key, e.opaque), eh
			gapOrd++
			gap, gapIdx = a.gap(gapOrd), 0
		} else {
			v, hh = c.lowered(gap.index(gapIdx))
			gapIdx++
		}
		n = append(n, v)
		h = h.merge(hh)
	}
	return n, h
}

// lowerOne narrows a single Node to its lowered vdom.Node form,
// panicking if n materializes to anything other than exactly one node
// (e.g. a Fragment with zero or multiple children).
func lowerOne(a addr, n Node) (vdom.Node, handlers) {
	ns, h := lower(a, n)
	if len(ns) != 1 {
		panic(fmt.Sprintf("domi: expected 1 node, got %d", len(ns)))
	}
	return ns[0], h
}

// prelowered wraps an already-lowered vdom.Node as a [Node]. Its
// handlers were harvested when it was first lowered.
type prelowered struct{ n vdom.Node }

func (prelowered) isNode() {}

func (p prelowered) lowered(addr) (vdom.Node, handlers) { return p.n, nil }
