package domi

import (
	"fmt"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// UnsafeParseRaw parses s as HTML and returns the result as a [Fragment].
// The fragment holds whatever the parser produces:
// a single element, several siblings, text, a mix of these,
// or nothing at all for empty or comment-only input.
//
// Comments are discarded.
// The returned Node is not guaranteed
// to render identically to s byte for byte.
//
// Do not use UnsafeParseRaw for untrusted input.
// It is only suitable for HTML text the app fully controls
// or knows to be trustworthy.
// Use [Safe] for HTML, or [Text] for plain text.
func UnsafeParseRaw(s string) (Node, error) {
	ctx := &html.Node{Type: html.ElementNode, DataAtom: atom.Template, Data: "template"}
	nodes, err := html.ParseFragment(strings.NewReader(s), ctx)
	if err != nil {
		return nil, fmt.Errorf("domi: UnsafeParseRaw: %w", err)
	}
	children := make([]Node, 0, len(nodes))
	for _, n := range nodes {
		if c := parseNode(n); c != nil {
			children = append(children, c)
		}
	}
	return Fragment(children...), nil
}

// parseNode converts a single parsed HTML node into its domi form,
// returning nil for nodes that carry no rendered content (comments,
// doctype, and the like).
func parseNode(n *html.Node) Node {
	switch n.Type {
	case html.TextNode:
		return Text(n.Data)
	case html.ElementNode:
		return parseElement(n)
	default:
		return nil
	}
}

// parseElement converts a parsed HTML element into a domi element,
// recursing into its children. A namespaced attribute (xlink:href on an
// SVG <use>, for instance) is rejoined into a single prefixed name so it
// round-trips through rendering.
func parseElement(n *html.Node) Node {
	var attrs []Attr
	for _, a := range n.Attr {
		name := a.Key
		if a.Namespace != "" {
			name = a.Namespace + ":" + a.Key
		}
		attrs = append(attrs, Name(name)(a.Val))
	}
	var children []Node
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if cn := parseNode(c); cn != nil {
			children = append(children, cn)
		}
	}
	return Tag(n.Data)(attrs...)(children...)
}
