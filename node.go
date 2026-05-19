// Package domi is a TEA-shaped, server-rendered VDOM framework: apps
// implement App[Msg], the framework drives an Update/View/diff loop per
// session and ships patches to the browser over SSE.
//
// The package exposes only the primitives needed to construct any
// node or attribute (Tag, Text, Attribute, On). Convenience wrappers
// for common HTML tags, attributes, and events live in domi/html,
// domi/attr, and domi/event.
//
// Node is an interface: it's satisfied by text, by a finished element,
// and by Element — the function-typed builder returned by Tag(name)(attrs).
// An Element with no children is itself a Node; the diff and render paths
// materialize it via its no-children call (e()). That lets void elements
// like Br() and Input() appear as children without a trailing empty-call
// for children.
//
// VDOM values are Msg-erased: handler attributes carry a content hash
// of the pre-marshaled Msg JSON. The Msg itself lives in a process-wide
// registry; only the hash crosses the wire. Multiple handlers for the
// same event combine via comma in the attribute value.
package domi

import (
	"hash/fnv"
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

// Node is anything that can appear in a domi tree: a text node, a finished
// element, or an Element builder. The interface is sealed via the unexported
// isNode marker — only types defined inside the domi package satisfy it.
type Node interface {
	isNode()
	// key returns the identifying key set via WithKey, or "" for unkeyed
	// nodes. Element returns "" because its key lives on its materialized
	// form; key an Element with WithKey to fix this.
	key() string
	// WithKey returns a copy of the node carrying the given key. Used by
	// the keyed-children diff path to give children stable identities
	// across renders.
	WithKey(key string) Node
}

// element is a fully-realized element node. The diff and render walk these
// after materializing any Element entries they encounter.
type element struct {
	tag      string
	k        string
	attrs    []Attr
	children []Node
}

func (element) isNode()                   {}
func (e element) key() string             { return e.k }
func (e element) WithKey(key string) Node { e.k = key; return e }

// text is a text node.
type text struct {
	k     string
	value string
}

func (text) isNode()                   {}
func (t text) key() string             { return t.k }
func (t text) WithKey(key string) Node { t.k = key; return t }

// Element is the curried element builder returned by Tag(name)(attrs).
// Calling it with children yields a finished element node; Element itself
// also satisfies Node — equivalent to "this element with no children",
// which is what render and diff materialize via e().
type Element func(...Node) element

func (Element) isNode()     {}
func (Element) key() string { return "" }
func (e Element) WithKey(key string) Node {
	n := e()
	n.k = key
	return n
}

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

// allKeyed reports whether every child has a non-empty key AND is an
// element. The element requirement is for the identity-based keyed
// protocol: keyed children carry their key in a data-domi-key attribute
// so the client can resolve them via a per-parent Map, and text nodes
// can't carry attributes. Text-keyed children fall back to positional
// diffing.
func allKeyed(children []Node) bool {
	if len(children) == 0 {
		return false
	}
	for _, c := range children {
		e, ok := c.(element)
		if !ok || e.k == "" {
			return false
		}
	}
	return true
}
