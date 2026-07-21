package domi

import (
	"fmt"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"ily.dev/domi/internal/vdom"
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
// round-trips through rendering. Reserved and invalid tag and attribute
// names are rejected.
// A raw-text element must contain only text,
// which must satisfy [vdom.CheckRawText].
func parseElement(n *html.Node) (Node, error) {
	if !isValidTagName(n.Data) {
		return nil, fmt.Errorf("invalid tag name %q", n.Data)
	}
	if isReservedTag(n.Data) {
		return nil, fmt.Errorf("reserved tag <%s>", n.Data)
	}
	var attrs []Attr
	for _, a := range n.Attr {
		name := a.Key
		if a.Namespace != "" {
			name = a.Namespace + ":" + a.Key
		}
		if !isValidName(name, foreignAttrNames) {
			return nil, fmt.Errorf("invalid attribute name %q on <%s>", name, n.Data)
		}
		if isReservedAttr(name) {
			return nil, fmt.Errorf("reserved attribute %s on <%s>", name, n.Data)
		}
		attrs = append(attrs, Name(name, a.Val))
	}
	var children []Node
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		cc, err := parseNode(c)
		if err != nil {
			return nil, err
		}
		children = append(children, cc)
	}
	// A raw-text element must end up with text that CheckRawText
	// accepts and no element children. Validate the text NewElement
	// will see once the tree lowers. Comments parse to nil and
	// contribute nothing, and the text around them coalesces.
	//
	// The parser guarantees text-only content but not safe text:
	// a script containing an open comment ("<!--<script" ...) parses
	// to a single text node we must reject. In foreign content,
	// where script is an ordinary element, the parser doesn't even
	// guarantee text: <svg><script> can come back with element
	// children, or with "&lt;/script&gt;" entity-decoded into text
	// that would end the element when written verbatim.
	if vdom.IsRawTextElement(n.Data) {
		var sb strings.Builder
		for _, c := range children {
			switch c := c.(type) {
			case nil:
			case text:
				sb.WriteString(string(c))
			default:
				return nil, fmt.Errorf("<%s> must contain only text", n.Data)
			}
		}
		if err := vdom.CheckRawText(n.Data, sb.String()); err != nil {
			return nil, err
		}
	}
	return Tag(n.Data, attrs...)(children...), nil
}
