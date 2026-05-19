// Package domi is a TEA-shaped, server-rendered VDOM framework: apps
// implement App[Msg], the framework drives an Update/View/diff loop per
// session and ships patches to the browser over SSE.
//
// The package exposes only the primitives needed to construct any
// node or attribute (Tag, Keyed, Text, Attribute, On). Convenience
// wrappers for common HTML tags, attributes, and events live in
// domi/html, domi/attr, and domi/event.
//
// Node is an interface: it's satisfied by text, by a finished element,
// by Element — the function-typed builder returned by Tag(name)(attrs)
// — and by a keyed element built via Keyed. An Element with no children
// is itself a Node; the diff and render paths materialize it via its
// no-children call (e()). That lets void elements like Br() and Input()
// appear as children without a trailing empty-call for children.
//
// VDOM values are Msg-erased: handler attributes carry a content hash
// of the pre-marshaled Msg JSON. The Msg itself lives in a process-wide
// registry; only the hash crosses the wire. Multiple handlers for the
// same event combine via comma in the attribute value.
package domi

import (
	"fmt"
	"hash/fnv"
	"iter"
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

// Node is anything that can appear in a domi tree: text, an element, or
// an Element builder. The interface is sealed via the unexported isNode
// marker — only types defined inside the domi package satisfy it.
type Node interface {
	isNode()
}

// element is a fully-realized element node. The diff and render walk these
// after materializing any Element entries they encounter.
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
	children []Node
	keys     []string
}

func (element) isNode() {}

// text is a text node.
type text struct {
	value string
}

func (text) isNode() {}

// Element is the curried element builder returned by Tag(name)(attrs).
// Calling it with children yields a finished element node; Element itself
// also satisfies Node — equivalent to "this element with no children",
// which is what render and diff materialize via e().
type Element func(...Node) element

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
		return func(children ...Node) element {
			return element{tag: name, attrs: attrs, children: children}
		}
	}
}

// Text constructs a text node.
func Text(s string) Node {
	return text{value: s}
}

// Keyed returns a curried builder for an element whose children are paired
// with stable keys for identity-based reconciliation. The children sequence
// yields (key, child) pairs in the desired DOM order:
//
//	Keyed("ul")(attr.Class("items"))(func(yield func(string, Node) bool) {
//	    for _, it := range items {
//	        if !yield(itemKey(it), itemRow(it)) {
//	            return
//	        }
//	    }
//	})
//
// Each yielded child must be an element — text can't carry a data-domi-key
// attribute. Keyed panics on a non-element child. The key is injected as a
// data-domi-key attribute on the child at construction time, so render and
// diff don't have to thread it through separately.
//
// An empty sequence is still a keyed element (distinct from an unkeyed one
// of the same tag for diff purposes): keys is allocated to a zero-length
// non-nil slice so the keyed-ness is preserved.
func Keyed(name string) func(...Attr) func(iter.Seq2[string, Node]) Node {
	return func(attrs ...Attr) func(iter.Seq2[string, Node]) Node {
		return func(seq iter.Seq2[string, Node]) Node {
			keys := []string{}
			var children []Node
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

// Attr is an opaque name/value attribute. Construct via Attribute or On.
type Attr struct {
	name  string
	value string
}

// Attribute constructs a static HTML attribute (e.g. class="foo").
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
