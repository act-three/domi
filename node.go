// Package domi is a server-rendered framework for building browser
// applications in Go. An application is a state machine: it implements
// [App], whose View method renders the current state as a [Node] tree
// and whose Update method transitions the state in response to events.
// The framework hosts the application behind an [http.Handler], keeps
// the browser's view in sync with whatever View returns, and dispatches
// user-generated events back through Update.
//
// The package exposes only the primitives needed to build any node or
// attribute ([Tag], [Keyed], [Fragment], [Text], [Attribute], [On]).
// Convenience wrappers for common HTML tags, attributes, and events
// live in [ily.dev/domi/html], [ily.dev/domi/attr], and
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
	"strings"
	"sync"
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

// node is the lowered (canonical) form of a Node — what the renderer
// and differ actually walk. Public constructors lower their inputs
// into nodes at construction time, so the interior of an element tree
// is uniformly element-or-text by type. New public node types compose
// by lowering to existing nodes, not by adding cases to the walk.
type node interface {
	Node
	lowered()
}

// element is a fully-realized element node.
//
// keys is the discriminator between positional and keyed elements: nil
// means the element is positional (Tag); non-nil (even empty) means it
// was built via Keyed and its children are paired with these keys for
// identity-based reconciliation. When non-nil, keys is the same length
// as children, and each child is an element (text can't carry a
// data-domi-key attribute).
type element struct {
	tag      string
	attrs    []Attr
	children []node
	keys     []string
}

func (element) isNode()  {}
func (element) lowered() {}

// text is a text node.
type text struct {
	value string
}

func (text) isNode()  {}
func (text) lowered() {}

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
		return func(children ...Node) Node {
			c := slices.Collect(iter.Seq[node](Fragment(children...).(fragment)))
			return element{tag: name, attrs: attrs, children: c}
		}
	}
}

// Text constructs a text node.
func Text(s string) Node {
	return text{value: s}
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
		return func(seq iter.Seq2[string, Node]) Node {
			var keys []string
			var children []node
			for k, n := range seq {
				if e, ok := n.(Element); ok {
					n = e()
				}
				v, ok := n.(element)
				if !ok {
					panic(fmt.Sprintf("domi: keyed child %q must be an element, got %T", k, n))
				}
				v.attrs = appendAttr(v.attrs, Attribute("data-domi-key", k))
				keys = append(keys, k)
				children = append(children, v)
			}
			return element{tag: name, attrs: attrs, children: children, keys: keys}
		}
	}
}

// fragment is the lowered form of a [Fragment]: a sequence of nodes that
// splats into a parent's child list. fragment satisfies Node but not
// node — the parent collects it into its own children slice rather
// than walking it as an interior node. Nested fragments compose by
// chaining iter.Seqs, so flattening is lazy and adds no per-level
// overhead.
type fragment iter.Seq[node]

func (fragment) isNode() {}

// Fragment returns a Node that, when used as a child of a [Tag]
// element, contributes its children to that element's child list in
// order, as if they had been written there directly.
//
// A Fragment cannot be keyed — [Keyed] children must each be a single
// element with an identity — and cannot stand at the root of the tree
// returned by [App.View]. Wrap it in an element first.
func Fragment(children ...Node) Node {
	return fragment(func(yield func(node) bool) {
		for _, c := range children {
			switch v := c.(type) {
			case Element:
				if !yield(v().(node)) {
					return
				}
			case fragment:
				for n := range v {
					if !yield(n) {
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

// lowerOne narrows a single Node to its canonical form. Called by
// the server on each [App.View] result. A Fragment is only valid
// inside an element; using one as the tree root panics.
func lowerOne(n Node) node {
	switch v := n.(type) {
	case Element:
		return v().(node)
	case node:
		return v
	case fragment:
		panic("domi: Fragment cannot stand alone; wrap it in an element")
	}
	panic(fmt.Sprintf("domi: cannot lower %T", n))
}

// appendAttr returns attrs with a single additional attribute. It always
// allocates a fresh backing array so the caller's slice can't be mutated
// through the returned one (avoids the classic append-aliasing footgun
// when the keyed parent injects data-domi-key into a child's attrs).
func appendAttr(attrs []Attr, extra Attr) []Attr {
	out := make([]Attr, len(attrs)+1)
	copy(out, attrs)
	out[len(attrs)] = extra
	return out
}

// Attr is an opaque attribute carried by an element. Construct via
// [Attribute] for a static name/value pair or [On] for an event handler.
type Attr struct {
	name  string
	value string
}

// Attribute constructs a static HTML attribute (e.g. class="foo").
//
// When the same attribute name appears more than once on the same
// element, the values are combined per name: class values are joined by
// a single space, style values are joined by a semicolon, and any other
// repeated attribute keeps its first value. This lets components layer
// classes and styles onto a host element without coordinating.
func Attribute(name, value string) Attr {
	return Attr{name: name, value: value}
}

// combineSep returns the separator for attributes whose duplicate
// occurrences should be combined. Non-combining attributes are first-wins.
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

// combinedAttrs returns attrs with duplicates resolved per the rules in
// combineSep. First-occurrence order is preserved. The walker is a single
// pass; each combining attribute accumulates into its own strings.Builder
// (amortized O(N) per name, replacing the previous quadratic string concat).
func combinedAttrs(attrs []Attr) []Attr {
	if len(attrs) < 2 {
		return attrs
	}
	out := make([]Attr, 0, len(attrs))
	idx := make(map[string]int, len(attrs))
	var bufs map[string]*strings.Builder // lazy; allocated on first duplicate
	for _, a := range attrs {
		i, dup := idx[a.name]
		if !dup {
			idx[a.name] = len(out)
			out = append(out, a)
			continue
		}
		sep, isComb := combineSep(a.name)
		if !isComb {
			continue // first-wins
		}
		if bufs == nil {
			bufs = map[string]*strings.Builder{}
		}
		buf, ok := bufs[a.name]
		if !ok {
			buf = &strings.Builder{}
			buf.WriteString(out[i].value)
			bufs[a.name] = buf
		}
		if a.value != "" {
			if buf.Len() > 0 {
				buf.WriteString(sep)
			}
			buf.WriteString(a.value)
		}
	}
	for name, buf := range bufs {
		out[idx[name]].value = buf.String()
	}
	return out
}
