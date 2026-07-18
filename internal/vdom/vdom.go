// Package vdom holds the lowered (canonical) form of the domi virtual
// DOM, plus the renderer and differ that walk it. The public-facing
// constructors live in [ily.dev/domi]; vdom is the internal data layer
// they lower into.
//
// The exported types here are deliberately minimal: an HTML element
// has two kinds of children (element and text) so a sum of two
// concrete types covers the tree shape exactly. Renderers and
// differs switch exhaustively on those two cases.
package vdom

import (
	"fmt"
	"iter"
	"slices"
	"strings"
)

// Node is anything in a lowered VDOM tree: an [Element] or a [Text].
type Node interface {
	vdomNode()
}

// Element is a fully-realized element node. Construct via [NewElement];
// fields are read-only outside this package so a constructed Element
// can't be mutated post-hoc (the renderer and differ rely on the tree
// being stable for the lifetime of a diff/render call).
//
// key is the element's reconciliation key in its parent's child list,
// set via [Element.WithKey]: nonempty means the differ matches the
// element by identity, empty means it is matched by position. Keyed
// and unkeyed children mix freely in one list; only elements carry
// keys (a text node has none, so text is always positional).
type Element struct {
	tag      string
	key      string
	attrs    []Attr
	children []Node

	// opaque marks the element as ignored by the differ.
	// It is rendered once and thereafter no patches are emitted
	// (until its eventual removal, if any).
	opaque bool
}

func (Element) vdomNode() {}

// NewElement constructs an [Element]. Children carry their own
// reconciliation keys (see [Element.WithKey]); sibling keys must be
// unique — NewElement panics otherwise.
//
// Attrs are sorted by name and deduplicated according to the combining rules.
func NewElement(tag string, attrs iter.Seq[Attr], children []Node) Element {
	a := slices.Collect(attrs)
	slices.SortStableFunc(a, Attr.cmp)

	children = coalesceText(children)
	validateChildren(children)

	return Element{tag: tag, attrs: combineAttrs(a), children: children}
}

// childKey returns n's reconciliation key: the key of an [Element], or
// the empty string — unkeyed — for any other node (text cannot carry
// a key).
func childKey(n Node) string {
	if e, ok := n.(Element); ok {
		return e.key
	}
	return ""
}

// validateChildren panics if nodes violates the child-list invariant
// the differ depends on: sibling keys must be unique (identity-based
// reconciliation — server- and client-side both — resolves siblings
// by key).
func validateChildren(nodes []Node) {
	var seen map[string]struct{}
	for _, n := range nodes {
		e, ok := n.(Element)
		if !ok || e.key == "" {
			continue
		}
		if seen == nil {
			seen = make(map[string]struct{}, len(nodes))
		}
		if _, dup := seen[e.key]; dup {
			panic(fmt.Sprintf("domi: duplicate key %q among sibling children", e.key))
		}
		seen[e.key] = struct{}{}
	}
}

// cmp orders attrs by name.
func (a Attr) cmp(b Attr) int {
	return strings.Compare(a.Name, b.Name)
}

// WithKey returns a copy of e keyed by key: the key is recorded for
// identity-based reconciliation and mirrored into the data-domi-key
// attribute the client resolves keyed ops against, replacing any
// previous key. Writing both in one place keeps them from diverging.
// Used by domi's lowering, and by client-mutation replay when the
// client re-keys a moved child.
//
// opaque marks the copy as ignored by the differ: it is matched by
// key but never descended into, so no patches touch the element or
// its subtree between its insertion and its eventual removal.
func (e Element) WithKey(key string, opaque bool) Element {
	if key == "" {
		panic("domi: a keyed child must have a nonempty key")
	}
	e.key = key
	e.opaque = opaque
	a := Attr{Name: "data-domi-key", Value: key}
	i, found := slices.BinarySearchFunc(e.attrs, a, Attr.cmp)
	attrs := make([]Attr, len(e.attrs), len(e.attrs)+1)
	copy(attrs, e.attrs)
	if found {
		attrs[i] = a
	} else {
		attrs = slices.Insert(attrs, i, a)
	}
	e.attrs = attrs
	return e
}

// Text is a text leaf node holding plain, unescaped text. The renderer
// escapes the content for safe embedding in HTML.
//
// When only a Text node's content changes between renders, the differ
// updates the existing DOM text node in place instead of replacing it,
// so a text selection anchored in that node survives the update.
type Text string

func (Text) vdomNode() {}

// Attr is an HTML attribute.
type Attr struct {
	Name  string
	Value string
}

// combineAttrs resolves duplicate attribute names.
// attrs must be sorted.
func combineAttrs(attrs []Attr) []Attr {
	if len(attrs) < 2 {
		return attrs
	}
	// Fast path: with sorted input, duplicates are adjacent.
	hasDup := false
	for i := 1; i < len(attrs); i++ {
		if attrs[i].Name == attrs[i-1].Name {
			hasDup = true
			break
		}
	}
	if !hasDup {
		return attrs
	}
	// Slow path: linear scan merging adjacent duplicates.
	out := make([]Attr, 0, len(attrs))
	out = append(out, attrs[0])
	for _, a := range attrs[1:] {
		prev := &out[len(out)-1]
		if a.Name != prev.Name {
			out = append(out, a)
			continue
		}
		sep, isComb := combineSep(a.Name)
		if !isComb {
			continue // first-wins
		}
		if a.Value != "" {
			if prev.Value != "" {
				prev.Value += sep + a.Value
			} else {
				prev.Value = a.Value
			}
		}
	}
	return out
}

var combining = map[string]string{
	"class": " ",
	"style": ";",
}

// RegisterCombining registers name as a combining attribute with the
// given separator. When a combining attribute appears more than once
// on an element, the values are joined with sep into a single
// attribute.
//
// RegisterCombining must be called before any call to [NewElement],
// typically from a package init function.
func RegisterCombining(name, sep string) {
	combining[name] = sep
}

// combineSep returns the separator for attributes whose duplicate
// occurrences should be combined.
func combineSep(name string) (string, bool) {
	if strings.HasPrefix(name, "data-msg-") {
		return ",", true
	}
	sep, ok := combining[name]
	return sep, ok
}

// coalesceText concatenates adjacent [Text] children into a single Text
// node, matching the shape an HTML parser produces: it merges adjacent
// text nodes when it reparses the rendered output, so the vdom child
// list must merge them too or positional indices drift off the live DOM.
// Empty text nodes are dropped for the same reason: they produce no DOM
// node when parsed from rendered HTML.
//
// Merging also keeps later edits cheap: one combined Text node lets a
// content change ride a single in-place update. Element nodes never
// participate here — in particular a keyed element between two text
// nodes keeps them apart, exactly as its DOM element does.
//
// Returns the input slice unchanged when no coalescing or empty-text
// dropping is needed.
func coalesceText(children []Node) []Node {
	changed := false
	for _, c := range children {
		if t, ok := c.(Text); ok && t == "" {
			changed = true
			break
		}
	}
	for i := 1; i < len(children); i++ {
		if isText(children[i-1]) && isText(children[i]) {
			changed = true
			break
		}
	}
	if !changed {
		return children
	}
	out := make([]Node, 0, len(children))
	var buf string
	flush := func() {
		if buf != "" {
			out = append(out, Text(buf))
			buf = ""
		}
	}
	for _, c := range children {
		if t, ok := c.(Text); ok {
			buf += string(t)
			continue
		}
		flush()
		out = append(out, c)
	}
	flush()
	return out
}

// isText reports whether n is a [Text] node.
func isText(n Node) bool {
	_, ok := n.(Text)
	return ok
}

// isVoid reports whether tag is a void HTML element (one that must not
// have a closing tag, per the HTML spec).
func isVoid(tag string) bool {
	switch tag {
	case "area", "base", "br", "col", "embed", "hr", "img", "input",
		"link", "meta", "source", "track", "wbr":
		return true
	}
	return false
}
