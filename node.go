package domi

import (
	"fmt"
	"iter"
	"slices"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
	"ily.dev/domi/internal/vdom"
)

// A Node is an HTML node,
// a [Text], [Raw], [Tag], [Keyed], or [Fragment].
type Node interface {
	isNode()
}

// node is the lowered form of a Node, satisfied only by element and
// raw. Public constructors lower their inputs to nodes at construction
// time; the lowered() method then yields the corresponding vdom.Node
// so the renderer and differ can operate on a tree they own.
type node interface {
	Node
	lowered() vdom.Node
}

// raw is the domi-side wrapper around [vdom.Raw].
type raw vdom.Raw

func (raw) isNode()              {}
func (r raw) lowered() vdom.Node { return vdom.Raw(r) }

// Raw returns a node whose content is written verbatim, without HTML
// escaping. Use Raw for trusted HTML: inline SVG, pre-sanitized
// markdown output, or fragments from third-party HTML generators.
// Never pass user-controlled input to Raw without prior sanitization.
//
// The content must produce exactly one DOM node when parsed: either a
// text string containing no markup, or a single properly nested HTML
// element with explicit closing tags. Raw panics if the content is
// empty, contains multiple top-level nodes, or has unclosed tags.
func Raw(s string) Node {
	if s == "" {
		panic("domi: Raw: empty string produces no DOM nodes")
	}
	ctx := &html.Node{Type: html.ElementNode, DataAtom: atom.Body, Data: "body"}
	nodes, err := html.ParseFragment(strings.NewReader(s), ctx)
	if err != nil {
		panic("domi: Raw: " + err.Error())
	}
	if len(nodes) != 1 {
		panic(fmt.Sprintf("domi: Raw: content must produce exactly one DOM node, got %d", len(nodes)))
	}
	return raw(vdom.Raw(s))
}

// Text returns a text node. The string is escaped for safe embedding
// in HTML; use [Raw] for pre-escaped or trusted content.
func Text(s string) Node {
	return raw(vdom.Text(s))
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

// element is the domi-side wrapper around [vdom.Element]: a zero-cost
// type def that adds isNode and lowered. Construction uses struct
// literals with vdom.Element's field names; lowering is a free cast.
type element vdom.Element

func (element) isNode()              {}
func (e element) lowered() vdom.Node { return vdom.Element(e) }

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
		a := iter.Seq[vdom.Attr](Group(attrs...).(group))
		return func(children ...Node) Node {
			return element(vdom.NewElement(name, a, lower(children...), nil))
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
		a := iter.Seq[vdom.Attr](Group(attrs...).(group))
		return func(seq iter.Seq2[string, Node]) Node {
			var keys []string
			var children []vdom.Node
			for k, n := range seq {
				if e, ok := n.(Element); ok {
					n = e()
				}
				v, ok := n.(element)
				if !ok {
					panic(fmt.Sprintf("domi: keyed child %q must be an element, got %T", k, n))
				}
				keyed := vdom.Element(v).WithAttr(vdom.Attr{Name: "data-domi-key", Value: k})
				keys = append(keys, k)
				children = append(children, keyed)
			}
			return element(vdom.NewElement(name, a, children, keys))
		}
	}
}

// fragment is the lowered form of a [Fragment]: a sequence of vdom.Nodes
// that splats into a parent's child list. fragment satisfies Node but
// not node — the parent collects it into its own children slice rather
// than walking it as an interior node. Nested fragments compose by
// chaining iter.Seqs, so flattening is lazy and adds no per-level
// overhead.
type fragment iter.Seq[vdom.Node]

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
	return fragment(func(yield func(vdom.Node) bool) {
		for _, c := range n {
			switch v := c.(type) {
			case Element:
				if !yield(v().(node).lowered()) {
					return
				}
			case fragment:
				for n := range v {
					if !yield(n) {
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
func lower(nodes ...Node) []vdom.Node {
	return slices.Collect(iter.Seq[vdom.Node](Fragment(nodes...).(fragment)))
}

// lowerOne narrows a single Node to its lowered vdom.Node form,
// panicking if n materializes to anything other than exactly one node
// (e.g. a Fragment with zero or multiple children).
func lowerOne(n Node) vdom.Node {
	out := lower(n)
	if len(out) != 1 {
		panic(fmt.Sprintf("domi: expected 1 node, got %d", len(out)))
	}
	return out[0]
}
