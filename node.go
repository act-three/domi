package domi

import (
	"fmt"
	"iter"

	"ily.dev/domi/internal/vdom"
)

// A Node is an HTML node (text or an element)
// or an HTML fragment (a sequence of nodes).
//
// The zero value of Node (nil) is an empty fragment.
type Node interface {
	isNode()
}

// node is the normalized form of a Node, satisfied only by element and
// text. Public constructors normalize their inputs at construction time;
// the vdom form is materialized later, by [lower].
type node interface {
	Node
	lowered() (vdom.Node, handlers)
}

// text is the domi-side wrapper around [vdom.Text].
type text vdom.Text

func (text) isNode()                          {}
func (t text) lowered() (vdom.Node, handlers) { return vdom.Text(t), nil }

// Text returns a text node. The string is escaped for safe embedding
// in HTML when rendered; use [UnsafeParseRaw] for trusted HTML markup.
func Text(s string) Node {
	return text(s)
}

// Textf returns a text node formatted with [fmt.Sprintf].
func Textf(format string, a ...any) Node {
	return Text(fmt.Sprintf(format, a...))
}

// Element is the partially-applied builder returned by Tag(name)(attrs).
// Calling it with children produces a finished element node; an Element
// with no children is itself a [Node], so childless tags can appear in a
// parent's child list without a trailing empty children call.
//
// Child nodes must not use the [Opaque] attr.
// If a child node is opaque, Element panics.
// See [Keyed] to use opaque nodes.
type Element func(...Node) Node

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
func (e element) lowered() (vdom.Node, handlers) {
	a, h := Group(e.attrs...).(group).lower()
	if e.keys != nil {
		children := make([]vdom.Node, len(e.children))
		for i, c := range e.children {
			n, ch := c.(element).lowered()
			children[i] = n.(vdom.Element).WithAttr(vdom.Attr{Name: "data-domi-key", Value: e.keys[i]})
			h = h.merge(ch)
		}
		return vdom.NewElement(e.tag, a, children, e.keys), h
	}
	children, ch := lower(e.children...)
	return vdom.NewElement(e.tag, a, children, nil), h.merge(ch)
}

// Tag returns a curried builder for an HTML element with the given name.
// Its first call takes attributes, and the second takes children.
//
//	Tag("div")(attr.Class("x"))(Text("hi"))
//
// Void elements (and any other "no children" case) can skip the trailing
// empty children call. Element is itself a Node.
//
//	Div()(Text("a"), Br(), Text("b"))
//
// Helpers for common tags can be found in [ily.dev/domi/html].
//
// Child nodes must not use the [Opaque] attr.
// If a child node is opaque, Tag panics.
// See [Keyed] to use opaque nodes.
func Tag(name string) func(...Attr) Element {
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

// Keyed returns a curried builder for an element whose children are
// paired with stable keys. The framework reconciles updates to keyed
// children by identity rather than position, so inserting, removing, or
// reordering items in the middle of a list updates the surviving
// children in place instead of replacing the entire affected suffix.
//
// The children sequence yields (key, child) pairs in the desired order:
//
//	Keyed("ul")(attr.Class("items"))(func(yield func(string, Node) bool) {
//	    for _, it := range items {
//	        if !yield(itemKey(it), itemRow(it)) {
//	            return
//	        }
//	    }
//	})
//
// Each yielded child must be an element; text and [Fragment] children
// cannot be keyed, and Keyed panics on a non-element child. Keys must
// be unique within the sequence and stable across renders for the same
// logical item.
//
// A child of Keyed can optionally use the [Opaque] attr.
// See [Opaque] for details on its behavior.
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

// lower materializes nodes into their lowered vdom.Node form in a
// single top-down walk, expanding any [Fragment] entries inline so the
// result is a flat slice of element and text nodes ready for vdom
// rendering or diffing, along with the handlers harvested from every
// subtree.
func lower(nodes ...Node) (n []vdom.Node, h handlers) {
	for nn := range Fragment(nodes...).(fragment) {
		v, hh := nn.lowered()
		n = append(n, v)
		h = h.merge(hh)
	}
	return n, h
}

// lowerOne narrows a single Node to its lowered vdom.Node form,
// panicking if n materializes to anything other than exactly one node
// (e.g. a Fragment with zero or multiple children).
func lowerOne(n Node) (vdom.Node, handlers) {
	ns, h := lower(n)
	if len(ns) != 1 {
		panic(fmt.Sprintf("domi: expected 1 node, got %d", len(ns)))
	}
	return ns[0], h
}
