package vdom

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"math/rand/v2"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// canonNode is the canonical form of an element tree used for diff/apply
// comparison. Both the Go-side Render(next) and the bun-side post-apply
// HTML pass through golang.org/x/net/html.ParseFragment + walkParsed,
// so the comparison sees only what the HTML5 parser sees — attribute
// order, void-element slash, whitespace quirks, and adjacent-text
// merging all wash out.
type canonNode struct {
	Text  string            `json:"text,omitempty"`
	Tag   string            `json:"tag,omitempty"`
	Attrs map[string]string `json:"attrs,omitempty"`
	Kids  []canonNode       `json:"kids,omitempty"`
}

// canonicalize parses an HTML fragment and returns its single root
// element in canonical form. The generator only produces single-rooted
// trees, so a multi-root result is a programming error.
func canonicalize(t *testing.T, htmlStr string) *canonNode {
	t.Helper()
	// ParseFragment with a <body> context handles general flow content
	// without inventing html/head wrappers around the fragment. The
	// context node needs DataAtom populated (not just Data) — the
	// parser uses atom equality, not string equality, internally.
	ctx := &html.Node{Type: html.ElementNode, DataAtom: atom.Body, Data: "body"}
	nodes, err := html.ParseFragment(strings.NewReader(htmlStr), ctx)
	if err != nil {
		t.Fatalf("canonicalize: parse %q: %v", htmlStr, err)
	}
	if len(nodes) != 1 {
		t.Fatalf("canonicalize: expected exactly one root, got %d for %q", len(nodes), htmlStr)
	}
	return walkParsed(nodes[0])
}

func walkParsed(n *html.Node) *canonNode {
	switch n.Type {
	case html.TextNode:
		if n.Data == "" {
			return nil
		}
		return &canonNode{Text: n.Data}
	case html.ElementNode:
		out := &canonNode{Tag: n.Data}
		if len(n.Attr) > 0 {
			out.Attrs = make(map[string]string, len(n.Attr))
			for _, a := range n.Attr {
				out.Attrs[a.Key] = a.Val
			}
		}
		var textBuf string
		flushText := func() {
			if textBuf != "" {
				out.Kids = append(out.Kids, canonNode{Text: textBuf})
				textBuf = ""
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.TextNode {
				textBuf += c.Data
				continue
			}
			flushText()
			if k := walkParsed(c); k != nil {
				out.Kids = append(out.Kids, *k)
			}
		}
		flushText()
		return out
	}
	return nil
}

// ---- random generator ----

type genConfig struct {
	rng         *rand.Rand
	maxDepth    int
	maxChildren int
	keys        []string
	tags        []string
	attrNames   []string
	attrValues  []string
	texts       []string
	keyedChance int // /100
	textChance  int // /100
	maxAttrs    int
}

func defaultConfig(rng *rand.Rand) *genConfig {
	return &genConfig{
		rng:         rng,
		maxDepth:    4,
		maxChildren: 5,
		keys:        []string{"a", "b", "c", "d", "e", "f"},
		// Only tags whose parsing has no implicit-close rules — keeps
		// the HTML round-trip an identity. <p> closes on block children,
		// <li> closes on a sibling <li>, etc.; including them would let
		// the generator emit trees the parser rearranges.
		tags:        []string{"div", "span", "section", "article", "header", "footer"},
		attrNames:   []string{"class", "id", "data-x", "title"},
		attrValues:  []string{"x", "y", "z"},
		texts:       []string{"hi", "bye", "x", "y"},
		keyedChance: 50,
		textChance:  30,
		maxAttrs:    3,
	}
}

func genElement(cfg *genConfig, depth int) Element {
	tag := cfg.tags[cfg.rng.IntN(len(cfg.tags))]
	attrs := genAttrs(cfg)
	n := 0
	if depth < cfg.maxDepth {
		n = cfg.rng.IntN(cfg.maxChildren + 1)
	}
	if n == 0 {
		return NewElement(tag, attrs, nil, nil)
	}

	// Decide whether this is a keyed parent. Keyed children must all be
	// elements (no text), so we always generate elements when keyed.
	keyed := cfg.rng.IntN(100) < cfg.keyedChance && n <= len(cfg.keys)
	if keyed {
		keys := slices.Clone(cfg.keys)
		cfg.rng.Shuffle(len(keys), func(i, j int) { keys[i], keys[j] = keys[j], keys[i] })
		keys = keys[:n]
		children := make([]Node, n)
		for i := range n {
			// Mirror what domi.Keyed does at construction: append the
			// data-domi-key attribute so the rendered HTML carries the
			// identity the diff/apply round-trip needs.
			children[i] = genElement(cfg, depth+1).WithAttr(Attr{Name: "data-domi-key", Value: keys[i]})
		}
		return NewElement(tag, attrs, children, keys)
	}
	children := make([]Node, 0, n)
	for range n {
		children = append(children, genNode(cfg, depth+1))
	}
	return NewElement(tag, attrs, children, nil)
}

func genNode(cfg *genConfig, depth int) Node {
	if depth >= cfg.maxDepth || cfg.rng.IntN(100) < cfg.textChance {
		return Text{Value: cfg.texts[cfg.rng.IntN(len(cfg.texts))]}
	}
	return genElement(cfg, depth)
}

func genAttrs(cfg *genConfig) []Attr {
	n := cfg.rng.IntN(cfg.maxAttrs + 1)
	if n == 0 {
		return nil
	}
	used := map[string]bool{}
	out := make([]Attr, 0, n)
	for len(out) < n && len(used) < len(cfg.attrNames) {
		name := cfg.attrNames[cfg.rng.IntN(len(cfg.attrNames))]
		if used[name] {
			continue
		}
		used[name] = true
		value := cfg.attrValues[cfg.rng.IntN(len(cfg.attrValues))]
		out = append(out, Attr{Name: name, Value: value})
	}
	return out
}

// ---- property test ----

// TestDiffApplyProperty: for each (old, next) the production differ
// emits patches; the production JS applier (running in jsdom under
// bun) applies them to Render(old). Both Render(next) and the bun-
// emitted result get parsed by golang.org/x/net/html and compared
// structurally — the parser's view is the ground truth, so any
// difference in HTML serialization (attr order, void slashes,
// adjacent-text merging) is absorbed.
//
// Reproduce a failure by setting DOMI_SEED to the printed seed:
//
//	DOMI_SEED=12345 go test -run TestDiffApplyProperty
func TestDiffApplyProperty(t *testing.T) {
	a := startBunApplier(t)

	seed := uint64(time.Now().UnixNano())
	if s := os.Getenv("DOMI_SEED"); s != "" {
		n, err := strconv.ParseUint(s, 10, 64)
		if err != nil {
			t.Fatalf("bad DOMI_SEED %q: %v", s, err)
		}
		seed = n
	}
	t.Logf("DOMI_SEED=%d", seed)
	rng := rand.New(rand.NewPCG(seed, 0xC0FFEE))
	cfg := defaultConfig(rng)

	const iterations = 2000
	for i := range iterations {
		old := genElement(cfg, 0)
		next := genElement(cfg, 0)

		patches := Diff(old, next)
		gotHTML, err := a.apply(Render(old), patches)
		if err != nil {
			t.Fatalf("iter %d (seed=%d): bun apply: %v\nold:  %s\nnext: %s\npatches: %s",
				i, seed, err, Render(old), Render(next), patchDebug(patches))
		}
		want := canonicalize(t, Render(next))
		got := canonicalize(t, gotHTML)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("iter %d (seed=%d): diff/apply mismatch\nold:  %s\nnext: %s\ngot html: %s\nwant: %s\ngot:  %s\npatches (%d): %s",
				i, seed,
				Render(old), Render(next), gotHTML,
				jsonStr(want), jsonStr(got),
				len(patches), patchDebug(patches),
			)
		}
	}
}

func jsonStr(v any) string {
	b, _ := json.Marshal(v, jsontext.WithIndent("  "))
	return string(b)
}

func patchDebug(patches []Patch) string {
	var b strings.Builder
	for i, p := range patches {
		fmt.Fprintf(&b, "\n  [%d] op=%s path=%v", i, p.op, p.path)
		if p.name != "" {
			fmt.Fprintf(&b, " name=%s", p.name)
		}
		if p.value != "" {
			fmt.Fprintf(&b, " value=%q", p.value)
		}
		if p.key != "" {
			fmt.Fprintf(&b, " key=%s", p.key)
		}
		if p.before != "" {
			fmt.Fprintf(&b, " before=%s", p.before)
		}
		if (p.op == "insert_child" || p.op == "remove_child") && !p.keyed {
			fmt.Fprintf(&b, " idx=%d", p.idx)
		}
		if p.op == "move_child" && !p.keyed {
			fmt.Fprintf(&b, " from=%d to=%d", p.from, p.to)
		}
		if p.html != "" {
			fmt.Fprintf(&b, " html=%q", p.html)
		}
	}
	return b.String()
}
