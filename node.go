package domi

import (
	"fmt"
	"iter"
	"slices"

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
// See [Tag].
//
// An Element is a function.
// Calling it with child values produces a Node
// that renders the HTML element including its children.
//
//	Tag("div")(attr.Class("a"))(Text("hello")) // <div class="a">hello</div>
//
// Element itself also satisfies [Node],
// and renders without children.
//
//	Tag("div")(attr.Class("bg"))() // <div class="bg"></div>
//	Tag("div")(attr.Class("bg"))   // <div class="bg"></div>
//	Tag("input")(attr.Value("x"))  // <input value="x">
//
// Note that [void elements] never render children,
// even when child values are provided.
//
//	Tag("input")()(Text("not rendered")) // <input>
//
// Child nodes must not use the [Opaque] attr.
// If a child node is opaque, Element panics.
// See [Keyed] to use opaque nodes.
//
// [void elements]: https://html.spec.whatwg.org/multipage/syntax.html#void-elements
type Element func(child ...Node) Node

func (Element) isNode() {}

// element is the normalized form of an element [Node]: its parts as
// given at construction, not yet materialized. The vdom form is built
// during the top-down walk in [lower], so tree-position information is
// in hand as each element materializes.
//
// keys discriminates positional from keyed elements, mirroring
// [vdom.NewElement]: nil means positional children; non-nil means
// children are paired with these keys and each child is an element
// (vetted at construction by [Keyed]).
//
// opaque records whether attrs contains the [Opaque] marker, so a
// parent can reject a misplaced opaque child at construction, where
// the panic's stack trace points at the offending call.
type element struct {
	tag      string
	attrs    []Attr
	children []Node
	keys     []string
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
	if e.keys != nil {
		children := make([]vdom.Node, len(e.children))
		for i, c := range e.children {
			n, ch := c.(element).lowered(a.key(e.keys[i]))
			children[i] = n.(vdom.Element).WithAttr(vdom.Attr{Name: "data-domi-key", Value: e.keys[i]})
			h = h.merge(ch)
		}
		return vdom.NewElement(e.tag, slices.Values(attrs), children, e.keys), h
	}
	children, ch := lower(a, e.children...)
	return vdom.NewElement(e.tag, slices.Values(attrs), children, nil), h.merge(ch)
}

// Tag constructs an HTML element with the given name and attributes.
// Helpers for common tags can be found in [ily.dev/domi/html].
func Tag(name string) func(attr ...Attr) Element {
	return func(attrs ...Attr) Element {
		opaque := hasOpaque(attrs)
		return func(children ...Node) Node {
			opaqueMustBeKeyed(children)
			return element{tag: name, attrs: attrs, children: children, opaque: opaque}
		}
	}
}

// opaqueMustBeKeyed panics if children — with fragments expanded —
// contain an opaque element. The differ needs a stable identity for
// each opaque node, so it must be a keyed child; see [Keyed]. Checking
// at construction keeps the panic's stack trace pointing at the call
// that introduced the violation.
func opaqueMustBeKeyed(children []Node) {
	for c := range Fragment(children...).(fragment) {
		if e, ok := c.(element); ok && e.opaque {
			panic("domi: an opaque node must be a keyed child")
		}
	}
}

// Keyed constructs an element whose children are paired with stable keys.
// Domi reconciles updates to keyed children by identity rather than position.
// Inserting, removing, or reordering items in the middle of a list
// moves the surviving children intact to their new positions
// instead of replacing their contents.
//
// The sequence argument yields key-child pairs in the desired order:
//
//	Keyed("ul")()(func(yield func(string, Node) bool) {
//	    for _, it := range items {
//	        if !yield(itemKey(it), itemRow(it)) {
//	            return
//	        }
//	    }
//	})
//
// Keys must be stable
// (any given item should be assigned the same key every time)
// and unique within the sequence.
//
// A child can optionally use the [Opaque] attr.
// See [Opaque] for details on its behavior.
//
// Each child must be an element.
// Text and [Fragment] children cannot be keyed.
// If Keyed is given a non-element child, it panics.
func Keyed(name string) func(...Attr) func(iter.Seq2[string, Node]) Node {
	return func(attrs ...Attr) func(iter.Seq2[string, Node]) Node {
		opaque := hasOpaque(attrs)
		return func(seq iter.Seq2[string, Node]) Node {
			var keys []string
			var children []Node
			for k, n := range seq {
				if e, ok := n.(Element); ok {
					n = e()
				}
				if _, ok := n.(element); !ok {
					panic(fmt.Sprintf("domi: keyed child %q must be an element, got %T", k, n))
				}
				keys = append(keys, k)
				children = append(children, n)
			}
			return element{tag: name, attrs: attrs, children: children, keys: keys, opaque: opaque}
		}
	}
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
//
// Fragments may be nested arbitrarily.
// A Fragment cannot be keyed.
// [Keyed] children must each be a single element with an identity.
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

// lower materializes nodes — the children of the node at address a —
// into their lowered vdom.Node form in a single top-down walk,
// expanding any [Fragment] entries inline so the result is a flat
// slice of element and text nodes ready for vdom rendering or diffing,
// along with the handlers harvested from every subtree. Each node's
// address extends a with its index in the flattened list.
func lower(a addr, nodes ...Node) (n []vdom.Node, h handlers) {
	for nn := range Fragment(nodes...).(fragment) {
		v, hh := nn.lowered(a.index(len(n)))
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
