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

// node is the lowered form of a Node, satisfied only by element and text.
// Public constructors lower their inputs to vdom nodes at construction time.
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

// element is the lowered form of an element [Node]: the [vdom.Element]
// to render plus the event handlers harvested from its own attributes
// and its entire subtree.
type element struct {
	elem     vdom.Element
	handlers handlers
}

func (element) isNode() {}
func (e element) lowered() (vdom.Node, handlers) {
	return e.elem, e.handlers
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
		a, ah := Group(attrs...).(group).lower()
		return func(children ...Node) Node {
			n, ch := lower(children...)
			h := handlers(nil).merge(ch).merge(ah)
			return element{vdom.NewElement(name, a, n, nil), h}
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
		a, ah := Group(attrs...).(group).lower()
		return func(seq iter.Seq2[string, Node]) Node {
			var keys []string
			var children []vdom.Node
			var h handlers
			for k, n := range seq {
				if e, ok := n.(Element); ok {
					n = e()
				}
				v, ok := n.(element)
				if !ok {
					panic(fmt.Sprintf("domi: keyed child %q must be an element, got %T", k, n))
				}
				keyed := v.elem.WithAttr(vdom.Attr{Name: "data-domi-key", Value: k})
				keys = append(keys, k)
				children = append(children, keyed)
				h = h.merge(v.handlers)
			}
			h = h.merge(ah)
			return element{vdom.NewElement(name, a, children, keys), h}
		}
	}
}

// fragment is the lowered form of a [Fragment]: a sequence of vdom.Nodes,
// each paired with its harvested handlers.
type fragment iter.Seq2[vdom.Node, handlers]

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
	return fragment(func(yield func(vdom.Node, handlers) bool) {
		for _, c := range n {
			switch v := c.(type) {
			case nil:
				// A nil Node contributes nothing, like an empty Fragment.
			case Element:
				if !yield(v().(node).lowered()) {
					return
				}
			case fragment:
				for n, h := range v {
					if !yield(n, h) {
						return
					}
				}
			case node:
				if !yield(v.lowered()) {
					return
				}
			default:
				panic(fmt.Sprintf("domi: cannot lower %T", c))
			}
		}
	})
}

// lower flattens nodes into their lowered vdom.Node form, expanding
// any [Fragment] entries inline so the result is a flat slice of
// element and text nodes ready for vdom rendering or diffing.
func lower(nodes ...Node) (n []vdom.Node, h handlers) {
	for nn, hh := range Fragment(nodes...).(fragment) {
		n = append(n, nn)
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
