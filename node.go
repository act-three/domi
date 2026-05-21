// Package domi is a server-rendered framework for building browser
// applications in Go. An application is a state machine: it implements
// [App], whose View method renders the current state as a [Node] tree
// and whose Update method transitions the state in response to events.
// The framework hosts the application behind an [http.Handler], keeps
// the browser's view in sync with whatever View returns, and dispatches
// user-generated events back through Update.
//
// The package exposes only the primitives needed to build any node or
// attribute ([Tag], [Keyed], [Fragment], [Text], [Attribute], [Group],
// [On]). Convenience wrappers for common HTML tags, attributes, and
// events live in [ily.dev/domi/html], [ily.dev/domi/attr], and
// [ily.dev/domi/event].
//
// A [Node] is anything that can appear in the tree: text, an element
// built via [Tag], or a keyed element built via [Keyed]. [Tag] and
// [Keyed] return curried builders — first attributes, then children.
// An element with no children is itself a [Node], so void elements
// (e.g. Br, Input) and other childless tags can appear in a parent's
// child list without a trailing empty children call.
//
// Attribute names beginning with "data-domi-" are reserved for use by
// this package and its subpackages. Application code and third-party
// packages should pick data attributes outside that prefix to avoid
// collisions with framework internals — present or future.
package domi

import (
	"fmt"
	"hash/fnv"
	"iter"
	"slices"
	"strconv"
	"sync"

	"ily.dev/domi/internal/vdom"
)

// Process-wide registry of event-handler messages, keyed by a content
// hash of the marshaled Msg JSON. On() inserts; serve.go's handleEvent
// looks up. The map is content-addressable, so identical Msgs from any
// session share a slot; size is bounded by the number of distinct Msg
// values constructed by all running apps, which is small in practice
// (TEA apps have a handful of variants, sometimes parameterized by IDs).
var (
	handlersMu sync.RWMutex
	handlers   = map[string][]byte{}
)

func registerHandler(raw []byte) string {
	h := fnv.New64a()
	h.Write(raw)
	key := strconv.FormatUint(h.Sum64(), 16)
	handlersMu.Lock()
	handlers[key] = raw
	handlersMu.Unlock()
	return key
}

func lookupHandler(key string) ([]byte, bool) {
	handlersMu.RLock()
	raw, ok := handlers[key]
	handlersMu.RUnlock()
	return raw, ok
}

// Node is anything that can appear in a domi tree. The interface is
// sealed — only types defined by this package satisfy it. Construct
// values via [Text], [Tag], or [Keyed].
type Node interface {
	isNode()
}

// node is the lowered form of a Node, satisfied only by element and
// text. Public constructors lower their inputs to nodes at construction
// time; the lowered() method then yields the corresponding vdom.Node
// so the renderer and differ can operate on a tree they own.
type node interface {
	Node
	lowered() vdom.Node
}

// element is the domi-side wrapper around [vdom.Element]: a zero-cost
// type def that adds isNode and lowered. Construction uses struct
// literals with vdom.Element's field names; lowering is a free cast.
type element vdom.Element

func (element) isNode()              {}
func (e element) lowered() vdom.Node { return vdom.Element(e) }

// text is the domi-side wrapper around [vdom.Text].
type text vdom.Text

func (text) isNode()              {}
func (t text) lowered() vdom.Node { return vdom.Text(t) }

// Element is the partially-applied builder returned by Tag(name)(attrs).
// Calling it with children produces a finished element node; an Element
// with no children is itself a [Node], so childless tags can appear in a
// parent's child list without a trailing empty children call.
type Element func(...Node) Node

func (Element) isNode() {}

// Tag returns a curried builder for an HTML element with the given name:
// the first call takes attributes, the second takes children.
//
//	Tag("div")(attr.Class("x"))(Text("hi"))
//
// Void elements (and any other "no children" case) can skip the trailing
// empty children call — Element is itself a Node:
//
//	Div()(Text("a"), Br(), Text("b"))
//
// Prebound helpers for common tags live in [ily.dev/domi/html].
func Tag(name string) func(...Attr) Element {
	return func(attrs ...Attr) Element {
		a := slices.Collect(iter.Seq[vdom.Attr](Group(attrs...).(group)))
		return func(children ...Node) Node {
			c := slices.Collect(iter.Seq[vdom.Node](Fragment(children...).(fragment)))
			return element(vdom.NewElement(name, a, c, nil))
		}
	}
}

// Text constructs a text node.
func Text(s string) Node {
	return text{Value: s}
}

// Keyed returns a curried builder for an element whose children are
// paired with stable keys. The framework reconciles updates to keyed
// children by identity rather than position, so inserting, removing, or
// reordering items in the middle of a list updates the surviving
// children in place instead of replacing the affected suffix.
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
// cannot be keyed, and Keyed panics on a non-element child. Keys should
// be unique within the sequence and stable across renders for the same
// logical item.
func Keyed(name string) func(...Attr) func(iter.Seq2[string, Node]) Node {
	return func(attrs ...Attr) func(iter.Seq2[string, Node]) Node {
		a := slices.Collect(iter.Seq[vdom.Attr](Group(attrs...).(group)))
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

// Fragment returns a Node that, when used as a child of a [Tag]
// element, contributes its children to that element's child list in
// order, as if they had been written there directly.
//
// A Fragment cannot be keyed — [Keyed] children must each be a single
// element with an identity — and cannot stand at the root of the tree
// returned by [App.View]. Wrap it in an element first.
func Fragment(children ...Node) Node {
	return fragment(func(yield func(vdom.Node) bool) {
		for _, c := range children {
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

// lowerOne narrows a single Node to its lowered vdom.Node form. Called
// by the server on each [App.View] result. A Fragment is only valid
// inside an element; using one as the tree root panics.
func lowerOne(n Node) vdom.Node {
	switch v := n.(type) {
	case Element:
		return v().(node).lowered()
	case node:
		return v.lowered()
	case fragment:
		panic("domi: Fragment cannot stand alone; wrap it in an element")
	}
	panic(fmt.Sprintf("domi: cannot lower %T", n))
}

// Attr is an opaque attribute carried by an element. Construct via
// [Attribute] for a static name/value pair, [On] for an event handler,
// or [Group] for a collection of attrs.
type Attr interface {
	isAttr()
}

// attr is the domi-side wrapper around [vdom.Attr]: a zero-cost type
// def that adds isAttr. Construction uses struct literals with
// vdom.Attr's field names; lowering is a free cast.
type attr vdom.Attr

func (attr) isAttr() {}

// Attribute constructs a static HTML attribute (e.g. class="foo").
//
// When the same attribute name appears more than once on the same
// element, the values are combined per name: class values are joined by
// a single space, style values are joined by a semicolon, and any other
// repeated attribute keeps its first value. This lets components layer
// classes and styles onto a host element without coordinating.
func Attribute(name, value string) Attr {
	return attr{Name: name, Value: value}
}

// group is the lowered form of a [Group]: a sequence of vdom.Attrs that
// splats into a parent's attribute list. group satisfies Attr but not
// attr — the parent collects it into its own Attrs slice rather than
// walking it as an interior attribute. Nested groups compose by
// chaining iter.Seqs, so flattening is lazy and adds no per-level
// overhead.
type group iter.Seq[vdom.Attr]

func (group) isAttr() {}

// Group returns an Attr that, when used in an element's attribute list,
// contributes its own attrs to that list in order, as if they had been
// written there directly. Groups may be nested arbitrarily.
func Group(attrs ...Attr) Attr {
	return group(func(yield func(vdom.Attr) bool) {
		for _, a := range attrs {
			switch v := a.(type) {
			case attr:
				if !yield(vdom.Attr(v)) {
					return
				}
			case group:
				for inner := range v {
					if !yield(inner) {
						return
					}
				}
			default:
				panic(fmt.Sprintf("domi: cannot lower %T", a))
			}
		}
	})
}
