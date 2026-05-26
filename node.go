package domi

import (
	"fmt"
	"hash/fnv"
	"iter"
	"slices"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
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

// element is the domi-side wrapper around [vdom.Element]: a zero-cost
// type def that adds isNode and lowered. Construction uses struct
// literals with vdom.Element's field names; lowering is a free cast.
type element vdom.Element

func (element) isNode()              {}
func (e element) lowered() vdom.Node { return vdom.Element(e) }

// raw is the domi-side wrapper around [vdom.Raw].
type raw vdom.Raw

func (raw) isNode()              {}
func (r raw) lowered() vdom.Node { return vdom.Raw(r) }

// Element is the partially-applied builder returned by Tag(name)(attrs).
// Calling it with children produces a finished element node; an Element
// with no children is itself a [Node], so childless tags can appear in a
// parent's child list without a trailing empty children call.
type Element func(...Node) Node

func (Element) isNode() {}

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
func Tag(name string) func(...Attr) Element {
	return func(attrs ...Attr) Element {
		a := iter.Seq[vdom.Attr](Group(attrs...).(group))
		return func(children ...Node) Node {
			return element(vdom.NewElement(name, a, lower(children...), nil))
		}
	}
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
	validateRaw(s)
	return raw(vdom.Raw(s))
}

// validateRaw panics if s does not produce exactly one DOM node when
// parsed as HTML in a flow-content context (a <body> child).
func validateRaw(s string) {
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

// An Attr is an HTML attribute.
//
// In rendered output,
// a single attribute name does not appear more than once
// on any given element:
//
//  1. For each combining attribute,
//     it concatenates the values according to the table below.
//  2. Event handlers are combined internally.
//  3. For all other attributes,
//     only the first occurrence appears.
//
// The combining attributes are:
//
//	name  sep
//	class " "
//	style ";"
type Attr interface {
	isAttr()
}

// attr is the domi-side wrapper around [vdom.Attr]: a zero-cost type
// def that adds isAttr. Construction uses struct literals with
// vdom.Attr's field names; lowering is a free cast.
type attr vdom.Attr

func (attr) isAttr() {}

// Name returns a builder for an HTML attribute with the given name. (e.g. class).
// Call it to obtain an [Attr] with the given value (e.g. class="foo").
func Name(name string) func(value string) Attr {
	return func(value string) Attr {
		return attr{Name: name, Value: value}
	}
}

// Bool returns a builder for a boolean HTML attribute. For standard
// boolean attributes (disabled, checked, …), true means present
// (name-only) and false means absent:
//
//	Tag("input")(attr.Disabled(true))()   // <input disabled>
//	Tag("input")(attr.Disabled(false))()  // <input>
//
// For enumerated boolean attributes (contenteditable, draggable,
// spellcheck, translate), true and false emit the corresponding
// string value instead:
//
//	Tag("div")(attr.ContentEditable(true))()   // <div contenteditable="true">
//	Tag("div")(attr.ContentEditable(false))()  // <div contenteditable="false">
func Bool(name string) func(bool) Attr {
	if enumeratedBool[name] {
		return func(v bool) Attr {
			if v {
				return attr{Name: name, Value: "true"}
			}
			return attr{Name: name, Value: "false"}
		}
	}
	return func(v bool) Attr {
		if v {
			return attr{Name: name}
		}
		return Group()
	}
}

// enumeratedBool is the set of HTML attributes that take the string
// values "true" and "false" rather than using presence/absence
// semantics. These look boolean but are technically enumerated
// attributes in the HTML spec.
var enumeratedBool = map[string]bool{
	"contenteditable": true,
	"draggable":       true,
	"spellcheck":      true,
	"translate":       true,
}

// group is the lowered form of a [Group]: a sequence of vdom.Attrs that
// splats into a parent's attribute list. group satisfies Attr but not
// attr — the parent collects it into its own Attrs slice rather than
// walking it as an interior attribute. Nested groups compose by
// chaining iter.Seqs, so flattening is lazy and adds no per-level
// overhead.
type group iter.Seq[vdom.Attr]

func (group) isAttr() {}

// A Group is a sequence of HTML attributes.
// It contributes its contents
// to its parent's child list in order,
// as if they had been written there directly.
//
// Groups may be nested arbitrarily.
func Group(a ...Attr) Attr {
	return group(func(yield func(vdom.Attr) bool) {
		for _, a := range a {
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
