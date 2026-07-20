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
//
// Note that domi's rendered output (e.g. from [RenderTo])
// can contain reserved tag names and attributes
// and should not be used as input to UnsafeParseRaw.
func UnsafeParseRaw(s string) (Node, error) {
	ctx := &html.Node{Type: html.ElementNode, DataAtom: atom.Template, Data: "template"}
	nodes, err := html.ParseFragment(strings.NewReader(s), ctx)
	if err != nil {
		return nil, fmt.Errorf("domi: UnsafeParseRaw: %w", err)
	}
	children := make([]Node, len(nodes))
	for i, n := range nodes {
		if children[i], err = parseNode(n); err != nil {
			return nil, fmt.Errorf("domi: UnsafeParseRaw: %w", err)
		}
	}
	return Fragment(children...), nil
}

// parseNode converts a single parsed HTML node into its domi form,
// returning nil for nodes that carry no rendered content (comments,
// doctype, and the like).
func parseNode(n *html.Node) (Node, error) {
	switch n.Type {
	case html.TextNode:
		return Text(n.Data), nil
	case html.ElementNode:
		return parseElement(n)
	default:
		return nil, nil
	}
}

// parseElement converts a parsed HTML element into a domi element,
// recursing into its children. A namespaced attribute (xlink:href on an
// SVG <use>, for instance) is rejoined into a single prefixed name so it
// round-trips through rendering. Reserved tag and attribute names are
// rejected.
func parseElement(n *html.Node) (Node, error) {
	if isReservedTag(n.Data) {
		return nil, fmt.Errorf("reserved tag <%s>", n.Data)
	}
	var attrs []Attr
	for _, a := range n.Attr {
		name := a.Key
		if a.Namespace != "" {
			name = a.Namespace + ":" + a.Key
		}
		if isReservedAttr(name) {
			return nil, fmt.Errorf("reserved attribute %s on <%s>", name, n.Data)
		}
		attrs = append(attrs, Name(name)(a.Val))
	}
	var children []Node
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		cc, err := parseNode(c)
		if err != nil {
			return nil, err
		}
		children = append(children, cc)
	}
	return Tag(n.Data)(attrs...)(children...), nil
}
