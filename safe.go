package domi

import (
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// Safe parses the given HTML string and returns a sanitized Node
// tree. It uses a hard-coded allowlist of tag names and tag-attribute
// pairs. Tags not in the allowlist are unwrapped (children preserved).
// Dangerous tags like script and style are removed entirely,
// including their children.
func Safe(s string) Node {
	if s == "" {
		return Fragment()
	}
	nodes, err := html.ParseFragment(
		strings.NewReader(s),
		&html.Node{
			Type:     html.ElementNode,
			DataAtom: atom.Body,
			Data:     "body",
		},
	)
	if err != nil {
		return Text(s)
	}
	children := make([]Node, len(nodes))
	for i, n := range nodes {
		children[i] = safeNode(n)
	}
	return Fragment(children...)
}

func safeNode(n *html.Node) Node {
	switch n.Type {
	case html.TextNode:
		return Text(n.Data)
	case html.ElementNode:
		return safeElement(n)
	default:
		return nil
	}
}

func safeElement(n *html.Node) Node {
	tag := n.Data
	if removeTags[tag] {
		return nil
	}
	children := safeChildren(n)
	if !allowedTags[tag] {
		return Fragment(children...)
	}
	allowed := safeAttrs[tag]
	var attrs []Attr
	for _, a := range n.Attr {
		if a.Namespace != "" {
			continue
		}
		if allowed == nil || !allowed[a.Key] {
			continue
		}
		if urlAttr[a.Key] && !isSafeURL(a.Val) {
			continue
		}
		attrs = append(attrs, Name(a.Key, a.Val))
	}
	return Tag(tag, attrs...)(children...)
}

func safeChildren(n *html.Node) []Node {
	var children []Node
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		children = append(children, safeNode(c))
	}
	return children
}

func isSafeURL(u string) bool {
	// Browsers strip ASCII tabs and newlines from the entire URL before
	// evaluating the scheme (WHATWG URL Standard §4.1 step 4). Strip
	// them here so embedded whitespace can't sneak past the prefix
	// checks — e.g. "java\tscript:alert(1)" must be caught.
	u = strings.Map(func(r rune) rune {
		if r == '\t' || r == '\n' || r == '\r' {
			return -1
		}
		return r
	}, u)
	u = strings.TrimSpace(strings.ToLower(u))
	return !strings.HasPrefix(u, "javascript:") &&
		!strings.HasPrefix(u, "data:") &&
		!strings.HasPrefix(u, "file:") &&
		!strings.HasPrefix(u, "vbscript:")
}

// allowedTags is the set of tag names permitted by [Safe].
var allowedTags = map[string]bool{
	"a": true, "abbr": true, "b": true,
	"blockquote": true, "br": true,
	"caption": true, "cite": true, "code": true,
	"col": true, "colgroup": true,
	"dd": true, "del": true, "dfn": true,
	"div": true, "dl": true, "dt": true,
	"em": true,
	"h1": true, "h2": true, "h3": true,
	"h4": true, "h5": true, "h6": true,
	"hr": true, "i": true, "img": true,
	"ins": true, "kbd": true,
	"li": true, "mark": true,
	"ol": true, "p": true, "pre": true,
	"q": true, "rp": true, "rt": true, "ruby": true,
	"s": true, "samp": true, "small": true,
	"span": true, "strong": true,
	"sub": true, "sup": true,
	"table": true, "tbody": true,
	"td": true, "tfoot": true,
	"th": true, "thead": true,
	"time": true, "tr": true,
	"u": true, "ul": true, "var": true,
}

// removeTags are removed entirely, including children.
var removeTags = map[string]bool{
	"script":   true,
	"style":    true,
	"iframe":   true,
	"object":   true,
	"embed":    true,
	"form":     true,
	"input":    true,
	"textarea": true,
	"select":   true,
	"button":   true,
}

// safeAttrs maps tag names to their allowed attribute names.
var safeAttrs = map[string]map[string]bool{
	"a":          {"href": true, "title": true, "rel": true},
	"abbr":       {"title": true},
	"blockquote": {"cite": true},
	"col":        {"span": true},
	"colgroup":   {"span": true},
	"del":        {"cite": true, "datetime": true},
	"img":        {"src": true, "alt": true, "width": true, "height": true, "title": true},
	"ins":        {"cite": true, "datetime": true},
	"ol":         {"start": true, "type": true, "reversed": true},
	"q":          {"cite": true},
	"td":         {"colspan": true, "rowspan": true},
	"th":         {"colspan": true, "rowspan": true},
	"time":       {"datetime": true},
}

// urlAttr is the set of attribute names that contain URLs and must
// be checked for safe schemes.
var urlAttr = map[string]bool{
	"href": true,
	"src":  true,
	"cite": true,
}
