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

// Text constructs a text node.
//
// The contents of s will be preserved exactly
// in the browser's DOM text node,
// so Text cannot be used to construct HTML elements from a string.
// Use [Safe] for HTML markup.
func Text(s string) Node {
	return text(s)
}

// Textf constructs a text node with [fmt.Sprintf].
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
// If a [void element] is called with a nonempty child list, it panics.
//
//	Tag("input")(Text("not allowed")) // panic
//
// [void element]: https://html.spec.whatwg.org/multipage/syntax.html#void-elements
type Element func(child ...Node) Node

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
	return vdom.NewElement(e.tag, slices.Values(attrs), children), h.merge(ch)
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
	return func(children ...Node) Node {
		if len(children) > 0 && vdom.IsVoid(name) {
			panic(fmt.Sprintf("domi: void element <%s> cannot have children", name))
		}
		return element{tag: name, attrs: attr, children: children}
	}
}

// WithKey assigns key to n, which must be an element.
// Keyed nodes are diffed by identity rather than position:
// inserting, removing, or reordering items in the middle of a list
// moves the surviving children intact to their new positions
// instead of replacing their contents.
//
//	var rows []Node
//	for _, it := range items {
//	    rows = append(rows, WithKey(itemKey(it), itemRow(it)))
//	}
//	list := Tag("ul")(header, Fragment(rows...), footer)
//
// Keys must be nonempty,
// stable (any given item should be assigned the same key every time),
// and unique within the enclosing element.
//
// Node n must be an element, not Text or a Fragment.
// If n is not an element, WithKey panics.
// If n already has a key, WithKey panics.
func WithKey(key string, n Node) Node {
	if key == "" {
		panic("domi: key must be nonempty")
	}
	if b, ok := n.(Element); ok {
		n = b()
	}
	e, ok := n.(element)
	if !ok {
		panic(fmt.Sprintf("domi: keyed node %q must be an element, got %T", key, n))
	}
	if e.key != "" {
		panic(fmt.Sprintf("domi: keyed node %q already has key %q", key, e.key))
	}
	e.key = key
	return e
}

// WithKeyOpaque assigns key to n,
// just as [WithKey] does,
// and additionally marks n as opaque, ignored by the virtual DOM diff.
// An opaque node is inserted,
// and then never modified until its eventual removal (if any).
// Any changes to its contents during its existence are ignored.
// This allows client-side browser code to take ownership of the node
// without worrying about patches modifying it underfoot.
//
// The key requirements and panics are as for [WithKey].
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
// It contributes its contents
// to its parent's child list in order,
// as if they had been written there directly.
func Fragment(n ...Node) Node {
	return fragment(func(yield func(node) bool) {
		for _, c := range n {
			switch v := c.(type) {
			case nil:
				// A nil Node contributes nothing, like an empty Fragment.
			case Element:
				if !yield(v().(node)) {
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
