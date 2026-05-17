// Package domi is a TEA-shaped, server-rendered VDOM framework: apps
// implement App[Msg], the framework drives an Update/View/diff loop per
// session and ships patches to the browser over SSE.
//
// The package exposes only the primitives needed to construct any
// node or attribute (E, Text, Attribute, On). Convenience wrappers for
// common HTML tags, attributes, and events live in domi/html, domi/attr,
// and domi/event.
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

// nodeKind discriminates between element and text nodes.
type nodeKind uint8

const (
	nodeText nodeKind = iota
	nodeElement
)

// Node is an opaque UI tree node. Construct via E or Text; key with WithKey.
type Node struct {
	kind     nodeKind
	key      string
	text     string // kind == nodeText
	tag      string // kind == nodeElement
	attrs    []Attr // kind == nodeElement
	children []Node // kind == nodeElement
}

// Attr is an opaque name/value attribute. Construct via Attribute or On.
type Attr struct {
	name  string
	value string
}

// E constructs an element node.
func E(tag string, attrs []Attr, children []Node) Node {
	return Node{kind: nodeElement, tag: tag, attrs: attrs, children: children}
}

// Text constructs a text node.
func Text(s string) Node {
	return Node{kind: nodeText, text: s}
}

// WithKey returns a copy of n with the given key. Used by the keyed-children
// diff path to give children stable identities across renders.
func (n Node) WithKey(key string) Node {
	n.key = key
	return n
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
		if c.key == "" || c.kind != nodeElement {
			return false
		}
	}
	return true
}
