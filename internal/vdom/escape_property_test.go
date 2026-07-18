package vdom

import (
	"math/rand/v2"
	"strings"
	"testing"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// nastyRunes are code points chosen to probe every branch in text and
// attribute escaping: the HTML-special characters, quote variants,
// whitespace the parser normalizes, control characters, multi-byte
// UTF-8, and the replacement character.
var nastyRunes = []rune{
	'<', '>', '&', '"', '\'',
	'\t', '\n', '\r', ' ',
	'\x01', '\x7f',
	'/', '=',
	'a', 'Z', '0',
	'€', '☃', '💣',
}

func randomNastyString(rng *rand.Rand) string {
	n := rng.IntN(20)
	rs := make([]rune, n)
	for i := range rs {
		rs[i] = nastyRunes[rng.IntN(len(nastyRunes))]
	}
	return string(rs)
}

// parseFragment is a small helper that parses an HTML fragment in a
// <body> context and returns the top-level nodes.
func parseFragment(t *testing.T, s string) []*html.Node {
	t.Helper()
	ctx := &html.Node{Type: html.ElementNode, DataAtom: atom.Body, Data: "body"}
	nodes, err := html.ParseFragment(strings.NewReader(s), ctx)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return nodes
}

// collectText walks a parsed node tree and concatenates all text.
func collectText(n *html.Node) string {
	if n.Type == html.TextNode {
		return n.Data
	}
	var sb strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		sb.WriteString(collectText(c))
	}
	return sb.String()
}

// htmlNormalize applies the same input-stream preprocessing the HTML5
// parser does: CR LF → LF, bare CR → LF.
func htmlNormalize(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return s
}

// TestTextEscapeRoundTrip checks that for any string s, rendering
// Text(s) inside a <span> and re-parsing recovers s as the text
// content. This is the fundamental correctness property of text
// escaping: the HTML serialization is a faithful encoding of the
// original string.
func TestTextEscapeRoundTrip(t *testing.T) {
	const iterations = 2000
	rng := rand.New(rand.NewPCG(42, 0))

	for i := range iterations {
		s := randomNastyString(rng)
		rendered := Render(NewElement("span", attrs(), []Node{Text(s)}))
		nodes := parseFragment(t, rendered)
		if len(nodes) != 1 {
			t.Fatalf("[%d] expected 1 root, got %d for input %q → %q",
				i, len(nodes), s, rendered)
		}
		got := collectText(nodes[0])
		// The HTML parser normalizes NUL→U+FFFD, CR LF→LF, CR→LF.
		want := htmlNormalize(s)
		if got != want {
			t.Fatalf("[%d] text round-trip failed\n  input:    %q\n  rendered: %q\n  got:      %q\n  want:     %q",
				i, s, rendered, got, want)
		}
	}
}

// TestAttrValueEscapeRoundTrip checks that for any string v, rendering
// an element with title=v and re-parsing recovers v as the attribute
// value. This is the fundamental correctness property of attribute
// value escaping.
func TestAttrValueEscapeRoundTrip(t *testing.T) {
	const iterations = 2000
	rng := rand.New(rand.NewPCG(99, 0))

	for i := range iterations {
		v := randomNastyString(rng)
		rendered := Render(NewElement("div", attrs(Attr{Name: "title", Value: v}), nil))
		nodes := parseFragment(t, rendered)
		if len(nodes) != 1 {
			t.Fatalf("[%d] expected 1 root, got %d for value %q → %q",
				i, len(nodes), v, rendered)
		}
		var got string
		for _, a := range nodes[0].Attr {
			if a.Key == "title" {
				got = a.Val
				break
			}
		}
		// The HTML parser normalizes NUL→U+FFFD, CR LF→LF, CR→LF.
		want := htmlNormalize(v)
		if got != want {
			t.Fatalf("[%d] attr round-trip failed\n  input:    %q\n  rendered: %q\n  got:      %q\n  want:     %q",
				i, v, rendered, got, want)
		}
	}
}

// TestTextEscapeNeverContainsRawAngleBracket verifies that rendered
// Text never contains a literal '<' — the character that separates text
// from markup. A literal '<' in rendered text would let an attacker
// inject elements.
func TestTextEscapeNeverContainsRawAngleBracket(t *testing.T) {
	const iterations = 2000
	rng := rand.New(rand.NewPCG(7, 0))

	for i := range iterations {
		s := randomNastyString(rng)
		escaped := Render(Text(s))
		if strings.Contains(escaped, "<") {
			t.Fatalf("[%d] Render(Text(%q)) contains literal '<': %q", i, s, escaped)
		}
	}
}

// TestAttrEscapeNeverBreaksOutOfQuotes verifies that rendering an
// attribute value never produces an unescaped '"', which would break
// out of the double-quoted attribute context.
func TestAttrEscapeNeverBreaksOutOfQuotes(t *testing.T) {
	const iterations = 2000
	rng := rand.New(rand.NewPCG(13, 0))

	for i := range iterations {
		v := randomNastyString(rng)
		rendered := Render(NewElement("div", attrs(Attr{Name: "x", Value: v}), nil))
		// Find the attribute value between the quotes.
		// Rendered form: <div x="...">
		start := strings.Index(rendered, `x="`)
		if start == -1 {
			continue // empty value renders as name-only
		}
		start += 3 // skip past x="
		end := strings.Index(rendered[start:], `"`)
		if end == -1 {
			t.Fatalf("[%d] no closing quote found in %q for value %q", i, rendered, v)
		}
		attrHTML := rendered[start : start+end]
		if strings.Contains(attrHTML, `"`) {
			t.Fatalf("[%d] attr value contains unescaped quote: %q (value %q)", i, attrHTML, v)
		}
	}
}
